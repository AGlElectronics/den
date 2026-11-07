// Package app wires configuration, the database pool, the object storage
// client, and HTTP into a runnable server. cmd/server/main.go calls Run.
//
// This package deliberately does NOT run migrations. The migrate binary
// (cmd/migrate) is the only process allowed to touch the Goose version
// table — see docs/adr/0005-no-shared-postgres.md. A fresh server boot
// against an un-migrated DB will fail fast on the first query, which is
// exactly what we want.
package app

import (
	"context"
	"errors"
	"fmt"
	nethttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/config"
	"github.com/AGlElectronics/den/backend/internal/db"
	"github.com/AGlElectronics/den/backend/internal/files"
	httpx "github.com/AGlElectronics/den/backend/internal/http"
	"github.com/AGlElectronics/den/backend/internal/keycloak"
	"github.com/AGlElectronics/den/backend/internal/tlscert"
)

// Version is set via -ldflags at build time; defaults to "dev" so a plain
// `go run` locally is self-describing.
var Version = "dev"

// Run boots the server and blocks until SIGINT / SIGTERM. Any fatal error
// during startup is returned so main() can log and exit non-zero.
func Run(ctx context.Context, logger zerolog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger.Info().
		Str("env", cfg.AppEnv).
		Str("addr", cfg.HTTPAddr).
		Str("auth_mode", string(cfg.AuthMode)).
		Str("version", Version).
		Msg("starting den backend")

	dbCtx, cancelDB := context.WithTimeout(ctx, 30*time.Second)
	pool, err := db.Open(dbCtx, cfg.DatabaseURL)
	cancelDB()
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer pool.Close()
	logger.Info().Msg("database connected")

	// The compose S3 server (SeaweedFS) is DEV ONLY — see docs/adr/0003-minio-dev-only.md.
	// In production S3_* must point at a real managed bucket; the files
	// client code path is the same, just a different endpoint.
	filesCtx, cancelFiles := context.WithTimeout(ctx, 30*time.Second)
	filesClient, err := files.New(filesCtx, cfg)
	cancelFiles()
	if err != nil {
		return fmt.Errorf("init files: %w", err)
	}
	logger.Info().Str("bucket", filesClient.Bucket()).Msg("object storage ready")

	var oidcVerifier *auth.OIDCVerifier
	if cfg.AuthMode == config.AuthModeOIDC {
		oidcCtx, cancelOIDC := context.WithTimeout(ctx, 30*time.Second)
		oidcVerifier, err = auth.NewOIDCVerifier(oidcCtx, cfg)
		cancelOIDC()
		if err != nil {
			return fmt.Errorf("init oidc verifier: %w", err)
		}
		logger.Info().Str("issuer", cfg.OIDCIssuerURL).Msg("oidc verifier ready")
	}

	kcSync := httpx.NewKeycloakSync(pool, filesClient, keycloak.Config{
		BaseURL:      cfg.KeycloakAdminURL,
		Realm:        cfg.KeycloakRealm,
		ClientID:     cfg.KeycloakClientID,
		ClientSecret: cfg.KeycloakClientSecret,
	}, cfg.AppBaseURL, logger)
	if kcSync.Status().Enabled {
		logger.Info().Str("realm", cfg.KeycloakRealm).Msg("keycloak login branding sync enabled")
	}
	syncCtx, stopSync := context.WithCancel(ctx)
	defer stopSync()
	go kcSync.Run(syncCtx)

	router := httpx.NewRouter(httpx.Deps{
		Config:       cfg,
		Pool:         pool,
		Logger:       logger,
		Files:        filesClient,
		OIDCVerifier: oidcVerifier,
		KeycloakSync: kcSync,
	}, Version)

	server := &nethttp.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if _, err := tlscert.Configure(ctx, server, logger); err != nil {
		return fmt.Errorf("tls: %w", err)
	}

	return runServer(ctx, server, pool, cfg.ShutdownTimeout, logger)
}

func runServer(ctx context.Context, server *nethttp.Server, pool *pgxpool.Pool, shutdownTimeout time.Duration, logger zerolog.Logger) error {
	signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info().Str("addr", server.Addr).Msg("http listening")
		if err := tlscert.ListenAndServe(server); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-signalCtx.Done():
		logger.Info().Msg("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("http shutdown error")
	}
	pool.Close()
	logger.Info().Msg("shutdown complete")
	return nil
}
