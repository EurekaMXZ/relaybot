package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"relaybot/internal/relay"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) CreateRelay(ctx context.Context, params relay.CreateRelayParams) (relay.Relay, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return relay.Relay{}, false, err
	}
	defer tx.Rollback(ctx)

	record, err := scanRelay(tx.QueryRow(ctx, `
		INSERT INTO relays (
			source_update_id, code_value, code_hash, code_hint, status,
			uploader_user_id, uploader_chat_id, expires_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, 'ready',
			$5, $6, $7, $8, $8
		)
		ON CONFLICT (source_update_id) DO NOTHING
		RETURNING
			id, COALESCE(source_update_id, 0), code_value, COALESCE(code_hash, ''), code_hint, status,
			uploader_user_id, uploader_chat_id, delivery_count, last_claimed_at, expires_at, created_at, updated_at
	`,
		params.SourceUpdateID,
		params.CodeValue,
		params.CodeHash,
		params.CodeHint,
		params.UploaderUserID,
		params.UploaderChatID,
		params.ExpiresAt,
		params.CreatedAt,
	))
	if err == nil {
		if _, _, err := s.addRelayItemTx(ctx, tx, relay.AddRelayItemParams{
			RelayID:              record.ID,
			SourceUpdateID:       params.SourceUpdateID,
			SourceMessageID:      params.SourceMessageID,
			ItemOrder:            1,
			MediaKind:            params.MediaKind,
			TelegramFileID:       params.TelegramFileID,
			TelegramFileUniqueID: params.TelegramFileUniqueID,
			FileName:             params.FileName,
			MIMEType:             params.MIMEType,
			FileSizeBytes:        params.FileSizeBytes,
			Caption:              params.Caption,
			CreatedAt:            params.CreatedAt,
		}, true); err != nil {
			return relay.Relay{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return relay.Relay{}, false, err
		}
		return record, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return relay.Relay{}, false, err
	}

	record, err = s.GetRelayBySourceUpdateID(ctx, params.SourceUpdateID)
	if err != nil {
		return relay.Relay{}, false, err
	}
	return record, false, nil
}

func (s *Store) CreateRelayBatch(ctx context.Context, params relay.CreateRelayBatchParams) (relay.Relay, error) {
	return scanRelay(s.pool.QueryRow(ctx, `
		INSERT INTO relays (
			code_value, code_hash, code_hint, status,
			uploader_user_id, uploader_chat_id, expires_at, created_at, updated_at
		) VALUES (
			'', NULL, '', 'collecting',
			$1, $2, $3, $3, $3
		)
		RETURNING
			id, COALESCE(source_update_id, 0), code_value, COALESCE(code_hash, ''), code_hint, status,
			uploader_user_id, uploader_chat_id, delivery_count, last_claimed_at, expires_at, created_at, updated_at
	`, params.UploaderUserID, params.UploaderChatID, params.CreatedAt))
}

func (s *Store) AddRelayItem(ctx context.Context, params relay.AddRelayItemParams) (relay.RelayItem, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return relay.RelayItem{}, false, err
	}
	defer tx.Rollback(ctx)

	item, created, err := s.addRelayItemTx(ctx, tx, params, false)
	if err != nil {
		return relay.RelayItem{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return relay.RelayItem{}, false, err
	}
	return item, created, nil
}

