// Package pairing issues one-shot pairing tokens that mated modules redeem
// for long-lived module credentials. See docs/adr/0002-pairing-url-and-token.md.
//
// Security model:
//   - The raw pairing token and the raw credential are shown to the operator
//     and the module exactly once. Only SHA-256 hashes are persisted.
//   - Tokens expire in 15 minutes and are single-use (`consumed_at`).
//   - Credentials can be revoked by setting `revoked_at`; the auth
//     middleware refuses them.
package pairing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/audit"
	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/domain"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// Default lifetime for a pairing token. Short on purpose — the operator is
// expected to redeem within a few minutes.
const DefaultTokenTTL = 15 * time.Minute

// Service bundles the DB pool and gives the HTTP layer a tidy surface.
type Service struct {
	pool       *pgxpool.Pool
	tokenTTL   time.Duration
	appBaseURL string
}

func NewService(pool *pgxpool.Pool, appBaseURL string) *Service {
	return &Service{pool: pool, tokenTTL: DefaultTokenTTL, appBaseURL: appBaseURL}
}

// MintTokenResult is handed back to the operator. The raw token appears
// exactly here, in the one API response that creates it.
type MintTokenResult struct {
	TokenID    uuid.UUID `json:"token_id"`
	Token      string    `json:"token"`
	PairingURL string    `json:"pairing_url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// MintToken creates a pairing token for the given module. The raw token is
// returned only in the response; the DB stores just its sha-256 hash.
func (s *Service) MintToken(ctx context.Context, moduleID uuid.UUID) (MintTokenResult, error) {
	raw, err := randomToken(32)
	if err != nil {
		return MintTokenResult{}, httperror.Internal(err)
	}
	hash := hashHex(raw)
	expiresAt := time.Now().Add(s.tokenTTL)

	var out MintTokenResult
	err = withTx(ctx, s.pool, func(tx pgx.Tx) error {
		var tokenID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO pairing_tokens (module_id, token_hash, expires_at, created_by)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, moduleID, hash, expiresAt, auth.ActorID(ctx)).Scan(&tokenID); err != nil {
			return err
		}
		out = MintTokenResult{
			TokenID:    tokenID,
			Token:      raw,
			PairingURL: fmt.Sprintf("%s/api/v1/integrations/pairing/redeem?token=%s", s.appBaseURL, raw),
			ExpiresAt:  expiresAt,
		}
		// Audit does not include the raw token.
		return audit.Record(ctx, tx, orgIDForModule(ctx, tx, moduleID),
			domain.ActionPairingTokenMint, "pairing_token", tokenID,
			nil, map[string]any{"module_id": moduleID, "expires_at": expiresAt}, "")
	})
	if err != nil {
		return MintTokenResult{}, httperror.Internal(err)
	}
	return out, nil
}

// RedeemResult is returned once to the mated module when it POSTs the
// pairing URL. The module stores `Credential` in its secret store and never
// learns the plaintext again.
type RedeemResult struct {
	ModuleID       uuid.UUID `json:"module_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Credential     string    `json:"credential"`
	CredentialID   uuid.UUID `json:"credential_id"`
	DenBaseURL     string    `json:"den_base_url"`
}

// RedeemToken trades a raw pairing token for a module credential. Validates
// the token, consumes it, mints the credential, and audits everything.
func (s *Service) RedeemToken(ctx context.Context, rawToken, label string) (RedeemResult, error) {
	if rawToken == "" {
		return RedeemResult{}, httperror.BadRequest("token is required")
	}
	tokenHash := hashHex(rawToken)

	var out RedeemResult
	err := withTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			tokenID    uuid.UUID
			moduleID   uuid.UUID
			expiresAt  time.Time
			consumedAt *time.Time
			orgID      uuid.UUID
		)
		err := tx.QueryRow(ctx, `
			SELECT pt.id, pt.module_id, pt.expires_at, pt.consumed_at, m.organization_id
			  FROM pairing_tokens pt
			  JOIN modules m ON m.id = pt.module_id
			 WHERE pt.token_hash = $1
		`, tokenHash).Scan(&tokenID, &moduleID, &expiresAt, &consumedAt, &orgID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httperror.Unauthorized("invalid pairing token")
			}
			return err
		}
		if consumedAt != nil {
			return httperror.Conflict("pairing token already redeemed")
		}
		if time.Now().After(expiresAt) {
			return httperror.Unauthorized("pairing token expired")
		}

		if _, err := tx.Exec(ctx, `
			UPDATE pairing_tokens SET consumed_at = now() WHERE id = $1
		`, tokenID); err != nil {
			return err
		}

		rawCred, err := randomToken(32)
		if err != nil {
			return err
		}
		credHash := hashHex(rawCred)
		var credentialID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO module_credentials (module_id, credential_hash, label)
			VALUES ($1, $2, $3)
			RETURNING id
		`, moduleID, credHash, label).Scan(&credentialID); err != nil {
			return err
		}
		// Mark module as paired. We don't overwrite 'disabled' etc.; a
		// re-pair implies the module is working again.
		if _, err := tx.Exec(ctx, `
			UPDATE modules
			   SET status = 'paired', last_seen_at = now(), updated_at = now()
			 WHERE id = $1
		`, moduleID); err != nil {
			return err
		}

		if err := audit.Record(ctx, tx, &orgID,
			domain.ActionPairingTokenRedeem, "module_credential", credentialID,
			nil, map[string]any{"module_id": moduleID, "token_id": tokenID}, ""); err != nil {
			return err
		}

		out = RedeemResult{
			ModuleID:       moduleID,
			OrganizationID: orgID,
			Credential:     rawCred,
			CredentialID:   credentialID,
			DenBaseURL:     s.appBaseURL,
		}
		return nil
	})
	if err != nil {
		return RedeemResult{}, err
	}
	return out, nil
}

// ResolveModuleCredential implements auth.ModuleResolver so the integration
// router can authenticate incoming server-to-server calls.
func (s *Service) ResolveModuleCredential(ctx context.Context, raw string) (*auth.ModulePrincipal, error) {
	if raw == "" {
		return nil, pgx.ErrNoRows
	}
	hash := hashHex(raw)

	var (
		credID uuid.UUID
		modID  uuid.UUID
		orgID  uuid.UUID
		slug   string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT mc.id, m.id, m.organization_id, m.slug
		  FROM module_credentials mc
		  JOIN modules m ON m.id = mc.module_id
		 WHERE mc.credential_hash = $1 AND mc.revoked_at IS NULL
	`, hash).Scan(&credID, &modID, &orgID, &slug)
	if err != nil {
		return nil, err
	}

	// Best-effort touch so operators can see which credentials are live.
	_, _ = s.pool.Exec(ctx, `UPDATE module_credentials SET last_used_at = now() WHERE id = $1`, credID)
	_, _ = s.pool.Exec(ctx, `UPDATE modules SET last_seen_at = now() WHERE id = $1`, modID)

	return &auth.ModulePrincipal{
		ModuleID:       modID,
		OrganizationID: orgID,
		Slug:           slug,
		CredentialID:   credID,
	}, nil
}

// --- helpers -----------------------------------------------------------------

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashHex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func withTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// orgIDForModule looks up the organization owning a module inside the given
// transaction; falls back to nil on error so audit still gets written.
func orgIDForModule(ctx context.Context, tx pgx.Tx, moduleID uuid.UUID) *uuid.UUID {
	var orgID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT organization_id FROM modules WHERE id = $1`, moduleID).Scan(&orgID); err != nil {
		return nil
	}
	return &orgID
}
