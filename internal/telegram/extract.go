package telegram

import (
	"regexp"
	"strings"

	"github.com/go-telegram/bot/models"

	"relaybot/internal/relay"
)

const claimRequestSequenceBits = 16

var claimCodePattern = regexp.MustCompile(`(?i)relaybot[_\s-]?[0-9a-hjkmnp-tv-z]{4}(?:[-_\s]?[0-9a-hjkmnp-tv-z]{4}){3}`)

func ExtractCreateRelayInput(update *models.Update) (relay.CreateRelayInput, bool) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return relay.CreateRelayInput{}, false
	}

	message := update.Message
	base := relay.CreateRelayInput{
		SourceUpdateID:  int64(update.ID),
		UploaderUserID:  message.From.ID,
		UploaderChatID:  message.Chat.ID,
		SourceMessageID: message.ID,
		Caption:         message.Caption,
	}

	switch {
	case message.Document != nil:
		base.MediaKind = relay.MediaKindDocument
		base.TelegramFileID = message.Document.FileID
		base.TelegramFileUniqueID = message.Document.FileUniqueID
		base.FileName = message.Document.FileName
		base.MIMEType = message.Document.MimeType
		base.FileSizeBytes = int64(message.Document.FileSize)
		return base, true
	case len(message.Photo) > 0:
		last := message.Photo[len(message.Photo)-1]
		base.MediaKind = relay.MediaKindPhoto
		base.TelegramFileID = last.FileID
		base.TelegramFileUniqueID = last.FileUniqueID
		base.MIMEType = "image/jpeg"
		base.FileSizeBytes = int64(last.FileSize)
		return base, true
	case message.Video != nil:
		base.MediaKind = relay.MediaKindVideo
		base.TelegramFileID = message.Video.FileID
		base.TelegramFileUniqueID = message.Video.FileUniqueID
		base.FileName = message.Video.FileName
		base.MIMEType = message.Video.MimeType
		base.FileSizeBytes = int64(message.Video.FileSize)
		return base, true
	case message.Audio != nil:
		base.MediaKind = relay.MediaKindAudio
		base.TelegramFileID = message.Audio.FileID
		base.TelegramFileUniqueID = message.Audio.FileUniqueID
		base.FileName = message.Audio.FileName
		base.MIMEType = message.Audio.MimeType
		base.FileSizeBytes = int64(message.Audio.FileSize)
		return base, true
	case message.Voice != nil:
		base.MediaKind = relay.MediaKindVoice
		base.TelegramFileID = message.Voice.FileID
		base.TelegramFileUniqueID = message.Voice.FileUniqueID
		base.MIMEType = message.Voice.MimeType
		base.FileSizeBytes = int64(message.Voice.FileSize)
		return base, true
	default:
		return relay.CreateRelayInput{}, false
	}
}

func ExtractClaimRelayInput(update *models.Update) (relay.ClaimRelayInput, bool) {
	claims := ExtractClaimRelayInputs(update)
	if len(claims) == 0 {
		return relay.ClaimRelayInput{}, false
	}
	return claims[0], true
}

func ExtractClaimRelayInputs(update *models.Update) []relay.ClaimRelayInput {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return nil
	}

	content := claimContent(update.Message)
	if content == "" {
		return nil
	}

	matches := claimCodePattern.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil
	}

	claims := make([]relay.ClaimRelayInput, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		normalized := canonicalClaimCode(match)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}

		claims = append(claims, relay.ClaimRelayInput{
			ClaimerUserID: update.Message.From.ID,
			ClaimerChatID: update.Message.Chat.ID,
			RawCode:       match,
		})
	}

	if len(claims) == 1 {
		claims[0].RequestUpdateID = int64(update.ID)
		return claims
	}

	for index := range claims {
		claims[index].RequestUpdateID = claimRequestUpdateID(int64(update.ID), index)
	}

	return claims
}

func claimContent(message *models.Message) string {
	parts := make([]string, 0, 2)
	if text := strings.TrimSpace(message.Text); text != "" {
		parts = append(parts, text)
	}
	if caption := strings.TrimSpace(message.Caption); caption != "" {
		parts = append(parts, caption)
	}
	return strings.Join(parts, "\n")
}

func canonicalClaimCode(raw string) string {
	candidate := strings.ToUpper(strings.TrimSpace(raw))
	if candidate == "" {
		return ""
	}

	candidate = strings.ReplaceAll(candidate, "-", "")
	candidate = strings.ReplaceAll(candidate, " ", "")
	candidate = strings.ReplaceAll(candidate, "_", "")
	if strings.HasPrefix(candidate, "RELAYBOT") && !strings.HasPrefix(candidate, "RELAYBOT_") {
		candidate = "RELAYBOT_" + strings.TrimPrefix(candidate, "RELAYBOT")
	}
	return candidate
}

func claimRequestUpdateID(updateID int64, sequence int) int64 {
	return (updateID << claimRequestSequenceBits) | int64(sequence+1)
}
