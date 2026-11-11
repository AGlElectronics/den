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
// Module settings). Every section is optional; a missing value means "use
// the module's own setting". Paired modules get them in the integration
// bundle, each module only the sections it uses:
//
//	retention, smtp, identity — every module
//	finance                    — the module with slug "finance"
//	stash                      — slug "stash"
//	rally                      — slug "rally" or "events"
//
// The SMTP password is write-only: the admin API reports only whether it is
// set; paired modules receive it in the bundle.

type moduleSettingsHandler struct {
	pool      *pgxpool.Pool
	allowHTTP bool
}

type retentionSettings struct {
	// PendingLegalReview is always true for now: the defaults have not been
	// checked by a lawyer yet. Modules show it next to the values.
	PendingLegalReview       bool    `json:"pending_legal_review"`
	OffboardedVolunteersDays *int    `json:"offboarded_volunteers_days,omitempty"`
	InactiveUsersDays        *int    `json:"inactive_users_days,omitempty"`
	ModuleDataDays           *int    `json:"module_data_days,omitempty"`
	BackupNote               *string `json:"backup_note,omitempty"`
}

type smtpSettings struct {
	Host     *string `json:"host,omitempty"`
	Port     *int    `json:"port,omitempty"`
	From     *string `json:"from,omitempty"`
	Username *string `json:"username,omitempty"`
	// Password is write-only on the admin API: a string sets it, null
	// clears it, leaving it out keeps it.
	Password    json.RawMessage `json:"password,omitempty"`
	PasswordSet bool            `json:"password_set"`
}

type financeSettings struct {
	SellerName    *string `json:"seller_name,omitempty"`
	SellerAddress *string `json:"seller_address,omitempty"`
	VATNumber     *string `json:"vat_number,omitempty"`
}

type lostAndFoundSettings struct {
	// Mode is days_after_con (items are kept N days after the con) or
	// hours_after_closing (items are deleted N hours after their case closes).
	Mode  string `json:"mode"`
	Days  *int   `json:"days,omitempty"`
	Hours *int   `json:"hours,omitempty"`
}

type stashSettings struct {
	LostAndFound *lostAndFoundSettings `json:"lost_and_found,omitempty"`
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
	Rally     *rallySettings     `json:"rally,omitempty"`
	Identity  *identitySettings  `json:"identity,omitempty"`
}

type moduleSettingsSecrets struct {
	SMTPPassword string `json:"smtp_password,omitempty"`
}

var (
	hostPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]*[a-z0-9])?(:[0-9]{1,5})?$`)
	vatPattern  = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]{2,13}$`)
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

