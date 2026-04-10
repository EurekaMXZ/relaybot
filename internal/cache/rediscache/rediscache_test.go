package rediscache

import (
	"testing"
	"time"

	"relaybot/internal/relay"
)

func TestMergeBatchUploadSessionKeepsProgressState(t *testing.T) {
	current := relay.BatchUploadSession{
		RelayID:                   10,
		UploaderUserID:            1,
		UploaderChatID:            2,
		ItemCount:                 6,
		ProgressMessageID:         100,
		LastProgressNotifiedAt:    time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC),
		LastProgressNotifiedCount: 6,
		StartedAt:                 time.Date(2026, 3, 24, 11, 0, 0, 0, time.UTC),
		LastActivityAt:            time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC),
	}
	next := relay.BatchUploadSession{
		RelayID:                   10,
		UploaderUserID:            1,
		UploaderChatID:            2,
		ItemCount:                 8,
		ProgressMessageID:         200,
		LastProgressNotifiedAt:    time.Date(2026, 3, 24, 12, 5, 0, 0, time.UTC),
		LastProgressNotifiedCount: 8,
		StartedAt:                 time.Date(2026, 3, 24, 11, 10, 0, 0, time.UTC),
		LastActivityAt:            time.Date(2026, 3, 24, 12, 6, 0, 0, time.UTC),
	}

	merged := mergeBatchUploadSession(current, next)
	if merged.ItemCount != 8 {
		t.Fatalf("merged item count = %d, want 8", merged.ItemCount)
	}
	if merged.ProgressMessageID != 200 {
		t.Fatalf("merged progress message id = %d, want 200", merged.ProgressMessageID)
	}
	if merged.LastProgressNotifiedCount != 8 {
		t.Fatalf("merged last progress count = %d, want 8", merged.LastProgressNotifiedCount)
	}
	if !merged.LastProgressNotifiedAt.Equal(next.LastProgressNotifiedAt) {
		t.Fatalf("merged last progress time = %v, want %v", merged.LastProgressNotifiedAt, next.LastProgressNotifiedAt)
	}
	if !merged.StartedAt.Equal(current.StartedAt) {
		t.Fatalf("merged started at = %v, want %v", merged.StartedAt, current.StartedAt)
	}
}

func TestMergeBatchUploadSessionDoesNotRollbackProgressState(t *testing.T) {
	current := relay.BatchUploadSession{
		RelayID:                   10,
		UploaderUserID:            1,
		UploaderChatID:            2,
		ItemCount:                 8,
		ProgressMessageID:         200,
		LastProgressNotifiedAt:    time.Date(2026, 3, 24, 12, 5, 0, 0, time.UTC),
		LastProgressNotifiedCount: 8,
		StartedAt:                 time.Date(2026, 3, 24, 11, 0, 0, 0, time.UTC),
		LastActivityAt:            time.Date(2026, 3, 24, 12, 6, 0, 0, time.UTC),
	}
	next := relay.BatchUploadSession{
		RelayID:                   10,
		UploaderUserID:            1,
		UploaderChatID:            2,
		ItemCount:                 7,
		ProgressMessageID:         0,
		LastProgressNotifiedAt:    time.Date(2026, 3, 24, 12, 4, 0, 0, time.UTC),
		LastProgressNotifiedCount: 7,
		StartedAt:                 time.Date(2026, 3, 24, 11, 10, 0, 0, time.UTC),
		LastActivityAt:            time.Date(2026, 3, 24, 12, 7, 0, 0, time.UTC),
	}

	merged := mergeBatchUploadSession(current, next)
	if merged.ProgressMessageID != 200 {
		t.Fatalf("merged progress message id = %d, want 200", merged.ProgressMessageID)
	}
	if merged.LastProgressNotifiedCount != 8 {
		t.Fatalf("merged last progress count = %d, want 8", merged.LastProgressNotifiedCount)
	}
	if !merged.LastProgressNotifiedAt.Equal(current.LastProgressNotifiedAt) {
		t.Fatalf("merged last progress time = %v, want %v", merged.LastProgressNotifiedAt, current.LastProgressNotifiedAt)
	}
	if merged.ItemCount != 8 {
		t.Fatalf("merged item count = %d, want 8", merged.ItemCount)
	}
}
