package telegram

import (
	"context"
	"errors"
	"fmt"
	"html"
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
	claims := ExtractClaimRelayInputs(update)

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
	switch text {
	case "/start", "/help":
		logger.Info("telegram help requested")
		r.replyText(ctx, b, msg.Chat.ID, usageText())
		return
	case "/batch_start":
		r.handleBatchStart(ctx, b, logger, msg)
		return
	case "/batch_done":
		r.handleBatchDone(ctx, b, logger, msg)
		return
	case "/batch_cancel":
		r.handleBatchCancel(ctx, b, logger, msg)
		return
	}

	if upload, ok := ExtractCreateRelayInput(update); ok {
		if r.tryAppendBatchItem(ctx, b, logger, msg, upload) {
			if len(claims) == 0 {
				return
			}
			r.handleClaimRelays(ctx, b, msg.Chat.ID, logger, claims)
			return
		}
		r.handleCreateRelay(ctx, b, logger, msg, upload)
		if len(claims) == 0 {
			return
		}
		r.handleClaimRelays(ctx, b, msg.Chat.ID, logger, claims)
		return
	}

	if len(claims) > 0 {
		r.handleClaimRelays(ctx, b, msg.Chat.ID, logger, claims)
		return
	}

	if text != "" {
		logger.Info("telegram text not recognized", slog.Int("text_length", len(text)))
		r.replyText(ctx, b, msg.Chat.ID, usageText())
		return
	}

	logger.Debug("telegram update ignored", slog.String("reason", "unsupported_content"))
}

func (r *Router) handleBatchStart(ctx context.Context, b *bot.Bot, logger *slog.Logger, msg *models.Message) {
	requestLogger := logger.With(slog.String("operation", "batch_start"))
	requestLogger.Info("batch upload start requested")

	result, err := r.service.StartBatchUpload(ctx, relay.StartBatchUploadInput{
		UploaderUserID: msg.From.ID,
		UploaderChatID: msg.Chat.ID,
	})
	if err != nil {
		logTelegramRequestFailure(ctx, requestLogger, "batch upload start failed", err)
		r.replyText(ctx, b, msg.Chat.ID, r.describeError(err))
		return
	}

	requestLogger.Info("batch upload started", slog.Int64("relay_id", result.Relay.ID))
	r.replyText(ctx, b, msg.Chat.ID, "已开始批量上传。继续发送文件，完成后发送 /batch_done 生成一个共享 code；放弃可发送 /batch_cancel。")
}

func (r *Router) handleBatchDone(ctx context.Context, b *bot.Bot, logger *slog.Logger, msg *models.Message) {
	requestLogger := logger.With(slog.String("operation", "batch_done"))
	requestLogger.Info("batch upload finish requested")

	result, err := r.service.FinishBatchUpload(ctx, relay.FinishBatchUploadInput{
		UploaderUserID: msg.From.ID,
		UploaderChatID: msg.Chat.ID,
	})
	if err != nil {
		logTelegramRequestFailure(ctx, requestLogger, "batch upload finish failed", err)
		r.replyText(ctx, b, msg.Chat.ID, r.describeError(err))
		return
	}

	requestLogger.Info("batch upload finished",
		slog.Int64("relay_id", result.Relay.ID),
		slog.String("code_hint", result.Relay.CodeHint),
		slog.Int("item_count", result.ItemCount),
		slog.Time("expires_at", result.ExpiresAt),
	)
	r.replyHTML(ctx, b, msg.Chat.ID, formatBatchFinishSuccessHTML(result))
}

