package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"relaybot/internal/relay"
)

type Router struct {
	logger  *slog.Logger
	service *relay.Service
}

func NewRouter(logger *slog.Logger) *Router {
	return &Router{logger: newRouterLogger(logger)}
}

func (r *Router) Bind(service *relay.Service) {
	r.service = service
}

func (r *Router) HandleUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	if r.service == nil {
		r.logger.Error("telegram router received update without bound service")
		return
	}
	if update == nil {
		r.logger.Warn("telegram router received nil update")
		return
	}
	if update.Message == nil {
		r.logger.Debug("ignore telegram update without message", slog.Int64("update_id", int64(update.ID)))
		return
	}

	msg := update.Message
	logger := r.updateLogger(update)
	logger.Debug("telegram update received", slog.String("chat_type", string(msg.Chat.Type)))

	if msg.Chat.Type != "private" {
		logger.Info("telegram update rejected", slog.String("reason", "non_private_chat"))
		r.replyText(ctx, b, msg.Chat.ID, "请在与 bot 的私聊中使用该功能。")
		return
	}
	if msg.From == nil {
		logger.Warn("telegram update rejected", slog.String("reason", "missing_sender"))
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "/start" || text == "/help" {
		logger.Info("telegram help requested")
		r.replyText(ctx, b, msg.Chat.ID, usageText())
		return
	}

	if upload, ok := ExtractCreateRelayInput(update); ok {
		requestLogger := logger.With(
			slog.String("operation", "create_relay"),
			slog.String("media_kind", string(upload.MediaKind)),
			slog.Int64("file_size_bytes", upload.FileSizeBytes),
		)
		requestLogger.Info("telegram relay create requested")

		result, err := r.service.CreateRelay(ctx, upload)
		if err != nil {
			logTelegramRequestFailure(ctx, requestLogger, "telegram relay create failed", err)
			r.replyText(ctx, b, msg.Chat.ID, r.describeError(err))
			return
		}
		if result.Duplicated {
			requestLogger.Info("telegram relay create duplicated",
				slog.Int64("relay_id", result.Relay.ID),
				slog.String("code_hint", result.Relay.CodeHint),
			)
			return
		}
		requestLogger.Info("telegram relay created",
			slog.Int64("relay_id", result.Relay.ID),
			slog.String("code_hint", result.Relay.CodeHint),
			slog.Time("expires_at", result.ExpiresAt),
		)
		r.replyText(ctx, b, msg.Chat.ID, formatCreateSuccess(result))
		return
	}

	if claim, ok := ExtractClaimRelayInput(update); ok {
		requestLogger := logger.With(
			slog.String("operation", "claim_relay"),
			slog.String("code_hint", codeHint(claim.RawCode)),
		)
		requestLogger.Info("telegram relay claim requested")

		result, err := r.service.ClaimRelay(ctx, claim)
		if err != nil {
			var attrs []any
			if result.Relay.ID != 0 {
				attrs = append(attrs,
					slog.Int64("relay_id", result.Relay.ID),
					slog.String("relay_status", string(result.Relay.Status)),
				)
			}
			if result.Delivery.ID != 0 {
				attrs = append(attrs,
					slog.Int64("delivery_id", result.Delivery.ID),
					slog.String("delivery_status", string(result.Delivery.Status)),
				)
			}
			if result.Method != "" {
				attrs = append(attrs, slog.String("delivery_method", string(result.Method)))
			}
			logTelegramRequestFailure(ctx, requestLogger, "telegram relay claim failed", err, attrs...)
			r.replyText(ctx, b, msg.Chat.ID, r.describeError(err))
			return
		}
		if result.Duplicated {
			requestLogger.Info("telegram relay claim duplicated",
				slog.Int64("relay_id", result.Relay.ID),
				slog.String("code_hint", result.Relay.CodeHint),
				slog.Int64("delivery_id", result.Delivery.ID),
				slog.String("delivery_status", string(result.Delivery.Status)),
				slog.String("delivery_method", string(result.Method)),
			)
			return
		}
		requestLogger.Info("telegram relay claimed",
			slog.Int64("relay_id", result.Relay.ID),
			slog.String("code_hint", result.Relay.CodeHint),
			slog.Int64("delivery_id", result.Delivery.ID),
			slog.String("delivery_method", string(result.Method)),
			slog.Int("out_message_id", result.OutMessageID),
		)
		return
	}

	if text != "" {
		logger.Info("telegram text not recognized", slog.Int("text_length", len(text)))
		r.replyText(ctx, b, msg.Chat.ID, usageText())
		return
	}

	logger.Debug("telegram update ignored", slog.String("reason", "unsupported_content"))
}

