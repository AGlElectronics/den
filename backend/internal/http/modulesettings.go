package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/audit"
	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/config"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// Module settings are the settings Den hands to paired modules (Den →
// Module settings), as defined by the module settings contract
// (docs: concepts/module-settings). Every key is optional; a key Den leaves
// out means "use the module's own setting". A paired module receives
// {"data": {…}, "updated_at": …} with the shared sections and only its own
// section:
//
//	retention, smtp — every module
//	finance         — slug "finance"
//	stash           — slug "stash"
//	wiki            — slug "wiki"
//	tasks           — slug "tasks" (tasks.trash_days, tasks.due_soon_days, …)
//	rally           — slug "events" or "rally"
//
// identity is Den's own (links on the portal) and is never sent. The SMTP
// password is write-only on the admin API and never in the bundle: a module
// fetches it from GET /api/v1/integrations/module-settings/secrets.

type moduleSettingsHandler struct {
	pool      *pgxpool.Pool
	allowHTTP bool
}

type retentionSettings struct {
	OffboardedVolunteersDays *int `json:"offboarded_volunteers_days,omitempty"`
	InactiveUsersDays        *int `json:"inactive_users_days,omitempty"`
	ModuleDataDays           *int `json:"module_data_days,omitempty"`
}

type smtpSettings struct {
	Host     *string `json:"host,omitempty"`
	Port     *int    `json:"port,omitempty"`
	Security *string `json:"security,omitempty"`
	Username *string `json:"username,omitempty"`
	// Password is write-only on the admin API: a string sets it, null
	// clears it, leaving it out keeps it.
	Password    json.RawMessage `json:"password,omitempty"`
	PasswordSet bool            `json:"password_set"`
	From        *string         `json:"from,omitempty"`
	FromName    *string         `json:"from_name,omitempty"`
}

type sellerSettings struct {
	LegalName    *string `json:"legal_name,omitempty"`
	Address      *string `json:"address,omitempty"`
	Country      *string `json:"country,omitempty"`
	VATID        *string `json:"vat_id,omitempty"`
	TaxNumber    *string `json:"tax_number,omitempty"`
	Registration *string `json:"registration,omitempty"`
	Email        *string `json:"email,omitempty"`
	Website      *string `json:"website,omitempty"`
}

type financeSettings struct {
	Seller *sellerSettings `json:"seller,omitempty"`
}

// Lost & Found modes (stash.lost_found.mode).
const (
	lostFoundKeptAfterCon        = "kept_after_con"
	lostFoundDeletedAfterClosing = "deleted_after_closing"
)

type lostFoundSettings struct {
	Mode  string `json:"mode"`
	Days  *int   `json:"days,omitempty"`
	Hours *int   `json:"hours,omitempty"`
}

type stashSettings struct {
	LostFound *lostFoundSettings `json:"lost_found,omitempty"`
}

type wikiSettings struct {
	RevisionRetentionDays *int `json:"revision_retention_days,omitempty"`
}

type tasksSettings struct {
	TrashDays         *int `json:"trash_days,omitempty"`
	NotificationDays  *int `json:"notification_days,omitempty"`
	AuditDays         *int `json:"audit_days,omitempty"`
	AutomationRunDays *int `json:"automation_run_days,omitempty"`
	DueSoonDays       *int `json:"due_soon_days,omitempty"`
}

type rallySettings struct {
	SubmissionSiteHost *string `json:"submission_site_host,omitempty"`
}

type identitySettings struct {
	AccountConsoleURL  *string `json:"account_console_url,omitempty"`
	SSOName            *string `json:"sso_name,omitempty"`
	IdPAdminConsoleURL *string `json:"idp_admin_console_url,omitempty"`
}

type moduleSettingsDoc struct {
	Retention *retentionSettings `json:"retention,omitempty"`
	SMTP      *smtpSettings      `json:"smtp,omitempty"`
	Finance   *financeSettings   `json:"finance,omitempty"`
	Stash     *stashSettings     `json:"stash,omitempty"`
	Wiki      *wikiSettings      `json:"wiki,omitempty"`
	Tasks     *tasksSettings     `json:"tasks,omitempty"`
	Rally     *rallySettings     `json:"rally,omitempty"`
	Identity  *identitySettings  `json:"identity,omitempty"`
}

type moduleSettingsSecrets struct {
	SMTPPassword string `json:"smtp_password,omitempty"`
}

var (
	hostPattern    = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]*[a-z0-9])?(:[0-9]{1,5})?$`)
	vatPattern     = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]{2,13}$`)
	countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
)

func trimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	s := strings.TrimSpace(*p)
	if s == "" {
		return nil
	}
	return &s
}

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }

