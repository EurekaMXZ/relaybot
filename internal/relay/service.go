package relay

import (
	"context"
	"errors"
	"log/slog"
	"path"
	"strings"
	"time"
)

type Service struct {
	store  Store
	cache  Cache
	sender Sender
	codes  CodeManager
	clock  Clock
	logger *slog.Logger
	limits Limits
}

func NewService(store Store, cache Cache, sender Sender, codes CodeManager, clock Clock, limits Limits) *Service {
	if cache == nil {
		panic("relay cache is required")
	}
	if clock == nil {
		clock = systemClock{}
	}
	return &Service{
		store:  store,
		cache:  cache,
		sender: sender,
		codes:  codes,
		clock:  clock,
		logger: slog.Default().With("component", "relay_service"),
		limits: limits,
	}
}

func (s *Service) CreateRelay(ctx context.Context, input CreateRelayInput) (CreateRelayResult, error) {
	now := s.clock.Now().UTC()
	attrs := createRelayAttrs(input)
	s.logger.LogAttrs(ctx, slog.LevelInfo, "relay create requested", attrs...)

	if !isSupportedMedia(input.MediaKind) {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "relay create rejected", append(attrs, slog.String("reason", "unsupported_media"))...)
		return CreateRelayResult{}, ErrUnsupportedMedia
	}
	if input.FileSizeBytes > s.limits.MaxFileBytes {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "relay create rejected",
			append(attrs,
				slog.String("reason", "file_too_large"),
				slog.Int64("file_size_bytes", input.FileSizeBytes),
				slog.Int64("max_file_bytes", s.limits.MaxFileBytes),
			)...,
		)
		return CreateRelayResult{}, ErrFileTooLarge
	}
	if s.isForbiddenExtension(input.FileName) {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "relay create rejected",
			append(attrs,
				slog.String("reason", "forbidden_extension"),
				slog.String("file_ext", strings.ToLower(path.Ext(input.FileName))),
			)...,
		)
		return CreateRelayResult{}, ErrForbiddenExtension
	}

	if input.SourceUpdateID != 0 {
		existing, err := s.store.GetRelayBySourceUpdateID(ctx, input.SourceUpdateID)
		switch {
		case err == nil:
			s.logger.LogAttrs(ctx, slog.LevelInfo, "relay create deduplicated",
				append(attrs,
					slog.Int64("relay_id", existing.ID),
					slog.String("code_hint", existing.CodeHint),
					slog.Time("expires_at", existing.ExpiresAt),
				)...,
			)
			return CreateRelayResult{
				Relay:      existing,
				Code:       existing.CodeValue,
				ExpiresAt:  existing.ExpiresAt,
				Duplicated: true,
			}, nil
		case err != nil && !errors.Is(err, ErrRelayNotFound):
			s.logger.LogAttrs(ctx, slog.LevelError, "relay create failed",
				append(attrs,
					slog.String("stage", "lookup_source_update"),
					slog.Any("error", err),
				)...,
			)
			return CreateRelayResult{}, err
		}
	}

	if allowed, err := s.cache.AllowUpload(ctx, input.UploaderUserID); err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "relay create failed",
			append(attrs,
				slog.String("stage", "allow_upload"),
				slog.Any("error", err),
			)...,
		)
		return CreateRelayResult{}, err
	} else if !allowed {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "relay create rejected",
			append(attrs, slog.String("reason", "upload_rate_limited"))...,
		)
		return CreateRelayResult{}, ErrUploadRateLimited
	}

	count, err := s.store.CountActiveRelaysByUploader(ctx, input.UploaderUserID, now)
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "relay create failed",
			append(attrs,
				slog.String("stage", "count_active_relays"),
				slog.Any("error", err),
			)...,
		)
		return CreateRelayResult{}, err
	}
	if count >= s.limits.MaxActiveRelays {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "relay create rejected",
			append(attrs,
				slog.String("reason", "too_many_active_relays"),
				slog.Int64("active_relays", count),
				slog.Int64("active_relays_limit", s.limits.MaxActiveRelays),
			)...,
		)
		return CreateRelayResult{}, ErrTooManyRelays
	}

	displayCode, codeHash, codeHint, err := s.codes.Generate()
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "relay create failed",
			append(attrs,
				slog.String("stage", "generate_code"),
				slog.Any("error", err),
			)...,
		)
		return CreateRelayResult{}, err
	}

	expiresAt := now.Add(s.limits.DefaultTTL)
	item, created, err := s.store.CreateRelay(ctx, CreateRelayParams{
		SourceUpdateID:       input.SourceUpdateID,
		CodeValue:            displayCode,
		CodeHash:             codeHash,
		CodeHint:             codeHint,
		UploaderUserID:       input.UploaderUserID,
		UploaderChatID:       input.UploaderChatID,
		SourceMessageID:      input.SourceMessageID,
		MediaKind:            input.MediaKind,
		TelegramFileID:       input.TelegramFileID,
		TelegramFileUniqueID: input.TelegramFileUniqueID,
		FileName:             input.FileName,
		MIMEType:             input.MIMEType,
		FileSizeBytes:        input.FileSizeBytes,
		Caption:              input.Caption,
		ExpiresAt:            expiresAt,
		CreatedAt:            now,
	})
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "relay create failed",
			append(attrs,
				slog.String("stage", "persist_relay"),
				slog.String("code_hint", codeHint),
				slog.Any("error", err),
			)...,
		)
		return CreateRelayResult{}, err
	}

	if ttl := time.Until(item.ExpiresAt); ttl > 0 {
		if err := s.cache.SetRelayIDByCodeHash(ctx, item.CodeHash, item.ID, ttl); err != nil {
			s.logger.LogAttrs(ctx, slog.LevelWarn, "relay cache warm failed",
				append(attrs,
					slog.Int64("relay_id", item.ID),
					slog.String("code_hint", item.CodeHint),
					slog.Any("error", err),
				)...,
			)
		}
	}

	level := slog.LevelInfo
	message := "relay created"
	if !created {
		message = "relay create returned existing row"
	}
	s.logger.LogAttrs(ctx, level, message,
		append(attrs,
			slog.Int64("relay_id", item.ID),
			slog.String("code_hint", item.CodeHint),
			slog.Time("expires_at", item.ExpiresAt),
			slog.Bool("created", created),
		)...,
	)

	return CreateRelayResult{
		Relay:      item,
		Code:       item.CodeValue,
		ExpiresAt:  item.ExpiresAt,
		Duplicated: !created,
	}, nil
}

