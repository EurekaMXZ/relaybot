package telegram

import (
	"testing"
	"time"

	"relaybot/internal/relay"
)

func TestShouldNotifyBatchProgress(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name     string
		session  relay.BatchUploadSession
		item     int
		expected bool
	}{
		{
			name: "first item notifies immediately",
			session: relay.BatchUploadSession{
				LastProgressNotifiedCount: 0,
			},
			item:     1,
			expected: true,
		},
		{
			name: "same count does not notify",
			session: relay.BatchUploadSession{
				LastProgressNotifiedCount: 5,
				LastProgressNotifiedAt:    now,
			},
			item:     5,
			expected: false,
		},
		{
			name: "step threshold notifies",
			session: relay.BatchUploadSession{
				LastProgressNotifiedCount: 9,
				LastProgressNotifiedAt:    now,
			},
			item:     10,
			expected: true,
		},
		{
			name: "interval threshold notifies",
			session: relay.BatchUploadSession{
				LastProgressNotifiedCount: 2,
				LastProgressNotifiedAt:    now.Add(-3 * time.Second),
			},
			item:     3,
			expected: true,
		},
		{
			name: "interval not reached skips update",
			session: relay.BatchUploadSession{
				LastProgressNotifiedCount: 2,
				LastProgressNotifiedAt:    now.Add(-500 * time.Millisecond),
			},
			item:     3,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldNotifyBatchProgress(tt.session, tt.item)
			if got != tt.expected {
				t.Fatalf("shouldNotifyBatchProgress() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFormatBatchProgressText(t *testing.T) {
	collecting := formatBatchProgressCollectingText(12)
	if collecting == "" {
		t.Fatal("formatBatchProgressCollectingText() returned empty text")
	}

	finished := formatBatchProgressFinishedText(12)
	if finished == "" {
		t.Fatal("formatBatchProgressFinishedText() returned empty text")
	}

	canceled := formatBatchProgressCanceledText(12)
	if canceled == "" {
		t.Fatal("formatBatchProgressCanceledText() returned empty text")
	}
}
