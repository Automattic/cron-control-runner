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
		wpCLIPath:  wpCLIPath,
		wpPath:     tmpDir,
		phpPath:    fakePHP(t, tmpDir),
		opcacheDir: tmpDir,
		metrics:    metrics.Mock{},
		logger:     logger.Logger{Logger: log.New(io.Discard, "", 0)},
	}

	_, err := perf.getSiteInfo()
	if err == nil {
		t.Fatal("expected error for empty WP-CLI response, got nil")
	}

	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("expected empty response error, got: %v", err)
	}
}

// fakePHP writes a stand-in for the php binary that drops the `-d key=value` pairs and executes
// the script argument directly, so tests can use shell scripts as fake wp-cli without php installed.
func fakePHP(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "php")
	script := "#!/bin/sh\nwhile [ \"$1\" = \"-d\" ]; do shift 2; done\nexec \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake php: %v", err)
	}
	return path
}

func TestWpCommand_RunsPHPWithOpcacheFlags(t *testing.T) {
	perf := &CLI{
		wpCLIPath:  "/usr/local/bin/wp",
		phpPath:    "/usr/bin/php",
		opcacheDir: "/tmp/opcache-test",
	}

	cmd := perf.wpCommand([]string{"option", "get", "home", "--allow-root"})

	if cmd.Path != "/usr/bin/php" && cmd.Args[0] != "/usr/bin/php" {
		t.Fatalf("expected php to be executed, got %q", cmd.Args[0])
	}

	args := cmd.Args[1:]
	want := []string{
		"-d", "opcache.enable_cli=1",
		"-d", "opcache.file_cache_only=1",
		"-d", "opcache.file_cache=/tmp/opcache-test",
		"/usr/local/bin/wp",
		"option", "get", "home", "--allow-root",
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("unexpected args:\n got: %v\nwant: %v", args, want)
	}
}

func TestWpCommand_DefaultsToPHPFromPath(t *testing.T) {
	perf := &CLI{wpCLIPath: "/usr/local/bin/wp", opcacheDir: "/tmp/x"}
	cmd := perf.wpCommand(nil)
	if cmd.Args[0] != phpBinary {
		t.Fatalf("expected %q, got %q", phpBinary, cmd.Args[0])
	}
}

func TestNewCLI_CreatesOpcacheDirUnderTempDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	perf := NewCLI("/usr/local/bin/wp", tmp, "", 0, metrics.Mock{}, logger.Logger{Logger: log.New(io.Discard, "", 0)})

	if !strings.HasPrefix(perf.opcacheDir, tmp) {
		t.Fatalf("expected opcache dir under %q, got %q", tmp, perf.opcacheDir)
	}
	info, err := os.Stat(perf.opcacheDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected opcache dir to exist: err=%v", err)
	}
}

func TestProcessCommand_PassesThroughToWpCLI(t *testing.T) {
	tmpDir := t.TempDir()
	wpCLIPath := filepath.Join(tmpDir, "wp")
	// echoes its arguments so we can verify what wp-cli would receive after php strips its -d flags
	if err := os.WriteFile(wpCLIPath, []byte("#!/bin/sh\nprintf '%s ' \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	perf := &CLI{
		wpCLIPath:  wpCLIPath,
		wpPath:     tmpDir,
		phpPath:    fakePHP(t, tmpDir),
		opcacheDir: tmpDir,
		metrics:    metrics.Mock{},
		logger:     logger.Logger{Logger: log.New(io.Discard, "", 0)},
	}

	out, err := perf.processCommand([]string{"option", "get", "home"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "option get home" {
		t.Fatalf("unexpected wp-cli args: %q", out)
	}
}
