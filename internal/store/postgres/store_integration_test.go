package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"relaybot/internal/relay"
)

func TestStoreCreateRelayRoundTrip(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	store := New(pool)
	now := time.Now().UTC()
	codeSuffix := now.Format("20060102150405.000000000")
	record, created, err := store.CreateRelay(ctx, relay.CreateRelayParams{
		SourceUpdateID:       now.UnixNano(),
		CodeValue:            fmt.Sprintf("relaybot_TEST-%s", codeSuffix),
		CodeHash:             fmt.Sprintf("test-hash-%s", codeSuffix),
		CodeHint:             "TEST",
		UploaderUserID:       1001,
		UploaderChatID:       2002,
		SourceMessageID:      11,
		MediaKind:            relay.MediaKindDocument,
		TelegramFileID:       "file-id",
		TelegramFileUniqueID: "file-unique-id",
		FileName:             "sample.pdf",
		MIMEType:             "application/pdf",
		FileSizeBytes:        1024,
		Caption:              "hello",
		ExpiresAt:            now.Add(time.Hour),
		CreatedAt:            now,
	})
	if err != nil {
		t.Fatalf("CreateRelay() error = %v", err)
	}
	if !created {
		t.Fatal("expected relay to be created")
	}

	got, err := store.GetRelayByCodeHash(ctx, record.CodeHash, now)
	if err != nil {
		t.Fatalf("GetRelayByCodeHash() error = %v", err)
	}
	if got.ID != record.ID {
		t.Fatalf("unexpected relay id: got %d want %d", got.ID, record.ID)
	}

	items, err := store.ListRelayItemsByRelayID(ctx, record.ID)
	if err != nil {
		t.Fatalf("ListRelayItemsByRelayID() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 relay item, got %d", len(items))
	}
	if items[0].SourceUpdateID != record.SourceUpdateID {
		t.Fatalf("unexpected relay item source update id: got %d want %d", items[0].SourceUpdateID, record.SourceUpdateID)
	}
}

