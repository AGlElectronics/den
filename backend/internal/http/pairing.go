package http

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

type pairingHandler struct {
	pool       *pgxpool.Pool
	appBaseURL string
}

func newPairingHandler(pool *pgxpool.Pool, appBaseURL string) *pairingHandler {
	return &pairingHandler{pool: pool, appBaseURL: appBaseURL}
}

// mintToken is POST /api/v1/modules/{id}/pairing-tokens — operator-triggered.
// Returns the raw token exactly once, embedded in a pairing URL the operator
// pastes into the mated module's "Pair with Den" screen.
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

	// Verify the module exists before minting (nicer error than a FK violation).
	var exists bool
	if err := h.pool.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM modules WHERE id = $1)`, moduleID).Scan(&exists); err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	if !exists {
		httperror.Render(w, r, httperror.NotFound("module"))
		return
	}

	raw, err := randomToken(32)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	hash := sha256Hex(raw)
	expiresAt := time.Now().Add(15 * time.Minute)

	var tokenID uuid.UUID
	err = h.pool.QueryRow(r.Context(), `
		INSERT INTO pairing_tokens (module_id, token_hash, expires_at, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, moduleID, hash, expiresAt, auth.ActorID(r.Context())).Scan(&tokenID)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}

	pairingURL := h.appBaseURL + "/api/v1/integrations/pairing/redeem?token=" + raw

	writeJSON(w, http.StatusCreated, map[string]any{
		"token_id":    tokenID.String(),
		"module_id":   moduleID.String(),
		"token":       raw, // shown ONCE; UI must warn the operator
		"pairing_url": pairingURL,
		"expires_at":  expiresAt.Format(time.RFC3339),
	})
}

type redeemInput struct {
	// Optional server-to-server metadata the module sends about itself.
	ModuleVersion string `json:"module_version,omitempty"`
	ModuleSlug    string `json:"module_slug,omitempty"`
}

// redeem is POST /api/v1/integrations/pairing/redeem?token=... — called by
// the mated module's backend, not the browser. Returns a long-lived module
// credential exactly once.
func (h *pairingHandler) redeem(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("token")
	if raw == "" {
		httperror.Render(w, r, httperror.BadRequest("missing token"))
		return
	}
	// Body is optional; swallow decode errors so a bare POST still works.
	var in redeemInput
	_ = decodeJSON(r, &in)

	if h.pool == nil {
		httperror.Render(w, r, httperror.Internal(errors.New("database not configured")))
		return
	}

	hash := sha256Hex(raw)

	var (
		tokenID    uuid.UUID
		moduleID   uuid.UUID
		expiresAt  time.Time
		consumedAt *time.Time
	)
	err := h.pool.QueryRow(r.Context(), `
		SELECT id, module_id, expires_at, consumed_at
		  FROM pairing_tokens
		 WHERE token_hash = $1
	`, hash).Scan(&tokenID, &moduleID, &expiresAt, &consumedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httperror.Render(w, r, httperror.Unauthorized("invalid pairing token"))
			return
		}
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	if consumedAt != nil {
		httperror.Render(w, r, httperror.Unauthorized("pairing token already consumed"))
		return
	}
	if time.Now().After(expiresAt) {
		httperror.Render(w, r, httperror.Unauthorized("pairing token expired"))
		return
	}

	// Look up the module to echo the org id back to the caller.
	var orgID uuid.UUID
	if err := h.pool.QueryRow(r.Context(),
		`SELECT organization_id FROM modules WHERE id = $1`, moduleID).Scan(&orgID); err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}

	credential, err := randomToken(32)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	credHash := sha256Hex(credential)

	label := "pairing-" + time.Now().UTC().Format("20060102-150405")
	if in.ModuleVersion != "" {
		label = label + " v" + in.ModuleVersion
	}

	var credentialID uuid.UUID
	err = auth.WithTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(),
			`UPDATE pairing_tokens SET consumed_at = now() WHERE id = $1 AND consumed_at IS NULL`,
			tokenID); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO module_credentials (module_id, credential_hash, label)
			VALUES ($1, $2, $3)
			RETURNING id
		`, moduleID, credHash, label).Scan(&credentialID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE modules SET status = 'paired', last_seen_at = now(), updated_at = now()
			 WHERE id = $1
		`, moduleID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO audit_events (organization_id, action, entity_type, entity_id, note)
			VALUES ($1, 'pairing.token.redeem', 'module', $2, $3)
		`, orgID, moduleID, label); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
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
// Exposed so the router can wire it without pairing having to import http.
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
	hash := sha256Hex(raw)

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
	`, hash).Scan(&credentialID, &moduleID, &orgID, &slug)
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