func (s *Service) ClaimRelay(ctx context.Context, input ClaimRelayInput) (ClaimRelayResult, error) {
	now := s.clock.Now().UTC()
	attrs := claimRelayAttrs(input)
	s.logger.LogAttrs(ctx, slog.LevelInfo, "relay claim requested", attrs...)

	if allowed, err := s.cache.AllowClaim(ctx, input.ClaimerUserID); err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "relay claim failed",
			append(attrs,
				slog.String("stage", "allow_claim"),
				slog.Any("error", err),
			)...,
		)
		return ClaimRelayResult{}, err
	} else if !allowed {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "relay claim rejected",
			append(attrs, slog.String("reason", "claim_rate_limited"))...,
		)
		return ClaimRelayResult{}, ErrClaimRateLimited
	}

	normalized, err := s.codes.Normalize(input.RawCode)
	if err != nil {
		return ClaimRelayResult{}, s.badCode(ctx, input, ErrInvalidCode)
	}

	item, err := s.lookupRelay(ctx, s.codes.Hash(normalized), now)
	if err != nil {
		if errors.Is(err, ErrRelayNotFound) {
			return ClaimRelayResult{}, s.badCode(ctx, input, err)
		}
		if errors.Is(err, ErrRelayExpired) {
			s.logger.LogAttrs(ctx, slog.LevelInfo, "relay claim rejected",
				append(attrs, slog.String("reason", "relay_expired"))...,
			)
			return ClaimRelayResult{}, err
		}
		s.logger.LogAttrs(ctx, slog.LevelError, "relay claim failed",
			append(attrs,
				slog.String("stage", "lookup_relay"),
				slog.Any("error", err),
			)...,
		)
		return ClaimRelayResult{}, err
	}

	delivery, created, err := s.store.CreateDelivery(ctx, CreateDeliveryParams{
		RelayID:         item.ID,
		RequestUpdateID: input.RequestUpdateID,
		ClaimerUserID:   input.ClaimerUserID,
		ClaimerChatID:   input.ClaimerChatID,
		CreatedAt:       now,
	})
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "relay claim failed",
			append(attrs,
				slog.String("stage", "create_delivery"),
				slog.Int64("relay_id", item.ID),
				slog.String("code_hint", item.CodeHint),
				slog.Any("error", err),
			)...,
		)
		return ClaimRelayResult{}, err
	}

	if !created {
		switch delivery.Status {
		case DeliveryStatusSent:
			s.logger.LogAttrs(ctx, slog.LevelInfo, "relay claim deduplicated",
				append(attrs,
					slog.Int64("relay_id", item.ID),
					slog.String("code_hint", item.CodeHint),
					slog.Int64("delivery_id", delivery.ID),
					slog.String("delivery_status", string(delivery.Status)),
					slog.String("delivery_method", string(delivery.Method)),
				)...,
			)
			result := ClaimRelayResult{
				Relay:      item,
				Delivery:   delivery,
				Method:     delivery.Method,
				Duplicated: true,
			}
			if delivery.TelegramOutMessageID != nil {
				result.OutMessageID = *delivery.TelegramOutMessageID
			}
			return result, nil
		case DeliveryStatusPending:
			s.logger.LogAttrs(ctx, slog.LevelInfo, "relay claim already in progress",
				append(attrs,
					slog.Int64("relay_id", item.ID),
					slog.String("code_hint", item.CodeHint),
					slog.Int64("delivery_id", delivery.ID),
					slog.String("delivery_status", string(delivery.Status)),
				)...,
			)
			return ClaimRelayResult{Relay: item, Delivery: delivery, Duplicated: true}, ErrDeliveryInProgress
		default:
			s.logger.LogAttrs(ctx, slog.LevelWarn, "relay claim blocked by previous delivery state",
				append(attrs,
					slog.Int64("relay_id", item.ID),
					slog.String("code_hint", item.CodeHint),
					slog.Int64("delivery_id", delivery.ID),
					slog.String("delivery_status", string(delivery.Status)),
				)...,
			)
			return ClaimRelayResult{Relay: item, Delivery: delivery, Duplicated: true}, ErrDeliveryFailed
		}
	}

	s.logger.LogAttrs(ctx, slog.LevelInfo, "relay delivery started",
		append(attrs,
			slog.Int64("relay_id", item.ID),
			slog.String("code_hint", item.CodeHint),
			slog.Int64("delivery_id", delivery.ID),
		)...,
	)

	method, outMessageID, err := s.sender.CopyOrResend(ctx, item, input.ClaimerChatID)
	if err != nil {
		var senderErr SenderError
		switch {
		case errors.As(err, &senderErr) && senderErr.UnknownResult():
			if markErr := s.store.MarkDeliveryUnknown(ctx, MarkDeliveryUnknownParams{
				DeliveryID: delivery.ID,
				ErrorCode:  senderErr.Code(),
				ErrorDesc:  senderErr.Description(),
				UpdatedAt:  now,
			}); markErr != nil {
				s.logger.LogAttrs(ctx, slog.LevelError, "relay delivery mark unknown failed",
					append(attrs,
						slog.Int64("relay_id", item.ID),
						slog.String("code_hint", item.CodeHint),
						slog.Int64("delivery_id", delivery.ID),
						slog.String("error_code", senderErr.Code()),
						slog.Any("error", markErr),
					)...,
				)
				return ClaimRelayResult{}, markErr
			}
			s.logger.LogAttrs(ctx, slog.LevelWarn, "relay delivery result unknown",
				append(attrs,
					slog.Int64("relay_id", item.ID),
					slog.String("code_hint", item.CodeHint),
					slog.Int64("delivery_id", delivery.ID),
					slog.String("error_code", senderErr.Code()),
					slog.String("error_desc", senderErr.Description()),
				)...,
			)
		case errors.As(err, &senderErr):
			if markErr := s.store.MarkDeliveryFailed(ctx, MarkDeliveryFailedParams{
				DeliveryID: delivery.ID,
				ErrorCode:  senderErr.Code(),
				ErrorDesc:  senderErr.Description(),
				UpdatedAt:  now,
			}); markErr != nil {
				s.logger.LogAttrs(ctx, slog.LevelError, "relay delivery mark failed failed",
					append(attrs,
						slog.Int64("relay_id", item.ID),
						slog.String("code_hint", item.CodeHint),
						slog.Int64("delivery_id", delivery.ID),
						slog.String("error_code", senderErr.Code()),
						slog.Any("error", markErr),
					)...,
				)
				return ClaimRelayResult{}, markErr
			}
			s.logger.LogAttrs(ctx, slog.LevelWarn, "relay delivery failed",
				append(attrs,
					slog.Int64("relay_id", item.ID),
					slog.String("code_hint", item.CodeHint),
					slog.Int64("delivery_id", delivery.ID),
					slog.String("error_code", senderErr.Code()),
					slog.String("error_desc", senderErr.Description()),
				)...,
			)
		default:
			if markErr := s.store.MarkDeliveryFailed(ctx, MarkDeliveryFailedParams{
				DeliveryID: delivery.ID,
				ErrorCode:  "delivery_failed",
				ErrorDesc:  err.Error(),
				UpdatedAt:  now,
			}); markErr != nil {
				s.logger.LogAttrs(ctx, slog.LevelError, "relay delivery mark failed failed",
					append(attrs,
						slog.Int64("relay_id", item.ID),
						slog.String("code_hint", item.CodeHint),
						slog.Int64("delivery_id", delivery.ID),
						slog.String("error_code", "delivery_failed"),
						slog.Any("error", markErr),
					)...,
				)
				return ClaimRelayResult{}, markErr
			}
			s.logger.LogAttrs(ctx, slog.LevelWarn, "relay delivery failed",
				append(attrs,
					slog.Int64("relay_id", item.ID),
					slog.String("code_hint", item.CodeHint),
					slog.Int64("delivery_id", delivery.ID),
					slog.String("error_code", "delivery_failed"),
					slog.Any("error", err),
				)...,
			)
		}
		return ClaimRelayResult{Relay: item, Delivery: delivery}, err
	}

	if err := s.store.MarkDeliverySent(ctx, MarkDeliverySentParams{
		DeliveryID:    delivery.ID,
		RelayID:       item.ID,
		Method:        method,
		OutMessageID:  outMessageID,
		SentAt:        now,
		LastClaimedAt: now,
	}); err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "relay delivery mark sent failed",
			append(attrs,
				slog.Int64("relay_id", item.ID),
				slog.String("code_hint", item.CodeHint),
				slog.Int64("delivery_id", delivery.ID),
				slog.String("delivery_method", string(method)),
				slog.Int("out_message_id", outMessageID),
				slog.Any("error", err),
			)...,
		)
		return ClaimRelayResult{}, err
	}

	delivery.Status = DeliveryStatusSent
	delivery.Method = method
	delivery.TelegramOutMessageID = &outMessageID
	delivery.SentAt = &now

	s.logger.LogAttrs(ctx, slog.LevelInfo, "relay delivered",
		append(attrs,
			slog.Int64("relay_id", item.ID),
			slog.String("code_hint", item.CodeHint),
			slog.Int64("delivery_id", delivery.ID),
			slog.String("delivery_method", string(method)),
			slog.Int("out_message_id", outMessageID),
		)...,
	)

	return ClaimRelayResult{
		Relay:        item,
		Delivery:     delivery,
		OutMessageID: outMessageID,
		Method:       method,
	}, nil
}