func (s *Store) addRelayItemTx(ctx context.Context, tx pgx.Tx, params relay.AddRelayItemParams, allowReady bool) (relay.RelayItem, bool, error) {
	if err := s.lockRelayForItemInsert(ctx, tx, params.RelayID, allowReady); err != nil {
		return relay.RelayItem{}, false, err
	}

	record, err := s.getRelayItemBySourceUpdateID(ctx, tx, params.SourceUpdateID)
	switch {
	case err == nil:
		return record, false, nil
	case err != nil && !errors.Is(err, relay.ErrRelayNotFound):
		return relay.RelayItem{}, false, err
	}

	if params.MaxBatchItems > 0 {
		var itemCount int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM relay_items
			WHERE relay_id = $1
		`, params.RelayID).Scan(&itemCount); err != nil {
			return relay.RelayItem{}, false, err
		}
		if itemCount >= params.MaxBatchItems {
			return relay.RelayItem{}, false, relay.ErrBatchItemLimit
		}
	}

	if params.ItemOrder == 0 {
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(item_order), 0) + 1
			FROM relay_items
			WHERE relay_id = $1
		`, params.RelayID).Scan(&params.ItemOrder); err != nil {
			return relay.RelayItem{}, false, err
		}
	}

	record, err = scanRelayItem(tx.QueryRow(ctx, `
		INSERT INTO relay_items (
			relay_id, source_update_id, source_message_id, media_group_id, item_order,
			media_kind, telegram_file_id, telegram_file_unique_id, file_name, mime_type,
			file_size_bytes, caption, created_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13
		)
		ON CONFLICT (source_update_id) DO NOTHING
		RETURNING
			id, relay_id, source_update_id, source_message_id, media_group_id, item_order,
			media_kind, telegram_file_id, telegram_file_unique_id, file_name, mime_type,
			file_size_bytes, caption, created_at
	`,
		params.RelayID,
		params.SourceUpdateID,
		params.SourceMessageID,
		params.MediaGroupID,
		params.ItemOrder,
		params.MediaKind,
		params.TelegramFileID,
		params.TelegramFileUniqueID,
		params.FileName,
		params.MIMEType,
		params.FileSizeBytes,
		params.Caption,
		params.CreatedAt,
	))
	if err == nil {
		if _, err := tx.Exec(ctx, `
			UPDATE relays
			SET updated_at = $2
			WHERE id = $1
		`, params.RelayID, params.CreatedAt); err != nil {
			return relay.RelayItem{}, false, err
		}
		return record, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return relay.RelayItem{}, false, err
	}

	record, err = s.getRelayItemBySourceUpdateID(ctx, tx, params.SourceUpdateID)
	if err != nil {
		return relay.RelayItem{}, false, err
	}
	return record, false, nil
}

func (s *Store) lockRelayForItemInsert(ctx context.Context, tx pgx.Tx, relayID int64, allowReady bool) error {
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT status
		FROM relays
		WHERE id = $1
		FOR UPDATE
	`, relayID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return relay.ErrRelayNotFound
		}
		return err
	}

	if status == string(relay.RelayStatusCollecting) {
		return nil
	}
	if allowReady && status == string(relay.RelayStatusReady) {
		return nil
	}
	return relay.ErrBatchNotCollecting
}

func (s *Store) ListRelayItemsByRelayID(ctx context.Context, relayID int64) ([]relay.RelayItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id, relay_id, source_update_id, source_message_id, media_group_id, item_order,
			media_kind, telegram_file_id, telegram_file_unique_id, file_name, mime_type,
			file_size_bytes, caption, created_at
		FROM relay_items
		WHERE relay_id = $1
		ORDER BY item_order ASC, id ASC
	`, relayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]relay.RelayItem, 0)
	for rows.Next() {
		item, err := scanRelayItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) FinalizeRelayBatch(ctx context.Context, params relay.FinalizeRelayBatchParams) (relay.Relay, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return relay.Relay{}, err
	}
	defer tx.Rollback(ctx)

	record, err := scanRelay(tx.QueryRow(ctx, `
		UPDATE relays
		SET code_value = $2,
			code_hash = $3,
			code_hint = $4,
			status = 'ready',
			expires_at = $5,
			updated_at = $6
		WHERE id = $1
		  AND status = 'collecting'
		RETURNING
			id, COALESCE(source_update_id, 0), code_value, COALESCE(code_hash, ''), code_hint, status,
			uploader_user_id, uploader_chat_id, delivery_count, last_claimed_at, expires_at, created_at, updated_at
	`, params.RelayID, params.CodeValue, params.CodeHash, params.CodeHint, params.ExpiresAt, params.UpdatedAt))
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return relay.Relay{}, err
		}
		return record, nil
	}
	if !errors.Is(err, relay.ErrRelayNotFound) {
		return relay.Relay{}, err
	}

	record, err = scanRelay(tx.QueryRow(ctx, `
		SELECT
			id, COALESCE(source_update_id, 0), code_value, COALESCE(code_hash, ''), code_hint, status,
			uploader_user_id, uploader_chat_id, delivery_count, last_claimed_at, expires_at, created_at, updated_at
		FROM relays
		WHERE id = $1
		  AND status = 'ready'
	`, params.RelayID))
	if err != nil {
		return relay.Relay{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return relay.Relay{}, err
	}
	return record, nil
}

