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

	if ttl := item.ExpiresAt.Sub(now); ttl > 0 {
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

func (s *Service) StartBatchUpload(ctx context.Context, input StartBatchUploadInput) (StartBatchUploadResult, error) {
	now := s.clock.Now().UTC()
	attrs := batchSessionAttrs(input.UploaderUserID, input.UploaderChatID)
	s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload start requested", attrs...)

	session, ok, err := s.cache.GetBatchUploadSession(ctx, input.UploaderChatID)
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "batch upload start failed", append(attrs, slog.Any("error", err))...)
		return StartBatchUploadResult{}, err
	}
	if ok {
		existing, err := s.store.GetRelayByID(ctx, session.RelayID)
		switch {
		case err == nil && existing.Status == RelayStatusCollecting:
			s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload start rejected",
				append(attrs, slog.String("reason", "active_batch_session"), slog.Int64("relay_id", existing.ID))...,
			)
			return StartBatchUploadResult{}, ErrBatchSessionActive
		case err == nil || errors.Is(err, ErrRelayNotFound):
			_ = s.cache.DeleteBatchUploadSession(ctx, input.UploaderChatID)
		case err != nil:
			s.logger.LogAttrs(ctx, slog.LevelError, "batch upload start failed", append(attrs, slog.Any("error", err))...)
			return StartBatchUploadResult{}, err
		}
	}

	batch, err := s.store.CreateRelayBatch(ctx, CreateRelayBatchParams{
		UploaderUserID: input.UploaderUserID,
		UploaderChatID: input.UploaderChatID,
		CreatedAt:      now,
	})
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "batch upload start failed",
			append(attrs, slog.String("stage", "create_batch"), slog.Any("error", err))...,
		)
		return StartBatchUploadResult{}, err
	}

	session = BatchUploadSession{
		RelayID:        batch.ID,
		UploaderUserID: input.UploaderUserID,
		UploaderChatID: input.UploaderChatID,
		ItemCount:      0,
		StartedAt:      now,
		LastActivityAt: now,
	}
	if err := s.cache.SetBatchUploadSession(ctx, session, s.limits.BatchSessionTTL); err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "batch upload start failed",
			append(attrs, slog.String("stage", "persist_session"), slog.Int64("relay_id", batch.ID), slog.Any("error", err))...,
		)
		_ = s.store.DeleteRelay(ctx, batch.ID)
		return StartBatchUploadResult{}, err
	}

	s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload session started",
		append(attrs, slog.Int64("relay_id", batch.ID))...,
	)
	return StartBatchUploadResult{Relay: batch}, nil
}