func (s *Service) lookupRelay(ctx context.Context, codeHash string, now time.Time) (Relay, error) {
	if relayID, ok, err := s.cache.GetRelayIDByCodeHash(ctx, codeHash); err == nil && ok {
		s.logger.LogAttrs(ctx, slog.LevelDebug, "relay cache hit",
			slog.Int64("relay_id", relayID),
		)
		item, err := s.store.GetRelayByID(ctx, relayID)
		if err == nil {
			if item.Status == RelayStatusExpired || !item.ExpiresAt.After(now) {
				s.logger.LogAttrs(ctx, slog.LevelInfo, "relay lookup found expired relay",
					slog.Int64("relay_id", item.ID),
					slog.String("code_hint", item.CodeHint),
					slog.String("lookup_source", "cache"),
				)
				return Relay{}, ErrRelayExpired
			}
			return item, nil
		}
		if !errors.Is(err, ErrRelayNotFound) {
			s.logger.LogAttrs(ctx, slog.LevelWarn, "relay cache lookup failed",
				slog.Int64("relay_id", relayID),
				slog.Any("error", err),
			)
			return Relay{}, err
		}
	} else if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelWarn, "relay cache lookup failed",
			slog.Any("error", err),
		)
	}

	item, err := s.store.GetRelayByCodeHash(ctx, codeHash, now)
	if err != nil {
		if !errors.Is(err, ErrRelayNotFound) {
			s.logger.LogAttrs(ctx, slog.LevelError, "relay lookup failed", slog.Any("error", err))
		}
		return Relay{}, err
	}
	if item.Status == RelayStatusExpired || !item.ExpiresAt.After(now) {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "relay lookup found expired relay",
			slog.Int64("relay_id", item.ID),
			slog.String("code_hint", item.CodeHint),
			slog.String("lookup_source", "store"),
		)
		return Relay{}, ErrRelayExpired
	}

	ttl := time.Until(item.ExpiresAt)
	if ttl > 0 {
		if err := s.cache.SetRelayIDByCodeHash(ctx, codeHash, item.ID, ttl); err != nil {
			s.logger.LogAttrs(ctx, slog.LevelWarn, "relay cache warm failed",
				slog.Int64("relay_id", item.ID),
				slog.String("code_hint", item.CodeHint),
				slog.Any("error", err),
			)
		}
	}
	return item, nil
}

