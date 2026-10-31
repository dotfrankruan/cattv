//go:build darwin || linux

package delivery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type socketHarness struct {
	cancel   context.CancelFunc
	done     chan error
	ready    <-chan struct{}
	stopOnce sync.Once
	stopErr  error
}

func (h *socketHarness) stop(t *testing.T) {
	t.Helper()
	h.stopOnce.Do(func() {
		h.cancel()
		select {
		case h.stopErr = <-h.done:
		case <-time.After(4 * time.Second):
			h.stopErr = errors.New("Unix socket server did not stop")
		}
	})
	if h.stopErr != nil {
		t.Fatalf("server shutdown: %v", h.stopErr)
	}
}

func startSocketHarness(t *testing.T, path string, gen GeneratorFunc) *socketHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	server := &SocketServer{
		Path:      path,
		Generator: gen,
		OnReady:   func() { close(ready) },
	}
	go func() { done <- server.Run(ctx) }()
	h := &socketHarness{cancel: cancel, done: done, ready: ready}
	t.Cleanup(func() { h.stop(t) })
	select {
	case <-ready:
	case err := <-done:
		h.stopOnce.Do(func() {})
		t.Fatalf("socket server failed before ready: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("socket server did not become ready")
	}
	return h
}

func socketTestPath(t *testing.T) string {
	t.Helper()
	// macOS sockaddr_un limits socket paths to roughly 104 bytes. Temporary
	// directories that embed a long test name can exceed that, so allocate a
	// short dedicated directory.
	dir, err := os.MkdirTemp("", "cattv-sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "c.sock")
}

func TestSocketSequentialRequests(t *testing.T) {
	path := socketTestPath(t)
	var calls atomic.Int32
	h := startSocketHarness(t, path, func(context.Context) ([]byte, error) {
		n := calls.Add(1)
		return []byte(fmt.Sprintf("strict-frame-%d", n)), nil
	})

	first, err := FetchFrame(context.Background(), path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != "strict-frame-1" {
		t.Fatalf("first = %q", first)
	}
	second, err := FetchFrame(context.Background(), path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != "strict-frame-2" {
		t.Fatalf("second = %q", second)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || !modeIsSocket(info) {
		t.Fatalf("socket mode = %v", info.Mode())
	}

	h.stop(t)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still exists after shutdown: %v", err)
	}
}

func TestSocketConcurrentClientsGetCompleteFrames(t *testing.T) {
	path := socketTestPath(t)
	var calls atomic.Int32
	h := startSocketHarness(t, path, func(context.Context) ([]byte, error) {
		n := calls.Add(1)
		return []byte(fmt.Sprintf("complete-response-%03d", n)), nil
	})

	const clients = 8
	results := make([][]byte, clients)
	errs := make([]error, clients)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = FetchFrame(context.Background(), path, 5*time.Second)
		}(i)
	}
	close(start)
	wg.Wait()

	seen := make(map[string]bool, clients)
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("client %d: %v", i, errs[i])
		}
		if !strings.HasPrefix(string(results[i]), "complete-response-") {
			t.Fatalf("client %d got a partial frame: %q", i, results[i])
		}
		if seen[string(results[i])] {
			t.Fatalf("client %d received a duplicate response %q", i, results[i])
		}
		seen[string(results[i])] = true
	}
	if len(seen) != clients {
		t.Fatalf("received %d unique responses for %d clients", len(seen), clients)
	}
	h.stop(t)
}

func TestSocketErrorIsReturnedToClientAndServerContinues(t *testing.T) {
	path := socketTestPath(t)
	var calls atomic.Int32
	h := startSocketHarness(t, path, func(context.Context) ([]byte, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("network unavailable")
		}
		return []byte("recovered-frame"), nil
	})
	_, err := FetchFrame(context.Background(), path, 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("error = %v", err)
	}
	got, err := FetchFrame(context.Background(), path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "recovered-frame" {
		t.Fatalf("got %q", got)
	}
	h.stop(t)
}

func TestSocketRefusesNonSocketPath(t *testing.T) {
	path := socketTestPath(t)
	if err := os.WriteFile(path, []byte("important data"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &SocketServer{Path: path, Generator: GeneratorFunc(func(context.Context) ([]byte, error) {
		return []byte("frame"), nil
	})}
	err := server.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "non-socket") {
		t.Fatalf("error = %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || string(got) != "important data" {
		t.Fatalf("existing object was modified: %q, %v", got, readErr)
	}
}

func TestSocketReplacesStaleSocket(t *testing.T) {
	path := socketTestPath(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if unixListener, ok := listener.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("stale socket was not left behind: %v", err)
	}

	h := startSocketHarness(t, path, func(context.Context) ([]byte, error) {
		return []byte("fresh-stale-socket"), nil
	})
	got, err := FetchFrame(context.Background(), path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fresh-stale-socket" {
		t.Fatalf("got %q", got)
	}
	h.stop(t)
}

func TestSocketRejectsSecondServer(t *testing.T) {
	path := socketTestPath(t)
	h := startSocketHarness(t, path, func(context.Context) ([]byte, error) {
		return []byte("frame"), nil
	})
	second := &SocketServer{Path: path, Generator: GeneratorFunc(func(context.Context) ([]byte, error) {
		return []byte("other"), nil
	})}
	err := second.Run(context.Background())
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("error = %v", err)
	}
	h.stop(t)
}

func TestSocketFramingPreservesBinaryBytes(t *testing.T) {
	path := socketTestPath(t)
	payload := bytes.Repeat([]byte{0x00, 0x1b, 0xff, '\n', '\r'}, 100_000)
	h := startSocketHarness(t, path, func(context.Context) ([]byte, error) {
		return payload, nil
	})
	got, err := FetchFrame(context.Background(), path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("binary payload changed: got %d bytes, want %d", len(got), len(payload))
	}
	h.stop(t)
}
