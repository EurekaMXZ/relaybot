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

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "network timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