func (s *Store) DeleteRelay(ctx context.Context, relayID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM relays WHERE id = $1`, relayID)
	return err
}

func (s *Store) GetRelayBySourceUpdateID(ctx context.Context, sourceUpdateID int64) (relay.Relay, error) {
	return scanRelay(s.pool.QueryRow(ctx, `
		SELECT
			r.id, COALESCE(r.source_update_id, 0), r.code_value, COALESCE(r.code_hash, ''), r.code_hint, r.status,
			r.uploader_user_id, r.uploader_chat_id, r.delivery_count, r.last_claimed_at, r.expires_at, r.created_at, r.updated_at
		FROM relay_items i
		JOIN relays r ON r.id = i.relay_id
		WHERE i.source_update_id = $1
	`, sourceUpdateID))
}

func (s *Store) GetRelayByCodeHash(ctx context.Context, codeHash string, now time.Time) (relay.Relay, error) {
	record, err := scanRelay(s.pool.QueryRow(ctx, `
		SELECT
			id, COALESCE(source_update_id, 0), code_value, COALESCE(code_hash, ''), code_hint, status,
			uploader_user_id, uploader_chat_id, delivery_count, last_claimed_at, expires_at, created_at, updated_at
		FROM relays
		WHERE code_hash = $1
	`, codeHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return relay.Relay{}, relay.ErrRelayNotFound
	}
	if err != nil {
		return relay.Relay{}, err
	}
	if record.Status == relay.RelayStatusExpired || !record.ExpiresAt.After(now) {
		return relay.Relay{}, relay.ErrRelayExpired
	}
	return record, nil
}

func (s *Store) GetRelayByID(ctx context.Context, relayID int64) (relay.Relay, error) {
	record, err := scanRelay(s.pool.QueryRow(ctx, `
		SELECT
			id, COALESCE(source_update_id, 0), code_value, COALESCE(code_hash, ''), code_hint, status,
			uploader_user_id, uploader_chat_id, delivery_count, last_claimed_at, expires_at, created_at, updated_at
		FROM relays
		WHERE id = $1
	`, relayID))
	if errors.Is(err, pgx.ErrNoRows) {
		return relay.Relay{}, relay.ErrRelayNotFound
	}
	return record, err
}

func (s *Store) CountActiveRelaysByUploader(ctx context.Context, uploaderUserID int64, now time.Time) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM relays
		WHERE uploader_user_id = $1
		  AND status = 'ready'
		  AND expires_at > $2
	`, uploaderUserID, now).Scan(&count)
	return count, err
}

func (s *Store) CreateDelivery(ctx context.Context, params relay.CreateDeliveryParams) (relay.Delivery, bool, error) {
	record, err := scanDelivery(s.pool.QueryRow(ctx, `
		INSERT INTO relay_deliveries (
			relay_id, request_update_id, claimer_user_id, claimer_chat_id, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'pending', $5, $5)
		ON CONFLICT (request_update_id) DO NOTHING
		RETURNING
			id, relay_id, request_update_id, claimer_user_id, claimer_chat_id, status,
			NULLIF(method, ''), telegram_out_message_id, telegram_error_code, telegram_error_desc,
			created_at, sent_at, updated_at
	`, params.RelayID, params.RequestUpdateID, params.ClaimerUserID, params.ClaimerChatID, params.CreatedAt))
	if err == nil {
		return record, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return relay.Delivery{}, false, err
	}

	record, err = scanDelivery(s.pool.QueryRow(ctx, `
		SELECT
			id, relay_id, request_update_id, claimer_user_id, claimer_chat_id, status,
			NULLIF(method, ''), telegram_out_message_id, telegram_error_code, telegram_error_desc,
			created_at, sent_at, updated_at
		FROM relay_deliveries
		WHERE request_update_id = $1
	`, params.RequestUpdateID))
	if err != nil {
		return relay.Delivery{}, false, err
	}
	return record, false, nil
}

func (s *Store) MarkDeliverySent(ctx context.Context, params relay.MarkDeliverySentParams) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE relay_deliveries
		SET status = 'sent',
			method = $2,
			telegram_out_message_id = $3,
			sent_at = $4,
			updated_at = $4,
			telegram_error_code = '',
			telegram_error_desc = ''
		WHERE id = $1
	`, params.DeliveryID, params.Method, params.OutMessageID, params.SentAt); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE relays
		SET delivery_count = delivery_count + 1,
			last_claimed_at = $2,
			updated_at = $2
		WHERE id = $1
	`, params.RelayID, params.LastClaimedAt); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) MarkDeliveryFailed(ctx context.Context, params relay.MarkDeliveryFailedParams) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE relay_deliveries
		SET status = 'failed',
			telegram_error_code = $2,
			telegram_error_desc = $3,
			updated_at = $4
		WHERE id = $1
	`, params.DeliveryID, params.ErrorCode, params.ErrorDesc, params.UpdatedAt)
	return err
}

func (s *Store) MarkDeliveryUnknown(ctx context.Context, params relay.MarkDeliveryUnknownParams) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE relay_deliveries
		SET status = 'unknown',
			telegram_error_code = $2,
			telegram_error_desc = $3,
			updated_at = $4
		WHERE id = $1
	`, params.DeliveryID, params.ErrorCode, params.ErrorDesc, params.UpdatedAt)
	return err
}

