package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"

	"relaybot/internal/relay"
)

func TestShouldFallbackAfterCopyError(t *testing.T) {
	t.Run("unknown result does not fallback", func(t *testing.T) {
		err := classifySendError(context.DeadlineExceeded)
		if shouldFallbackAfterCopyError(err) {
			t.Fatal("expected unknown-result copy error to skip fallback")
		}
	})

	t.Run("explicit delivery error with unknown result does not fallback", func(t *testing.T) {
		err := &relay.DeliveryError{
			Method:         relay.DeliveryMethodCopyMessage,
			ErrCode:        "telegram_timeout",
			ErrDescription: "request timed out",
			Unknown:        true,
		}
		if shouldFallbackAfterCopyError(err) {
			t.Fatal("expected relay delivery error with unknown result to skip fallback")
		}
	})

	t.Run("definite failure does fallback", func(t *testing.T) {
		err := classifySendError(errors.New("copy failed"))
		if !shouldFallbackAfterCopyError(err) {
			t.Fatal("expected definite copy failure to fallback")
		}
	})

	t.Run("network timeout does not fallback", func(t *testing.T) {
		err := classifySendError(timeoutErr{})
		if shouldFallbackAfterCopyError(err) {
			t.Fatal("expected network timeout copy error to skip fallback")
		}
	})
}

func TestCanSendAsMediaGroup(t *testing.T) {
	if !canSendAsMediaGroup([]relay.RelayItem{
		{MediaKind: relay.MediaKindPhoto},
		{MediaKind: relay.MediaKindVideo},
	}) {
		t.Fatal("expected photo/video items to be sent as media group")
	}

	if !canSendAsMediaGroup([]relay.RelayItem{
		{MediaKind: relay.MediaKindDocument},
		{MediaKind: relay.MediaKindDocument},
	}) {
		t.Fatal("expected document items to be sent as media group")
	}

	if !canSendAsMediaGroup([]relay.RelayItem{
		{MediaKind: relay.MediaKindAudio},
		{MediaKind: relay.MediaKindAudio},
	}) {
		t.Fatal("expected audio items to be sent as media group")
	}

	if canSendAsMediaGroup([]relay.RelayItem{
		{MediaKind: relay.MediaKindPhoto},
		{MediaKind: relay.MediaKindDocument},
	}) {
		t.Fatal("expected mixed photo/document items to not be sent as media group")
	}

	if canSendAsMediaGroup([]relay.RelayItem{
		{MediaKind: relay.MediaKindAudio},
		{MediaKind: relay.MediaKindDocument},
	}) {
		t.Fatal("expected mixed audio/document items to not be sent as media group")
	}

	if canSendAsMediaGroup([]relay.RelayItem{
		{MediaKind: relay.MediaKindVoice},
		{MediaKind: relay.MediaKindVoice},
	}) {
		t.Fatal("expected voice items to not be sent as media group")
	}
}

func TestSplitPageItems(t *testing.T) {
	items := make([]relay.RelayItem, 0, 21)
	for i := 1; i <= 21; i++ {
		items = append(items, relay.RelayItem{ID: int64(i)})
	}

	chunks := splitPageItems(items, 10)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if len(chunks[0]) != 10 || len(chunks[1]) != 10 || len(chunks[2]) != 1 {
		t.Fatalf("unexpected chunk sizes: %d, %d, %d", len(chunks[0]), len(chunks[1]), len(chunks[2]))
	}
	if chunks[0][0].ID != 1 || chunks[1][0].ID != 11 || chunks[2][0].ID != 21 {
		t.Fatalf("unexpected chunk ordering: %+v", chunks)
	}
}

func TestDeliverPageSingleItemSkipsCopyAndCaption(t *testing.T) {
	httpClient := &recordingHTTPClient{t: t}
	b := newTestBot(t, httpClient)

	sender := NewSender()
	sender.Bind(b)

	method, outMessageID, err := sender.DeliverPage(context.Background(), relay.Relay{
		ID:             1,
		UploaderChatID: 111,
	}, relay.DeliveryPage{
		Index: 1,
		Total: 1,
		Items: []relay.RelayItem{
			{
				ID:              1,
				SourceMessageID: 9,
				MediaKind:       relay.MediaKindDocument,
				TelegramFileID:  "doc-file-id",
				Caption:         "must not be sent",
			},
		},
	}, 222)
	if err != nil {
		t.Fatalf("DeliverPage() error = %v", err)
	}
	if method != relay.DeliveryMethodSendDocument {
		t.Fatalf("DeliverPage() method = %q, want %q", method, relay.DeliveryMethodSendDocument)
	}
	if outMessageID != 101 {
		t.Fatalf("DeliverPage() out message id = %d, want 101", outMessageID)
	}
	if hasRequestWithSuffix(httpClient.requests, "/copyMessage") {
		t.Fatal("expected DeliverPage() to not call copyMessage")
	}
	if !hasRequestWithSuffix(httpClient.requests, "/sendDocument") {
		t.Fatal("expected DeliverPage() to call sendDocument")
	}
	sendDocumentBody := requestBodyBySuffix(httpClient.requests, "/sendDocument")
	if strings.Contains(strings.ToLower(sendDocumentBody), "caption") {
		t.Fatalf("sendDocument payload must not include caption: %s", sendDocumentBody)
	}
}

