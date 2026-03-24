package worker

import (
	"context"
	"log/slog"
	"time"

	"relaybot/internal/relay"
)

type Runner struct {
	store  relay.Store
	clock  relay.Clock
	limits relay.Limits
	logger *slog.Logger
}

func NewRunner(store relay.Store, clock relay.Clock, limits relay.Limits, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	if clock == nil {
		clock = systemClock{}
	}
	return &Runner{
		store:  store,
		clock:  clock,
		limits: limits,
		logger: logger,
	}
}

func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	r.runOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) {
	now := r.clock.Now().UTC()

	if count, err := r.store.ExpireRelays(ctx, now); err != nil {
		r.logger.Error("expire relays failed", "error", err)
	} else if count > 0 {
		r.logger.Info("expired relays", "count", count)
	}

	if count, err := r.store.MarkUnknownDeliveriesBefore(ctx, now.Add(-r.limits.UnknownDeliveryAfter)); err != nil {
		r.logger.Error("mark unknown deliveries failed", "error", err)
	} else if count > 0 {
		r.logger.Info("marked unknown deliveries", "count", count)
	}

	if count, err := r.store.DeleteExpiredDeliveriesBefore(ctx, now.Add(-r.limits.ExpiredDeliveryPurge)); err != nil {
		r.logger.Error("purge expired deliveries failed", "error", err)
	} else if count > 0 {
		r.logger.Info("purged expired deliveries", "count", count)
	}
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}