func (s *Store) ExpireRelays(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE relays
		SET status = 'expired',
			updated_at = $1
		WHERE status = 'ready'
		  AND expires_at <= $1
	`, now)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) DeleteCollectingRelaysBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM relays
		WHERE status = 'collecting'
		  AND updated_at < $1
	`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) MarkUnknownDeliveriesBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE relay_deliveries
		SET status = 'unknown',
			updated_at = $1,
			telegram_error_code = CASE WHEN telegram_error_code = '' THEN 'delivery_timeout' ELSE telegram_error_code END,
			telegram_error_desc = CASE
				WHEN telegram_error_desc = '' THEN 'delivery result is unknown after timeout'
				ELSE telegram_error_desc
			END
		WHERE status = 'pending'
		  AND created_at < $1
	`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) DeleteExpiredDeliveriesBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM relay_deliveries d
		USING relays r
		WHERE d.relay_id = r.id
		  AND r.status = 'expired'
		  AND r.expires_at < $1
		  AND d.created_at < $1
	`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) getRelayItemBySourceUpdateID(ctx context.Context, query rowQuerier, sourceUpdateID int64) (relay.RelayItem, error) {
	return scanRelayItem(query.QueryRow(ctx, `
		SELECT
			id, relay_id, source_update_id, source_message_id, media_group_id, item_order,
			media_kind, telegram_file_id, telegram_file_unique_id, file_name, mime_type,
			file_size_bytes, caption, created_at
		FROM relay_items
		WHERE source_update_id = $1
	`, sourceUpdateID))
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func scanRelay(row pgx.Row) (relay.Relay, error) {
	var (
		record relay.Relay
		status string
	)

	err := row.Scan(
		&record.ID,
		&record.SourceUpdateID,
		&record.CodeValue,
		&record.CodeHash,
		&record.CodeHint,
		&status,
		&record.UploaderUserID,
		&record.UploaderChatID,
		&record.DeliveryCount,
		&record.LastClaimedAt,
		&record.ExpiresAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return relay.Relay{}, relay.ErrRelayNotFound
	}
	if err != nil {
		return relay.Relay{}, err
	}

	record.Status = relay.RelayStatus(status)
	return record, nil
}

func scanRelayItem(row pgx.Row) (relay.RelayItem, error) {
	var (
		record    relay.RelayItem
		mediaKind string
	)

	err := row.Scan(
		&record.ID,
		&record.RelayID,
		&record.SourceUpdateID,
		&record.SourceMessageID,
		&record.MediaGroupID,
		&record.ItemOrder,
		&mediaKind,
		&record.TelegramFileID,
		&record.TelegramFileUniqueID,
		&record.FileName,
		&record.MIMEType,
		&record.FileSizeBytes,
		&record.Caption,
		&record.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return relay.RelayItem{}, relay.ErrRelayNotFound
	}
	if err != nil {
		return relay.RelayItem{}, err
	}

	record.MediaKind = relay.MediaKind(mediaKind)
	return record, nil
}

func scanDelivery(row pgx.Row) (relay.Delivery, error) {
	var (
		record relay.Delivery
		status string
		method *string
	)

	err := row.Scan(
		&record.ID,
		&record.RelayID,
		&record.RequestUpdateID,
		&record.ClaimerUserID,
		&record.ClaimerChatID,
		&status,
		&method,
		&record.TelegramOutMessageID,
		&record.TelegramErrorCode,
		&record.TelegramErrorDesc,
		&record.CreatedAt,
		&record.SentAt,
		&record.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return relay.Delivery{}, relay.ErrRelayNotFound
	}
	if err != nil {
		return relay.Delivery{}, err
	}

	record.Status = relay.DeliveryStatus(status)
	if method != nil {
		record.Method = relay.DeliveryMethod(*method)
	}
	return record, nil
}

var _ relay.Store = (*Store)(nil)
