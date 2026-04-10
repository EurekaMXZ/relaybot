package telegram

import (
	"context"
	"errors"
	"log/slog"
	"net"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"relaybot/internal/relay"
)

type Sender struct {
	bot    *bot.Bot
	logger *slog.Logger
}

type deliverySegment struct {
	items []relay.RelayItem
}

const maxDeliveryPageItems = 10

func NewSender() *Sender {
	return &Sender{logger: slog.Default().With("component", "telegram_sender")}
}

func (s *Sender) Bind(b *bot.Bot) {
	s.bot = b
}

func (s *Sender) Deliver(ctx context.Context, batch relay.Relay, items []relay.RelayItem, targetChatID int64) (relay.DeliveryMethod, int, error) {
	return s.DeliverPage(ctx, batch, relay.DeliveryPage{
		Index: 1,
		Total: 1,
		Items: items,
	}, targetChatID)
}

func (s *Sender) DeliverPage(ctx context.Context, batch relay.Relay, page relay.DeliveryPage, targetChatID int64) (relay.DeliveryMethod, int, error) {
	logger := s.batchDeliveryLogger(batch, page.Items, targetChatID).With(
		slog.Int("page_index", page.Index),
		slog.Int("page_total", page.Total),
	)
	logger.Info("telegram page delivery started")

	if len(page.Items) == 0 {
		err := classifySendError(relay.ErrRelayNotFound)
		logDeliveryError(logger, "telegram page delivery failed", relay.DeliveryMethodSendBatch, err, false)
		return relay.DeliveryMethodSendBatch, 0, err
	}

	if len(page.Items) == 1 {
		method, messageID, err := s.deliverSingle(ctx, batch, page.Items[0], targetChatID)
		if err != nil {
			logDeliveryError(logger, "telegram page delivery failed", method, err, false)
			return method, 0, err
		}
		logger.Info("telegram page delivery completed",
			slog.String("delivery_method", string(method)),
			slog.Int("out_message_id", messageID),
		)
		return method, messageID, nil
	}

	firstMessageID := 0
	pageChunks := splitPageItems(page.Items, maxDeliveryPageItems)
	for chunkIndex, chunkItems := range pageChunks {
		chunkLogger := logger.With(
			slog.Int("chunk_index", chunkIndex),
			slog.Int("chunk_item_count", len(chunkItems)),
		)
		messageID, err := s.deliverChunk(ctx, batch, chunkItems, targetChatID, chunkLogger)
		if err != nil {
			logDeliveryError(chunkLogger, "telegram page chunk failed", relay.DeliveryMethodSendBatch, err, false)
			return relay.DeliveryMethodSendBatch, 0, err
		}
		if firstMessageID == 0 {
			firstMessageID = messageID
		}
	}

	logger.Info("telegram page delivery completed",
		slog.String("delivery_method", string(relay.DeliveryMethodSendBatch)),
		slog.Int("out_message_id", firstMessageID),
	)
	return relay.DeliveryMethodSendBatch, firstMessageID, nil
}

func (s *Sender) deliverChunk(ctx context.Context, batch relay.Relay, items []relay.RelayItem, targetChatID int64, logger *slog.Logger) (int, error) {
	if len(items) == 1 {
		_, messageID, err := s.deliverSingle(ctx, batch, items[0], targetChatID)
		return messageID, err
	}
	if canSendAsMediaGroup(items) {
		return s.sendMediaGroup(ctx, items, targetChatID, logger)
	}
	firstMessageID := 0
	for _, item := range items {
		_, messageID, err := s.deliverSingle(ctx, batch, item, targetChatID)
		if err != nil {
			return 0, err
		}
		if firstMessageID == 0 {
			firstMessageID = messageID
		}
	}
	return firstMessageID, nil
}

