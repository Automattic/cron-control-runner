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
)

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

			if got, want := aux.EventType, "COMMAND_COMPLETED"; got != want {
				t.Fatalf("Event=%x, want %x", got, want)
			}

			if got, want := aux.Timestamp, int64(1653084877); got != want {
				t.Fatalf("Timestamp=%x, want %x", got, want)
			}

			if got, want := aux.ExitCode, 0; got != want {
				t.Fatalf("ExitCode=%x, want %x", got, want)
			}

			if !aux.Success {
				t.Fatalf("ExitCode=%x, want %x", strconv.FormatBool(aux.Success), strconv.FormatBool(true))
			}

			signature1, _ := signRequestBody("supertoken", body)
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
			"supertoken",
		)

		err := wpCliEventSender.send(context.Background(), commandCompleted{
			GUID:      "123",
			EventType: CommandCompletedType,
			Timestamp: 1653084877,
			ExitCode:  0,
			Success:   true,
		})
		if nil != err {
			t.Fatal(err)
		}
	})
}
