package remote

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidateCommand(t *testing.T) {
	tests := map[string]struct {
		errString string
		input     string
		want      string
	}{
		"media import file should pass": {errString: "", want: "media import https://example.com/cutekitties.png", input: "media import https://example.com/cutekitties.png"},
		"vip whatever should pass":      {errString: "", want: "vip whatever", input: "vip whatever"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := validateCommand(tc.input)

			if err != nil && tc.errString != err.Error() {
				t.Fatalf("testing '%v' validateCommand(\"%v\") expected error: %v, got: %v", name, tc.input, tc.errString, err.Error())
			}

			if err == nil && tc.errString != "" {
				t.Fatalf("testing '%v' validateCommand(\"%v\") expected error string: %v, got: nil", name, tc.input, tc.errString)
			}

			if tc.want != got {
				t.Fatalf("testing '%v' validateCommand(\"%v\") expected: %v, got: %v", name, tc.input, tc.want, got)
			}
		})
	}
}

func TestTokenizeString(t *testing.T) {
	tests := map[string]struct {
		input string
		want  []string
	}{
		"no quotes":            {want: []string{"option", "update", "cow", "a"}, input: "option update cow a"},
		"single quotes":        {want: []string{"option", "update", "cow", `'a b'`}, input: "option update cow 'a b'"},
		"double quotes":        {want: []string{"option", "update", "cow", `"a b"`}, input: `option update cow "a b"`},
		"nested double quotes": {want: []string{"option", "update", "cow", `"a \"b\""`}, input: `option update cow "a \"b\""`},
		"one nested quote":     {want: []string{`"a\"b"`}, input: `"a\"b"`},

		// Whitespaces
		"newline in arguments":  {want: []string{"option", "update", "cow", "\"a\nb\""}, input: "option update cow \"a\nb\""},
		"newline between args":  {want: []string{"option", "update", "cow", "a"}, input: "option \n update cow a"},
		"different whitespaces": {want: []string{"option", "update", "cow", "a"}, input: "option \n \t update\v\fcow\ra"},

		// Compatibility with VIP CLI bugs
		"named parameters": {want: []string{"option", "list", `--search="a b"`}, input: `option list --search="a b"`},

		// These sequences should not occur; if they do, someone is trying to break the system
		"embedded quotes":       {want: []string{`opt""i''on`}, input: `opt""i''on`},
		"unbalanced quotes (1)": {want: []string{`"a 'b`}, input: `"a 'b`},
		"unbalanced quotes (2)": {want: []string{`a" 'b`}, input: `a" 'b`},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tokenizeString(tc.input)

			if !reflect.DeepEqual(tc.want, got) {
				t.Fatalf("testing '%v' tokenizeString(\"%v\") expected: %v, got: %v", name, tc.input, tc.want, got)
			}
		})
	}
}

func TestGetCleanWpCliArgumentArray(t *testing.T) {
	tests := map[string]struct {
		input string
		want  []string
	}{
		"no quotes":                {want: []string{"option", "update", "cow", "a"}, input: "option update cow a"},
		"single quotes":            {want: []string{"option", "update", "cow", "a b"}, input: "option update cow 'a b'"},
		"double quotes":            {want: []string{"option", "update", "cow", "a b"}, input: `option update cow "a b"`},
		"nested double quotes":     {want: []string{"option", "update", "cow", `a "b"`}, input: `option update cow "a \"b\""`},
		"json":                     {want: []string{"option", "update", "cow", `{"a":"b"}`}, input: `option update cow {"a":"b"}`},
		"vip-cli bugs":             {want: []string{"option", "list", `--search=a b`}, input: `option list --search="a b"`},
		"proper quoting":           {want: []string{"option", "list", `--search=a b`}, input: `"option" "list" "--search=a b"`},
		"proper quoting w/nesting": {want: []string{"option", "list", `--search="a b"`}, input: `"option" "list" "--search=\"a b\""`},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := getCleanWpCliArgumentArray(tc.input)

			if !reflect.DeepEqual(tc.want, got) {
				t.Fatalf("testing '%v' getCleanWpCliArgumentArray(\"%v\") expected: %v, got: %v", name, tc.input, tc.want, got)
			}
		})
	}
}

