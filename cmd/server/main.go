// Command server runs the HTTP API (internal/api) with graceful shutdown —
// the read path for rules, employees, assignments, and explain traces.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ramanasai/wrap-policy-assignment-system/internal/api"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/config"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/logging"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/repo"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/utils"
)

func main() {
	if err := utils.LoadDotEnv(); err != nil {
		panic("server: .env: " + err.Error())
	}
	cfg := config.MustLoad()
	logger := logging.New(logging.SetupFromEnv(), logging.ComponentAPI)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := repo.New(ctx, cfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot connect — run `make migrate` and start Postgres first")
	}
	defer store.Close()

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           api.New(api.Deps{Store: store, Logger: logger}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		logger.Info().Msg("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error().Err(err).Msg("graceful shutdown failed")
		}
	}()

	logger.Info().Int("port", cfg.HTTPPort).Msg("api listening")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal().Err(err).Msg("server failed")
	}
}