// normalize trims values, drops empty ones, fills the contract's defaults
// and returns per-key errors. It returns the new SMTP password (set),
// whether it is cleared, or neither.
func (h *moduleSettingsHandler) normalize(d *moduleSettingsDoc) (fields map[string]string, newPassword *string, clearPassword bool) {
	fields = map[string]string{}
	intIn := func(key string, v *int, lo, hi int) {
		if v != nil && (*v < lo || *v > hi) {
			fields[key] = "must be between " + strconv.Itoa(lo) + " and " + strconv.Itoa(hi)
		}
	}
	days := func(key string, v *int) { intIn(key, v, 0, 3650) }
	strMax := func(key string, p **string, max int) {
		*p = trimPtr(*p)
		if *p != nil && len(**p) > max {
			fields[key] = "must be at most " + strconv.Itoa(max) + " characters"
		}
	}
	urlOK := func(key string, p **string) {
		*p = trimPtr(*p)
		if *p != nil {
			if err := config.ValidateModuleURL(**p, h.allowHTTP); err != nil && !strings.Contains(err.Error(), "query or fragment") {
				fields[key] = err.Error()
			}
		}
	}
	emailOK := func(key string, p **string, example string) {
		if *p = trimPtr(*p); *p != nil {
			if _, err := mail.ParseAddress(**p); err != nil {
				fields[key] = "must be an e-mail address, for example " + example
			}
		}
	}

	if r := d.Retention; r != nil {
		days("retention.offboarded_volunteers_days", r.OffboardedVolunteersDays)
		days("retention.inactive_users_days", r.InactiveUsersDays)
		days("retention.module_data_days", r.ModuleDataDays)
	}
	if s := d.SMTP; s != nil {
		if s.Host = trimPtr(s.Host); s.Host != nil {
			*s.Host = strings.ToLower(*s.Host)
			if !hostPattern.MatchString(*s.Host) {
				fields["smtp.host"] = "must be a host name such as smtp.example.org"
			}
			if s.Port == nil {
				s.Port = intPtr(587)
			}
			if s.Security == nil {
				s.Security = strPtr("starttls")
			}
		}
		intIn("smtp.port", s.Port, 1, 65535)
		if s.Security = trimPtr(s.Security); s.Security != nil {
			*s.Security = strings.ToLower(*s.Security)
			switch *s.Security {
			case "starttls", "tls":
			case "none":
				if !h.allowHTTP {
					fields["smtp.security"] = "none is only allowed with APP_ENV=development"
				}
			default:
				fields["smtp.security"] = "must be starttls, tls or none"
			}
		}
		emailOK("smtp.from", &s.From, "noreply@example.org")
		strMax("smtp.from_name", &s.FromName, 200)
		strMax("smtp.username", &s.Username, 256)
		switch raw := strings.TrimSpace(string(s.Password)); {
		case raw == "":
		case raw == "null":
			clearPassword = true
		default:
			var pw string
			if err := json.Unmarshal(s.Password, &pw); err != nil {
				fields["smtp.password"] = "must be a string or null"
			} else if len(pw) > 512 {
				fields["smtp.password"] = "must be at most 512 characters"
			} else if pw != "" {
				newPassword = &pw
			}
		}
		s.Password = nil
	}
	if f := d.Finance; f != nil && f.Seller != nil {
		s := f.Seller
		strMax("finance.seller.legal_name", &s.LegalName, 200)
		strMax("finance.seller.address", &s.Address, 1000)
		if s.Country = trimPtr(s.Country); s.Country != nil {
			*s.Country = strings.ToUpper(*s.Country)
			if !countryPattern.MatchString(*s.Country) {
				fields["finance.seller.country"] = "must be a two-letter ISO 3166 country code such as AT"
			}
		}
		if s.VATID = trimPtr(s.VATID); s.VATID != nil {
			v := strings.ToUpper(strings.NewReplacer(" ", "", ".", "", "-", "").Replace(*s.VATID))
			s.VATID = &v
			if !vatPattern.MatchString(v) {
				fields["finance.seller.vat_id"] = "must start with a two-letter country code, for example ATU12345678"
			}
		}
		strMax("finance.seller.tax_number", &s.TaxNumber, 64)
		strMax("finance.seller.registration", &s.Registration, 200)
		emailOK("finance.seller.email", &s.Email, "finance@example.org")
		urlOK("finance.seller.website", &s.Website)
	}
	if s := d.Stash; s != nil && s.LostFound != nil {
		lf := s.LostFound
		switch lf.Mode {
		case lostFoundKeptAfterCon:
			lf.Hours = nil
			if lf.Days == nil {
				lf.Days = intPtr(30)
			}
			days("stash.lost_found.days", lf.Days)
		case lostFoundDeletedAfterClosing:
			lf.Days = nil
			if lf.Hours == nil {
				lf.Hours = intPtr(48)
			}
			intIn("stash.lost_found.hours", lf.Hours, 0, 8760)
		default:
			fields["stash.lost_found.mode"] = "must be kept_after_con or deleted_after_closing"
		}
	}
	if w := d.Wiki; w != nil {
		days("wiki.revision_retention_days", w.RevisionRetentionDays)
	}
	if t := d.Tasks; t != nil {
		days("tasks.trash_days", t.TrashDays)
		days("tasks.notification_days", t.NotificationDays)
		days("tasks.audit_days", t.AuditDays)
		days("tasks.automation_run_days", t.AutomationRunDays)
		intIn("tasks.due_soon_days", t.DueSoonDays, 0, 30)
	}
	if r := d.Rally; r != nil {
		if r.SubmissionSiteHost = trimPtr(r.SubmissionSiteHost); r.SubmissionSiteHost != nil {
			*r.SubmissionSiteHost = strings.ToLower(*r.SubmissionSiteHost)
			if !hostPattern.MatchString(*r.SubmissionSiteHost) || !strings.Contains(*r.SubmissionSiteHost, ".") {
				fields["rally.submission_site_host"] = "must be a host name without https://, for example submit.example.org"
			}
		}
	}
	if i := d.Identity; i != nil {
		urlOK("identity.account_console_url", &i.AccountConsoleURL)
		urlOK("identity.idp_admin_console_url", &i.IdPAdminConsoleURL)
		strMax("identity.sso_name", &i.SSOName, 100)
	}
	return fields, newPassword, clearPassword
}