func TestAuthConnRejectsInvalidToken(t *testing.T) {
	done := make(chan struct{})

	prevConfig := remoteConfig
	prevGuidRegex := guidRegex
	prevGUIDttys := gGUIDttys
	prevPadlock := padlock
	t.Cleanup(func() {
		remoteConfig = prevConfig
		guidRegex = prevGuidRegex
		gGUIDttys = prevGUIDttys
		padlock = prevPadlock
	})

	remoteConfig = config{remoteTokenB: []byte("supersecrettoken")}
	guidRegex = regexp.MustCompile(`^[a-fA-F0-9\-]+$`)
	gGUIDttys = make(map[string]*wpCLIProcess)
	padlock = &sync.Mutex{}

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("authConn did not terminate during cleanup")
		}
	})

	go func() {
		defer close(done)
		authConn(serverConn)
	}()

	handshake := "xupersecrettoken;123e4567-e89b-12d3-a456-426614174000;24;80;vip whatever\n"
	if _, err := clientConn.Write([]byte(handshake)); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	buf := make([]byte, 256)
	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read auth response: %v", err)
	}

	if got := string(buf[:n]); got != "invalid auth handshake" {
		t.Fatalf("expected invalid auth handshake, got %q", got)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("authConn did not terminate")
	}
}

func TestAuthConnRejectsEmptyConfiguredToken(t *testing.T) {
	done := make(chan struct{})

	prevConfig := remoteConfig
	prevGuidRegex := guidRegex
	prevGUIDttys := gGUIDttys
	prevPadlock := padlock
	t.Cleanup(func() {
		remoteConfig = prevConfig
		guidRegex = prevGuidRegex
		gGUIDttys = prevGUIDttys
		padlock = prevPadlock
	})

	remoteConfig = config{remoteTokenB: []byte("")}
	guidRegex = regexp.MustCompile(`^[a-fA-F0-9\-]+$`)
	gGUIDttys = make(map[string]*wpCLIProcess)
	padlock = &sync.Mutex{}

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("authConn did not terminate during cleanup")
		}
	})

	go func() {
		defer close(done)
		authConn(serverConn)
	}()

	buf := make([]byte, 256)
	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read auth response: %v", err)
	}

	if got := string(buf[:n]); got != "invalid auth handshake" {
		t.Fatalf("expected invalid auth handshake, got %q", got)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("authConn did not terminate")
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// signalingSender closes sent when the command completed event is sent.
type signalingSender struct {
	sent chan struct{}
}

func (s signalingSender) Send(ctx context.Context, e Event) error {
	close(s.sent)
	return nil
}

func TestAuthConnRedactsLoggedArguments(t *testing.T) {
	done := make(chan struct{})
	sent := make(chan struct{})
	guid := "123e4567-e89b-12d3-a456-426614174000"
	logs := &lockedBuffer{}

	prevConfig := remoteConfig
	prevGuidRegex := guidRegex
	prevGUIDttys := gGUIDttys
	prevPadlock := padlock
	prevEventSender := wpCliEventSender
	prevLogOutput := log.Writer()
	t.Cleanup(func() {
		remoteConfig = prevConfig
		guidRegex = prevGuidRegex
		gGUIDttys = prevGUIDttys
		padlock = prevPadlock
		wpCliEventSender = prevEventSender
		log.SetOutput(prevLogOutput)
		_ = os.Remove("/tmp/wp-cli-" + guid)
	})

	// "true" stands in for WP-CLI: it accepts any arguments and exits.
	remoteConfig = config{remoteTokenB: []byte("supersecrettoken"), wpCLIPath: "true", wpPath: "/var/www"}
	guidRegex = regexp.MustCompile(`^[a-fA-F0-9\-]+$`)
	gGUIDttys = make(map[string]*wpCLIProcess)
	padlock = &sync.Mutex{}
	wpCliEventSender = signalingSender{sent: sent}
	log.SetOutput(logs)

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})

	go func() {
		defer close(done)
		authConn(serverConn)
	}()

	handshake := "supersecrettoken;" + guid + `;24;80;"user" "update" "5" "--user_pass=hunter2"` + "\n"
	if _, err := clientConn.Write([]byte(handshake)); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	if err := clientConn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}
	_, _ = io.Copy(io.Discard, clientConn)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("authConn did not terminate")
	}

	// The completed event is sent in the background; wait for it before the cleanup resets the sender.
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("command completed event was not sent")
	}

	// The tty reader also outlives authConn and locks the package padlock the cleanup resets.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "closing logfile") {
		if time.Now().After(deadline) {
			t.Fatal("WP-CLI tty reader did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}

	got := logs.String()
	if strings.Contains(got, "hunter2") {
		t.Fatalf("expected the password to be redacted from the logs, got:\n%s", got)
	}
	if !strings.Contains(got, "args: --path=/var/www user update 5 --user_pass=<redacted>") {
		t.Fatalf("expected the redacted command to be logged, got:\n%s", got)
	}
}