func TestDeliverPageSplitsOversizedMediaGroupIntoChunks(t *testing.T) {
	httpClient := &recordingHTTPClient{t: t}
	b := newTestBot(t, httpClient)

	sender := NewSender()
	sender.Bind(b)

	items := make([]relay.RelayItem, 0, 12)
	for i := 0; i < 12; i++ {
		items = append(items, relay.RelayItem{
			ID:           int64(i + 1),
			MediaKind:    relay.MediaKindPhoto,
			MediaGroupID: "group-1",
			TelegramFileID: func(index int) string {
				return "photo-" + string(rune('a'+index))
			}(i),
			Caption: "must not be sent",
		})
	}

	method, outMessageID, err := sender.DeliverPage(context.Background(), relay.Relay{
		ID:             2,
		UploaderChatID: 333,
	}, relay.DeliveryPage{
		Index: 1,
		Total: 1,
		Items: items,
	}, 444)
	if err != nil {
		t.Fatalf("DeliverPage() error = %v", err)
	}
	if method != relay.DeliveryMethodSendBatch {
		t.Fatalf("DeliverPage() method = %q, want %q", method, relay.DeliveryMethodSendBatch)
	}
	if outMessageID != 201 {
		t.Fatalf("DeliverPage() out message id = %d, want 201", outMessageID)
	}

	sendMediaGroupCalls := 0
	for _, req := range httpClient.requests {
		if !strings.HasSuffix(req.path, "/sendMediaGroup") {
			continue
		}
		sendMediaGroupCalls++
		if strings.Contains(strings.ToLower(req.body), "caption") {
			t.Fatalf("sendMediaGroup payload must not include caption: %s", req.body)
		}
		mediaCount := strings.Count(req.body, "photo-")
		if mediaCount == 0 || mediaCount > 10 {
			t.Fatalf("unexpected media group size inferred from payload: %d", mediaCount)
		}
	}
	if sendMediaGroupCalls != 2 {
		t.Fatalf("expected 2 sendMediaGroup calls, got %d", sendMediaGroupCalls)
	}
}

func TestDeliverPageSingleDocumentsGroupedByPage(t *testing.T) {
	httpClient := &recordingHTTPClient{t: t}
	b := newTestBot(t, httpClient)

	sender := NewSender()
	sender.Bind(b)

	items := make([]relay.RelayItem, 0, 12)
	for i := 0; i < 12; i++ {
		items = append(items, relay.RelayItem{
			ID:             int64(i + 1),
			MediaKind:      relay.MediaKindDocument,
			TelegramFileID: "doc-" + strconv.Itoa(i+1),
		})
	}

	method, outMessageID, err := sender.DeliverPage(context.Background(), relay.Relay{
		ID:             3,
		UploaderChatID: 777,
	}, relay.DeliveryPage{
		Index: 1,
		Total: 2,
		Items: items,
	}, 888)
	if err != nil {
		t.Fatalf("DeliverPage() error = %v", err)
	}
	if method != relay.DeliveryMethodSendBatch {
		t.Fatalf("DeliverPage() method = %q, want %q", method, relay.DeliveryMethodSendBatch)
	}
	if outMessageID != 201 {
		t.Fatalf("DeliverPage() out message id = %d, want 201", outMessageID)
	}

	sendMediaGroupCalls := 0
	sendDocumentCalls := 0
	for _, req := range httpClient.requests {
		if strings.HasSuffix(req.path, "/sendMediaGroup") {
			sendMediaGroupCalls++
		}
		if strings.HasSuffix(req.path, "/sendDocument") {
			sendDocumentCalls++
		}
	}
	if sendMediaGroupCalls != 2 {
		t.Fatalf("expected 2 sendMediaGroup calls for 12 docs, got %d", sendMediaGroupCalls)
	}
	if sendDocumentCalls != 0 {
		t.Fatalf("expected 0 sendDocument calls for grouped page docs, got %d", sendDocumentCalls)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "network timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

type requestRecord struct {
	path string
	body string
}

type recordingHTTPClient struct {
	t        *testing.T
	requests []requestRecord
}

func (c *recordingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body.Close()

	c.requests = append(c.requests, requestRecord{
		path: req.URL.Path,
		body: string(bodyBytes),
	})

	var payload string
	switch {
	case strings.HasSuffix(req.URL.Path, "/sendDocument"),
		strings.HasSuffix(req.URL.Path, "/sendPhoto"),
		strings.HasSuffix(req.URL.Path, "/sendVideo"),
		strings.HasSuffix(req.URL.Path, "/sendAudio"),
		strings.HasSuffix(req.URL.Path, "/sendVoice"):
		payload = `{"ok":true,"result":{"message_id":101}}`
	case strings.HasSuffix(req.URL.Path, "/sendMediaGroup"):
		payload = `{"ok":true,"result":[{"message_id":201},{"message_id":202}]}`
	case strings.HasSuffix(req.URL.Path, "/copyMessage"):
		payload = `{"ok":true,"result":{"message_id":301}}`
	default:
		c.t.Fatalf("unexpected telegram api endpoint: %s", req.URL.Path)
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(payload)),
	}, nil
}

func newTestBot(t *testing.T, client bot.HttpClient) *bot.Bot {
	t.Helper()
	b, err := bot.New("token", bot.WithSkipGetMe(), bot.WithHTTPClient(time.Second, client))
	if err != nil {
		t.Fatalf("bot.New() error = %v", err)
	}
	return b
}

func hasRequestWithSuffix(records []requestRecord, suffix string) bool {
	for _, record := range records {
		if strings.HasSuffix(record.path, suffix) {
			return true
		}
	}
	return false
}

func requestBodyBySuffix(records []requestRecord, suffix string) string {
	for _, record := range records {
		if strings.HasSuffix(record.path, suffix) {
			return record.body
		}
	}
	return ""
}
