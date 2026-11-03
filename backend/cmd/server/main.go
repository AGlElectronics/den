// Command server is Den's HTTP API process. It is intentionally thin:
// everything substantive (config, DB, HTTP wiring, graceful shutdown)
// lives in internal/app so the server stays testable.
//
// This binary never runs migrations — that is cmd/migrate's job. See
// docs/adr/0005-no-shared-postgres.md for the rationale.
package main

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/app"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	zerolog.TimeFieldFormat = time.RFC3339

	// Pretty-print logs in dev; stay JSON in production so log aggregators
	// get structured data without extra parsing.
	if os.Getenv("APP_ENV") == "development" {
		logger = logger.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339})
	}

	ctx := context.Background()
	if err := app.Run(ctx, logger); err != nil {
		logger.Fatal().Err(err).Msg("server failed")
	}
}