func (s *Sender) deliverSingle(ctx context.Context, batch relay.Relay, item relay.RelayItem, targetChatID int64) (relay.DeliveryMethod, int, error) {
	logger := s.itemDeliveryLogger(batch, item, targetChatID)

	var (
		method    relay.DeliveryMethod
		messageID int
		err       error
	)
	switch item.MediaKind {
	case relay.MediaKindDocument:
		method = relay.DeliveryMethodSendDocument
		messageID, err = s.sendDocument(ctx, item, targetChatID)
	case relay.MediaKindPhoto:
		method = relay.DeliveryMethodSendPhoto
		messageID, err = s.sendPhoto(ctx, item, targetChatID)
	case relay.MediaKindVideo:
		method = relay.DeliveryMethodSendVideo
		messageID, err = s.sendVideo(ctx, item, targetChatID)
	case relay.MediaKindAudio:
		method = relay.DeliveryMethodSendAudio
		messageID, err = s.sendAudio(ctx, item, targetChatID)
	case relay.MediaKindVoice:
		method = relay.DeliveryMethodSendVoice
		messageID, err = s.sendVoice(ctx, item, targetChatID)
	default:
		err = classifySendError(relay.ErrUnsupportedMedia)
	}

	if err != nil {
		logDeliveryError(logger, "telegram resend failed", method, err, false)
		return method, 0, err
	}

	logger.Info("telegram delivery completed",
		slog.String("delivery_method", string(method)),
		slog.Int("out_message_id", messageID),
	)
	return method, messageID, nil
}

func splitPageItems(items []relay.RelayItem, maxPageItems int) [][]relay.RelayItem {
	if len(items) == 0 || maxPageItems <= 0 {
		return nil
	}

	chunks := make([][]relay.RelayItem, 0, (len(items)+maxPageItems-1)/maxPageItems)
	for start := 0; start < len(items); start += maxPageItems {
		end := start + maxPageItems
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[start:end])
	}
	return chunks
}

func canSendAsMediaGroup(items []relay.RelayItem) bool {
	if len(items) < 2 {
		return false
	}

	mode := mediaGroupKindMode(items[0].MediaKind)
	if mode == mediaGroupModeInvalid {
		return false
	}

	for _, item := range items[1:] {
		nextMode := mediaGroupKindMode(item.MediaKind)
		if nextMode == mediaGroupModeInvalid || nextMode != mode {
			return false
		}
	}
	return true
}

type mediaGroupMode int

const (
	mediaGroupModeInvalid mediaGroupMode = iota
	mediaGroupModeVisual
	mediaGroupModeAudio
	mediaGroupModeDocument
)

func mediaGroupKindMode(kind relay.MediaKind) mediaGroupMode {
	switch kind {
	case relay.MediaKindPhoto, relay.MediaKindVideo:
		return mediaGroupModeVisual
	case relay.MediaKindAudio:
		return mediaGroupModeAudio
	case relay.MediaKindDocument:
		return mediaGroupModeDocument
	default:
		return mediaGroupModeInvalid
	}
}

func shouldFallbackAfterCopyError(err error) bool {
	if err == nil {
		return false
	}

	var senderErr relay.SenderError
	if errors.As(err, &senderErr) && senderErr.UnknownResult() {
		return false
	}

	return true
}

func (s *Sender) sendDocument(ctx context.Context, item relay.RelayItem, targetChatID int64) (int, error) {
	resp, err := s.bot.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID:   targetChatID,
		Document: &models.InputFileString{Data: item.TelegramFileID},
	})
	if err != nil {
		return 0, classifySendError(err)
	}
	return resp.ID, nil
}

func (s *Sender) sendPhoto(ctx context.Context, item relay.RelayItem, targetChatID int64) (int, error) {
	resp, err := s.bot.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID: targetChatID,
		Photo:  &models.InputFileString{Data: item.TelegramFileID},
	})
	if err != nil {
		return 0, classifySendError(err)
	}
	return resp.ID, nil
}

func (s *Sender) sendVideo(ctx context.Context, item relay.RelayItem, targetChatID int64) (int, error) {
	resp, err := s.bot.SendVideo(ctx, &bot.SendVideoParams{
		ChatID: targetChatID,
		Video:  &models.InputFileString{Data: item.TelegramFileID},
	})
	if err != nil {
		return 0, classifySendError(err)
	}
	return resp.ID, nil
}

func (s *Sender) sendAudio(ctx context.Context, item relay.RelayItem, targetChatID int64) (int, error) {
	resp, err := s.bot.SendAudio(ctx, &bot.SendAudioParams{
		ChatID: targetChatID,
		Audio:  &models.InputFileString{Data: item.TelegramFileID},
	})
	if err != nil {
		return 0, classifySendError(err)
	}
	return resp.ID, nil
}

