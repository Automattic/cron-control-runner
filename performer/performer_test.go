package performer

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
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
	wpCLIPath := writeScript(t, tmpDir, "wp", "#!/bin/sh\nprintf '[]'\n")

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

// writeScript writes an executable file named name under dir and returns its path.
func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("failed to write %s: %v", name, err)
	}
	return path
}

// requirePHP skips the test unless a real php binary is on PATH.
func requirePHP(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(phpBinary); err != nil {
		t.Skip("php not found on PATH")
	}
}

func newTestCLI(t *testing.T, wpCLIPath string) *CLI {
	t.Helper()
	return NewCLI(wpCLIPath, t.TempDir(), "", 0, metrics.Mock{}, logger.Logger{Logger: log.New(io.Discard, "", 0)})
}

func TestIsPHPScript(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"phar env shebang", "#!/usr/bin/env php\n<?php\nPhar::mapPhar();", true},
		{"direct php shebang", "#!/usr/bin/php\n<?php echo 1;", true},
		{"versioned php shebang", "#!/usr/bin/php8.2\n<?php echo 1;", true},
		{"php open tag", "<?php\necho 1;", true},
		{"sh launcher", "#!/bin/sh\nexec php \"$0.phar\" \"$@\"\n", false},
		{"env sh launcher", "#!/usr/bin/env sh\nphp wp-cli.phar \"$@\"\n", false},
		{"binary", "\x7fELF\x02\x01\x01", false},
		{"empty", "", false},
	}
	dir := t.TempDir()
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeScript(t, dir, fmt.Sprintf("wp%d", i), tc.content)
			got, err := isPHPScript(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("isPHPScript(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

func TestWpCommand_RunsPHPWithOpcacheFlags(t *testing.T) {
	perf := &CLI{
		wpCLIPath: "/usr/local/bin/wp",
		phpPath:   "/usr/bin/php",
		phpFlags:  opcachePHPFlags("/tmp/opcache-test"),
	}

	cmd := perf.wpCommand([]string{"option", "get", "home", "--allow-root"})
	if cmd.Args[0] != "/usr/bin/php" {
		t.Fatalf("expected php to be executed, got %q", cmd.Args[0])
	}

	want := []string{
		"-d", "opcache.enable_cli=1",
		"-d", "opcache.file_cache_only=1",
		"-d", "opcache.file_cache=/tmp/opcache-test",
		"-d", "opcache.validate_timestamps=1",
		"-d", "opcache.file_cache_consistency_checks=0",
		"/usr/local/bin/wp",
		"option", "get", "home", "--allow-root",
	}
	if got := cmd.Args[1:]; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("unexpected args:\n got: %v\nwant: %v", got, want)
	}
}

func TestWpCommand_DefaultsToPHPFromPath(t *testing.T) {
	perf := &CLI{wpCLIPath: "/usr/local/bin/wp", phpFlags: opcachePHPFlags("/tmp/x")}
	cmd := perf.wpCommand(nil)
	if cmd.Args[0] != phpBinary {
		t.Fatalf("expected %q, got %q", phpBinary, cmd.Args[0])
	}
}

func TestWpCommand_ExecutesDirectlyWithoutPHPFlags(t *testing.T) {
	perf := &CLI{wpCLIPath: "/usr/local/bin/wp", phpPath: "/usr/bin/php"}
	cmd := perf.wpCommand([]string{"option", "get", "home"})
	want := []string{"/usr/local/bin/wp", "option", "get", "home"}
	if strings.Join(cmd.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("unexpected args:\n got: %v\nwant: %v", cmd.Args, want)
	}
}

func TestNewCLI_CreatesOpcacheDirUnderTempDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	wpCLIPath := writeScript(t, tmp, "wp", "#!/usr/bin/env php\n<?php\n")

	perf := newTestCLI(t, wpCLIPath)

	dir := defaultOpcacheDir()
	if !strings.HasPrefix(dir, tmp) {
		t.Fatalf("expected opcache dir under %q, got %q", tmp, dir)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected opcache dir to exist: err=%v", err)
	}
	if perf.phpFlags == nil {
		t.Fatal("expected php flags for a PHP entry point")
	}
}

func TestNewCLI_PanicsWhenOpcacheDirCannotBeCreated(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	wpCLIPath := writeScript(t, tmp, "wp", "#!/usr/bin/env php\n<?php\n")
	// A regular file where the cache dir should go makes MkdirAll fail.
	if err := os.WriteFile(defaultOpcacheDir(), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected NewCLI to panic")
		}
	}()
	newTestCLI(t, wpCLIPath)
}

func TestNewCLI_PanicsWhenWPCLIPathIsUnreadable(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected NewCLI to panic")
		}
	}()
	newTestCLI(t, filepath.Join(t.TempDir(), "missing"))
}

func TestNewCLI_ShellLauncherSkipsOpcache(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	wpCLIPath := writeScript(t, tmp, "wp", "#!/bin/sh\nexec php wp-cli.phar \"$@\"\n")

	perf := newTestCLI(t, wpCLIPath)

	if perf.phpFlags != nil {
		t.Fatalf("expected no php flags for a shell launcher, got %v", perf.phpFlags)
	}
	if _, err := os.Stat(defaultOpcacheDir()); !os.IsNotExist(err) {
		t.Fatalf("expected no opcache dir for a shell launcher: err=%v", err)
	}
}

func TestNewCLI_WithFPMSkipsOpcache(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	perf := NewCLI("/usr/local/bin/wp", tmp, "unix:///tmp/php-fpm.sock", 0, metrics.Mock{}, logger.Logger{Logger: log.New(io.Discard, "", 0)})

	if perf.phpFlags != nil {
		t.Fatalf("expected no php flags with FPM, got %v", perf.phpFlags)
	}
	if _, err := os.Stat(defaultOpcacheDir()); !os.IsNotExist(err) {
		t.Fatalf("expected no opcache dir with FPM: err=%v", err)
	}
}

// The next two tests run real php (skipped when it is not installed), since a stand-in would hide
// how php treats the launcher: a shell script passed to php as the script is printed, not run.

func TestProcessCommand_RealPHP_PHPEntryPointRunsWithOpcache(t *testing.T) {
	requirePHP(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	wpCLIPath := writeScript(t, tmp, "wp", "#!/usr/bin/env php\n<?php\necho ini_get('opcache.file_cache'), ' ', implode(' ', array_slice($argv, 1));\n")

	perf := newTestCLI(t, wpCLIPath)
	out, err := perf.processCommand([]string{"option", "get", "home"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := defaultOpcacheDir() + " option get home"; strings.TrimSpace(out) != want {
		t.Fatalf("unexpected output:\n got: %q\nwant: %q", out, want)
	}
}

func TestProcessCommand_RealPHP_ShellLauncherIsExecuted(t *testing.T) {
	requirePHP(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	wpCLIPath := writeScript(t, tmp, "wp", "#!/bin/sh\nprintf 'EXECUTED %s' \"$*\"\n")

	perf := newTestCLI(t, wpCLIPath)
	out, err := perf.processCommand([]string{"option", "get", "home"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "EXECUTED option get home" {
		t.Fatalf("expected the launcher to run, got: %q", out)
	}
}
