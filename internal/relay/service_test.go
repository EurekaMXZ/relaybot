package relay

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateRelayReturnsCacheErrorOnUploadLimitCheck(t *testing.T) {
	cacheErr := errors.New("redis unavailable")
	service := NewService(
		stubStore{},
		stubCache{
			allowUploadFunc: func(context.Context, int64) (bool, error) {
				return false, cacheErr
			},
		},
		nil,
		NewHMACCodeManager("secret"),
		fixedClock{now: time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)},
		Limits{MaxFileBytes: 1024, MaxActiveRelays: 10, DefaultTTL: time.Hour},
	)

	_, err := service.CreateRelay(context.Background(), CreateRelayInput{
		SourceUpdateID:       1,
		UploaderUserID:       42,
		UploaderChatID:       99,
		SourceMessageID:      7,
		MediaKind:            MediaKindDocument,
		TelegramFileID:       "file-id",
		TelegramFileUniqueID: "file-unique-id",
		FileName:             "demo.txt",
		MIMEType:             "text/plain",
		FileSizeBytes:        100,
	})
	if !errors.Is(err, cacheErr) {
		t.Fatalf("CreateRelay() error = %v, want %v", err, cacheErr)
	}
}

func TestNewServicePanicsWhenCacheIsNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewService() expected panic when cache is nil")
		}
	}()

	NewService(
		stubStore{},
		nil,
		nil,
		NewHMACCodeManager("secret"),
		fixedClock{now: time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)},
		Limits{DefaultTTL: time.Hour},
	)
}

func TestClaimRelayReturnsCacheErrorOnClaimLimitCheck(t *testing.T) {
	cacheErr := errors.New("redis unavailable")
	service := NewService(
		stubStore{},
		stubCache{
			allowClaimFunc: func(context.Context, int64) (bool, error) {
				return false, cacheErr
			},
		},
		nil,
		NewHMACCodeManager("secret"),
		fixedClock{now: time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)},
		Limits{DefaultTTL: time.Hour},
	)

	_, err := service.ClaimRelay(context.Background(), ClaimRelayInput{
		RequestUpdateID: 1,
		ClaimerUserID:   42,
		ClaimerChatID:   99,
		RawCode:         "relaybot_bad",
	})
	if !errors.Is(err, cacheErr) {
		t.Fatalf("ClaimRelay() error = %v, want %v", err, cacheErr)
	}
}

func TestClaimRelayReturnsCacheErrorOnBadCodeLimitCheck(t *testing.T) {
	cacheErr := errors.New("redis unavailable")
	service := NewService(
		stubStore{},
		stubCache{
			allowClaimFunc: func(context.Context, int64) (bool, error) {
				return true, nil
			},
			allowBadCodeFunc: func(context.Context, int64) (bool, error) {
				return false, cacheErr
			},
		},
		nil,
		NewHMACCodeManager("secret"),
		fixedClock{now: time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)},
		Limits{DefaultTTL: time.Hour},
	)

	_, err := service.ClaimRelay(context.Background(), ClaimRelayInput{
		RequestUpdateID: 1,
		ClaimerUserID:   42,
		ClaimerChatID:   99,
		RawCode:         "relaybot_bad",
	})
	if !errors.Is(err, cacheErr) {
		t.Fatalf("ClaimRelay() error = %v, want %v", err, cacheErr)
	}
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type stubCache struct {
	allowUploadFunc  func(context.Context, int64) (bool, error)
	allowClaimFunc   func(context.Context, int64) (bool, error)
	allowBadCodeFunc func(context.Context, int64) (bool, error)
}

func (c stubCache) GetRelayIDByCodeHash(context.Context, string) (int64, bool, error) {
	return 0, false, nil
}

func (c stubCache) SetRelayIDByCodeHash(context.Context, string, int64, time.Duration) error {
	return nil
}

func (c stubCache) GetCreatedCodeBySourceUpdate(context.Context, int64) (string, bool, error) {
	return "", false, nil
}

func (c stubCache) SetCreatedCodeBySourceUpdate(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c stubCache) AllowUpload(ctx context.Context, userID int64) (bool, error) {
	if c.allowUploadFunc != nil {
		return c.allowUploadFunc(ctx, userID)
	}
	return true, nil
}

func (c stubCache) AllowClaim(ctx context.Context, userID int64) (bool, error) {
	if c.allowClaimFunc != nil {
		return c.allowClaimFunc(ctx, userID)
	}
	return true, nil
}

func (c stubCache) AllowBadCode(ctx context.Context, userID int64) (bool, error) {
	if c.allowBadCodeFunc != nil {
		return c.allowBadCodeFunc(ctx, userID)
	}
	return true, nil
}

func (c stubCache) MarkSeenUpdate(context.Context, int64) (bool, error) {
	return true, nil
}

func (c stubCache) Ping(context.Context) error {
	return nil
}

type stubStore struct{}

func (stubStore) CreateRelay(context.Context, CreateRelayParams) (Relay, bool, error) {
	return Relay{}, false, nil
}

func (stubStore) GetRelayBySourceUpdateID(context.Context, int64) (Relay, error) {
	return Relay{}, ErrRelayNotFound
}

func (stubStore) GetRelayByCodeHash(context.Context, string, time.Time) (Relay, error) {
	return Relay{}, ErrRelayNotFound
}

func (stubStore) GetRelayByID(context.Context, int64) (Relay, error) {
	return Relay{}, ErrRelayNotFound
}

func (stubStore) CountActiveRelaysByUploader(context.Context, int64, time.Time) (int64, error) {
	return 0, nil
}

func (stubStore) CreateDelivery(context.Context, CreateDeliveryParams) (Delivery, bool, error) {
	return Delivery{}, false, nil
}

func (stubStore) MarkDeliverySent(context.Context, MarkDeliverySentParams) error {
	return nil
}

func (stubStore) MarkDeliveryFailed(context.Context, MarkDeliveryFailedParams) error {
	return nil
}

func (stubStore) MarkDeliveryUnknown(context.Context, MarkDeliveryUnknownParams) error {
	return nil
}

func (stubStore) ExpireRelays(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (stubStore) MarkUnknownDeliveriesBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (stubStore) DeleteExpiredDeliveriesBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (stubStore) Ping(context.Context) error {
	return nil
}
