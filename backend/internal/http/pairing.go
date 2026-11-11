package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/audit"
	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// pairingTokenTTL is how long a minted pairing URL can be redeemed.
const pairingTokenTTL = 15 * time.Minute

type pairingHandler struct {
	pool       *pgxpool.Pool
	appBaseURL string
	// credentialKey derives module credentials (DEN_CREDENTIAL_KEY) so Den
	// can present a module's credential when it calls the module. Empty:
	// credentials are random and Den cannot call modules.
	credentialKey []byte
}

func newPairingHandler(pool *pgxpool.Pool, appBaseURL string, credentialKey []byte) *pairingHandler {
	return &pairingHandler{pool: pool, appBaseURL: appBaseURL, credentialKey: credentialKey}
}

// mintToken is POST /api/v1/modules/{id}/pairing-tokens — admin-triggered.
// Returns the raw token exactly once, embedded in a pairing URL the admin
// pastes into the module's "Den connection" screen.
func (h *pairingHandler) mintToken(w http.ResponseWriter, r *http.Request) {
	moduleID, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	if h.pool == nil {
		httperror.Render(w, r, httperror.Internal(errors.New("database not configured")))
		return
	}

	raw, err := randomToken(32)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	expiresAt := time.Now().Add(pairingTokenTTL)

	var tokenID uuid.UUID
	err = auth.WithTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		var slug string
		if err := tx.QueryRow(r.Context(),
			`SELECT slug FROM modules WHERE id = $1 AND organization_id = $2`, moduleID, defaultOrgID).Scan(&slug); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httperror.NotFound("module")
			}
			return err
		}
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO pairing_tokens (module_id, token_hash, expires_at, created_by)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, moduleID, sha256Hex(raw), expiresAt, auth.ActorID(r.Context())).Scan(&tokenID); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Event{
			OrgID: orgRef(), Action: "pairing.token.create", EntityType: "module", EntityID: moduleID,
			After: map[string]any{"token_id": tokenID, "expires_at": expiresAt.UTC().Format(time.RFC3339)},
			Note:  "pairing URL created for " + slug,
		})
	})
	if err != nil {
		httperror.Render(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"token_id":    tokenID.String(),
		"module_id":   moduleID.String(),
		"token":       raw, // shown ONCE; UI must warn the admin
		"pairing_url": h.appBaseURL + "/api/v1/integrations/pairing/redeem?token=" + raw,
		"expires_at":  expiresAt.Format(time.RFC3339),
	})
}

type redeemInput struct {
	// Optional server-to-server metadata the module sends about itself. When
	// module_id or module_slug is sent it must match the module the token
	// was minted for (ADR 0002).
	ModuleID      string `json:"module_id,omitempty"`
	ModuleVersion string `json:"module_version,omitempty"`
	ModuleSlug    string `json:"module_slug,omitempty"`
}

const maxRedeemBody = 16 << 10

func readRedeemInput(r *http.Request) (redeemInput, error) {
	var in redeemInput
	if r.Body == nil {
		return in, nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxRedeemBody+1))
	if err != nil {
		return in, httperror.BadRequest("could not read the request body")
	}
	if len(raw) > maxRedeemBody {
		return in, httperror.BadRequest("request body too large")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return in, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, httperror.BadRequest("invalid JSON: " + err.Error())
	}
	return in, nil
}