func (s *Service) AppendBatchItem(ctx context.Context, input AppendBatchItemInput) (AppendBatchItemResult, error) {
	now := s.clock.Now().UTC()
	attrs := append(batchSessionAttrs(input.UploaderUserID, input.UploaderChatID), createRelayAttrs(CreateRelayInput{
		SourceUpdateID:  input.SourceUpdateID,
		UploaderUserID:  input.UploaderUserID,
		UploaderChatID:  input.UploaderChatID,
		SourceMessageID: input.SourceMessageID,
		MediaKind:       input.MediaKind,
		FileName:        input.FileName,
		FileSizeBytes:   input.FileSizeBytes,
	})...)
	s.logger.LogAttrs(ctx, slog.LevelInfo, "batch item append requested", attrs...)

	session, batch, err := s.getCollectingBatchSession(ctx, input.UploaderChatID)
	if err != nil {
		if errors.Is(err, ErrBatchSessionNotFound) {
			s.logger.LogAttrs(ctx, slog.LevelInfo, "batch item append rejected",
				append(attrs, slog.String("reason", "batch_session_not_found"))...,
			)
		}
		return AppendBatchItemResult{}, err
	}

	if err := s.validateUploadInput(ctx, input.UploaderUserID, input.MediaKind, input.FileName, input.FileSizeBytes, attrs); err != nil {
		return AppendBatchItemResult{}, err
	}

	item, created, err := s.store.AddRelayItem(ctx, AddRelayItemParams{
		RelayID:              batch.ID,
		SourceUpdateID:       input.SourceUpdateID,
		SourceMessageID:      input.SourceMessageID,
		MediaGroupID:         input.MediaGroupID,
		MaxBatchItems:        s.limits.MaxBatchItems,
		ItemOrder:            0,
		MediaKind:            input.MediaKind,
		TelegramFileID:       input.TelegramFileID,
		TelegramFileUniqueID: input.TelegramFileUniqueID,
		FileName:             input.FileName,
		MIMEType:             input.MIMEType,
		FileSizeBytes:        input.FileSizeBytes,
		Caption:              input.Caption,
		CreatedAt:            now,
	})
	if err != nil {
		if errors.Is(err, ErrBatchNotCollecting) {
			s.logger.LogAttrs(ctx, slog.LevelInfo, "batch item append rejected",
				append(attrs, slog.String("reason", "batch_session_not_collecting"), slog.Int64("relay_id", batch.ID))...,
			)
			return AppendBatchItemResult{}, ErrBatchSessionNotFound
		}
		if errors.Is(err, ErrBatchItemLimit) {
			s.logger.LogAttrs(ctx, slog.LevelInfo, "batch item append rejected",
				append(attrs,
					slog.String("reason", "batch_item_limit"),
					slog.Int64("relay_id", batch.ID),
					slog.Int("max_batch_items", s.limits.MaxBatchItems),
				)...,
			)
			return AppendBatchItemResult{}, err
		}
		s.logger.LogAttrs(ctx, slog.LevelError, "batch item append failed",
			append(attrs, slog.String("stage", "persist_batch_item"), slog.Int64("relay_id", batch.ID), slog.Any("error", err))...,
		)
		return AppendBatchItemResult{}, err
	}

	if created {
		session.ItemCount = item.ItemOrder
	} else {
		items, err := s.store.ListRelayItemsByRelayID(ctx, batch.ID)
		if err != nil {
			s.logger.LogAttrs(ctx, slog.LevelError, "batch item append failed",
				append(attrs, slog.String("stage", "list_batch_items"), slog.Int64("relay_id", batch.ID), slog.Any("error", err))...,
			)
			return AppendBatchItemResult{}, err
		}
		session.ItemCount = len(items)
	}
	session.LastActivityAt = now
	mergedSession, err := s.cache.MergeBatchUploadSession(ctx, session, s.limits.BatchSessionTTL)
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "batch item append failed",
			append(attrs, slog.String("stage", "persist_session"), slog.Int64("relay_id", batch.ID), slog.Any("error", err))...,
		)
		return AppendBatchItemResult{}, err
	}
	if mergedSession.RelayID == session.RelayID {
		session = mergedSession
	}

	level := slog.LevelInfo
	message := "batch item appended"
	if !created {
		message = "batch item append deduplicated"
	}
	s.logger.LogAttrs(ctx, level, message,
		append(attrs,
			slog.Int64("relay_id", batch.ID),
			slog.Int64("item_id", item.ID),
			slog.Int("item_count", session.ItemCount),
			slog.Bool("created", created),
		)...,
	)
	return AppendBatchItemResult{
		Relay:      batch,
		Item:       item,
		ItemCount:  session.ItemCount,
		Duplicated: !created,
	}, nil
}

