// Command worker runs the reconciliation services: the outbox reconciler
// (LISTEN/NOTIFY + poll safety net), the sweep backstop, and the
// future-dated transition scheduler. One static binary.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/ramanasai/wrap-policy-assignment-system/internal/config"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/logging"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/reconciler"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/repo"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/utils"
)

func main() {
	if err := utils.LoadDotEnv(); err != nil {
		panic("worker: .env: " + err.Error())
	}
	cfg := config.MustLoad()
	logger := logging.New(logging.SetupFromEnv(), logging.ComponentReconciler)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := repo.New(ctx, cfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot connect — run `make migrate` and start Postgres first")
	}
	defer store.Close()

	rec := reconciler.New(store, logger, reconciler.Config{
		BatchSize:     int32(cfg.ReconcilerBatchSize),
		PollInterval:  cfg.ReconcilerPoll,
		MaxAttempts:   5,
		NotifyChannel: "new_outbox",
	})

	// Sweep backstop: startup + interval (the correctness floor).
	go sweepLoop(ctx, store, rec, logger, cfg.SweeperInterval)

	// Scheduler: fire future-dated transitions (tenure gates, "Jan 1" rules).
	go reconciler.NewScheduler(rec, logger).Run(ctx, cfg.SchedulerInterval)

	logger.Info().
		Int32("batch_size", rec.BatchSize()).
		Dur("poll", rec.PollInterval()).
		Dur("sweep", cfg.SweeperInterval).
		Dur("scheduler", cfg.SchedulerInterval).
		Msg("worker started")

	rec.Run(ctx, cfg.DatabaseURL) // blocks; drains on NOTIFY + poll
	logger.Info().Msg("worker stopped cleanly")
}

// sweepLoop runs the expected-vs-actual backstop. Drift is repaired from
// truth (the resolver) and logged — the projection is a cache by design.
func sweepLoop(ctx context.Context, store *repo.Store, rec *reconciler.Reconciler, logger zerolog.Logger, interval time.Duration) {
	sweeper := reconciler.NewSweeper(rec)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	run := func() {
		stats, err := sweeper.Run(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Error().Err(err).Msg("sweep failed")
			return
		}
		if stats.Repaired > 0 {
			logger.Warn().
				Int("repaired", stats.Repaired).
				Int("checked", stats.Checked).
				Int("drift_rows", len(stats.Drift)).
				Msg("sweeper repaired projection drift")
		} else {
			logger.Debug().Int("checked", stats.Checked).Msg("sweep clean")
		}
	}

	run() // startup sweep
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
