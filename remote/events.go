package remote

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	CommandCompletedType = "command-completed"
	CommandRunningType   = "command-running"
	CommandErroredType   = "command-errored"
	CommandCanceledType  = "command-canceled"
	CommandHeartbeatType = "command-heartbeat"
)

type Event interface {
	GUIDValue() string
	EventTypeValue() string
}

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type CommandCompleted struct {
	GUID      string `json:"guid"`
	EventType string `json:"event"`
	Timestamp int64  `json:"timestamp"`
	ExitCode  int    `json:"exit_code"`
	Success   bool   `json:"success"`
}

func (c CommandCompleted) GUIDValue() string {
	return c.GUID
}

func (c CommandCompleted) EventTypeValue() string {
	return c.EventType
}

type EventSender interface {
	Send(ctx context.Context, e Event) error
}

type WebhookSender struct {
	httpClient *http.Client
	endpoint   string
	token      string
}

func NewWebhookSender(client *http.Client, endpoint string, token string) *WebhookSender {
	return &WebhookSender{
		httpClient: client,
		endpoint:   endpoint,
		token:      token,
	}
}

func (sender *WebhookSender) Send(ctx context.Context, e Event) error {
	jsonData, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("webhookSender failed to marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", sender.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("webhookSender failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	signature, err := signRequestBody(sender.token, jsonData, realClock{})
	if err != nil {
		return fmt.Errorf("webhookSender failed sign the request: %w", err)
	}

	req.Header.Set("X-WPCLI-SIGNATURE", signature)

	response, err := sender.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhookSender error sending request to API endpoint: %w", err)
	}

	defer response.Body.Close()

	if response.StatusCode >= 200 && response.StatusCode <= 299 {
		return nil
	}

	body, _ := io.ReadAll(response.Body)

	return fmt.Errorf("webhookSender webhook not accepted. Status Code: %d; Body: %s", response.StatusCode, string(body))
}

type NopSender struct {
}

func NewNopSender() *NopSender {
	return &NopSender{}
}

func (sender *NopSender) Send(ctx context.Context, e Event) error {
	return nil
}

func signRequestBody(token string, body []byte, clock Clock) (string, error) {
	timestamp := clock.Now().Unix()
	mac := hmac.New(sha256.New, []byte(token))
	payload := fmt.Sprintf("%d%s", timestamp, string(body))
	mac.Write([]byte(payload))

	signature := fmt.Sprintf("ts=%d;sha256=%s", timestamp, hex.EncodeToString(mac.Sum(nil)))
	return signature, nil
}
