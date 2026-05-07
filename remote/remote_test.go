package remote

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

type mockNetConn struct {
	deadlines []time.Time
}

func (m *mockNetConn) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (m *mockNetConn) Write(b []byte) (int, error) {
	return len(b), nil
}

func (m *mockNetConn) Close() error {
	return nil
}

func (m *mockNetConn) LocalAddr() net.Addr {
	return &net.TCPAddr{}
}

func (m *mockNetConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{}
}

func (m *mockNetConn) SetDeadline(_ time.Time) error {
	return nil
}

func (m *mockNetConn) SetReadDeadline(t time.Time) error {
	m.deadlines = append(m.deadlines, t)
	return nil
}

func (m *mockNetConn) SetWriteDeadline(_ time.Time) error {
	return nil
}

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

func TestReadHandshakeData(t *testing.T) {
	t.Run("returns payload", func(t *testing.T) {
		SetupWithOptions("token", false, "/tmp/wp", "/tmp", "", SetupOptions{
			MaxHandshakeBytes:       1024,
			HandshakeInitialTimeout: 5 * time.Second,
			HandshakeIdleTimeout:    200 * time.Millisecond,
			MaxConcurrentHandshakes: 16,
		})

		conn := &mockNetConn{}
		payload := []byte("token-guid-rows-cols-cmd\n")
		bufReader := bufio.NewReader(bytes.NewReader(payload))

		data, err := readHandshakeData(conn, bufReader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if string(data) != string(payload) {
			t.Fatalf("unexpected payload: %q", string(data))
		}

		if len(conn.deadlines) == 0 {
			t.Fatal("expected read deadline to be set")
		}
	})

	t.Run("rejects oversized handshake", func(t *testing.T) {
		SetupWithOptions("token", false, "/tmp/wp", "/tmp", "", SetupOptions{
			MaxHandshakeBytes:       16,
			HandshakeInitialTimeout: 5 * time.Second,
			HandshakeIdleTimeout:    200 * time.Millisecond,
			MaxConcurrentHandshakes: 16,
		})

		conn := &mockNetConn{}
		payload := bytes.Repeat([]byte("x"), 17)
		bufReader := bufio.NewReader(bytes.NewReader(payload))

		_, err := readHandshakeData(conn, bufReader)
		if err == nil {
			t.Fatal("expected oversized handshake error")
		}

		if !strings.Contains(err.Error(), "exceeds maximum size of 16 bytes") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestSetupWithOptions_DefaultsInvalidValues(t *testing.T) {
	SetupWithOptions("token", false, "/tmp/wp", "/tmp", "", SetupOptions{
		MaxHandshakeBytes:       -1,
		HandshakeInitialTimeout: -1,
		HandshakeIdleTimeout:    -1,
		MaxConcurrentHandshakes: -1,
	})

	if got, want := remoteConfig.maxHandshakeBytes, defaultMaxHandshakeBytes; got != want {
		t.Fatalf("maxHandshakeBytes=%d, want %d", got, want)
	}

	if got, want := remoteConfig.handshakeInitialTimeout, defaultHandshakeInitialTimeout; got != want {
		t.Fatalf("handshakeInitialTimeout=%s, want %s", got, want)
	}

	if got, want := remoteConfig.handshakeIdleTimeout, defaultHandshakeIdleTimeout; got != want {
		t.Fatalf("handshakeIdleTimeout=%s, want %s", got, want)
	}

	if got, want := remoteConfig.maxConcurrentHandshakes, defaultMaxConcurrentHandshakes; got != want {
		t.Fatalf("maxConcurrentHandshakes=%d, want %d", got, want)
	}
}
