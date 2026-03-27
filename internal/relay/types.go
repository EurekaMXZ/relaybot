package relay

import "time"

type MediaKind string

const (
	MediaKindDocument MediaKind = "document"
	MediaKindPhoto    MediaKind = "photo"
	MediaKindVideo    MediaKind = "video"
	MediaKindAudio    MediaKind = "audio"
	MediaKindVoice    MediaKind = "voice"
)

type RelayStatus string

const (
	RelayStatusCollecting RelayStatus = "collecting"
	RelayStatusReady      RelayStatus = "ready"
	RelayStatusExpired    RelayStatus = "expired"
)

type DeliveryStatus string

const (
	DeliveryStatusPending DeliveryStatus = "pending"
	DeliveryStatusSent    DeliveryStatus = "sent"
	DeliveryStatusFailed  DeliveryStatus = "failed"
	DeliveryStatusUnknown DeliveryStatus = "unknown"
)

type DeliveryMethod string

const (
	DeliveryMethodCopyMessage  DeliveryMethod = "copy_message"
	DeliveryMethodSendDocument DeliveryMethod = "send_document"
	DeliveryMethodSendPhoto    DeliveryMethod = "send_photo"
	DeliveryMethodSendVideo    DeliveryMethod = "send_video"
	DeliveryMethodSendAudio    DeliveryMethod = "send_audio"
	DeliveryMethodSendVoice    DeliveryMethod = "send_voice"
	DeliveryMethodSendBatch    DeliveryMethod = "send_batch"
)

type Relay struct {
	ID             int64
	SourceUpdateID int64
	CodeValue      string
	CodeHash       string
	CodeHint       string
	Status         RelayStatus
	UploaderUserID int64
	UploaderChatID int64
	DeliveryCount  int64
	LastClaimedAt  *time.Time
	ExpiresAt      time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type RelayItem struct {
	ID                   int64
	RelayID              int64
	SourceUpdateID       int64
	SourceMessageID      int
	MediaGroupID         string
	ItemOrder            int
	MediaKind            MediaKind
	TelegramFileID       string
	TelegramFileUniqueID string
	FileName             string
	MIMEType             string
	FileSizeBytes        int64
	Caption              string
	CreatedAt            time.Time
}

type BatchUploadSession struct {
	RelayID        int64
	UploaderUserID int64
	UploaderChatID int64
	ItemCount      int
	StartedAt      time.Time
	LastActivityAt time.Time
}

type Delivery struct {
	ID                   int64
	RelayID              int64
	RequestUpdateID      int64
	ClaimerUserID        int64
	ClaimerChatID        int64
	Status               DeliveryStatus
	Method               DeliveryMethod
	TelegramOutMessageID *int
	TelegramErrorCode    string
	TelegramErrorDesc    string
	CreatedAt            time.Time
	SentAt               *time.Time
	UpdatedAt            time.Time
}

type CreateRelayInput struct {
	SourceUpdateID       int64
	UploaderUserID       int64
	UploaderChatID       int64
	SourceMessageID      int
	MediaKind            MediaKind
	TelegramFileID       string
	TelegramFileUniqueID string
	FileName             string
	MIMEType             string
	FileSizeBytes        int64
	Caption              string
}

type CreateRelayResult struct {
	Relay      Relay
	Code       string
	ExpiresAt  time.Time
	Duplicated bool
}

type ClaimRelayInput struct {
	RequestUpdateID int64
	ClaimerUserID   int64
	ClaimerChatID   int64
	RawCode         string
}

type ClaimRelayResult struct {
	Relay        Relay
	Delivery     Delivery
	OutMessageID int
	Method       DeliveryMethod
	Duplicated   bool
}

type StartBatchUploadInput struct {
	UploaderUserID int64
	UploaderChatID int64
}

type AppendBatchItemInput struct {
	SourceUpdateID       int64
	UploaderUserID       int64
	UploaderChatID       int64
	SourceMessageID      int
	MediaGroupID         string
	MediaKind            MediaKind
	TelegramFileID       string
	TelegramFileUniqueID string
	FileName             string
	MIMEType             string
	FileSizeBytes        int64
	Caption              string
}

type FinishBatchUploadInput struct {
	UploaderUserID int64
	UploaderChatID int64
}

type CancelBatchUploadInput struct {
	UploaderUserID int64
	UploaderChatID int64
}

type StartBatchUploadResult struct {
	Relay Relay
}

type AppendBatchItemResult struct {
	Relay      Relay
	Item       RelayItem
	ItemCount  int
	Duplicated bool
}

type FinishBatchUploadResult struct {
	Relay     Relay
	Code      string
	ExpiresAt time.Time
	ItemCount int
}

type CancelBatchUploadResult struct {
	RelayID   int64
	ItemCount int
}

type CreateRelayParams struct {
	SourceUpdateID       int64
	CodeValue            string
	CodeHash             string
	CodeHint             string
	UploaderUserID       int64
	UploaderChatID       int64
	SourceMessageID      int
	MediaKind            MediaKind
	TelegramFileID       string
	TelegramFileUniqueID string
	FileName             string
	MIMEType             string
	FileSizeBytes        int64
	Caption              string
	ExpiresAt            time.Time
	CreatedAt            time.Time
}

type CreateRelayBatchParams struct {
	UploaderUserID int64
	UploaderChatID int64
	CreatedAt      time.Time
}

type AddRelayItemParams struct {
	RelayID              int64
	SourceUpdateID       int64
	SourceMessageID      int
	MediaGroupID         string
	MaxBatchItems        int
	ItemOrder            int
	MediaKind            MediaKind
	TelegramFileID       string
	TelegramFileUniqueID string
	FileName             string
	MIMEType             string
	FileSizeBytes        int64
	Caption              string
	CreatedAt            time.Time
}

type FinalizeRelayBatchParams struct {
	RelayID   int64
	CodeValue string
	CodeHash  string
	CodeHint  string
	ExpiresAt time.Time
	UpdatedAt time.Time
}

type CreateDeliveryParams struct {
	RelayID         int64
	RequestUpdateID int64
	ClaimerUserID   int64
	ClaimerChatID   int64
	CreatedAt       time.Time
}

type MarkDeliverySentParams struct {
	DeliveryID    int64
	RelayID       int64
	Method        DeliveryMethod
	OutMessageID  int
	SentAt        time.Time
	LastClaimedAt time.Time
}

type MarkDeliveryFailedParams struct {
	DeliveryID int64
	ErrorCode  string
	ErrorDesc  string
	UpdatedAt  time.Time
}

type MarkDeliveryUnknownParams struct {
	DeliveryID int64
	ErrorCode  string
	ErrorDesc  string
	UpdatedAt  time.Time
}
