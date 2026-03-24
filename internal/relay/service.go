package relay

import (
	"context"
	"errors"
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
		limits: limits,
	}
}

func (s *Service) CreateRelay(ctx context.Context, input CreateRelayInput) (CreateRelayResult, error) {
	now := s.clock.Now().UTC()

	if !isSupportedMedia(input.MediaKind) {
		return CreateRelayResult{}, ErrUnsupportedMedia
	}
	if input.FileSizeBytes > s.limits.MaxFileBytes {
		return CreateRelayResult{}, ErrFileTooLarge
	}
	if s.isForbiddenExtension(input.FileName) {
		return CreateRelayResult{}, ErrForbiddenExtension
	}

	if input.SourceUpdateID != 0 {
		existing, err := s.store.GetRelayBySourceUpdateID(ctx, input.SourceUpdateID)
		switch {
		case err == nil:
			return CreateRelayResult{
				Relay:      existing,
				Code:       existing.CodeValue,
				ExpiresAt:  existing.ExpiresAt,
				Duplicated: true,
			}, nil
		case err != nil && !errors.Is(err, ErrRelayNotFound):
			return CreateRelayResult{}, err
		}
	}

	if allowed, err := s.cache.AllowUpload(ctx, input.UploaderUserID); err != nil {
		return CreateRelayResult{}, err
	} else if !allowed {
		return CreateRelayResult{}, ErrUploadRateLimited
	}

	count, err := s.store.CountActiveRelaysByUploader(ctx, input.UploaderUserID, now)
	if err != nil {
		return CreateRelayResult{}, err
	}
	if count >= s.limits.MaxActiveRelays {
		return CreateRelayResult{}, ErrTooManyRelays
	}

	displayCode, codeHash, codeHint, err := s.codes.Generate()
	if err != nil {
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
		return CreateRelayResult{}, err
	}

	if ttl := time.Until(item.ExpiresAt); ttl > 0 {
		_ = s.cache.SetRelayIDByCodeHash(ctx, item.CodeHash, item.ID, ttl)
	}

	return CreateRelayResult{
		Relay:      item,
		Code:       item.CodeValue,
		ExpiresAt:  item.ExpiresAt,
		Duplicated: !created,
	}, nil
}

func (s *Service) ClaimRelay(ctx context.Context, input ClaimRelayInput) (ClaimRelayResult, error) {
	now := s.clock.Now().UTC()

	if allowed, err := s.cache.AllowClaim(ctx, input.ClaimerUserID); err != nil {
		return ClaimRelayResult{}, err
	} else if !allowed {
		return ClaimRelayResult{}, ErrClaimRateLimited
	}

	normalized, err := s.codes.Normalize(input.RawCode)
	if err != nil {
		return ClaimRelayResult{}, s.badCode(ctx, input.ClaimerUserID, ErrInvalidCode)
	}

	item, err := s.lookupRelay(ctx, s.codes.Hash(normalized), now)
	if err != nil {
		if errors.Is(err, ErrRelayNotFound) {
			return ClaimRelayResult{}, s.badCode(ctx, input.ClaimerUserID, err)
		}
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
		return ClaimRelayResult{}, err
	}

	if !created {
		switch delivery.Status {
		case DeliveryStatusSent:
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
			return ClaimRelayResult{Relay: item, Delivery: delivery, Duplicated: true}, ErrDeliveryInProgress
		default:
			return ClaimRelayResult{Relay: item, Delivery: delivery, Duplicated: true}, ErrDeliveryFailed
		}
	}

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
				return ClaimRelayResult{}, markErr
			}
		case errors.As(err, &senderErr):
			if markErr := s.store.MarkDeliveryFailed(ctx, MarkDeliveryFailedParams{
				DeliveryID: delivery.ID,
				ErrorCode:  senderErr.Code(),
				ErrorDesc:  senderErr.Description(),
				UpdatedAt:  now,
			}); markErr != nil {
				return ClaimRelayResult{}, markErr
			}
		default:
			if markErr := s.store.MarkDeliveryFailed(ctx, MarkDeliveryFailedParams{
				DeliveryID: delivery.ID,
				ErrorCode:  "delivery_failed",
				ErrorDesc:  err.Error(),
				UpdatedAt:  now,
			}); markErr != nil {
				return ClaimRelayResult{}, markErr
			}
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
		return ClaimRelayResult{}, err
	}

	delivery.Status = DeliveryStatusSent
	delivery.Method = method
	delivery.TelegramOutMessageID = &outMessageID
	delivery.SentAt = &now

	return ClaimRelayResult{
		Relay:        item,
		Delivery:     delivery,
		OutMessageID: outMessageID,
		Method:       method,
	}, nil
}

func (s *Service) lookupRelay(ctx context.Context, codeHash string, now time.Time) (Relay, error) {
	if relayID, ok, err := s.cache.GetRelayIDByCodeHash(ctx, codeHash); err == nil && ok {
		item, err := s.store.GetRelayByID(ctx, relayID)
		if err == nil {
			if item.Status == RelayStatusExpired || !item.ExpiresAt.After(now) {
				return Relay{}, ErrRelayExpired
			}
			return item, nil
		}
		if !errors.Is(err, ErrRelayNotFound) {
			return Relay{}, err
		}
	}

	item, err := s.store.GetRelayByCodeHash(ctx, codeHash, now)
	if err != nil {
		return Relay{}, err
	}
	if item.Status == RelayStatusExpired || !item.ExpiresAt.After(now) {
		return Relay{}, ErrRelayExpired
	}

	ttl := time.Until(item.ExpiresAt)
	if ttl > 0 {
		_ = s.cache.SetRelayIDByCodeHash(ctx, codeHash, item.ID, ttl)
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

func (s *Service) badCode(ctx context.Context, userID int64, err error) error {
	allowed, cacheErr := s.cache.AllowBadCode(ctx, userID)
	if cacheErr != nil {
		return cacheErr
	}
	if !allowed {
		return ErrBadCodeRateLimit
	}
	return err
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
