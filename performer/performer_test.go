package performer

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Automattic/cron-control-runner/logger"
	"github.com/Automattic/cron-control-runner/metrics"
	"github.com/yookoala/gofast"
)

func TestEvent_LockKey(t *testing.T) {
	type fields struct {
		URL       string
		Timestamp int
		Action    string
		Instance  string
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name: "hello",
			fields: fields{
				URL:       "https://foo.bar",
				Timestamp: 12345,
				Action:    "yolo",
				Instance:  "daddy",
			},
			want: "WQQ5XKF6R4C6VWN3WYKFJQFGLMHDFBC2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := Event{
				URL:       tt.fields.URL,
				Timestamp: tt.fields.Timestamp,
				Action:    tt.fields.Action,
				Instance:  tt.fields.Instance,
			}
			if got := e.LockKey(); got != tt.want {
				t.Errorf("LockKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProcessCommandWithFPM_ResponseTimeout(t *testing.T) {
	perf := &CLI{
		wpPath:             t.TempDir(),
		metrics:            metrics.Mock{},
		logger:             logger.Logger{Logger: log.New(io.Discard, "", 0)},
		fpmResponseTimeout: 100 * time.Millisecond,
		fpm: func() (gofast.Client, error) {
			return gofast.ClientFunc(func(req *gofast.Request) (*gofast.ResponsePipe, error) {
				return gofast.NewResponsePipe(), nil
			}), nil
		},
	}

	_, err := perf.processCommandWithFPM([]string{"cron-control", "orchestrate", "runner-only", "get-info", "--format=json"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "response read timed out") {
		t.Fatalf("expected response read timeout error, got: %v", err)
	}
}

func TestSanitizeJSONInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain json unchanged",
			input: `[{"url":"https://example.com"}]`,
			want:  `[{"url":"https://example.com"}]`,
		},
		{
			name:  "strips bom",
			input: "\uFEFF[{\"url\":\"https://example.com\"}]",
			want:  `[{"url":"https://example.com"}]`,
		},
		{
			name:  "strips leading whitespace",
			input: " \n\t[{\"url\":\"https://example.com\"}]",
			want:  `[{"url":"https://example.com"}]`,
		},
		{
			name:  "strips whitespace then bom",
			input: " \n\t\uFEFF[{\"url\":\"https://example.com\"}]",
			want:  `[{"url":"https://example.com"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimJSONPreamble(tt.input)
			if got != tt.want {
				t.Fatalf("sanitizeJSONInput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetSiteInfo_EmptyJSONArrayReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	wpCLIPath := filepath.Join(tmpDir, "wp")

	script := "#!/bin/sh\nprintf '[]'\n"
	if err := os.WriteFile(wpCLIPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake wp-cli: %v", err)
	}

	perf := &CLI{
		wpCLIPath: wpCLIPath,
		wpPath:    tmpDir,
		metrics:   metrics.Mock{},
		logger:    logger.Logger{Logger: log.New(io.Discard, "", 0)},
	}

	_, err := perf.getSiteInfo()
	if err == nil {
		t.Fatal("expected error for empty WP-CLI response, got nil")
	}

	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("expected empty response error, got: %v", err)
	}
}
