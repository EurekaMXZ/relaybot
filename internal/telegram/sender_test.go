package telegram

import (
	"context"
	"errors"
	"testing"

	"relaybot/internal/relay"
)

func TestShouldFallbackAfterCopyError(t *testing.T) {
	t.Run("unknown result does not fallback", func(t *testing.T) {
		err := classifySendError(context.DeadlineExceeded)
		if shouldFallbackAfterCopyError(err) {
			t.Fatal("expected unknown-result copy error to skip fallback")
		}
	})

	t.Run("explicit delivery error with unknown result does not fallback", func(t *testing.T) {
		err := &relay.DeliveryError{
			Method:         relay.DeliveryMethodCopyMessage,
			ErrCode:        "telegram_timeout",
			ErrDescription: "request timed out",
			Unknown:        true,
		}
		if shouldFallbackAfterCopyError(err) {
			t.Fatal("expected relay delivery error with unknown result to skip fallback")
		}
	})

	t.Run("definite failure does fallback", func(t *testing.T) {
		err := classifySendError(errors.New("copy failed"))
		if !shouldFallbackAfterCopyError(err) {
			t.Fatal("expected definite copy failure to fallback")
		}
	})

	t.Run("network timeout does not fallback", func(t *testing.T) {
		err := classifySendError(timeoutErr{})
		if shouldFallbackAfterCopyError(err) {
			t.Fatal("expected network timeout copy error to skip fallback")
		}
	})
}

func TestBuildDeliverySegments(t *testing.T) {
	items := []relay.RelayItem{
		{ID: 1, MediaKind: relay.MediaKindPhoto, MediaGroupID: "g1"},
		{ID: 2, MediaKind: relay.MediaKindVideo, MediaGroupID: "g1"},
		{ID: 3, MediaKind: relay.MediaKindDocument},
		{ID: 4, MediaKind: relay.MediaKindPhoto, MediaGroupID: "g2"},
	}

	segments := buildDeliverySegments(items)
	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segments))
	}
	if len(segments[0].items) != 2 {
		t.Fatalf("expected first segment to contain 2 items, got %d", len(segments[0].items))
	}
	if segments[1].items[0].ID != 3 {
		t.Fatalf("expected second segment to contain document item, got %#v", segments[1].items[0])
	}
	if segments[2].items[0].ID != 4 {
		t.Fatalf("expected third segment to contain trailing grouped item, got %#v", segments[2].items[0])
	}
}

func TestCanSendAsMediaGroup(t *testing.T) {
	if !canSendAsMediaGroup([]relay.RelayItem{
		{MediaKind: relay.MediaKindPhoto, MediaGroupID: "g1"},
		{MediaKind: relay.MediaKindVideo, MediaGroupID: "g1"},
	}) {
		t.Fatal("expected photo/video items with same media_group_id to be sent as media group")
	}

	if canSendAsMediaGroup([]relay.RelayItem{
		{MediaKind: relay.MediaKindPhoto, MediaGroupID: "g1"},
		{MediaKind: relay.MediaKindDocument, MediaGroupID: "g1"},
	}) {
		t.Fatal("expected mixed photo/document items to not be sent as media group")
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "network timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