// redeem is POST /api/v1/integrations/pairing/redeem?token=... — called by
// the module's backend, not the browser. Returns a long-lived module
// credential exactly once. Everything happens in one transaction: the token
// row is locked, consumed (a concurrent redeem finds it consumed), every
// older credential of the module is revoked and the new one is stored.
func (h *pairingHandler) redeem(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("token")
	if raw == "" {
		httperror.Render(w, r, httperror.BadRequest("missing token"))
		return
	}
	in, err := readRedeemInput(r)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	if h.pool == nil {
		httperror.Render(w, r, httperror.Internal(errors.New("database not configured")))
		return
	}
	ctx := r.Context()

	var (
		moduleID     uuid.UUID
		orgID        uuid.UUID
		credential   string
		credentialID = uuid.New()
	)
	err = auth.WithTx(ctx, h.pool, func(tx pgx.Tx) error {
		var (
			tokenID    uuid.UUID
			expiresAt  time.Time
			consumedAt *time.Time
			createdBy  *uuid.UUID
			slug       string
		)
		err := tx.QueryRow(ctx, `
			SELECT pt.id, pt.module_id, pt.expires_at, pt.consumed_at, pt.created_by, m.organization_id, m.slug
			  FROM pairing_tokens pt
			  JOIN modules m ON m.id = pt.module_id
			 WHERE pt.token_hash = $1
			   FOR UPDATE OF pt
		`, sha256Hex(raw)).Scan(&tokenID, &moduleID, &expiresAt, &consumedAt, &createdBy, &orgID, &slug)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return httperror.Unauthorized("invalid pairing token")
		case err != nil:
			return err
		case consumedAt != nil:
			return httperror.Unauthorized("pairing token already consumed")
		case !time.Now().Before(expiresAt):
			return httperror.Unauthorized("pairing token expired")
		}
		if in.ModuleID != "" {
			if id, err := uuid.Parse(in.ModuleID); err != nil || id != moduleID {
				return httperror.Conflict("this pairing URL was made for another module (module_id does not match)")
			}
		}
		if in.ModuleSlug != "" && in.ModuleSlug != slug {
			return httperror.Conflict("this pairing URL was made for the module " + slug + ", not " + in.ModuleSlug)
		}

		tag, err := tx.Exec(ctx,
			`UPDATE pairing_tokens SET consumed_at = now() WHERE id = $1 AND consumed_at IS NULL`, tokenID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return httperror.Unauthorized("pairing token already consumed")
		}

		revoked, err := tx.Exec(ctx,
			`UPDATE module_credentials SET revoked_at = now() WHERE module_id = $1 AND revoked_at IS NULL`, moduleID)
		if err != nil {
			return err
		}

		if credential, err = h.newCredential(credentialID); err != nil {
			return err
		}
		label := "pairing-" + time.Now().UTC().Format("20060102-150405")
		if in.ModuleVersion != "" {
			label += " v" + in.ModuleVersion
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO module_credentials (id, module_id, credential_hash, label)
			VALUES ($1, $2, $3, $4)
		`, credentialID, moduleID, sha256Hex(credential), label); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE modules SET status = 'paired', last_seen_at = now(), updated_at = now()
			 WHERE id = $1
		`, moduleID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Event{
			OrgID: &orgID, Actor: createdBy, Action: "pairing.token.redeem", EntityType: "module", EntityID: moduleID,
			After: map[string]any{
				"token_id": tokenID, "credential_id": credentialID, "label": label,
				"module_version": in.ModuleVersion, "revoked_credentials": revoked.RowsAffected(),
			},
			Note: label,
		})
	})
	if err != nil {
		httperror.Render(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"credential":      credential, // shown ONCE
		"credential_id":   credentialID.String(),
		"module_id":       moduleID.String(),
		"organization_id": orgID.String(),
		"den_base_url":    h.appBaseURL,
	})
}

func (h *pairingHandler) newCredential(id uuid.UUID) (string, error) {
	if len(h.credentialKey) == 0 {
		return randomToken(32)
	}
	return deriveCredential(h.credentialKey, id), nil
}

// deriveCredential is HMAC-SHA256(key, credential id): Den can recompute a
// module's credential to call the module, while the database only holds its
// hash.
func deriveCredential(key []byte, id uuid.UUID) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("den-module-credential:"))
	m.Write(id[:])
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// --- Credentials: list and revoke -------------------------------------------

type credentialDTO struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	RevokedAt  *string `json:"revoked_at"`
}

func (h *pairingHandler) credentialRoutes(r chi.Router) {
	r.Get("/", h.listCredentials)
	r.Delete("/", h.revokeAllCredentials)
	r.Delete("/{credentialID}", h.revokeCredential)
}

func (h *pairingHandler) moduleSlug(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, moduleID uuid.UUID) (string, error) {
	var slug string
	err := q.QueryRow(ctx, `SELECT slug FROM modules WHERE id = $1 AND organization_id = $2`, moduleID, defaultOrgID).Scan(&slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httperror.NotFound("module")
	}
	return slug, err
}