func (s *Service) MarkSeenUpdate(ctx context.Context, updateID int64) (bool, error) {
	return s.cache.MarkSeenUpdate(ctx, updateID)
}

func (s *Service) ExpireReadyRelays(ctx context.Context) (int64, error) {
	return s.store.ExpireRelays(ctx, s.clock.Now().UTC())
}

func (s *Service) MarkUnknownDeliveries(ctx context.Context) (int64, error) {
	return s.store.MarkUnknownDeliveriesBefore(ctx, s.clock.Now().UTC().Add(-s.limits.UnknownDeliveryAfter))
}

func (s *Service) PurgeExpiredDeliveries(ctx context.Context) (int64, error) {
	return s.store.DeleteExpiredDeliveriesBefore(ctx, s.clock.Now().UTC().Add(-s.limits.ExpiredDeliveryPurge))
}

func (s *Service) isForbiddenExtension(fileName string) bool {
	if fileName == "" || len(s.limits.ForbiddenExtensions) == 0 {
		return false
	}
	ext := strings.ToLower(path.Ext(fileName))
	if ext == "" {
		return false
	}
	_, blocked := s.limits.ForbiddenExtensions[ext]
	return blocked
}

func (s *Service) badCode(ctx context.Context, input ClaimRelayInput, err error) error {
	attrs := claimRelayAttrs(input)
	allowed, cacheErr := s.cache.AllowBadCode(ctx, input.ClaimerUserID)
	if cacheErr != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "bad code rate-limit check failed",
			append(attrs, slog.Any("error", cacheErr))...,
		)
		return cacheErr
	}
	if !allowed {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "bad code rate limited",
			append(attrs, slog.String("reason", "bad_code_rate_limited"))...,
		)
		return ErrBadCodeRateLimit
	}
	s.logger.LogAttrs(ctx, slog.LevelInfo, "bad code received",
		append(attrs, slog.String("reason", badCodeReason(err)))...,
	)
	return err
}