func (s *Service) FinishBatchUpload(ctx context.Context, input FinishBatchUploadInput) (FinishBatchUploadResult, error) {
	now := s.clock.Now().UTC()
	attrs := batchSessionAttrs(input.UploaderUserID, input.UploaderChatID)
	s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload finish requested", attrs...)

	session, ok, err := s.cache.GetBatchUploadSession(ctx, input.UploaderChatID)
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "batch upload finish failed", append(attrs, slog.Any("error", err))...)
		return FinishBatchUploadResult{}, err
	}
	if !ok {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload finish rejected",
			append(attrs, slog.String("reason", "batch_session_not_found"))...,
		)
		return FinishBatchUploadResult{}, ErrBatchSessionNotFound
	}

	batch, err := s.store.GetRelayByID(ctx, session.RelayID)
	if errors.Is(err, ErrRelayNotFound) {
		_ = s.cache.DeleteBatchUploadSession(ctx, input.UploaderChatID)
		s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload finish rejected",
			append(attrs, slog.String("reason", "batch_session_not_found"))...,
		)
		return FinishBatchUploadResult{}, ErrBatchSessionNotFound
	}
	if err != nil {
		return FinishBatchUploadResult{}, err
	}
	if batch.Status == RelayStatusReady {
		items, err := s.store.ListRelayItemsByRelayID(ctx, batch.ID)
		if err != nil {
			s.logger.LogAttrs(ctx, slog.LevelError, "batch upload finish failed",
				append(attrs, slog.String("stage", "list_batch_items"), slog.Int64("relay_id", batch.ID), slog.Any("error", err))...,
			)
			return FinishBatchUploadResult{}, err
		}
		if ttl := batch.ExpiresAt.Sub(now); ttl > 0 {
			if err := s.cache.SetRelayIDByCodeHash(ctx, batch.CodeHash, batch.ID, ttl); err != nil {
				s.logger.LogAttrs(ctx, slog.LevelWarn, "relay cache warm failed",
					append(attrs, slog.Int64("relay_id", batch.ID), slog.String("code_hint", batch.CodeHint), slog.Any("error", err))...,
				)
			}
		}
		if err := s.cache.DeleteBatchUploadSession(ctx, input.UploaderChatID); err != nil {
			s.logger.LogAttrs(ctx, slog.LevelWarn, "batch session cleanup failed",
				append(attrs, slog.Int64("relay_id", batch.ID), slog.Any("error", err))...,
			)
		}
		s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload finish deduplicated",
			append(attrs,
				slog.Int64("relay_id", batch.ID),
				slog.String("code_hint", batch.CodeHint),
				slog.Int("item_count", len(items)),
				slog.Time("expires_at", batch.ExpiresAt),
			)...,
		)
		return FinishBatchUploadResult{
			Relay:     batch,
			Code:      batch.CodeValue,
			ExpiresAt: batch.ExpiresAt,
			ItemCount: len(items),
		}, nil
	}
	if batch.Status != RelayStatusCollecting {
		_ = s.cache.DeleteBatchUploadSession(ctx, input.UploaderChatID)
		s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload finish rejected",
			append(attrs, slog.String("reason", "batch_session_not_found"))...,
		)
		return FinishBatchUploadResult{}, ErrBatchSessionNotFound
	}

	items, err := s.store.ListRelayItemsByRelayID(ctx, batch.ID)
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "batch upload finish failed",
			append(attrs, slog.String("stage", "list_batch_items"), slog.Int64("relay_id", batch.ID), slog.Any("error", err))...,
		)
		return FinishBatchUploadResult{}, err
	}
	if len(items) == 0 {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload finish rejected",
			append(attrs, slog.String("reason", "batch_session_empty"), slog.Int64("relay_id", batch.ID))...,
		)
		return FinishBatchUploadResult{}, ErrBatchSessionEmpty
	}

	count, err := s.store.CountActiveRelaysByUploader(ctx, input.UploaderUserID, now)
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "batch upload finish failed",
			append(attrs, slog.String("stage", "count_active_relays"), slog.Any("error", err))...,
		)
		return FinishBatchUploadResult{}, err
	}

	deduplicated := false
	if count >= s.limits.MaxActiveRelays {
		existing, getErr := s.store.GetRelayByID(ctx, batch.ID)
		switch {
		case getErr == nil && existing.Status == RelayStatusReady:
			batch = existing
			deduplicated = true
		case getErr != nil && !errors.Is(getErr, ErrRelayNotFound):
			s.logger.LogAttrs(ctx, slog.LevelError, "batch upload finish failed",
				append(attrs, slog.String("stage", "recheck_batch_after_limit"), slog.Int64("relay_id", batch.ID), slog.Any("error", getErr))...,
			)
			return FinishBatchUploadResult{}, getErr
		default:
			s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload finish rejected",
				append(attrs,
					slog.String("reason", "too_many_active_relays"),
					slog.Int64("active_relays", count),
					slog.Int64("active_relays_limit", s.limits.MaxActiveRelays),
				)...,
			)
			return FinishBatchUploadResult{}, ErrTooManyRelays
		}
	}

	if !deduplicated {
		displayCode, generatedCodeHash, codeHint, err := s.codes.Generate()
		if err != nil {
			s.logger.LogAttrs(ctx, slog.LevelError, "batch upload finish failed",
				append(attrs, slog.String("stage", "generate_code"), slog.Any("error", err))...,
			)
			return FinishBatchUploadResult{}, err
		}

		expiresAt := now.Add(s.limits.DefaultTTL)
		relayID := batch.ID
		batch, err = s.store.FinalizeRelayBatch(ctx, FinalizeRelayBatchParams{
			RelayID:   relayID,
			CodeValue: displayCode,
			CodeHash:  generatedCodeHash,
			CodeHint:  codeHint,
			ExpiresAt: expiresAt,
			UpdatedAt: now,
		})
		if err != nil {
			s.logger.LogAttrs(ctx, slog.LevelError, "batch upload finish failed",
				append(attrs, slog.String("stage", "finalize_batch"), slog.Int64("relay_id", relayID), slog.Any("error", err))...,
			)
			return FinishBatchUploadResult{}, err
		}
		deduplicated = batch.CodeHash != generatedCodeHash
	}

	if ttl := batch.ExpiresAt.Sub(now); ttl > 0 {
		if err := s.cache.SetRelayIDByCodeHash(ctx, batch.CodeHash, batch.ID, ttl); err != nil {
			s.logger.LogAttrs(ctx, slog.LevelWarn, "relay cache warm failed",
				append(attrs, slog.Int64("relay_id", batch.ID), slog.String("code_hint", batch.CodeHint), slog.Any("error", err))...,
			)
		}
	}
	if err := s.cache.DeleteBatchUploadSession(ctx, input.UploaderChatID); err != nil {
		s.logger.LogAttrs(ctx, slog.LevelWarn, "batch session cleanup failed",
			append(attrs, slog.Int64("relay_id", batch.ID), slog.Any("error", err))...,
		)
	}

	message := "batch upload finished"
	if deduplicated {
		message = "batch upload finish deduplicated"
	}
	s.logger.LogAttrs(ctx, slog.LevelInfo, message,
		append(attrs,
			slog.Int64("relay_id", batch.ID),
			slog.String("code_hint", batch.CodeHint),
			slog.Int("item_count", len(items)),
			slog.Time("expires_at", batch.ExpiresAt),
		)...,
	)
	return FinishBatchUploadResult{
		Relay:     batch,
		Code:      batch.CodeValue,
		ExpiresAt: batch.ExpiresAt,
		ItemCount: len(items),
	}, nil
}

