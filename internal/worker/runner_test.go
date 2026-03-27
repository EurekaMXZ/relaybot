package worker

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"relaybot/internal/relay"
)

func TestRunnerRunOnceKeepsHadErrorAfterLaterTasksSucceed(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	runner := NewRunner(
		runnerStoreStub{
			deleteCollectingRelaysBeforeFunc: func(context.Context, time.Time) (int64, error) {
				return 0, errors.New("purge failed")
			},
		},
		runnerFixedClock{now: time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)},
		relay.Limits{
			BatchSessionTTL:      30 * time.Minute,
			UnknownDeliveryAfter: 2 * time.Minute,
			ExpiredDeliveryPurge: 24 * time.Hour,
		},
		logger,
	)

	runner.runOnce(context.Background())

	logs := logBuffer.String()
	if !strings.Contains(logs, `"msg":"maintenance cycle completed"`) {
		t.Fatalf("expected maintenance summary log, got %s", logs)
	}
	if !strings.Contains(logs, `"had_error":true`) {
		t.Fatalf("expected maintenance summary to keep had_error=true, got %s", logs)
	}
}

type runnerFixedClock struct {
	now time.Time
}

func (c runnerFixedClock) Now() time.Time {
	return c.now
}

type runnerStoreStub struct {
	deleteCollectingRelaysBeforeFunc func(context.Context, time.Time) (int64, error)
}

func (runnerStoreStub) CreateRelay(context.Context, relay.CreateRelayParams) (relay.Relay, bool, error) {
	return relay.Relay{}, false, nil
}

func (runnerStoreStub) CreateRelayBatch(context.Context, relay.CreateRelayBatchParams) (relay.Relay, error) {
	return relay.Relay{}, nil
}

func (runnerStoreStub) AddRelayItem(context.Context, relay.AddRelayItemParams) (relay.RelayItem, bool, error) {
	return relay.RelayItem{}, false, nil
}

func (runnerStoreStub) ListRelayItemsByRelayID(context.Context, int64) ([]relay.RelayItem, error) {
	return nil, nil
}

func (runnerStoreStub) FinalizeRelayBatch(context.Context, relay.FinalizeRelayBatchParams) (relay.Relay, error) {
	return relay.Relay{}, nil
}

func (runnerStoreStub) DeleteRelay(context.Context, int64) error {
	return nil
}

func (runnerStoreStub) GetRelayBySourceUpdateID(context.Context, int64) (relay.Relay, error) {
	return relay.Relay{}, relay.ErrRelayNotFound
}

func (runnerStoreStub) GetRelayByCodeHash(context.Context, string, time.Time) (relay.Relay, error) {
	return relay.Relay{}, relay.ErrRelayNotFound
}

func (runnerStoreStub) GetRelayByID(context.Context, int64) (relay.Relay, error) {
	return relay.Relay{}, relay.ErrRelayNotFound
}

func (runnerStoreStub) CountActiveRelaysByUploader(context.Context, int64, time.Time) (int64, error) {
	return 0, nil
}

func (runnerStoreStub) CreateDelivery(context.Context, relay.CreateDeliveryParams) (relay.Delivery, bool, error) {
	return relay.Delivery{}, false, nil
}

func (runnerStoreStub) MarkDeliverySent(context.Context, relay.MarkDeliverySentParams) error {
	return nil
}

func (runnerStoreStub) MarkDeliveryFailed(context.Context, relay.MarkDeliveryFailedParams) error {
	return nil
}

func (runnerStoreStub) MarkDeliveryUnknown(context.Context, relay.MarkDeliveryUnknownParams) error {
	return nil
}

func (runnerStoreStub) ExpireRelays(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (s runnerStoreStub) DeleteCollectingRelaysBefore(ctx context.Context, before time.Time) (int64, error) {
	if s.deleteCollectingRelaysBeforeFunc != nil {
		return s.deleteCollectingRelaysBeforeFunc(ctx, before)
	}
	return 0, nil
}

func (runnerStoreStub) MarkUnknownDeliveriesBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (runnerStoreStub) DeleteExpiredDeliveriesBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (runnerStoreStub) Ping(context.Context) error {
	return nil
}