func createRelayAttrs(input CreateRelayInput) []slog.Attr {
	return []slog.Attr{
		slog.Int64("source_update_id", input.SourceUpdateID),
		slog.Int64("uploader_user_id", input.UploaderUserID),
		slog.Int64("uploader_chat_id", input.UploaderChatID),
		slog.Int("source_message_id", input.SourceMessageID),
		slog.String("media_kind", string(input.MediaKind)),
		slog.Int64("file_size_bytes", input.FileSizeBytes),
	}
}

func claimRelayAttrs(input ClaimRelayInput) []slog.Attr {
	return []slog.Attr{
		slog.Int64("request_update_id", input.RequestUpdateID),
		slog.Int64("claimer_user_id", input.ClaimerUserID),
		slog.Int64("claimer_chat_id", input.ClaimerChatID),
		slog.String("code_hint", claimCodeHint(input.RawCode)),
	}
}

func badCodeReason(err error) string {
	switch {
	case errors.Is(err, ErrInvalidCode):
		return "invalid_code"
	case errors.Is(err, ErrRelayNotFound):
		return "relay_not_found"
	default:
		return "bad_code"
	}
}

func isSupportedMedia(kind MediaKind) bool {
	switch kind {
	case MediaKindDocument, MediaKindPhoto, MediaKindVideo, MediaKindAudio, MediaKindVoice:
		return true
	default:
		return false
	}
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

func claimCodeHint(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return ""
	}
	if strings.HasPrefix(normalized, "relaybot_") {
		suffix := strings.TrimPrefix(normalized, "relaybot_")
		switch {
		case suffix == "":
			return "relaybot_"
		case len(suffix) <= 2:
			return "relaybot_" + suffix[:1] + "..."
		default:
			return "relaybot_" + suffix[:2] + "..."
		}
	}
	if len(normalized) <= 4 {
		return normalized[:1] + "..."
	}
	return normalized[:4] + "..."
}