func (r *Router) describeError(err error) string {
	switch {
	case errors.Is(err, relay.ErrInvalidCode):
		return "code 格式不正确。"
	case errors.Is(err, relay.ErrRelayNotFound):
		return "没有找到对应文件，请检查 code 是否正确。"
	case errors.Is(err, relay.ErrRelayExpired):
		return "这个 code 已经过期。"
	case errors.Is(err, relay.ErrUnsupportedMedia):
		return "当前只支持 document、photo、video、audio、voice。"
	case errors.Is(err, relay.ErrFileTooLarge):
		return "文件过大，超过当前配置限制。"
	case errors.Is(err, relay.ErrDangerousFile):
		return "该文件扩展名被默认策略拦截。"
	case errors.Is(err, relay.ErrDeliveryInProgress):
		return "该 code 正在处理中，请稍后再试。"
	case errors.Is(err, relay.ErrUploadRateLimited):
		return "上传太频繁了，请稍后再试。"
	case errors.Is(err, relay.ErrClaimRateLimited):
		return "领取太频繁了，请稍后再试。"
	case errors.Is(err, relay.ErrBadCodeRateLimit):
		return "无效 code 尝试过多，请稍后再试。"
	case errors.Is(err, relay.ErrTooManyRelays):
		return "你当前活跃的中转文件过多，请稍后再试。"
	case errors.Is(err, relay.ErrDeliveryFailed):
		return "文件回传失败，请稍后重试。"
	default:
		return "处理失败，请稍后重试。"
	}
}

func (r *Router) replyText(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	}); err != nil {
		r.logger.Error("send message failed", slog.Int64("chat_id", chatID), slog.Any("error", err))
	}
}

func formatCreateSuccess(result relay.CreateRelayResult) string {
	return fmt.Sprintf(
		"中转码：%s\n有效期至：%s\n把这串 code 发给 bot，即可取回原文件。",
		result.Code,
		result.ExpiresAt.UTC().Format(time.RFC3339),
	)
}

func usageText() string {
	return "把文件或常见媒体直接发给我，我会返回一串 relaybot code；把 code 再发给我，就能取回原文件。"
}

func (r *Router) updateLogger(update *models.Update) *slog.Logger {
	return loggerWithUpdate(r.logger, update)
}

func logTelegramRequestFailure(ctx context.Context, logger *slog.Logger, message string, err error, extraAttrs ...any) {
	attrs := []any{slog.Any("error", err)}
	attrs = append(attrs, extraAttrs...)
	if isExpectedTelegramError(err) {
		logger.InfoContext(ctx, message, attrs...)
		return
	}
	logger.ErrorContext(ctx, message, attrs...)
}

func isExpectedTelegramError(err error) bool {
	switch {
	case errors.Is(err, relay.ErrInvalidCode),
		errors.Is(err, relay.ErrRelayNotFound),
		errors.Is(err, relay.ErrRelayExpired),
		errors.Is(err, relay.ErrUnsupportedMedia),
		errors.Is(err, relay.ErrFileTooLarge),
		errors.Is(err, relay.ErrDangerousFile),
		errors.Is(err, relay.ErrDeliveryInProgress),
		errors.Is(err, relay.ErrUploadRateLimited),
		errors.Is(err, relay.ErrClaimRateLimited),
		errors.Is(err, relay.ErrBadCodeRateLimit),
		errors.Is(err, relay.ErrTooManyRelays),
		errors.Is(err, relay.ErrDeliveryFailed):
		return true
	default:
		return false
	}
}
