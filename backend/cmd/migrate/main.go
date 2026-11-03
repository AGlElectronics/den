// Command migrate is Den's ONE-SHOT migration runner. It applies all
// pending Goose migrations against DATABASE_URL and exits.
//
// Why a dedicated binary rather than a flag on cmd/server?
//
//   - The server MUST NOT run migrations. See docs/adr/0005-no-shared-postgres.md:
//     concurrent replicas fighting over the Goose version table is a nightmare
//     we never want to debug. In Compose this binary runs as a service with
//     `restart: no`; in Kubernetes it runs as a Job.
//   - Scaling the API from 1 → N replicas should never risk a schema change
//     race. By never shipping migration code in the server image path, we
//     make that impossible, not just discouraged.
//
// Exit codes:
//
//	0  — migrations applied cleanly (or already up to date)
//	1  — any failure (config, connectivity, migration error)
package main

import (
	"context"
	"flag"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/config"
	"github.com/AGlElectronics/den/backend/internal/db"
)

func main() {
	statusOnly := flag.Bool("status", false, "print migration status and exit without applying")
	flag.Parse()

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	zerolog.TimeFieldFormat = time.RFC3339
	if os.Getenv("APP_ENV") == "development" {
		logger = logger.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339})
	}

	cfg, err := config.LoadMigrate()
	if err != nil {
		logger.Fatal().Err(err).Msg("load migrate config")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if *statusOnly {
		if err := db.MigrateStatus(ctx, cfg.DatabaseURL); err != nil {
			logger.Fatal().Err(err).Msg("migrate status failed")
		}
		return
	}

	logger.Info().Msg("applying migrations")
	if err := db.MigrateUp(ctx, cfg.DatabaseURL); err != nil {
		logger.Fatal().Err(err).Msg("migrate up failed")
	}
	logger.Info().Msg("migrations complete")
}