func (s *Sender) sendVoice(ctx context.Context, item relay.RelayItem, targetChatID int64) (int, error) {
	resp, err := s.bot.SendVoice(ctx, &bot.SendVoiceParams{
		ChatID: targetChatID,
		Voice:  &models.InputFileString{Data: item.TelegramFileID},
	})
	if err != nil {
		return 0, classifySendError(err)
	}
	return resp.ID, nil
}

func (s *Sender) sendMediaGroup(ctx context.Context, items []relay.RelayItem, targetChatID int64, logger *slog.Logger) (int, error) {
	media := make([]models.InputMedia, 0, len(items))
	for _, item := range items {
		switch item.MediaKind {
		case relay.MediaKindPhoto:
			media = append(media, &models.InputMediaPhoto{
				Media: item.TelegramFileID,
			})
		case relay.MediaKindVideo:
			media = append(media, &models.InputMediaVideo{
				Media: item.TelegramFileID,
			})
		case relay.MediaKindAudio:
			media = append(media, &models.InputMediaAudio{
				Media: item.TelegramFileID,
			})
		case relay.MediaKindDocument:
			media = append(media, &models.InputMediaDocument{
				Media: item.TelegramFileID,
			})
		default:
			return 0, classifySendError(relay.ErrUnsupportedMedia)
		}
	}

	resp, err := s.bot.SendMediaGroup(ctx, &bot.SendMediaGroupParams{
		ChatID: targetChatID,
		Media:  media,
	})
	if err != nil {
		return 0, classifySendError(err)
	}
	if len(resp) == 0 {
		return 0, classifySendError(errors.New("telegram returned empty media group result"))
	}

	logger.Info("telegram media group sent",
		slog.Int("segment_item_count", len(items)),
		slog.String("media_group_id", items[0].MediaGroupID),
		slog.Int("out_message_id", resp[0].ID),
	)
	return resp[0].ID, nil
}

type sendError struct {
	cause   error
	method  relay.DeliveryMethod
	code    string
	desc    string
	unknown bool
}

func (e *sendError) Error() string {
	return e.cause.Error()
}

func (e *sendError) Unwrap() error {
	return e.cause
}

func (e *sendError) Code() string {
	return e.code
}

func (e *sendError) Description() string {
	if e.desc != "" {
		return e.desc
	}
	return e.cause.Error()
}

func (e *sendError) UnknownResult() bool {
	return e.unknown
}

func classifySendError(err error) error {
	if err == nil {
		return nil
	}

	sendErr := &sendError{
		cause: err,
		code:  "telegram_error",
		desc:  err.Error(),
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		sendErr.code = "telegram_timeout"
		sendErr.unknown = true
		return sendErr
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		sendErr.code = "telegram_timeout"
		sendErr.unknown = true
	}
	return sendErr
}

func (s *Sender) batchDeliveryLogger(batch relay.Relay, items []relay.RelayItem, targetChatID int64) *slog.Logger {
	logger := s.logger
	if logger == nil {
		logger = slog.Default().With("component", "telegram_sender")
	}
	return logger.With(
		slog.Int64("relay_id", batch.ID),
		slog.String("code_hint", batch.CodeHint),
		slog.Int64("uploader_chat_id", batch.UploaderChatID),
		slog.Int64("target_chat_id", targetChatID),
		slog.Int("item_count", len(items)),
	)
}

func (s *Sender) itemDeliveryLogger(batch relay.Relay, item relay.RelayItem, targetChatID int64) *slog.Logger {
	return s.batchDeliveryLogger(batch, []relay.RelayItem{item}, targetChatID).With(
		slog.Int64("item_id", item.ID),
		slog.Int("source_message_id", item.SourceMessageID),
		slog.String("media_kind", string(item.MediaKind)),
	)
}

func logDeliveryError(logger *slog.Logger, message string, method relay.DeliveryMethod, err error, fallback bool) {
	attrs := []any{
		slog.String("delivery_method", string(method)),
		slog.Any("error", err),
		slog.Bool("fallback", fallback),
	}

	var senderErr relay.SenderError
	if errors.As(err, &senderErr) {
		attrs = append(attrs,
			slog.String("error_code", senderErr.Code()),
			slog.Bool("unknown_result", senderErr.UnknownResult()),
		)
	}

	logger.Warn(message, attrs...)
}
