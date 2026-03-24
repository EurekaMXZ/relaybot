package postgres

import (
	"context"
	"os"
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
	record, created, err := store.CreateRelay(ctx, relay.CreateRelayParams{
		SourceUpdateID:       now.UnixNano(),
		CodeValue:            "relaybot_TEST-TEST-TEST-TEST",
		CodeHash:             "test-hash",
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
}
