package telegram

import (
	"context"
	"errors"
	"net"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"relaybot/internal/relay"
)

type Sender struct {
	bot *bot.Bot
}

func NewSender() *Sender {
	return &Sender{}
}

func (s *Sender) Bind(b *bot.Bot) {
	s.bot = b
}

func (s *Sender) CopyOrResend(ctx context.Context, item relay.Relay, targetChatID int64) (relay.DeliveryMethod, int, error) {
	if messageID, err := s.copyMessage(ctx, item, targetChatID); err == nil {
		return relay.DeliveryMethodCopyMessage, messageID, nil
	} else if !shouldFallbackAfterCopyError(err) {
		return relay.DeliveryMethodCopyMessage, 0, err
	}

	switch item.MediaKind {
	case relay.MediaKindDocument:
		messageID, err := s.sendDocument(ctx, item, targetChatID)
		return relay.DeliveryMethodSendDocument, messageID, err
	case relay.MediaKindPhoto:
		messageID, err := s.sendPhoto(ctx, item, targetChatID)
		return relay.DeliveryMethodSendPhoto, messageID, err
	case relay.MediaKindVideo:
		messageID, err := s.sendVideo(ctx, item, targetChatID)
		return relay.DeliveryMethodSendVideo, messageID, err
	case relay.MediaKindAudio:
		messageID, err := s.sendAudio(ctx, item, targetChatID)
		return relay.DeliveryMethodSendAudio, messageID, err
	case relay.MediaKindVoice:
		messageID, err := s.sendVoice(ctx, item, targetChatID)
		return relay.DeliveryMethodSendVoice, messageID, err
	default:
		return "", 0, classifySendError(relay.ErrUnsupportedMedia)
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