// pruneEmpty removes objects that hold no value (after normalize dropped
// empty strings), so a section without values is not sent at all.
func pruneEmpty(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	for k, child := range m {
		child = pruneEmpty(child)
		if cm, isMap := child.(map[string]any); isMap && len(cm) == 0 {
			delete(m, k)
			continue
		}
		m[k] = child
	}
	return m
}

// docMap is d as a JSON object without empty sections. password_set is
// computed and never stored.
func docMap(d moduleSettingsDoc) (map[string]any, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if smtp, ok := m["smtp"].(map[string]any); ok {
		delete(smtp, "password_set")
	}
	m, _ = pruneEmpty(m).(map[string]any)
	return m, nil
}

type moduleSettingsDTO struct {
	Data      moduleSettingsDoc `json:"data"`
	UpdatedAt *string           `json:"updated_at"`
}

// moduleSettingsPayload is what a module receives: module_settings in the
// bundle and GET /api/v1/integrations/module-settings.
type moduleSettingsPayload struct {
	Data      map[string]any `json:"data"`
	UpdatedAt *string        `json:"updated_at,omitempty"`
}

func (h *moduleSettingsHandler) load(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (moduleSettingsDoc, moduleSettingsSecrets, *time.Time, error) {
	var (
		doc     moduleSettingsDoc
		sec     moduleSettingsSecrets
		raw     []byte
		rawSec  []byte
		updated time.Time
	)
	err := q.QueryRow(ctx, `SELECT data, secrets, updated_at FROM module_settings WHERE organization_id = $1`, defaultOrgID).
		Scan(&raw, &rawSec, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return doc, sec, nil, nil
	}
	if err != nil {
		return doc, sec, nil, err
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return doc, sec, nil, err
	}
	if err := json.Unmarshal(rawSec, &sec); err != nil {
		return doc, sec, nil, err
	}
	if sec.SMTPPassword != "" {
		if doc.SMTP == nil {
			doc.SMTP = &smtpSettings{}
		}
		doc.SMTP.PasswordSet = true
	}
	return doc, sec, &updated, nil
}

func formatUpdated(updated *time.Time) *string {
	if updated == nil {
		return nil
	}
	s := updated.UTC().Format(time.RFC3339)
	return &s
}

func (h *moduleSettingsHandler) get(w http.ResponseWriter, r *http.Request) {
	doc, _, updated, err := h.load(r.Context(), h.pool)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, moduleSettingsDTO{Data: doc, UpdatedAt: formatUpdated(updated)})
}

