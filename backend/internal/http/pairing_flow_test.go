package http_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/config"
	denhttp "github.com/AGlElectronics/den/backend/internal/http"
)

// Integration: mint pairing token → redeem once → brand profile with module bearer.
// Skips unless DEN_TEST_DATABASE_URL is set.
func TestPairingMintRedeemBrand(t *testing.T) {
	dsn := os.Getenv("DEN_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("DEN_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.MustParse("00000000-0000-0000-0000-00000000d001")
	slug := "pt-" + uuid.NewString()[:12]
	var moduleID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO modules (organization_id, slug, display_name, base_url, environment)
		VALUES ($1, $2, 'Pairing Test Module', 'http://localhost:3999', 'dev')
		RETURNING id
	`, orgID, slug).Scan(&moduleID); err != nil {
		t.Fatalf("insert module: %v", err)
	}

	cfg := &config.Config{
		AppEnv:     "development",
		AppBaseURL: "http://localhost:8090",
		HTTPAddr:   ":0",
		AuthMode:   config.AuthModeDev,
	}
	router := denhttp.NewRouter(denhttp.Deps{
		Config: cfg,
		Pool:   pool,
		Logger: zerolog.Nop(),
	}, "test")

	mintReq := httptest.NewRequest(http.MethodPost, "/api/v1/modules/"+moduleID.String()+"/pairing-tokens", nil)
	mintReq.Header.Set("X-Dev-User", "pairing-tester")
	mintRec := httptest.NewRecorder()
	router.ServeHTTP(mintRec, mintReq)
	if mintRec.Code != http.StatusCreated {
		t.Fatalf("mint status=%d body=%s", mintRec.Code, mintRec.Body.String())
	}
	var mint struct {
		Token      string `json:"token"`
		PairingURL string `json:"pairing_url"`
	}
	if err := json.Unmarshal(mintRec.Body.Bytes(), &mint); err != nil {
		t.Fatalf("mint json: %v", err)
	}
	if mint.Token == "" {
		t.Fatal("mint missing token")
	}

	redeemReq := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/pairing/redeem?token="+mint.Token, bytes.NewReader([]byte(`{"module_version":"test"}`)))
	redeemReq.Header.Set("Content-Type", "application/json")
	redeemRec := httptest.NewRecorder()
	router.ServeHTTP(redeemRec, redeemReq)
	if redeemRec.Code != http.StatusOK {
		t.Fatalf("redeem status=%d body=%s", redeemRec.Code, redeemRec.Body.String())
	}
	var redeem struct {
		Credential string `json:"credential"`
	}
	if err := json.Unmarshal(redeemRec.Body.Bytes(), &redeem); err != nil || redeem.Credential == "" {
		t.Fatalf("redeem credential missing: %s", redeemRec.Body.String())
	}

	redeem2 := httptest.NewRecorder()
	router.ServeHTTP(redeem2, httptest.NewRequest(http.MethodPost, "/api/v1/integrations/pairing/redeem?token="+mint.Token, nil))
	if redeem2.Code == http.StatusOK {
		t.Fatal("second redeem should fail")
	}

	brandReq := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/branding/profile", nil)
	brandReq.Header.Set("Authorization", "Bearer "+redeem.Credential)
	brandRec := httptest.NewRecorder()
	router.ServeHTTP(brandRec, brandReq)
	if brandRec.Code != http.StatusOK {
		t.Fatalf("brand profile status=%d body=%s", brandRec.Code, brandRec.Body.String())
	}

	sum := sha256.Sum256([]byte(redeem.Credential))
	hash := hex.EncodeToString(sum[:])
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM module_credentials
		 WHERE module_id = $1 AND credential_hash = $2 AND revoked_at IS NULL
	`, moduleID, hash).Scan(&n); err != nil || n != 1 {
		t.Fatalf("credential row count=%d err=%v", n, err)
	}
}
