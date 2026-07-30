package remote

import (
	"bytes"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

type partialWriter struct {
	maxChunk int
	buf      bytes.Buffer
}

func (w *partialWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n := w.maxChunk
	if n > len(p) {
		n = len(p)
	}
	return w.buf.Write(p[:n])
}

type errWriter struct{}

func (w errWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("boom")
}

func TestOpenOrCreateLogFileForWrite_CreatesNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wp-cli-guid.log")

	f, err := openOrCreateLogFileForWrite(path)
	if err != nil {
		t.Fatalf("openOrCreateLogFileForWrite() error = %v", err)
	}
	defer f.Close()

	if _, err = f.Write([]byte("hello")); err != nil {
		t.Fatalf("write() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat() error = %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("expected regular file, got mode=%v", info.Mode())
	}
}

func TestOpenOrCreateLogFileForWrite_TruncatesExistingRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wp-cli-guid.log")

	if err := os.WriteFile(path, []byte("existing-content"), 0600); err != nil {
		t.Fatalf("writeFile() setup error = %v", err)
	}

	f, err := openOrCreateLogFileForWrite(path)
	if err != nil {
		t.Fatalf("openOrCreateLogFileForWrite() error = %v", err)
	}
	defer f.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat() error = %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("expected truncated file size 0, got %d", info.Size())
	}
}

func TestOpenOrCreateLogFileForWrite_RejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on windows")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "target.log")
	link := filepath.Join(dir, "wp-cli-guid.log")

	if err := os.WriteFile(target, []byte("do-not-touch"), 0600); err != nil {
		t.Fatalf("writeFile() setup error = %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink() setup error = %v", err)
	}

	f, err := openOrCreateLogFileForWrite(link)
	if err == nil {
		_ = f.Close()
		t.Fatal("expected error when log path is a symlink")
	}

	data, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("readFile() error = %v", readErr)
	}
	if string(data) != "do-not-touch" {
		t.Fatalf("symlink target was modified, got %q", string(data))
	}
}

func TestOpenOrCreateLogFileForWrite_RejectsFifoWithoutBlocking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fifo behavior differs on windows")
	}

	path := filepath.Join(t.TempDir(), "wp-cli-guid.fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatalf("mkfifo() setup error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		f, err := openOrCreateLogFileForWrite(path)
		if err == nil {
			_ = f.Close()
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error when log path is a fifo")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("openOrCreateLogFileForWrite() blocked on fifo")
	}
}

func TestStreamLogs_MissingFileMessage(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		streamLogs(serverConn, "guid-that-does-not-exist")
	}()

	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("setReadDeadline() error = %v", err)
	}

	buf := make([]byte, 512)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("read() error = %v", err)
	}

	got := string(buf[:n])
	if got != "The WP CLI log file for GUID guid-that-does-not-exist does not exist\n" {
		t.Fatalf("unexpected message: %q", got)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("streamLogs did not return")
	}
}

func TestOpenLogFileForRead_RegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wp-cli-guid.log")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatalf("writeFile() setup error = %v", err)
	}

	f, err := openLogFileForRead(path)
	if err != nil {
		t.Fatalf("openLogFileForRead() error = %v", err)
	}
	defer f.Close()
}

func TestOpenLogFileForRead_RejectsFifoWithoutBlocking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fifo behavior differs on windows")
	}

	path := filepath.Join(t.TempDir(), "wp-cli-guid.fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatalf("mkfifo() setup error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		f, err := openLogFileForRead(path)
		if err == nil {
			_ = f.Close()
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error when log path is a fifo")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("openLogFileForRead() blocked on fifo")
	}
}

func TestOpenLogFileForRead_RejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on windows")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "target.log")
	link := filepath.Join(dir, "wp-cli-guid.log")

	if err := os.WriteFile(target, []byte("do-not-touch"), 0600); err != nil {
		t.Fatalf("writeFile() setup error = %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink() setup error = %v", err)
	}

	f, err := openLogFileForRead(link)
	if err == nil {
		_ = f.Close()
		t.Fatal("expected error when log path is a symlink")
	}

	data, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("readFile() error = %v", readErr)
	}
	if string(data) != "do-not-touch" {
		t.Fatalf("symlink target was modified, got %q", string(data))
	}
}

func TestWriteAll_PartialWrites(t *testing.T) {
	w := &partialWriter{maxChunk: 2}
	if err := writeAll(w, []byte("abcdef")); err != nil {
		t.Fatalf("writeAll() error = %v", err)
	}
	if got := w.buf.String(); got != "abcdef" {
		t.Fatalf("writeAll() wrote %q, want %q", got, "abcdef")
	}
}

func TestWriteAll_WriteError(t *testing.T) {
	err := writeAll(errWriter{}, []byte("abc"))
	if err == nil {
		t.Fatal("expected writeAll() to return error")
	}
}