func (r *Router) handleBatchCancel(ctx context.Context, b *bot.Bot, logger *slog.Logger, msg *models.Message) {
	requestLogger := logger.With(slog.String("operation", "batch_cancel"))
	requestLogger.Info("batch upload cancel requested")

	result, err := r.service.CancelBatchUpload(ctx, relay.CancelBatchUploadInput{
		UploaderUserID: msg.From.ID,
		UploaderChatID: msg.Chat.ID,
	})
	if err != nil {
		logTelegramRequestFailure(ctx, requestLogger, "batch upload cancel failed", err)
		r.replyText(ctx, b, msg.Chat.ID, r.describeError(err))
		return
	}

	requestLogger.Info("batch upload canceled",
		slog.Int64("relay_id", result.RelayID),
		slog.Int("item_count", result.ItemCount),
	)
	r.replyText(ctx, b, msg.Chat.ID, fmt.Sprintf("已取消当前批量上传，会话内的 %d 个文件已丢弃。", result.ItemCount))
}

func (r *Router) tryAppendBatchItem(ctx context.Context, b *bot.Bot, logger *slog.Logger, msg *models.Message, upload relay.CreateRelayInput) bool {
	requestLogger := logger.With(
		slog.String("operation", "append_batch_item"),
		slog.String("media_kind", string(upload.MediaKind)),
		slog.Int64("file_size_bytes", upload.FileSizeBytes),
	)
	requestLogger.Info("batch item append requested")

	result, err := r.service.AppendBatchItem(ctx, relay.AppendBatchItemInput{
		SourceUpdateID:       upload.SourceUpdateID,
		UploaderUserID:       upload.UploaderUserID,
		UploaderChatID:       upload.UploaderChatID,
		SourceMessageID:      upload.SourceMessageID,
		MediaGroupID:         strings.TrimSpace(msg.MediaGroupID),
		MediaKind:            upload.MediaKind,
		TelegramFileID:       upload.TelegramFileID,
		TelegramFileUniqueID: upload.TelegramFileUniqueID,
		FileName:             upload.FileName,
		MIMEType:             upload.MIMEType,
		FileSizeBytes:        upload.FileSizeBytes,
		Caption:              upload.Caption,
	})
	switch {
	case err == nil:
		requestLogger.Info("batch item appended",
			slog.Int64("relay_id", result.Relay.ID),
			slog.Int64("item_id", result.Item.ID),
			slog.Int("item_count", result.ItemCount),
			slog.Bool("duplicated", result.Duplicated),
		)
		if result.Duplicated {
			r.replyText(ctx, b, msg.Chat.ID, fmt.Sprintf("这个文件已经在当前批量上传会话中了，当前共 %d 个文件。", result.ItemCount))
		} else {
			r.replyText(ctx, b, msg.Chat.ID, fmt.Sprintf("已加入批量上传，当前共 %d 个文件。完成后发送 /batch_done，取消发送 /batch_cancel。", result.ItemCount))
		}
		return true
	case errors.Is(err, relay.ErrBatchSessionNotFound):
		requestLogger.Debug("batch session not found, fallback to single relay upload")
		return false
	default:
		logTelegramRequestFailure(ctx, requestLogger, "batch item append failed", err)
		r.replyText(ctx, b, msg.Chat.ID, r.describeError(err))
		return true
	}
}

func (r *Router) handleCreateRelay(ctx context.Context, b *bot.Bot, logger *slog.Logger, msg *models.Message, upload relay.CreateRelayInput) {
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
		r.replyHTML(ctx, b, msg.Chat.ID, formatCreateSuccessHTML(result))
		return
	}
	requestLogger.Info("telegram relay created",
		slog.Int64("relay_id", result.Relay.ID),
		slog.String("code_hint", result.Relay.CodeHint),
		slog.Time("expires_at", result.ExpiresAt),
	)
	r.replyHTML(ctx, b, msg.Chat.ID, formatCreateSuccessHTML(result))
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
	case errors.Is(err, relay.ErrBatchItemLimit):
		return "当前批量上传的文件数已达到上限，请先发送 /batch_done 生成 code，或发送 /batch_cancel 取消。"
	case errors.Is(err, relay.ErrDeliveryFailed):
		return "文件回传失败，请稍后重试。"
	case errors.Is(err, relay.ErrBatchSessionActive):
		return "你当前已有一个批量上传会话。继续发文件，或发送 /batch_done 生成 code，发送 /batch_cancel 取消。"
	case errors.Is(err, relay.ErrBatchSessionNotFound):
		return "当前没有正在进行的批量上传。发送 /batch_start 开始。"
	case errors.Is(err, relay.ErrBatchSessionEmpty):
		return "当前批量上传会话里还没有文件。先发文件，再发送 /batch_done。"
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