// listCredentials is GET /api/v1/modules/{id}/credentials, newest first.
func (h *pairingHandler) listCredentials(w http.ResponseWriter, r *http.Request) {
	moduleID, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	if _, err := h.moduleSlug(r.Context(), h.pool, moduleID); err != nil {
		httperror.Render(w, r, err)
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, label, created_at, last_used_at, revoked_at
		  FROM module_credentials WHERE module_id = $1
		 ORDER BY created_at DESC`, moduleID)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	defer rows.Close()
	out := make([]credentialDTO, 0)
	ts := func(t *time.Time) *string {
		if t == nil {
			return nil
		}
		s := t.UTC().Format(time.RFC3339)
		return &s
	}
	for rows.Next() {
		var (
			id                uuid.UUID
			c                 credentialDTO
			created           time.Time
			lastUsed, revoked *time.Time
		)
		if err := rows.Scan(&id, &c.Label, &created, &lastUsed, &revoked); err != nil {
			httperror.Render(w, r, httperror.Internal(err))
			return
		}
		c.ID, c.CreatedAt, c.LastUsedAt, c.RevokedAt = id.String(), created.UTC().Format(time.RFC3339), ts(lastUsed), ts(revoked)
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, out)
}

// revokeCredential is DELETE /api/v1/modules/{id}/credentials/{credentialID}.
func (h *pairingHandler) revokeCredential(w http.ResponseWriter, r *http.Request) {
	moduleID, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	credID, err := parseUUIDParam(r, "credentialID")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	h.revoke(w, r, moduleID, &credID)
}

// revokeAllCredentials is DELETE /api/v1/modules/{id}/credentials.
func (h *pairingHandler) revokeAllCredentials(w http.ResponseWriter, r *http.Request) {
	moduleID, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	h.revoke(w, r, moduleID, nil)
}

// revoke revokes one credential (credID) or every active one (nil). A module
// left without an active credential is marked unpaired; pairing it again is
// a manual admin action.
func (h *pairingHandler) revoke(w http.ResponseWriter, r *http.Request, moduleID uuid.UUID, credID *uuid.UUID) {
	ctx := r.Context()
	var revoked int64
	var status string
	err := auth.WithTx(ctx, h.pool, func(tx pgx.Tx) error {
		slug, err := h.moduleSlug(ctx, tx, moduleID)
		if err != nil {
			return err
		}
		if credID != nil {
			var already *time.Time
			err := tx.QueryRow(ctx, `SELECT revoked_at FROM module_credentials WHERE id = $1 AND module_id = $2 FOR UPDATE`,
				*credID, moduleID).Scan(&already)
			if errors.Is(err, pgx.ErrNoRows) {
				return httperror.NotFound("credential")
			}
			if err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `
			UPDATE module_credentials SET revoked_at = now()
			 WHERE module_id = $1 AND revoked_at IS NULL AND ($2::uuid IS NULL OR id = $2)`, moduleID, credID)
		if err != nil {
			return err
		}
		revoked = tag.RowsAffected()
		if err := tx.QueryRow(ctx, `
			UPDATE modules SET status = CASE WHEN EXISTS (
			         SELECT 1 FROM module_credentials WHERE module_id = $1 AND revoked_at IS NULL)
			       THEN status ELSE 'unpaired' END,
			       updated_at = now()
			 WHERE id = $1 RETURNING status`, moduleID).Scan(&status); err != nil {
			return err
		}
		if revoked == 0 {
			return nil
		}
		ev := audit.Event{OrgID: orgRef(), EntityType: "module", EntityID: moduleID,
			After: map[string]any{"revoked": revoked, "status": status}}
		if credID != nil {
			ev.Action, ev.Note = "module.credential.revoke", "credential "+credID.String()+" of "+slug+" revoked"
			ev.After = map[string]any{"credential_id": *credID, "revoked": revoked, "status": status}
		} else {
			ev.Action, ev.Note = "module.credentials.revoke_all", "every credential of "+slug+" revoked"
		}
		return audit.Write(ctx, tx, ev)
	})
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": revoked, "module_status": status})
}

// --- helpers -----------------------------------------------------------------

func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// --- ModuleResolver for integration auth -------------------------------------

// NewModuleResolver returns an auth.ModuleResolver backed by the shared pool.
func NewModuleResolver(pool *pgxpool.Pool) auth.ModuleResolver {
	return &moduleCredResolver{pool: pool}
}

type moduleCredResolver struct {
	pool *pgxpool.Pool
}

// ResolveModuleCredential implements auth.ModuleResolver. The input is the
// raw credential from the Authorization header; we hash it, look up a
// non-revoked row, and attach the owning org for scoped queries.
func (r *moduleCredResolver) ResolveModuleCredential(ctx context.Context, raw string) (*auth.ModulePrincipal, error) {
	if r.pool == nil {
		return nil, errors.New("database not configured")
	}
	var (
		credentialID uuid.UUID
		moduleID     uuid.UUID
		orgID        uuid.UUID
		slug         string
	)
	err := r.pool.QueryRow(ctx, `
		SELECT mc.id, mc.module_id, m.organization_id, m.slug
		  FROM module_credentials mc
		  JOIN modules m ON m.id = mc.module_id
		 WHERE mc.credential_hash = $1
		   AND mc.revoked_at IS NULL
	`, sha256Hex(raw)).Scan(&credentialID, &moduleID, &orgID, &slug)
	if err != nil {
		return nil, err
	}

	// Best-effort touch; failure here must not reject the request.
	_, _ = r.pool.Exec(ctx, `UPDATE module_credentials SET last_used_at = now() WHERE id = $1`, credentialID)

	return &auth.ModulePrincipal{
		ModuleID:       moduleID,
		OrganizationID: orgID,
		Slug:           slug,
		CredentialID:   credentialID,
	}, nil
}