func (s *Service) CancelBatchUpload(ctx context.Context, input CancelBatchUploadInput) (CancelBatchUploadResult, error) {
	attrs := batchSessionAttrs(input.UploaderUserID, input.UploaderChatID)
	s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload cancel requested", attrs...)

	_, batch, err := s.getCollectingBatchSession(ctx, input.UploaderChatID)
	if err != nil {
		if errors.Is(err, ErrBatchSessionNotFound) {
			s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload cancel rejected",
				append(attrs, slog.String("reason", "batch_session_not_found"))...,
			)
		}
		return CancelBatchUploadResult{}, err
	}

	items, err := s.store.ListRelayItemsByRelayID(ctx, batch.ID)
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "batch upload cancel failed",
			append(attrs, slog.String("stage", "list_batch_items"), slog.Int64("relay_id", batch.ID), slog.Any("error", err))...,
		)
		return CancelBatchUploadResult{}, err
	}

	if err := s.store.DeleteRelay(ctx, batch.ID); err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "batch upload cancel failed",
			append(attrs, slog.String("stage", "delete_batch"), slog.Int64("relay_id", batch.ID), slog.Any("error", err))...,
		)
		return CancelBatchUploadResult{}, err
	}
	if err := s.cache.DeleteBatchUploadSession(ctx, input.UploaderChatID); err != nil {
		s.logger.LogAttrs(ctx, slog.LevelWarn, "batch session cleanup failed",
			append(attrs, slog.Int64("relay_id", batch.ID), slog.Any("error", err))...,
		)
	}

	s.logger.LogAttrs(ctx, slog.LevelInfo, "batch upload canceled",
		append(attrs, slog.Int64("relay_id", batch.ID), slog.Int("item_count", len(items)))...,
	)
	return CancelBatchUploadResult{
		RelayID:   batch.ID,
		ItemCount: len(items),
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

	items, err := s.store.ListRelayItemsByRelayID(ctx, item.ID)
	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "relay claim failed",
			append(attrs,
				slog.String("stage", "list_relay_items"),
				slog.Int64("relay_id", item.ID),
				slog.Any("error", err),
			)...,
		)
		return ClaimRelayResult{}, err
	}
	if len(items) == 0 {
		missingItemsErr := errors.New("relay items missing")
		s.logger.LogAttrs(ctx, slog.LevelError, "relay claim failed",
			append(attrs,
				slog.String("stage", "list_relay_items"),
				slog.Int64("relay_id", item.ID),
				slog.Any("error", missingItemsErr),
			)...,
		)
		return ClaimRelayResult{}, missingItemsErr
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

	method, outMessageID, err := s.sender.Deliver(ctx, item, items, input.ClaimerChatID)
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
			if item.Status != RelayStatusReady {
				return Relay{}, ErrRelayNotFound
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
	if item.Status != RelayStatusReady {
		return Relay{}, ErrRelayNotFound
	}

	ttl := item.ExpiresAt.Sub(now)
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

func (s *Service) CleanupExpiredBatchSessions(ctx context.Context) (int64, error) {
	return s.store.DeleteCollectingRelaysBefore(ctx, s.clock.Now().UTC().Add(-s.limits.BatchSessionTTL))
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

func (s *Service) getCollectingBatchSession(ctx context.Context, chatID int64) (BatchUploadSession, Relay, error) {
	session, ok, err := s.cache.GetBatchUploadSession(ctx, chatID)
	if err != nil {
		return BatchUploadSession{}, Relay{}, err
	}
	if !ok {
		return BatchUploadSession{}, Relay{}, ErrBatchSessionNotFound
	}

	batch, err := s.store.GetRelayByID(ctx, session.RelayID)
	if errors.Is(err, ErrRelayNotFound) {
		_ = s.cache.DeleteBatchUploadSession(ctx, chatID)
		return BatchUploadSession{}, Relay{}, ErrBatchSessionNotFound
	}
	if err != nil {
		return BatchUploadSession{}, Relay{}, err
	}
	if batch.Status != RelayStatusCollecting {
		_ = s.cache.DeleteBatchUploadSession(ctx, chatID)
		return BatchUploadSession{}, Relay{}, ErrBatchSessionNotFound
	}
	return session, batch, nil
}

func (s *Service) validateUploadInput(ctx context.Context, userID int64, mediaKind MediaKind, fileName string, fileSizeBytes int64, attrs []slog.Attr) error {
	if !isSupportedMedia(mediaKind) {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "relay create rejected", append(attrs, slog.String("reason", "unsupported_media"))...)
		return ErrUnsupportedMedia
	}
	if fileSizeBytes > s.limits.MaxFileBytes {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "relay create rejected",
			append(attrs,
				slog.String("reason", "file_too_large"),
				slog.Int64("file_size_bytes", fileSizeBytes),
				slog.Int64("max_file_bytes", s.limits.MaxFileBytes),
			)...,
		)
		return ErrFileTooLarge
	}
	if s.isForbiddenExtension(fileName) {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "relay create rejected",
			append(attrs,
				slog.String("reason", "forbidden_extension"),
				slog.String("file_ext", strings.ToLower(path.Ext(fileName))),
			)...,
		)
		return ErrForbiddenExtension
	}
	if allowed, err := s.cache.AllowUpload(ctx, userID); err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, "relay create failed",
			append(attrs,
				slog.String("stage", "allow_upload"),
				slog.Any("error", err),
			)...,
		)
		return err
	} else if !allowed {
		s.logger.LogAttrs(ctx, slog.LevelInfo, "relay create rejected",
			append(attrs, slog.String("reason", "upload_rate_limited"))...,
		)
		return ErrUploadRateLimited
	}
	return nil
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

func batchSessionAttrs(userID, chatID int64) []slog.Attr {
	return []slog.Attr{
		slog.Int64("uploader_user_id", userID),
		slog.Int64("uploader_chat_id", chatID),
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