func (r *Router) replyHTML(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
	}); err != nil {
		r.logger.Error("send HTML message failed", slog.Int64("chat_id", chatID), slog.Any("error", err))
	}
}

func formatCreateSuccessHTML(result relay.CreateRelayResult) string {
	return fmt.Sprintf(
		"中转码：\n<code>%s</code>\n\n有效期至：%s\n把这串 code 发给 bot，即可取回原文件。",
		html.EscapeString(result.Code),
		result.ExpiresAt.UTC().Format(time.RFC3339),
	)
}

func formatBatchFinishSuccessHTML(result relay.FinishBatchUploadResult) string {
	return fmt.Sprintf(
		"批量中转码：\n<code>%s</code>\n\n文件数：%d\n有效期至：%s\n把这串 code 发给 bot，即可取回这批文件。",
		html.EscapeString(result.Code),
		result.ItemCount,
		result.ExpiresAt.UTC().Format(time.RFC3339),
	)
}

func usageText() string {
	return "单文件：直接发文件，我会返回一个 relaybot code。\n批量文件：先发 /batch_start，再连续发多个文件，最后发 /batch_done 生成一个共享 code；放弃可发 /batch_cancel。\n领取：把一个或多个 code 发给我，就能取回原文件。"
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
		errors.Is(err, relay.ErrBatchItemLimit),
		errors.Is(err, relay.ErrDeliveryFailed),
		errors.Is(err, relay.ErrBatchSessionActive),
		errors.Is(err, relay.ErrBatchSessionNotFound),
		errors.Is(err, relay.ErrBatchSessionEmpty):
		return true
	default:
		return false
	}
}

func (r *Router) handleClaimRelays(ctx context.Context, b *bot.Bot, chatID int64, logger *slog.Logger, claims []relay.ClaimRelayInput) {
	failedLines := make([]string, 0, len(claims))

	for index, claim := range claims {
		requestLogger := logger.With(
			slog.String("operation", "claim_relay"),
			slog.String("code_hint", codeHint(claim.RawCode)),
			slog.Int("claim_index", index),
			slog.Int("claim_count", len(claims)),
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
			failedLines = append(failedLines, formatClaimFailure(claim.RawCode, r.describeError(err), len(claims) > 1))
			continue
		}
		if result.Duplicated {
			requestLogger.Info("telegram relay claim duplicated",
				slog.Int64("relay_id", result.Relay.ID),
				slog.String("code_hint", result.Relay.CodeHint),
				slog.Int64("delivery_id", result.Delivery.ID),
				slog.String("delivery_status", string(result.Delivery.Status)),
				slog.String("delivery_method", string(result.Method)),
			)
			continue
		}
		requestLogger.Info("telegram relay claimed",
			slog.Int64("relay_id", result.Relay.ID),
			slog.String("code_hint", result.Relay.CodeHint),
			slog.Int64("delivery_id", result.Delivery.ID),
			slog.String("delivery_method", string(result.Method)),
			slog.Int("out_message_id", result.OutMessageID),
		)
	}

	if len(failedLines) == 0 {
		return
	}
	r.replyText(ctx, b, chatID, strings.Join(failedLines, "\n"))
}

func formatClaimFailure(rawCode, message string, includeCodeHint bool) string {
	if !includeCodeHint {
		return message
	}
	return fmt.Sprintf("%s：%s", codeHint(rawCode), message)
}
