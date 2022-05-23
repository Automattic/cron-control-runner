package remote

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	expectedTimestamp = 1653231519
	token             = "supertoken"
)

type mockClock struct{}

func (mockClock) Now() time.Time { return time.Unix(expectedTimestamp, 0) }

func getSignatureFromHeader(signature string) []byte {
	prefix := "sha256="
	position := strings.Index(signature, prefix) + len(prefix)

	return []byte(signature[position:])
}

func TestWebhookSender(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var aux commandCompleted

			body, _ := ioutil.ReadAll(r.Body)

			err := json.Unmarshal([]byte(body), &aux)
			if err != nil {
				t.Fatal(err)
			}

			if got, want := aux.GUID, "123"; got != want {
				t.Fatalf("GUID=%x, want %x", got, want)
			}

			if got, want := aux.EventType, CommandCompletedType; got != want {
				t.Fatalf("Event=%x, want %x", got, want)
			}

			if got, want := aux.Timestamp, int64(expectedTimestamp); got != want {
				t.Fatalf("Timestamp=%x, want %x", got, want)
			}

			if got, want := aux.ExitCode, 0; got != want {
				t.Fatalf("ExitCode=%x, want %x", got, want)
			}

			if !aux.Success {
				t.Fatalf("ExitCode=%x, want %x", strconv.FormatBool(aux.Success), strconv.FormatBool(true))
			}

			signature1, _ := signRequestBody(token, body, realClock{})
			signature2 := r.Header.Get("X-WPCLI-SIGNATURE")

			if !hmac.Equal(getSignatureFromHeader(signature1), getSignatureFromHeader(signature2)) {
				t.Fatal("invalid signature")
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		wpCliEventSender := NewWebhookSender(
			&http.Client{},
			server.URL,
			token,
		)

		err := wpCliEventSender.send(context.Background(), commandCompleted{
			GUID:      "123",
			EventType: CommandCompletedType,
			Timestamp: expectedTimestamp,
			ExitCode:  0,
			Success:   true,
		})
		if nil != err {
			t.Fatal(err)
		}
	})

	t.Run("ErrHttpRequest", func(t *testing.T) {
		wpCliEventSender := NewWebhookSender(
			&http.Client{},
			"http://invalidWebHook",
			token,
		)

		err := wpCliEventSender.send(context.Background(), commandCompleted{
			GUID:      "123",
			EventType: CommandCompletedType,
			Timestamp: expectedTimestamp,
			ExitCode:  0,
			Success:   true,
		})
		if err == nil || !strings.Contains(err.Error(), `webhookSender error sending request to API endpoint`) {
			t.Fatalf("unexpected error: %s", err)
		}
	})

	t.Run("ErrHttpRequestNotOK", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("ERROR"))
		}))
		defer server.Close()

		wpCliEventSender := NewWebhookSender(
			&http.Client{},
			server.URL,
			token,
		)

		err := wpCliEventSender.send(context.Background(), commandCompleted{
			GUID:      "123",
			EventType: CommandCompletedType,
			Timestamp: expectedTimestamp,
			ExitCode:  0,
			Success:   true,
		})

		if err == nil || err.Error() != "webhookSender webhook not accepted. Status Code: 400; Body: ERROR" {
			t.Fatalf("unexpected error: %s", err)
		}
	})
}

func TestSignRequestBody(t *testing.T) {
	body, err := json.Marshal(commandCompleted{
		GUID:      "123",
		EventType: CommandCompletedType,
		Timestamp: expectedTimestamp,
		ExitCode:  0,
		Success:   true,
	})
	if err != nil {
		t.Fatal("Could not build body data")
	}
	signature, _ := signRequestBody(token, body, mockClock{})

	if signature != "ts=1653231519;sha256=f369b71ff6d61aeb5c152f05cbbde90158ff16008898a602f3ab004082d359e5" {
		t.Fatal("Signature not calculated correctly")
	}
}
