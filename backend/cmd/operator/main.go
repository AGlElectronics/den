// Command den-operator is Den's internal operations service: module
// database provisioning and encrypted suite backups. It holds the Postgres
// admin login, so it must only be reachable by Den on the internal network.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "time/tzdata"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/operator"
	"github.com/AGlElectronics/den/backend/internal/tlscert"
)

func main() {
	log := zerolog.New(os.Stdout).With().Timestamp().Str("service", "den-operator").Logger()

	cfg, err := operator.LoadConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("config")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	op, err := operator.New(ctx, cfg, log)
	if err != nil {
		log.Fatal().Err(err).Msg("start")
	}
	if op.BackupsEnabled() {
		go op.Schedule(ctx)
	}

	srv := &http.Server{Addr: cfg.Addr, Handler: op.Router(), ReadHeaderTimeout: 10 * time.Second}
	tlsOn, err := tlscert.Configure(ctx, srv, log)
	if err != nil {
		log.Fatal().Err(err).Msg("tls")
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info().Str("addr", cfg.Addr).
		Bool("managed_databases", cfg.ManagedDatabases).
		Bool("backups", cfg.BackupsEnabled).
		Str("timezone", cfg.Location.String()).
		Bool("tls", tlsOn).
		Msg("den operator ready")
	if err := tlscert.ListenAndServe(srv); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal().Err(err).Msg("serve")
	}
}
