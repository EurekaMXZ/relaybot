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

func TestStartBatchUploadReturnsActiveSessionError(t *testing.T) {
	now := time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)
	service := NewService(
		stubStore{
			getRelayByIDFunc: func(context.Context, int64) (Relay, error) {
				return Relay{ID: 10, Status: RelayStatusCollecting}, nil
			},
		},
		stubCache{
			getBatchSessionFunc: func(context.Context, int64) (BatchUploadSession, bool, error) {
				return BatchUploadSession{RelayID: 10}, true, nil
			},
		},
		nil,
		NewHMACCodeManager("secret"),
		fixedClock{now: now},
		Limits{BatchSessionTTL: 30 * time.Minute},
	)

	_, err := service.StartBatchUpload(context.Background(), StartBatchUploadInput{
		UploaderUserID: 1,
		UploaderChatID: 2,
	})
	if !errors.Is(err, ErrBatchSessionActive) {
		t.Fatalf("StartBatchUpload() error = %v, want %v", err, ErrBatchSessionActive)
	}
}

func TestFinishBatchUploadReturnsEmptyError(t *testing.T) {
	now := time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)
	service := NewService(
		stubStore{
			getRelayByIDFunc: func(context.Context, int64) (Relay, error) {
				return Relay{ID: 10, Status: RelayStatusCollecting}, nil
			},
			listRelayItemsByRelayIDFunc: func(context.Context, int64) ([]RelayItem, error) {
				return nil, nil
			},
		},
		stubCache{
			getBatchSessionFunc: func(context.Context, int64) (BatchUploadSession, bool, error) {
				return BatchUploadSession{RelayID: 10}, true, nil
			},
		},
		nil,
		NewHMACCodeManager("secret"),
		fixedClock{now: now},
		Limits{BatchSessionTTL: 30 * time.Minute},
	)

	_, err := service.FinishBatchUpload(context.Background(), FinishBatchUploadInput{
		UploaderUserID: 1,
		UploaderChatID: 2,
	})
	if !errors.Is(err, ErrBatchSessionEmpty) {
		t.Fatalf("FinishBatchUpload() error = %v, want %v", err, ErrBatchSessionEmpty)
	}
}

