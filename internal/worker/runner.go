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
	logger = logger.With("component", "worker_runner")
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

	r.logger.Info("maintenance runner started",
		"interval", time.Minute.String(),
		"unknown_delivery_after", r.limits.UnknownDeliveryAfter.String(),
		"expired_delivery_purge", r.limits.ExpiredDeliveryPurge.String(),
	)

	r.runOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("maintenance runner stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) {
	now := r.clock.Now().UTC()
	startedAt := time.Now()
	unknownCutoff := now.Add(-r.limits.UnknownDeliveryAfter)
	purgeCutoff := now.Add(-r.limits.ExpiredDeliveryPurge)
	var (
		expiredRelays           int64
		unknownDeliveries       int64
		purgedExpiredDeliveries int64
		hadError                bool
	)

	expiredRelays, hadError = r.runTask(ctx, "expire_relays", []any{"cutoff", now}, func(taskCtx context.Context) (int64, error) {
		return r.store.ExpireRelays(taskCtx, now)
	})

	var taskErr bool
	unknownDeliveries, taskErr = r.runTask(ctx, "mark_unknown_deliveries", []any{"cutoff", unknownCutoff}, func(taskCtx context.Context) (int64, error) {
		return r.store.MarkUnknownDeliveriesBefore(taskCtx, unknownCutoff)
	})
	hadError = hadError || taskErr

	purgedExpiredDeliveries, taskErr = r.runTask(ctx, "purge_expired_deliveries", []any{"cutoff", purgeCutoff}, func(taskCtx context.Context) (int64, error) {
		return r.store.DeleteExpiredDeliveriesBefore(taskCtx, purgeCutoff)
	})
	hadError = hadError || taskErr

	if hadError || expiredRelays > 0 || unknownDeliveries > 0 || purgedExpiredDeliveries > 0 {
		r.logger.Info("maintenance cycle completed",
			"expired_relays", expiredRelays,
			"unknown_deliveries", unknownDeliveries,
			"purged_expired_deliveries", purgedExpiredDeliveries,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"had_error", hadError,
		)
		return
	}

	r.logger.Debug("maintenance cycle completed with no changes", "duration_ms", time.Since(startedAt).Milliseconds())
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

func (r *Runner) runTask(ctx context.Context, task string, attrs []any, fn func(context.Context) (int64, error)) (int64, bool) {
	startedAt := time.Now()
	count, err := fn(ctx)
	logArgs := append([]any{"task", task}, attrs...)
	logArgs = append(logArgs, "duration_ms", time.Since(startedAt).Milliseconds())
	if err != nil {
		logArgs = append(logArgs, "error", err)
		r.logger.Error("maintenance task failed", logArgs...)
		return 0, true
	}
	if count > 0 {
		logArgs = append(logArgs, "count", count)
		r.logger.Info("maintenance task completed", logArgs...)
		return count, false
	}
	logArgs = append(logArgs, "count", count)
	r.logger.Debug("maintenance task completed with no changes", logArgs...)
	return 0, false
}
