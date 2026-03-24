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
	if logger == nil {
		logger = slog.Default()
	}
	return &Router{logger: logger}
}

func (r *Router) Bind(service *relay.Service) {
	r.service = service
}

func (r *Router) HandleUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	if r.service == nil || update == nil || update.Message == nil {
		return
	}

	msg := update.Message
	if msg.Chat.Type != "private" {
		r.replyText(ctx, b, msg.Chat.ID, "请在与 bot 的私聊中使用该功能。")
		return
	}
	if msg.From == nil {
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "/start" || text == "/help" {
		r.replyText(ctx, b, msg.Chat.ID, usageText())
		return
	}

	if upload, ok := ExtractCreateRelayInput(update); ok {
		result, err := r.service.CreateRelay(ctx, upload)
		if err != nil {
			r.replyText(ctx, b, msg.Chat.ID, r.describeError(err))
			return
		}
		if result.Duplicated {
			return
		}
		r.replyText(ctx, b, msg.Chat.ID, formatCreateSuccess(result))
		return
	}

	if claim, ok := ExtractClaimRelayInput(update); ok {
		result, err := r.service.ClaimRelay(ctx, claim)
		if err != nil {
			r.replyText(ctx, b, msg.Chat.ID, r.describeError(err))
			return
		}
		if result.Duplicated {
			return
		}
		return
	}

	if text != "" {
		r.replyText(ctx, b, msg.Chat.ID, usageText())
	}
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
		return "文件过大，当前只支持 45 MB 以内的文件。"
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
		r.logger.Error("telegram request failed", slog.Any("error", err))
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