func TestAppendBatchItemUsesPersistedBatchSizeForSessionCount(t *testing.T) {
	now := time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)
	var savedSession BatchUploadSession

	service := NewService(
		stubStore{
			getRelayByIDFunc: func(context.Context, int64) (Relay, error) {
				return Relay{ID: 10, Status: RelayStatusCollecting}, nil
			},
			addRelayItemFunc: func(context.Context, AddRelayItemParams) (RelayItem, bool, error) {
				return RelayItem{ID: 100, RelayID: 10, ItemOrder: 3}, true, nil
			},
			listRelayItemsByRelayIDFunc: func(context.Context, int64) ([]RelayItem, error) {
				return []RelayItem{
					{ID: 1, RelayID: 10, ItemOrder: 1},
					{ID: 2, RelayID: 10, ItemOrder: 2},
					{ID: 100, RelayID: 10, ItemOrder: 3},
				}, nil
			},
		},
		stubCache{
			getBatchSessionFunc: func(context.Context, int64) (BatchUploadSession, bool, error) {
				return BatchUploadSession{RelayID: 10, ItemCount: 1}, true, nil
			},
			mergeBatchSessionFunc: func(_ context.Context, session BatchUploadSession, _ time.Duration) (BatchUploadSession, error) {
				savedSession = session
				return session, nil
			},
		},
		nil,
		NewHMACCodeManager("secret"),
		fixedClock{now: now},
		Limits{MaxFileBytes: 1024, BatchSessionTTL: 30 * time.Minute},
	)

	result, err := service.AppendBatchItem(context.Background(), AppendBatchItemInput{
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
	if err != nil {
		t.Fatalf("AppendBatchItem() error = %v", err)
	}
	if result.ItemCount != 3 {
		t.Fatalf("AppendBatchItem() item count = %d, want 3", result.ItemCount)
	}
	if savedSession.ItemCount != 3 {
		t.Fatalf("saved batch session item count = %d, want 3", savedSession.ItemCount)
	}
}

func TestAppendBatchItemReturnsBatchSessionNotFoundWhenBatchStopsCollecting(t *testing.T) {
	now := time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)
	service := NewService(
		stubStore{
			getRelayByIDFunc: func(context.Context, int64) (Relay, error) {
				return Relay{ID: 10, Status: RelayStatusCollecting}, nil
			},
			addRelayItemFunc: func(context.Context, AddRelayItemParams) (RelayItem, bool, error) {
				return RelayItem{}, false, ErrBatchNotCollecting
			},
		},
		stubCache{
			getBatchSessionFunc: func(context.Context, int64) (BatchUploadSession, bool, error) {
				return BatchUploadSession{RelayID: 10}, true, nil
			},
		},
		nil,
		NewHMACCodeManager("secret"),
		fixedClock{now: now},
		Limits{MaxFileBytes: 1024, BatchSessionTTL: 30 * time.Minute},
	)

	_, err := service.AppendBatchItem(context.Background(), AppendBatchItemInput{
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
	if !errors.Is(err, ErrBatchSessionNotFound) {
		t.Fatalf("AppendBatchItem() error = %v, want %v", err, ErrBatchSessionNotFound)
	}
}

func TestAppendBatchItemReturnsBatchItemLimit(t *testing.T) {
	now := time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)
	service := NewService(
		stubStore{
			getRelayByIDFunc: func(context.Context, int64) (Relay, error) {
				return Relay{ID: 10, Status: RelayStatusCollecting}, nil
			},
			addRelayItemFunc: func(context.Context, AddRelayItemParams) (RelayItem, bool, error) {
				return RelayItem{}, false, ErrBatchItemLimit
			},
		},
		stubCache{
			getBatchSessionFunc: func(context.Context, int64) (BatchUploadSession, bool, error) {
				return BatchUploadSession{RelayID: 10, ItemCount: 2}, true, nil
			},
		},
		nil,
		NewHMACCodeManager("secret"),
		fixedClock{now: now},
		Limits{MaxFileBytes: 1024, MaxBatchItems: 2, BatchSessionTTL: 30 * time.Minute},
	)

	_, err := service.AppendBatchItem(context.Background(), AppendBatchItemInput{
		SourceUpdateID:       3,
		UploaderUserID:       42,
		UploaderChatID:       99,
		SourceMessageID:      9,
		MediaKind:            MediaKindDocument,
		TelegramFileID:       "file-id",
		TelegramFileUniqueID: "file-unique-id",
		FileName:             "demo.txt",
		MIMEType:             "text/plain",
		FileSizeBytes:        100,
	})
	if !errors.Is(err, ErrBatchItemLimit) {
		t.Fatalf("AppendBatchItem() error = %v, want %v", err, ErrBatchItemLimit)
	}
}

func TestFinishBatchUploadReturnsStoredCodeWhenFinalizeIsDeduplicated(t *testing.T) {
	now := time.Now().UTC()
	var warmedCodeHash string

	service := NewService(
		stubStore{
			getRelayByIDFunc: func(context.Context, int64) (Relay, error) {
				return Relay{ID: 10, Status: RelayStatusCollecting}, nil
			},
			listRelayItemsByRelayIDFunc: func(context.Context, int64) ([]RelayItem, error) {
				return []RelayItem{{ID: 100, RelayID: 10, ItemOrder: 1}}, nil
			},
			countActiveRelaysByUploaderFunc: func(context.Context, int64, time.Time) (int64, error) {
				return 0, nil
			},
			finalizeRelayBatchFunc: func(_ context.Context, params FinalizeRelayBatchParams) (Relay, error) {
				if params.CodeValue != "relaybot_candidate" {
					t.Fatalf("FinalizeRelayBatch() code value = %q, want relaybot_candidate", params.CodeValue)
				}
				return Relay{
					ID:        10,
					Status:    RelayStatusReady,
					CodeValue: "relaybot_existing",
					CodeHash:  "existing-hash",
					CodeHint:  "ABCD",
					ExpiresAt: now.Add(2 * time.Hour),
				}, nil
			},
		},
		stubCache{
			getBatchSessionFunc: func(context.Context, int64) (BatchUploadSession, bool, error) {
				return BatchUploadSession{RelayID: 10}, true, nil
			},
			setRelayIDByCodeHashFunc: func(_ context.Context, codeHash string, relayID int64, ttl time.Duration) error {
				if relayID != 10 {
					t.Fatalf("SetRelayIDByCodeHash() relayID = %d, want 10", relayID)
				}
				if ttl <= 0 {
					t.Fatalf("SetRelayIDByCodeHash() ttl = %v, want > 0", ttl)
				}
				warmedCodeHash = codeHash
				return nil
			},
		},
		nil,
		stubCodeManager{
			generateFunc: func() (string, string, string, error) {
				return "relaybot_candidate", "candidate-hash", "WXYZ", nil
			},
		},
		fixedClock{now: now},
		Limits{DefaultTTL: time.Hour, MaxActiveRelays: 1, BatchSessionTTL: 30 * time.Minute},
	)

	result, err := service.FinishBatchUpload(context.Background(), FinishBatchUploadInput{
		UploaderUserID: 1,
		UploaderChatID: 2,
	})
	if err != nil {
		t.Fatalf("FinishBatchUpload() error = %v", err)
	}
	if result.Code != "relaybot_existing" {
		t.Fatalf("FinishBatchUpload() code = %q, want relaybot_existing", result.Code)
	}
	if result.Relay.CodeHash != "existing-hash" {
		t.Fatalf("FinishBatchUpload() code hash = %q, want existing-hash", result.Relay.CodeHash)
	}
	if warmedCodeHash != "existing-hash" {
		t.Fatalf("SetRelayIDByCodeHash() code hash = %q, want existing-hash", warmedCodeHash)
	}
}

func TestFinishBatchUploadReturnsStoredCodeWhenBatchAlreadyReady(t *testing.T) {
	now := time.Now().UTC()
	var (
		warmedCodeHash string
		deletedSession bool
	)

	service := NewService(
		stubStore{
			getRelayByIDFunc: func(context.Context, int64) (Relay, error) {
				return Relay{
					ID:        10,
					Status:    RelayStatusReady,
					CodeValue: "relaybot_ready_code",
					CodeHash:  "ready-hash",
					CodeHint:  "READY",
					ExpiresAt: now.Add(2 * time.Hour),
				}, nil
			},
			listRelayItemsByRelayIDFunc: func(context.Context, int64) ([]RelayItem, error) {
				return []RelayItem{
					{ID: 1, RelayID: 10, ItemOrder: 1},
					{ID: 2, RelayID: 10, ItemOrder: 2},
				}, nil
			},
			countActiveRelaysByUploaderFunc: func(context.Context, int64, time.Time) (int64, error) {
				t.Fatal("CountActiveRelaysByUploader() should not be called for already-ready batch")
				return 0, nil
			},
		},
		stubCache{
			getBatchSessionFunc: func(context.Context, int64) (BatchUploadSession, bool, error) {
				return BatchUploadSession{RelayID: 10}, true, nil
			},
			setRelayIDByCodeHashFunc: func(_ context.Context, codeHash string, relayID int64, ttl time.Duration) error {
				warmedCodeHash = codeHash
				if relayID != 10 {
					t.Fatalf("SetRelayIDByCodeHash() relayID = %d, want 10", relayID)
				}
				if ttl <= 0 {
					t.Fatalf("SetRelayIDByCodeHash() ttl = %v, want > 0", ttl)
				}
				return nil
			},
			deleteBatchSessionFunc: func(context.Context, int64) error {
				deletedSession = true
				return nil
			},
		},
		nil,
		stubCodeManager{
			generateFunc: func() (string, string, string, error) {
				t.Fatal("Generate() should not be called for already-ready batch")
				return "", "", "", nil
			},
		},
		fixedClock{now: now},
		Limits{DefaultTTL: time.Hour, MaxActiveRelays: 1, BatchSessionTTL: 30 * time.Minute},
	)

	result, err := service.FinishBatchUpload(context.Background(), FinishBatchUploadInput{
		UploaderUserID: 1,
		UploaderChatID: 2,
	})
	if err != nil {
		t.Fatalf("FinishBatchUpload() error = %v", err)
	}
	if result.Code != "relaybot_ready_code" {
		t.Fatalf("FinishBatchUpload() code = %q, want relaybot_ready_code", result.Code)
	}
	if result.ItemCount != 2 {
		t.Fatalf("FinishBatchUpload() item count = %d, want 2", result.ItemCount)
	}
	if warmedCodeHash != "ready-hash" {
		t.Fatalf("SetRelayIDByCodeHash() code hash = %q, want ready-hash", warmedCodeHash)
	}
	if !deletedSession {
		t.Fatal("expected batch session to be deleted after returning ready batch")
	}
}

func TestFinishBatchUploadReturnsStoredCodeWhenBatchIsAlreadyReadyInStore(t *testing.T) {
	now := time.Now().UTC()
	var (
		deletedSession bool
		warmedCodeHash string
	)

	service := NewService(
		stubStore{
			getRelayByIDFunc: func(context.Context, int64) (Relay, error) {
				return Relay{
					ID:        10,
					Status:    RelayStatusReady,
					CodeValue: "relaybot_ready_in_store",
					CodeHash:  "ready-in-store-hash",
					CodeHint:  "READY",
					ExpiresAt: now.Add(2 * time.Hour),
				}, nil
			},
			listRelayItemsByRelayIDFunc: func(context.Context, int64) ([]RelayItem, error) {
				return []RelayItem{
					{ID: 100, RelayID: 10, ItemOrder: 1},
					{ID: 101, RelayID: 10, ItemOrder: 2},
				}, nil
			},
			countActiveRelaysByUploaderFunc: func(context.Context, int64, time.Time) (int64, error) {
				t.Fatal("CountActiveRelaysByUploader() should not be called when batch is already ready")
				return 0, nil
			},
		},
		stubCache{
			getBatchSessionFunc: func(context.Context, int64) (BatchUploadSession, bool, error) {
				return BatchUploadSession{RelayID: 10}, true, nil
			},
			deleteBatchSessionFunc: func(context.Context, int64) error {
				deletedSession = true
				return nil
			},
			setRelayIDByCodeHashFunc: func(_ context.Context, codeHash string, relayID int64, ttl time.Duration) error {
				if relayID != 10 {
					t.Fatalf("SetRelayIDByCodeHash() relayID = %d, want 10", relayID)
				}
				if ttl <= 0 {
					t.Fatalf("SetRelayIDByCodeHash() ttl = %v, want > 0", ttl)
				}
				warmedCodeHash = codeHash
				return nil
			},
		},
		nil,
		stubCodeManager{
			generateFunc: func() (string, string, string, error) {
				t.Fatal("Generate() should not be called when batch is already ready")
				return "", "", "", nil
			},
		},
		fixedClock{now: now},
		Limits{DefaultTTL: time.Hour, MaxActiveRelays: 1, BatchSessionTTL: 30 * time.Minute},
	)

	result, err := service.FinishBatchUpload(context.Background(), FinishBatchUploadInput{
		UploaderUserID: 1,
		UploaderChatID: 2,
	})
	if err != nil {
		t.Fatalf("FinishBatchUpload() error = %v", err)
	}
	if result.Code != "relaybot_ready_in_store" {
		t.Fatalf("FinishBatchUpload() code = %q, want relaybot_ready_in_store", result.Code)
	}
	if result.ItemCount != 2 {
		t.Fatalf("FinishBatchUpload() item count = %d, want 2", result.ItemCount)
	}
	if warmedCodeHash != "ready-in-store-hash" {
		t.Fatalf("SetRelayIDByCodeHash() code hash = %q, want ready-in-store-hash", warmedCodeHash)
	}
	if !deletedSession {
		t.Fatal("expected batch session to be deleted after ready batch dedup")
	}
}

func TestFinishBatchUploadReturnsStoredCodeWhenLimitWasReachedByConcurrentFinish(t *testing.T) {
	now := time.Now().UTC()
	getRelayByIDCalls := 0
	var warmedCodeHash string

	service := NewService(
		stubStore{
			getRelayByIDFunc: func(context.Context, int64) (Relay, error) {
				getRelayByIDCalls++
				if getRelayByIDCalls == 1 {
					return Relay{ID: 10, Status: RelayStatusCollecting}, nil
				}
				return Relay{
					ID:        10,
					Status:    RelayStatusReady,
					CodeValue: "relaybot_existing_after_limit",
					CodeHash:  "existing-hash-after-limit",
					CodeHint:  "EFGH",
					ExpiresAt: now.Add(2 * time.Hour),
				}, nil
			},
			listRelayItemsByRelayIDFunc: func(context.Context, int64) ([]RelayItem, error) {
				return []RelayItem{{ID: 100, RelayID: 10, ItemOrder: 1}}, nil
			},
			countActiveRelaysByUploaderFunc: func(context.Context, int64, time.Time) (int64, error) {
				return 1, nil
			},
		},
		stubCache{
			getBatchSessionFunc: func(context.Context, int64) (BatchUploadSession, bool, error) {
				return BatchUploadSession{RelayID: 10}, true, nil
			},
			setRelayIDByCodeHashFunc: func(_ context.Context, codeHash string, relayID int64, ttl time.Duration) error {
				if relayID != 10 {
					t.Fatalf("SetRelayIDByCodeHash() relayID = %d, want 10", relayID)
				}
				if ttl <= 0 {
					t.Fatalf("SetRelayIDByCodeHash() ttl = %v, want > 0", ttl)
				}
				warmedCodeHash = codeHash
				return nil
			},
		},
		nil,
		stubCodeManager{
			generateFunc: func() (string, string, string, error) {
				t.Fatal("Generate() should not be called when finish is deduplicated after limit recheck")
				return "", "", "", nil
			},
		},
		fixedClock{now: now},
		Limits{DefaultTTL: time.Hour, MaxActiveRelays: 1, BatchSessionTTL: 30 * time.Minute},
	)

	result, err := service.FinishBatchUpload(context.Background(), FinishBatchUploadInput{
		UploaderUserID: 1,
		UploaderChatID: 2,
	})
	if err != nil {
		t.Fatalf("FinishBatchUpload() error = %v", err)
	}
	if result.Code != "relaybot_existing_after_limit" {
		t.Fatalf("FinishBatchUpload() code = %q, want relaybot_existing_after_limit", result.Code)
	}
	if result.Relay.CodeHash != "existing-hash-after-limit" {
		t.Fatalf("FinishBatchUpload() code hash = %q, want existing-hash-after-limit", result.Relay.CodeHash)
	}
	if warmedCodeHash != "existing-hash-after-limit" {
		t.Fatalf("SetRelayIDByCodeHash() code hash = %q, want existing-hash-after-limit", warmedCodeHash)
	}
}

func TestFinishBatchUploadReturnsStoredCodeWhenBatchAlreadyReadyBeforeSessionCleanup(t *testing.T) {
	now := time.Now().UTC()
	var (
		warmedCodeHash string
		deletedSession bool
	)

	service := NewService(
		stubStore{
			getRelayByIDFunc: func(context.Context, int64) (Relay, error) {
				return Relay{
					ID:        10,
					Status:    RelayStatusReady,
					CodeValue: "relaybot_ready_from_cache_window",
					CodeHash:  "ready-cache-window-hash",
					CodeHint:  "IJKL",
					ExpiresAt: now.Add(2 * time.Hour),
				}, nil
			},
			listRelayItemsByRelayIDFunc: func(context.Context, int64) ([]RelayItem, error) {
				return []RelayItem{
					{ID: 100, RelayID: 10, ItemOrder: 1},
					{ID: 101, RelayID: 10, ItemOrder: 2},
				}, nil
			},
		},
		stubCache{
			getBatchSessionFunc: func(context.Context, int64) (BatchUploadSession, bool, error) {
				return BatchUploadSession{RelayID: 10}, true, nil
			},
			setRelayIDByCodeHashFunc: func(_ context.Context, codeHash string, relayID int64, ttl time.Duration) error {
				if relayID != 10 {
					t.Fatalf("SetRelayIDByCodeHash() relayID = %d, want 10", relayID)
				}
				if ttl <= 0 {
					t.Fatalf("SetRelayIDByCodeHash() ttl = %v, want > 0", ttl)
				}
				warmedCodeHash = codeHash
				return nil
			},
			deleteBatchSessionFunc: func(context.Context, int64) error {
				deletedSession = true
				return nil
			},
		},
		nil,
		stubCodeManager{
			generateFunc: func() (string, string, string, error) {
				t.Fatal("Generate() should not be called when batch is already ready")
				return "", "", "", nil
			},
		},
		fixedClock{now: now},
		Limits{DefaultTTL: time.Hour, MaxActiveRelays: 1, BatchSessionTTL: 30 * time.Minute},
	)

	result, err := service.FinishBatchUpload(context.Background(), FinishBatchUploadInput{
		UploaderUserID: 1,
		UploaderChatID: 2,
	})
	if err != nil {
		t.Fatalf("FinishBatchUpload() error = %v", err)
	}
	if result.Code != "relaybot_ready_from_cache_window" {
		t.Fatalf("FinishBatchUpload() code = %q, want relaybot_ready_from_cache_window", result.Code)
	}
	if result.ItemCount != 2 {
		t.Fatalf("FinishBatchUpload() item count = %d, want 2", result.ItemCount)
	}
	if warmedCodeHash != "ready-cache-window-hash" {
		t.Fatalf("SetRelayIDByCodeHash() code hash = %q, want ready-cache-window-hash", warmedCodeHash)
	}
	if !deletedSession {
		t.Fatal("expected batch session to be deleted after returning ready batch")
	}
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type stubCache struct {
	allowUploadFunc          func(context.Context, int64) (bool, error)
	allowClaimFunc           func(context.Context, int64) (bool, error)
	allowBadCodeFunc         func(context.Context, int64) (bool, error)
	setRelayIDByCodeHashFunc func(context.Context, string, int64, time.Duration) error
	getBatchSessionFunc      func(context.Context, int64) (BatchUploadSession, bool, error)
	setBatchSessionFunc      func(context.Context, BatchUploadSession, time.Duration) error
	mergeBatchSessionFunc    func(context.Context, BatchUploadSession, time.Duration) (BatchUploadSession, error)
	deleteBatchSessionFunc   func(context.Context, int64) error
}

func (c stubCache) GetRelayIDByCodeHash(context.Context, string) (int64, bool, error) {
	return 0, false, nil
}

func (c stubCache) SetRelayIDByCodeHash(ctx context.Context, codeHash string, relayID int64, ttl time.Duration) error {
	if c.setRelayIDByCodeHashFunc != nil {
		return c.setRelayIDByCodeHashFunc(ctx, codeHash, relayID, ttl)
	}
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

func (c stubCache) GetBatchUploadSession(ctx context.Context, chatID int64) (BatchUploadSession, bool, error) {
	if c.getBatchSessionFunc != nil {
		return c.getBatchSessionFunc(ctx, chatID)
	}
	return BatchUploadSession{}, false, nil
}

func (c stubCache) SetBatchUploadSession(ctx context.Context, session BatchUploadSession, ttl time.Duration) error {
	if c.setBatchSessionFunc != nil {
		return c.setBatchSessionFunc(ctx, session, ttl)
	}
	return nil
}

func (c stubCache) MergeBatchUploadSession(ctx context.Context, session BatchUploadSession, ttl time.Duration) (BatchUploadSession, error) {
	if c.mergeBatchSessionFunc != nil {
		return c.mergeBatchSessionFunc(ctx, session, ttl)
	}
	return session, nil
}

func (c stubCache) DeleteBatchUploadSession(ctx context.Context, chatID int64) error {
	if c.deleteBatchSessionFunc != nil {
		return c.deleteBatchSessionFunc(ctx, chatID)
	}
	return nil
}

func (c stubCache) Ping(context.Context) error {
	return nil
}

type stubStore struct {
	createRelayBatchFunc            func(context.Context, CreateRelayBatchParams) (Relay, error)
	addRelayItemFunc                func(context.Context, AddRelayItemParams) (RelayItem, bool, error)
	finalizeRelayBatchFunc          func(context.Context, FinalizeRelayBatchParams) (Relay, error)
	getRelayByIDFunc                func(context.Context, int64) (Relay, error)
	listRelayItemsByRelayIDFunc     func(context.Context, int64) ([]RelayItem, error)
	countActiveRelaysByUploaderFunc func(context.Context, int64, time.Time) (int64, error)
}

func (stubStore) CreateRelay(context.Context, CreateRelayParams) (Relay, bool, error) {
	return Relay{}, false, nil
}

func (s stubStore) CreateRelayBatch(ctx context.Context, params CreateRelayBatchParams) (Relay, error) {
	if s.createRelayBatchFunc != nil {
		return s.createRelayBatchFunc(ctx, params)
	}
	return Relay{}, nil
}

func (s stubStore) AddRelayItem(ctx context.Context, params AddRelayItemParams) (RelayItem, bool, error) {
	if s.addRelayItemFunc != nil {
		return s.addRelayItemFunc(ctx, params)
	}
	return RelayItem{}, false, nil
}

func (s stubStore) ListRelayItemsByRelayID(ctx context.Context, relayID int64) ([]RelayItem, error) {
	if s.listRelayItemsByRelayIDFunc != nil {
		return s.listRelayItemsByRelayIDFunc(ctx, relayID)
	}
	return nil, nil
}

func (s stubStore) FinalizeRelayBatch(ctx context.Context, params FinalizeRelayBatchParams) (Relay, error) {
	if s.finalizeRelayBatchFunc != nil {
		return s.finalizeRelayBatchFunc(ctx, params)
	}
	return Relay{}, nil
}

func (stubStore) DeleteRelay(context.Context, int64) error {
	return nil
}

func (stubStore) GetRelayBySourceUpdateID(context.Context, int64) (Relay, error) {
	return Relay{}, ErrRelayNotFound
}

func (stubStore) GetRelayByCodeHash(context.Context, string, time.Time) (Relay, error) {
	return Relay{}, ErrRelayNotFound
}

func (s stubStore) GetRelayByID(ctx context.Context, relayID int64) (Relay, error) {
	if s.getRelayByIDFunc != nil {
		return s.getRelayByIDFunc(ctx, relayID)
	}
	return Relay{}, ErrRelayNotFound
}

func (s stubStore) CountActiveRelaysByUploader(ctx context.Context, uploaderUserID int64, now time.Time) (int64, error) {
	if s.countActiveRelaysByUploaderFunc != nil {
		return s.countActiveRelaysByUploaderFunc(ctx, uploaderUserID, now)
	}
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

func (stubStore) DeleteCollectingRelaysBefore(context.Context, time.Time) (int64, error) {
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

type stubCodeManager struct {
	generateFunc  func() (string, string, string, error)
	normalizeFunc func(string) (string, error)
	hashFunc      func(string) string
}

func (m stubCodeManager) Generate() (string, string, string, error) {
	if m.generateFunc != nil {
		return m.generateFunc()
	}
	return "", "", "", nil
}

func (m stubCodeManager) Normalize(raw string) (string, error) {
	if m.normalizeFunc != nil {
		return m.normalizeFunc(raw)
	}
	return raw, nil
}

func (m stubCodeManager) Hash(normalized string) string {
	if m.hashFunc != nil {
		return m.hashFunc(normalized)
	}
	return normalized
}
