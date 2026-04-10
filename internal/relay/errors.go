package relay

import "errors"

var (
	ErrInvalidCode          = errors.New("invalid relay code")
	ErrRelayNotFound        = errors.New("relay not found")
	ErrRelayExpired         = errors.New("relay expired")
	ErrUnsupportedMedia     = errors.New("unsupported media type")
	ErrFileTooLarge         = errors.New("file too large")
	ErrForbiddenExtension   = errors.New("forbidden file extension")
	ErrDangerousFile        = ErrForbiddenExtension
	ErrForbiddenChatType    = errors.New("forbidden chat type")
	ErrUploadRateLimited    = errors.New("upload rate limited")
	ErrClaimRateLimited     = errors.New("claim rate limited")
	ErrBadCodeRateLimit     = errors.New("bad code rate limited")
	ErrBadCodeRateLimited   = ErrBadCodeRateLimit
	ErrTooManyRelays        = errors.New("too many active relays")
	ErrDeliveryFailed       = errors.New("delivery failed")
	ErrDeliveryInProgress   = errors.New("delivery in progress")
	ErrBatchSessionActive   = errors.New("batch upload session already active")
	ErrBatchNotCollecting   = errors.New("batch relay is not collecting")
	ErrBatchSessionNotFound = errors.New("batch upload session not found")
	ErrBatchSessionEmpty    = errors.New("batch upload session is empty")
	ErrBatchItemLimit       = errors.New("batch upload item limit reached")
	ErrPageOutOfRange       = errors.New("page out of range")
)

type DeliveryError struct {
	Method         DeliveryMethod
	ErrCode        string
	ErrDescription string
	Unknown        bool
}

func (e *DeliveryError) Error() string {
	if e == nil {
		return ""
	}
	if e.ErrDescription != "" {
		return e.ErrDescription
	}
	return "delivery failed"
}

func (e *DeliveryError) Code() string {
	if e == nil {
		return ""
	}
	return e.ErrCode
}

func (e *DeliveryError) Description() string {
	if e == nil {
		return ""
	}
	return e.ErrDescription
}

func (e *DeliveryError) UnknownResult() bool {
	return e != nil && e.Unknown
}
