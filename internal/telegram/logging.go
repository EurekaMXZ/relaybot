package telegram

import (
	"log/slog"
	"strings"
	"unicode"

	"github.com/go-telegram/bot/models"
)

func newRouterLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger.With("component", "telegram_router")
}

func loggerWithUpdate(logger *slog.Logger, update *models.Update) *slog.Logger {
	if logger == nil {
		logger = newRouterLogger(nil)
	}
	if update == nil {
		return logger
	}

	logger = logger.With(slog.Int64("update_id", int64(update.ID)))
	if update.Message == nil {
		return logger
	}

	logger = logger.With(
		slog.Int64("chat_id", update.Message.Chat.ID),
		slog.Int("message_id", update.Message.ID),
	)
	if update.Message.From != nil {
		logger = logger.With(slog.Int64("user_id", update.Message.From.ID))
	}
	return logger
}

func codeHint(raw string) string {
	normalized := sanitizeClaimCode(raw)
	if normalized == "" {
		return ""
	}
	if len(normalized) <= 4 {
		return normalized
	}
	return normalized[len(normalized)-4:]
}

func sanitizeClaimCode(raw string) string {
	trimmed := strings.TrimSpace(strings.ToUpper(raw))
	if trimmed == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}

	return strings.TrimPrefix(b.String(), "RELAYBOT")
}