func TestStoreBatchRelayRoundTrip(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	store := New(pool)
	now := time.Now().UTC()
	codeSuffix := now.Format("20060102150405.000000000")

	batch, err := store.CreateRelayBatch(ctx, relay.CreateRelayBatchParams{
		UploaderUserID: 3003,
		UploaderChatID: 4004,
		CreatedAt:      now,
	})
	if err != nil {
		t.Fatalf("CreateRelayBatch() error = %v", err)
	}
	if batch.Status != relay.RelayStatusCollecting {
		t.Fatalf("expected collecting batch, got %q", batch.Status)
	}

	item, created, err := store.AddRelayItem(ctx, relay.AddRelayItemParams{
		RelayID:              batch.ID,
		SourceUpdateID:       now.UnixNano(),
		SourceMessageID:      21,
		MediaGroupID:         "group-1",
		ItemOrder:            1,
		MediaKind:            relay.MediaKindPhoto,
		TelegramFileID:       "file-id-1",
		TelegramFileUniqueID: "unique-id-1",
		FileName:             "photo-1.jpg",
		MIMEType:             "image/jpeg",
		FileSizeBytes:        2048,
		Caption:              "first",
		CreatedAt:            now,
	})
	if err != nil {
		t.Fatalf("AddRelayItem() error = %v", err)
	}
	if !created {
		t.Fatal("expected relay item to be created")
	}

	_, created, err = store.AddRelayItem(ctx, relay.AddRelayItemParams{
		RelayID:              batch.ID,
		SourceUpdateID:       now.UnixNano() + 1,
		SourceMessageID:      22,
		MediaGroupID:         "group-1",
		ItemOrder:            2,
		MediaKind:            relay.MediaKindVideo,
		TelegramFileID:       "file-id-2",
		TelegramFileUniqueID: "unique-id-2",
		FileName:             "video-1.mp4",
		MIMEType:             "video/mp4",
		FileSizeBytes:        4096,
		Caption:              "second",
		CreatedAt:            now,
	})
	if err != nil {
		t.Fatalf("AddRelayItem() second call error = %v", err)
	}
	if !created {
		t.Fatal("expected second relay item to be created")
	}

	ready, err := store.FinalizeRelayBatch(ctx, relay.FinalizeRelayBatchParams{
		RelayID:   batch.ID,
		CodeValue: fmt.Sprintf("relaybot_BATCH-%s", codeSuffix),
		CodeHash:  fmt.Sprintf("batch-test-hash-%s", codeSuffix),
		CodeHint:  "TEST",
		ExpiresAt: now.Add(time.Hour),
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("FinalizeRelayBatch() error = %v", err)
	}
	if ready.Status != relay.RelayStatusReady {
		t.Fatalf("expected ready batch, got %q", ready.Status)
	}

	items, err := store.ListRelayItemsByRelayID(ctx, batch.ID)
	if err != nil {
		t.Fatalf("ListRelayItemsByRelayID() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 relay items, got %d", len(items))
	}
	if items[0].ID != item.ID {
		t.Fatalf("expected first item id %d, got %d", item.ID, items[0].ID)
	}
}

func TestStoreAddRelayItemAssignsSequentialItemOrder(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	store := New(pool)
	now := time.Now().UTC()

	batch, err := store.CreateRelayBatch(ctx, relay.CreateRelayBatchParams{
		UploaderUserID: 5005,
		UploaderChatID: 6006,
		CreatedAt:      now,
	})
	if err != nil {
		t.Fatalf("CreateRelayBatch() error = %v", err)
	}

	first, created, err := store.AddRelayItem(ctx, relay.AddRelayItemParams{
		RelayID:              batch.ID,
		SourceUpdateID:       now.UnixNano(),
		SourceMessageID:      31,
		MediaKind:            relay.MediaKindDocument,
		TelegramFileID:       "file-id-3",
		TelegramFileUniqueID: "unique-id-3",
		FileName:             "doc-1.txt",
		MIMEType:             "text/plain",
		FileSizeBytes:        512,
		CreatedAt:            now,
	})
	if err != nil {
		t.Fatalf("AddRelayItem() first call error = %v", err)
	}
	if !created {
		t.Fatal("expected first relay item to be created")
	}
	if first.ItemOrder != 1 {
		t.Fatalf("first item order = %d, want 1", first.ItemOrder)
	}

	second, created, err := store.AddRelayItem(ctx, relay.AddRelayItemParams{
		RelayID:              batch.ID,
		SourceUpdateID:       now.UnixNano() + 1,
		SourceMessageID:      32,
		MediaKind:            relay.MediaKindDocument,
		TelegramFileID:       "file-id-4",
		TelegramFileUniqueID: "unique-id-4",
		FileName:             "doc-2.txt",
		MIMEType:             "text/plain",
		FileSizeBytes:        1024,
		CreatedAt:            now,
	})
	if err != nil {
		t.Fatalf("AddRelayItem() second call error = %v", err)
	}
	if !created {
		t.Fatal("expected second relay item to be created")
	}
	if second.ItemOrder != 2 {
		t.Fatalf("second item order = %d, want 2", second.ItemOrder)
	}
}

func TestStoreAddRelayItemRespectsBatchItemLimit(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	store := New(pool)
	now := time.Now().UTC()

	batch, err := store.CreateRelayBatch(ctx, relay.CreateRelayBatchParams{
		UploaderUserID: 5100,
		UploaderChatID: 6100,
		CreatedAt:      now,
	})
	if err != nil {
		t.Fatalf("CreateRelayBatch() error = %v", err)
	}

	first, created, err := store.AddRelayItem(ctx, relay.AddRelayItemParams{
		RelayID:              batch.ID,
		SourceUpdateID:       now.UnixNano(),
		SourceMessageID:      51,
		MaxBatchItems:        2,
		MediaKind:            relay.MediaKindDocument,
		TelegramFileID:       "file-id-limit-1",
		TelegramFileUniqueID: "unique-id-limit-1",
		FileName:             "doc-1.txt",
		MIMEType:             "text/plain",
		FileSizeBytes:        256,
		CreatedAt:            now,
	})
	if err != nil {
		t.Fatalf("AddRelayItem() first call error = %v", err)
	}
	if !created {
		t.Fatal("expected first relay item to be created")
	}

	_, created, err = store.AddRelayItem(ctx, relay.AddRelayItemParams{
		RelayID:              batch.ID,
		SourceUpdateID:       now.UnixNano() + 1,
		SourceMessageID:      52,
		MaxBatchItems:        2,
		MediaKind:            relay.MediaKindDocument,
		TelegramFileID:       "file-id-limit-2",
		TelegramFileUniqueID: "unique-id-limit-2",
		FileName:             "doc-2.txt",
		MIMEType:             "text/plain",
		FileSizeBytes:        512,
		CreatedAt:            now,
	})
	if err != nil {
		t.Fatalf("AddRelayItem() second call error = %v", err)
	}
	if !created {
		t.Fatal("expected second relay item to be created")
	}

	duplicate, created, err := store.AddRelayItem(ctx, relay.AddRelayItemParams{
		RelayID:              batch.ID,
		SourceUpdateID:       first.SourceUpdateID,
		SourceMessageID:      first.SourceMessageID,
		MaxBatchItems:        2,
		MediaKind:            relay.MediaKindDocument,
		TelegramFileID:       first.TelegramFileID,
		TelegramFileUniqueID: first.TelegramFileUniqueID,
		FileName:             first.FileName,
		MIMEType:             first.MIMEType,
		FileSizeBytes:        first.FileSizeBytes,
		CreatedAt:            now,
	})
	if err != nil {
		t.Fatalf("AddRelayItem() duplicate call error = %v", err)
	}
	if created {
		t.Fatal("expected duplicate relay item to reuse existing row")
	}
	if duplicate.ID != first.ID {
		t.Fatalf("expected duplicate item id %d, got %d", first.ID, duplicate.ID)
	}

	_, _, err = store.AddRelayItem(ctx, relay.AddRelayItemParams{
		RelayID:              batch.ID,
		SourceUpdateID:       now.UnixNano() + 2,
		SourceMessageID:      53,
		MaxBatchItems:        2,
		MediaKind:            relay.MediaKindDocument,
		TelegramFileID:       "file-id-limit-3",
		TelegramFileUniqueID: "unique-id-limit-3",
		FileName:             "doc-3.txt",
		MIMEType:             "text/plain",
		FileSizeBytes:        1024,
		CreatedAt:            now,
	})
	if !errors.Is(err, relay.ErrBatchItemLimit) {
		t.Fatalf("AddRelayItem() error = %v, want %v", err, relay.ErrBatchItemLimit)
	}
}

func TestStoreFinalizeRelayBatchIsIdempotent(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	store := New(pool)
	now := time.Now().UTC()
	codeSuffix := now.Format("20060102150405.000000000")

	batch, err := store.CreateRelayBatch(ctx, relay.CreateRelayBatchParams{
		UploaderUserID: 5005,
		UploaderChatID: 6006,
		CreatedAt:      now,
	})
	if err != nil {
		t.Fatalf("CreateRelayBatch() error = %v", err)
	}

	if _, created, err := store.AddRelayItem(ctx, relay.AddRelayItemParams{
		RelayID:              batch.ID,
		SourceUpdateID:       now.UnixNano(),
		SourceMessageID:      31,
		MediaKind:            relay.MediaKindDocument,
		TelegramFileID:       "file-id-batch",
		TelegramFileUniqueID: "file-unique-id-batch",
		FileName:             "sample.txt",
		MIMEType:             "text/plain",
		FileSizeBytes:        256,
		Caption:              "sample",
		CreatedAt:            now,
	}); err != nil {
		t.Fatalf("AddRelayItem() error = %v", err)
	} else if !created {
		t.Fatal("expected relay item to be created")
	}

	first, err := store.FinalizeRelayBatch(ctx, relay.FinalizeRelayBatchParams{
		RelayID:   batch.ID,
		CodeValue: fmt.Sprintf("relaybot_BATCH-FIRST-%s", codeSuffix),
		CodeHash:  fmt.Sprintf("batch-first-hash-%s", codeSuffix),
		CodeHint:  "0001",
		ExpiresAt: now.Add(time.Hour),
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("first FinalizeRelayBatch() error = %v", err)
	}

	second, err := store.FinalizeRelayBatch(ctx, relay.FinalizeRelayBatchParams{
		RelayID:   batch.ID,
		CodeValue: fmt.Sprintf("relaybot_BATCH-SECOND-%s", codeSuffix),
		CodeHash:  fmt.Sprintf("batch-second-hash-%s", codeSuffix),
		CodeHint:  "0002",
		ExpiresAt: now.Add(2 * time.Hour),
		UpdatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("second FinalizeRelayBatch() error = %v", err)
	}

	if second.CodeValue != first.CodeValue {
		t.Fatalf("expected second finalize to return original code value %q, got %q", first.CodeValue, second.CodeValue)
	}
	if second.CodeHash != first.CodeHash {
		t.Fatalf("expected second finalize to return original code hash %q, got %q", first.CodeHash, second.CodeHash)
	}
	if second.CodeHint != first.CodeHint {
		t.Fatalf("expected second finalize to return original code hint %q, got %q", first.CodeHint, second.CodeHint)
	}
	if !second.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("expected second finalize to keep original expires_at %v, got %v", first.ExpiresAt, second.ExpiresAt)
	}

	got, err := store.GetRelayByID(ctx, batch.ID)
	if err != nil {
		t.Fatalf("GetRelayByID() error = %v", err)
	}
	if got.CodeHash != first.CodeHash {
		t.Fatalf("expected persisted code hash %q, got %q", first.CodeHash, got.CodeHash)
	}
}

func TestStoreFinalizeRelayBatchIsConcurrencySafe(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	store := New(pool)
	now := time.Now().UTC()
	codeSuffix := now.Format("20060102150405.000000000")

	batch, err := store.CreateRelayBatch(ctx, relay.CreateRelayBatchParams{
		UploaderUserID: 6100,
		UploaderChatID: 6200,
		CreatedAt:      now,
	})
	if err != nil {
		t.Fatalf("CreateRelayBatch() error = %v", err)
	}

	if _, created, err := store.AddRelayItem(ctx, relay.AddRelayItemParams{
		RelayID:              batch.ID,
		SourceUpdateID:       now.UnixNano(),
		SourceMessageID:      41,
		MediaKind:            relay.MediaKindDocument,
		TelegramFileID:       "file-id-concurrent-finalize",
		TelegramFileUniqueID: "file-unique-id-concurrent-finalize",
		FileName:             "sample.txt",
		MIMEType:             "text/plain",
		FileSizeBytes:        128,
		CreatedAt:            now,
	}); err != nil {
		t.Fatalf("AddRelayItem() error = %v", err)
	} else if !created {
		t.Fatal("expected relay item to be created")
	}

	type finalizeResult struct {
		relay relay.Relay
		err   error
	}

	results := make(chan finalizeResult, 2)
	start := make(chan struct{})
	paramsList := []relay.FinalizeRelayBatchParams{
		{
			RelayID:   batch.ID,
			CodeValue: fmt.Sprintf("relaybot_BATCH-A-%s", codeSuffix),
			CodeHash:  fmt.Sprintf("batch-a-hash-%s", codeSuffix),
			CodeHint:  "AAAA",
			ExpiresAt: now.Add(time.Hour),
			UpdatedAt: now,
		},
		{
			RelayID:   batch.ID,
			CodeValue: fmt.Sprintf("relaybot_BATCH-B-%s", codeSuffix),
			CodeHash:  fmt.Sprintf("batch-b-hash-%s", codeSuffix),
			CodeHint:  "BBBB",
			ExpiresAt: now.Add(2 * time.Hour),
			UpdatedAt: now.Add(time.Second),
		},
	}

	for _, params := range paramsList {
		params := params
		go func() {
			<-start
			record, err := store.FinalizeRelayBatch(ctx, params)
			results <- finalizeResult{relay: record, err: err}
		}()
	}

	close(start)

	first := <-results
	second := <-results

	if first.err != nil {
		t.Fatalf("first concurrent FinalizeRelayBatch() error = %v", first.err)
	}
	if second.err != nil {
		t.Fatalf("second concurrent FinalizeRelayBatch() error = %v", second.err)
	}
	if first.relay.CodeHash != second.relay.CodeHash {
		t.Fatalf("expected concurrent finalize calls to converge to one code hash, got %q and %q", first.relay.CodeHash, second.relay.CodeHash)
	}
	if first.relay.CodeValue != second.relay.CodeValue {
		t.Fatalf("expected concurrent finalize calls to converge to one code value, got %q and %q", first.relay.CodeValue, second.relay.CodeValue)
	}

	got, err := store.GetRelayByID(ctx, batch.ID)
	if err != nil {
		t.Fatalf("GetRelayByID() error = %v", err)
	}
	if got.CodeHash != first.relay.CodeHash {
		t.Fatalf("expected persisted code hash %q, got %q", first.relay.CodeHash, got.CodeHash)
	}
}

func TestStoreAddRelayItemAssignsUniqueItemOrder(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	store := New(pool)
	now := time.Now().UTC()

	batch, err := store.CreateRelayBatch(ctx, relay.CreateRelayBatchParams{
		UploaderUserID: 7007,
		UploaderChatID: 8008,
		CreatedAt:      now,
	})
	if err != nil {
		t.Fatalf("CreateRelayBatch() error = %v", err)
	}

	const itemCount = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	errCh := make(chan error, itemCount)

	for i := 0; i < itemCount; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			_, created, err := store.AddRelayItem(ctx, relay.AddRelayItemParams{
				RelayID:              batch.ID,
				SourceUpdateID:       now.UnixNano() + int64(i) + 1,
				SourceMessageID:      40 + i,
				MediaGroupID:         "group-concurrent",
				MediaKind:            relay.MediaKindPhoto,
				TelegramFileID:       "file-id",
				TelegramFileUniqueID: "file-unique-id",
				FileName:             "photo.jpg",
				MIMEType:             "image/jpeg",
				FileSizeBytes:        1024 + int64(i),
				Caption:              "item",
				CreatedAt:            now.Add(time.Duration(i) * time.Millisecond),
			})
			if err != nil {
				errCh <- err
				return
			}
			if !created {
				errCh <- errors.New("expected concurrent AddRelayItem call to create a row")
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("AddRelayItem() concurrent call error = %v", err)
		}
	}

	items, err := store.ListRelayItemsByRelayID(ctx, batch.ID)
	if err != nil {
		t.Fatalf("ListRelayItemsByRelayID() error = %v", err)
	}
	if len(items) != itemCount {
		t.Fatalf("expected %d relay items, got %d", itemCount, len(items))
	}

	for i, item := range items {
		expectedOrder := i + 1
		if item.ItemOrder != expectedOrder {
			t.Fatalf("unexpected item order at index %d: got %d want %d", i, item.ItemOrder, expectedOrder)
		}
	}
}
