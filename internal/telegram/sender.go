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

func NewSender() *Sender {
	return &Sender{logger: slog.Default().With("component", "telegram_sender")}
}

func (s *Sender) Bind(b *bot.Bot) {
	s.bot = b
}

func (s *Sender) CopyOrResend(ctx context.Context, item relay.Relay, targetChatID int64) (relay.DeliveryMethod, int, error) {
	logger := s.deliveryLogger(item, targetChatID)
	logger.Info("telegram delivery started")

	if messageID, err := s.copyMessage(ctx, item, targetChatID); err == nil {
		logger.Info("telegram delivery completed",
			slog.String("delivery_method", string(relay.DeliveryMethodCopyMessage)),
			slog.Int("out_message_id", messageID),
		)
		return relay.DeliveryMethodCopyMessage, messageID, nil
	} else if !shouldFallbackAfterCopyError(err) {
		logDeliveryError(logger, "telegram copy message finished with unknown result", relay.DeliveryMethodCopyMessage, err, false)
		return relay.DeliveryMethodCopyMessage, 0, err
	} else {
		logDeliveryError(logger, "telegram copy message failed, fallback to resend", relay.DeliveryMethodCopyMessage, err, true)
	}

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

func (s *Sender) copyMessage(ctx context.Context, item relay.Relay, targetChatID int64) (int, error) {
	resp, err := s.bot.CopyMessage(ctx, &bot.CopyMessageParams{
		ChatID:     targetChatID,
		FromChatID: item.UploaderChatID,
		MessageID:  item.SourceMessageID,
	})
	if err != nil {
		return 0, classifySendError(err)
	}
	return resp.ID, nil
}

func (s *Sender) sendDocument(ctx context.Context, item relay.Relay, targetChatID int64) (int, error) {
	resp, err := s.bot.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID:   targetChatID,
		Document: &models.InputFileString{Data: item.TelegramFileID},
		Caption:  item.Caption,
	})
	if err != nil {
		return 0, classifySendError(err)
	}
	return resp.ID, nil
}

func (s *Sender) sendPhoto(ctx context.Context, item relay.Relay, targetChatID int64) (int, error) {
	resp, err := s.bot.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:  targetChatID,
		Photo:   &models.InputFileString{Data: item.TelegramFileID},
		Caption: item.Caption,
	})
	if err != nil {
		return 0, classifySendError(err)
	}
	return resp.ID, nil
}

func (s *Sender) sendVideo(ctx context.Context, item relay.Relay, targetChatID int64) (int, error) {
	resp, err := s.bot.SendVideo(ctx, &bot.SendVideoParams{
		ChatID:  targetChatID,
		Video:   &models.InputFileString{Data: item.TelegramFileID},
		Caption: item.Caption,
	})
	if err != nil {
		return 0, classifySendError(err)
	}
	return resp.ID, nil
}

func (s *Sender) sendAudio(ctx context.Context, item relay.Relay, targetChatID int64) (int, error) {
	resp, err := s.bot.SendAudio(ctx, &bot.SendAudioParams{
		ChatID:  targetChatID,
		Audio:   &models.InputFileString{Data: item.TelegramFileID},
		Caption: item.Caption,
	})
	if err != nil {
		return 0, classifySendError(err)
	}
	return resp.ID, nil
}

func (s *Sender) sendVoice(ctx context.Context, item relay.Relay, targetChatID int64) (int, error) {
	resp, err := s.bot.SendVoice(ctx, &bot.SendVoiceParams{
		ChatID:  targetChatID,
		Voice:   &models.InputFileString{Data: item.TelegramFileID},
		Caption: item.Caption,
	})
	if err != nil {
		return 0, classifySendError(err)
	}
	return resp.ID, nil
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

func (s *Sender) deliveryLogger(item relay.Relay, targetChatID int64) *slog.Logger {
	logger := s.logger
	if logger == nil {
		logger = slog.Default().With("component", "telegram_sender")
	}
	return logger.With(
		slog.Int64("relay_id", item.ID),
		slog.String("code_hint", item.CodeHint),
		slog.Int64("uploader_chat_id", item.UploaderChatID),
		slog.Int64("target_chat_id", targetChatID),
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
