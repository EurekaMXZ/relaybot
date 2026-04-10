package relay

import (
	"context"
	"time"
)

type Store interface {
	CreateRelay(ctx context.Context, params CreateRelayParams) (Relay, bool, error)
	CreateRelayBatch(ctx context.Context, params CreateRelayBatchParams) (Relay, error)
	AddRelayItem(ctx context.Context, params AddRelayItemParams) (RelayItem, bool, error)
	ListRelayItemsByRelayID(ctx context.Context, relayID int64) ([]RelayItem, error)
	FinalizeRelayBatch(ctx context.Context, params FinalizeRelayBatchParams) (Relay, error)
	DeleteRelay(ctx context.Context, relayID int64) error
	GetRelayBySourceUpdateID(ctx context.Context, sourceUpdateID int64) (Relay, error)
	GetRelayByCodeHash(ctx context.Context, codeHash string, now time.Time) (Relay, error)
	GetRelayByID(ctx context.Context, relayID int64) (Relay, error)
	CountActiveRelaysByUploader(ctx context.Context, uploaderUserID int64, now time.Time) (int64, error)
	CreateDelivery(ctx context.Context, params CreateDeliveryParams) (Delivery, bool, error)
	MarkDeliverySent(ctx context.Context, params MarkDeliverySentParams) error
	MarkDeliveryFailed(ctx context.Context, params MarkDeliveryFailedParams) error
	MarkDeliveryUnknown(ctx context.Context, params MarkDeliveryUnknownParams) error
	ExpireRelays(ctx context.Context, now time.Time) (int64, error)
	DeleteCollectingRelaysBefore(ctx context.Context, before time.Time) (int64, error)
	MarkUnknownDeliveriesBefore(ctx context.Context, before time.Time) (int64, error)
	DeleteExpiredDeliveriesBefore(ctx context.Context, before time.Time) (int64, error)
	Ping(ctx context.Context) error
}

type Cache interface {
	GetRelayIDByCodeHash(ctx context.Context, codeHash string) (int64, bool, error)
	SetRelayIDByCodeHash(ctx context.Context, codeHash string, relayID int64, ttl time.Duration) error
	GetCreatedCodeBySourceUpdate(ctx context.Context, sourceUpdateID int64) (string, bool, error)
	SetCreatedCodeBySourceUpdate(ctx context.Context, sourceUpdateID int64, code string, ttl time.Duration) error
	AllowUpload(ctx context.Context, userID int64) (bool, error)
	AllowClaim(ctx context.Context, userID int64) (bool, error)
	AllowBadCode(ctx context.Context, userID int64) (bool, error)
	MarkSeenUpdate(ctx context.Context, updateID int64) (bool, error)
	GetBatchUploadSession(ctx context.Context, chatID int64) (BatchUploadSession, bool, error)
	SetBatchUploadSession(ctx context.Context, session BatchUploadSession, ttl time.Duration) error
	MergeBatchUploadSession(ctx context.Context, session BatchUploadSession, ttl time.Duration) (BatchUploadSession, error)
	DeleteBatchUploadSession(ctx context.Context, chatID int64) error
	Ping(ctx context.Context) error
}

type Sender interface {
	DeliverPage(ctx context.Context, relay Relay, page DeliveryPage, targetChatID int64) (DeliveryMethod, int, error)
}

type DeliveryPage struct {
	Index int
	Total int
	Items []RelayItem
}

type SenderError interface {
	error
	Code() string
	Description() string
	UnknownResult() bool
}

type CodeManager interface {
	Generate() (displayCode string, codeHash string, codeHint string, err error)
	Normalize(raw string) (string, error)
	Hash(normalized string) string
}

type Clock interface {
	Now() time.Time
}

type Limits struct {
	MaxFileBytes         int64
	MaxActiveRelays      int64
	MaxBatchItems        int
	DefaultTTL           time.Duration
	BatchSessionTTL      time.Duration
	UnknownDeliveryAfter time.Duration
	ExpiredDeliveryPurge time.Duration
	ForbiddenExtensions  map[string]struct{}
}
