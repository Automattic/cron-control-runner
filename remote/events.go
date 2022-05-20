package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

const (
	CommandCompletedType = "COMMAND_COMPLETED"
)

type event interface {
	guid() string
	eventType() string
}

type commandCompleted struct {
	GUID      string `json:"guid"`
	EventType string `json:"event"`
	Timestamp int64  `json:"timestamp"`
	ExitCode  int    `json:"exit_code"`
	Success   bool   `json:"success"`
}

func (c commandCompleted) guid() string {
	return c.GUID
}


func (c commandCompleted) eventType() string {
	return c.EventType
}

type eventSender interface {
	send(ctx context.Context, e event) error
}

type webhookSender struct {
	httpClient *http.Client
	endpoint string
	token string
}

func NewWebhookSender(client *http.Client, endpoint string, token string) *webhookSender {
	return &webhookSender{
		httpClient: client,
		endpoint:   endpoint,
		token:      token,
	};
}

type webhookData struct {
	GUID string `json:"guid"`
	Token string `json:"token"`
	Event event `json:"event"`
}

func (sender *webhookSender) send(ctx context.Context, e event) error {
	data := webhookData{
		GUID:  e.guid(),
		Token: sender.token,
		Event: e,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("webhookSender failed to marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", sender.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("webhookSender failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("guid", e.guid())
	req.Header.Set("event", e.eventType())

	response, err := sender.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhookSender error sending request to API endpoint: %w", err)
	}

	defer response.Body.Close()

	if response.StatusCode >= 200 && response.StatusCode <= 299 {
		return nil
	}

	body, _ := ioutil.ReadAll(response.Body)

	return fmt.Errorf("webhookSender webhook not accepted. Status Code: %d; Body: %s", response.StatusCode, string(body))
}