// normalize trims values, drops empty ones and returns per-field errors.
// It returns the new SMTP password (set), whether it is cleared, or neither.
func (h *moduleSettingsHandler) normalize(d *moduleSettingsDoc) (fields map[string]string, newPassword *string, clearPassword bool) {
	fields = map[string]string{}
	intIn := func(key string, v *int, lo, hi int) {
		if v != nil && (*v < lo || *v > hi) {
			fields[key] = "must be between " + strconv.Itoa(lo) + " and " + strconv.Itoa(hi)
		}
	}
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

	if r := d.Retention; r != nil {
		r.PendingLegalReview = true
		intIn("retention.offboarded_volunteers_days", r.OffboardedVolunteersDays, 1, 3650)
		intIn("retention.inactive_users_days", r.InactiveUsersDays, 1, 3650)
		intIn("retention.module_data_days", r.ModuleDataDays, 1, 3650)
		strMax("retention.backup_note", &r.BackupNote, 2000)
	}
	if s := d.SMTP; s != nil {
		if s.Host = trimPtr(s.Host); s.Host != nil {
			*s.Host = strings.ToLower(*s.Host)
			if !hostPattern.MatchString(*s.Host) {
				fields["smtp.host"] = "must be a host name such as smtp.example.org"
			}
		}
		intIn("smtp.port", s.Port, 1, 65535)
		if s.From = trimPtr(s.From); s.From != nil {
			if _, err := mail.ParseAddress(*s.From); err != nil {
				fields["smtp.from"] = "must be an e-mail address, for example Example Con <noreply@example.org>"
			}
		}
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
	if f := d.Finance; f != nil {
		strMax("finance.seller_name", &f.SellerName, 200)
		strMax("finance.seller_address", &f.SellerAddress, 1000)
		if f.VATNumber = trimPtr(f.VATNumber); f.VATNumber != nil {
			v := strings.ToUpper(strings.NewReplacer(" ", "", ".", "", "-", "").Replace(*f.VATNumber))
			f.VATNumber = &v
			if !vatPattern.MatchString(v) {
				fields["finance.vat_number"] = "must start with a two-letter country code, for example NL123456789B01"
			}
		}
	}
	if s := d.Stash; s != nil && s.LostAndFound != nil {
		lf := s.LostAndFound
		switch lf.Mode {
		case "days_after_con":
			lf.Hours = nil
			if lf.Days == nil {
				fields["stash.lost_and_found.days"] = "is required for days_after_con"
			}
			intIn("stash.lost_and_found.days", lf.Days, 0, 3650)
		case "hours_after_closing":
			lf.Days = nil
			if lf.Hours == nil {
				fields["stash.lost_and_found.hours"] = "is required for hours_after_closing"
			}
			intIn("stash.lost_and_found.hours", lf.Hours, 0, 8760)
		default:
			fields["stash.lost_and_found.mode"] = "must be days_after_con or hours_after_closing"
		}
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

type moduleSettingsDTO struct {
	Data      moduleSettingsDoc `json:"data"`
	UpdatedAt *string           `json:"updated_at"`
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
	if doc.SMTP != nil {
		doc.SMTP.PasswordSet = sec.SMTPPassword != ""
	}
	return doc, sec, &updated, nil
}

func (h *moduleSettingsHandler) get(w http.ResponseWriter, r *http.Request) {
	doc, _, updated, err := h.load(r.Context(), h.pool)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, settingsDTO(doc, updated))
}

func settingsDTO(doc moduleSettingsDoc, updated *time.Time) moduleSettingsDTO {
	out := moduleSettingsDTO{Data: doc}
	if updated != nil {
		s := updated.UTC().Format(time.RFC3339)
		out.UpdatedAt = &s
	}
	return out
}

// put is PUT /api/v1/module-settings: replaces the whole document (the SMTP
// password is kept unless sent).
func (h *moduleSettingsHandler) put(w http.ResponseWriter, r *http.Request) {
	var in moduleSettingsDoc
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	fields, newPassword, clearPassword := h.normalize(&in)
	if len(fields) > 0 {
		httperror.Render(w, r, httperror.Validation("validation failed", fields))
		return
	}
	var (
		saved   moduleSettingsDoc
		updated *time.Time
	)
	err := auth.WithTx(r.Context(), h.pool, func(tx pgx.Tx) error {
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
		data, err := json.Marshal(in)
		if err != nil {
			return err
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
	writeJSON(w, http.StatusOK, settingsDTO(saved, updated))
}

// moduleSection names the module-specific section a slug receives.
func moduleSection(slug string) string {
	switch slug {
	case "finance":
		return "finance"
	case "stash":
		return "stash"
	case "rally", "events":
		return "rally"
	}
	return ""
}

// forModule is what a paired module receives: the shared sections, its own
// section, and the SMTP password in clear.
func (h *moduleSettingsHandler) forModule(ctx context.Context, slug string) (map[string]any, error) {
	doc, sec, updated, err := h.load(ctx, h.pool)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if doc.Retention != nil {
		out["retention"] = doc.Retention
	}
	if doc.SMTP != nil {
		smtp := map[string]any{"host": doc.SMTP.Host, "port": doc.SMTP.Port, "from": doc.SMTP.From, "username": doc.SMTP.Username}
		if sec.SMTPPassword != "" {
			smtp["password"] = sec.SMTPPassword
		}
		out["smtp"] = smtp
	}
	if doc.Identity != nil {
		out["identity"] = doc.Identity
	}
	switch moduleSection(slug) {
	case "finance":
		if doc.Finance != nil {
			out["finance"] = doc.Finance
		}
	case "stash":
		if doc.Stash != nil {
			out["stash"] = doc.Stash
		}
	case "rally":
		if doc.Rally != nil {
			out["rally"] = doc.Rally
		}
	}
	if updated != nil {
		out["updated_at"] = updated.UTC().Format(time.RFC3339)
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