// put is PUT /api/v1/module-settings with {"data": {…}}: replaces the whole
// document (the SMTP password is kept unless sent).
func (h *moduleSettingsHandler) put(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Data *moduleSettingsDoc `json:"data"`
		// UpdatedAt is accepted (a GET answer sent back) and ignored.
		UpdatedAt *string `json:"updated_at"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httperror.Render(w, r, err)
		return
	}
	if body.Data == nil {
		httperror.Render(w, r, httperror.BadRequest(`request body must be {"data": {…}}`))
		return
	}
	in := *body.Data
	fields, newPassword, clearPassword := h.normalize(&in)
	if len(fields) > 0 {
		httperror.Render(w, r, httperror.Validation("validation failed", fields))
		return
	}
	data, err := docMap(in)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	var (
		saved   moduleSettingsDoc
		updated *time.Time
	)
	err = auth.WithTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO module_settings (organization_id) VALUES ($1) ON CONFLICT DO NOTHING`, defaultOrgID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `SELECT 1 FROM module_settings WHERE organization_id = $1 FOR UPDATE`, defaultOrgID); err != nil {
			return err
		}
		before, sec, _, err := h.load(r.Context(), tx)
		if err != nil {
			return err
		}
		passwordChange := ""
		switch {
		case newPassword != nil:
			sec.SMTPPassword, passwordChange = *newPassword, "set"
		case clearPassword || in.SMTP == nil:
			if sec.SMTPPassword != "" {
				passwordChange = "cleared"
			}
			sec.SMTPPassword = ""
		}
		secrets, err := json.Marshal(sec)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE module_settings SET data = $2, secrets = $3, updated_at = now(), updated_by = $4
			 WHERE organization_id = $1`, defaultOrgID, data, secrets, auth.ActorID(r.Context())); err != nil {
			return err
		}
		saved, _, updated, err = h.load(r.Context(), tx)
		if err != nil {
			return err
		}
		note := ""
		if passwordChange != "" {
			note = "SMTP password " + passwordChange
		}
		return audit.Write(r.Context(), tx, audit.Event{
			OrgID: orgRef(), Action: "module_settings.update", EntityType: "organization", EntityID: defaultOrgID,
			Before: before, After: saved, Note: note,
		})
	})
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, moduleSettingsDTO{Data: saved, UpdatedAt: formatUpdated(updated)})
}

// moduleSection names the module-specific section a slug receives. Rally's
// slug is "events"; its section is "rally".
func moduleSection(slug string) string {
	switch slug {
	case "finance", "stash", "wiki", "tasks":
		return slug
	case "rally", "events":
		return "rally"
	}
	return ""
}

// forModule is what a module with slug receives: retention, smtp (with
// password_set, never the password) and its own section.
func (h *moduleSettingsHandler) forModule(ctx context.Context, slug string) (moduleSettingsPayload, error) {
	doc, _, updated, err := h.load(ctx, h.pool)
	if err != nil {
		return moduleSettingsPayload{}, err
	}
	all, err := docMap(doc)
	if err != nil {
		return moduleSettingsPayload{}, err
	}
	out := moduleSettingsPayload{Data: map[string]any{}, UpdatedAt: formatUpdated(updated)}
	for _, key := range []string{"retention", "smtp", moduleSection(slug)} {
		if v, ok := all[key]; ok && key != "" {
			out.Data[key] = v
		}
	}
	if smtp, ok := out.Data["smtp"].(map[string]any); ok {
		smtp["password_set"] = doc.SMTP != nil && doc.SMTP.PasswordSet
	}
	return out, nil
}

// integrationGet is GET /api/v1/integrations/module-settings.
func (h *moduleSettingsHandler) integrationGet(w http.ResponseWriter, r *http.Request) {
	m, ok := auth.EnsureModule(w, r)
	if !ok {
		return
	}
	out, err := h.forModule(r.Context(), m.Slug)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// integrationSecrets is GET /api/v1/integrations/module-settings/secrets:
// the secret values of the calling module's settings. Keys without a value
// are left out.
func (h *moduleSettingsHandler) integrationSecrets(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.EnsureModule(w, r); !ok {
		return
	}
	_, sec, _, err := h.load(r.Context(), h.pool)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	out := map[string]any{}
	if sec.SMTPPassword != "" {
		out["smtp"] = map[string]string{"password": sec.SMTPPassword}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

// moduleView is GET /api/v1/modules/{id}/settings: exactly what that module
// receives (without secrets), for admins checking a module.
func (h *moduleSettingsHandler) moduleView(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var slug string
	err = h.pool.QueryRow(r.Context(), `SELECT slug FROM modules WHERE id = $1 AND organization_id = $2`, id, defaultOrgID).Scan(&slug)
	if errors.Is(err, pgx.ErrNoRows) {
		httperror.Render(w, r, httperror.NotFound("module"))
		return
	}
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	out, err := h.forModule(r.Context(), slug)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// identityLinks is the part of the identity section the portal shows:
// everyone gets the account console and SSO name, admins also the IdP admin
// console.
func (h *moduleSettingsHandler) identityLinks(ctx context.Context, admin bool) map[string]string {
	doc, _, _, err := h.load(ctx, h.pool)
	out := map[string]string{}
	if err != nil || doc.Identity == nil {
		return out
	}
	if v := doc.Identity.AccountConsoleURL; v != nil {
		out["account_console_url"] = *v
	}
	if v := doc.Identity.SSOName; v != nil {
		out["sso_name"] = *v
	}
	if v := doc.Identity.IdPAdminConsoleURL; v != nil && admin {
		out["idp_admin_console_url"] = *v
	}
	return out
}
