package remote

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
)

// newTestWSPair creates a matched server-side *wsNetConn and a raw client-side
// *gorilla.Conn backed by an httptest server.  The caller must invoke cleanup()
// when done to release all resources.
func newTestWSPair(t *testing.T) (server *wsNetConn, client *gorilla.Conn, cleanup func()) {
	t.Helper()

	upgrader := gorilla.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	serverConnCh := make(chan *gorilla.Conn, 1)
	handlerDone := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		serverConnCh <- ws
		<-handlerDone // keep the handler alive until cleanup
	}))

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	clientWS, _, err := gorilla.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		close(handlerDone)
		srv.Close()
		t.Fatalf("dial: %v", err)
	}

	serverWS := <-serverConnCh

	return newWSNetConn(serverWS), clientWS, func() {
		close(handlerDone)
		clientWS.Close()
		serverWS.Close()
		srv.Close()
	}
}

// TestWSNetConnImplementsNetConn verifies at compile time that *wsNetConn
// satisfies the net.Conn interface.
func TestWSNetConnImplementsNetConn(t *testing.T) {
	t.Parallel()
	var _ net.Conn = (*wsNetConn)(nil)
}

// TestWSNetConnRead_SingleMessage verifies that a single message sent by the
// client is fully returned by one (or more) Read calls on the server side.
func TestWSNetConnRead_SingleMessage(t *testing.T) {
	t.Parallel()
	server, client, cleanup := newTestWSPair(t)
	defer cleanup()

	want := []byte("hello world")
	if err := client.WriteMessage(gorilla.BinaryMessage, want); err != nil {
		t.Fatalf("client write: %v", err)
	}

	got := make([]byte, len(want))
	n, err := server.Read(got)
	if err != nil {
		t.Fatalf("server Read: %v", err)
	}
	if n != len(want) {
		t.Fatalf("Read returned %d bytes, want %d", n, len(want))
	}
	if !bytes.Equal(got[:n], want) {
		t.Fatalf("Read returned %q, want %q", got[:n], want)
	}
}

// TestWSNetConnRead_Chunked verifies that a large message is returned
// correctly when the caller provides a buffer smaller than the message.
func TestWSNetConnRead_Chunked(t *testing.T) {
	t.Parallel()
	server, client, cleanup := newTestWSPair(t)
	defer cleanup()

	want := bytes.Repeat([]byte("abcde"), 100) // 500 bytes
	if err := client.WriteMessage(gorilla.BinaryMessage, want); err != nil {
		t.Fatalf("client write: %v", err)
	}

	// Read in small chunks of 64 bytes.
	var got []byte
	chunk := make([]byte, 64)
	for len(got) < len(want) {
		n, err := server.Read(chunk)
		if err != nil {
			t.Fatalf("Read error after %d bytes: %v", len(got), err)
		}
		got = append(got, chunk[:n]...)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("chunked Read returned unexpected data (len %d, want %d)", len(got), len(want))
	}
}

// TestWSNetConnRead_MultipleMessages verifies that successive Read calls
// drain messages in order.
func TestWSNetConnRead_MultipleMessages(t *testing.T) {
	t.Parallel()
	server, client, cleanup := newTestWSPair(t)
	defer cleanup()

	messages := [][]byte{
		[]byte("first"),
		[]byte("second"),
		[]byte("third"),
	}
	for _, m := range messages {
		if err := client.WriteMessage(gorilla.BinaryMessage, m); err != nil {
			t.Fatalf("client write: %v", err)
		}
	}

	for _, want := range messages {
		buf := make([]byte, 256)
		n, err := server.Read(buf)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if !bytes.Equal(buf[:n], want) {
			t.Fatalf("got %q, want %q", buf[:n], want)
		}
	}
}

// TestWSNetConnWrite verifies that data written via wsNetConn.Write is
// received intact by the client.
func TestWSNetConnWrite(t *testing.T) {
	t.Parallel()
	server, client, cleanup := newTestWSPair(t)
	defer cleanup()

	want := []byte("server says hello")
	n, err := server.Write(want)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(want) {
		t.Fatalf("Write returned n=%d, want %d", n, len(want))
	}

	_, got, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("client ReadMessage: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("client received %q, want %q", got, want)
	}
}

// TestWSNetConnWrite_ConcurrentSafe verifies that concurrent Write calls do
// not panic or return errors (gorilla requires serialised writes; the mutex
// inside wsNetConn should guarantee this).
func TestWSNetConnWrite_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	server, client, cleanup := newTestWSPair(t)
	defer cleanup()

	const goroutines = 20
	var wg sync.WaitGroup

	// Drain client messages concurrently so the server is never blocked.
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				client.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
				client.ReadMessage() //nolint:errcheck // best-effort drain
			}
		}
	}()

	var writeWg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		writeWg.Add(1)
		go func() {
			defer writeWg.Done()
			if _, err := server.Write([]byte("ping")); err != nil {
				t.Errorf("concurrent Write: %v", err)
			}
		}()
	}
	writeWg.Wait()
	close(stop)
	wg.Wait()
}

// TestWSNetConnWriteClose verifies that writeClose sends a well-formed
// WebSocket close frame carrying the expected status code.
func TestWSNetConnWriteClose(t *testing.T) {
	t.Parallel()
	server, client, cleanup := newTestWSPair(t)
	defer cleanup()

	const code = 4001
	if err := server.writeClose(code); err != nil {
		t.Fatalf("writeClose: %v", err)
	}

	// The client's next read should surface the close message.
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := client.ReadMessage()
	if err == nil {
		t.Fatal("expected close error from client ReadMessage, got nil")
	}

	closeErr, ok := err.(*gorilla.CloseError)
	if !ok {
		t.Fatalf("expected *gorilla.CloseError, got %T: %v", err, err)
	}
	if closeErr.Code != code {
		t.Fatalf("close code = %d, want %d", closeErr.Code, code)
	}
}

// TestWSNetConnAddrs verifies that LocalAddr and RemoteAddr return non-nil
// addresses (the exact values depend on the OS).
func TestWSNetConnAddrs(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newTestWSPair(t)
	defer cleanup()

	if server.LocalAddr() == nil {
		t.Error("LocalAddr() returned nil")
	}
	if server.RemoteAddr() == nil {
		t.Error("RemoteAddr() returned nil")
	}
}

// TestWSNetConnSetReadDeadline verifies that SetReadDeadline causes a
// subsequent Read to time out rather than block forever.
func TestWSNetConnSetReadDeadline(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newTestWSPair(t)
	defer cleanup()

	if err := server.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}

	buf := make([]byte, 16)
	_, err := server.Read(buf)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// The error should indicate a timeout.
	if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("expected net.Error with Timeout()=true, got %T: %v", err, err)
	}
}
