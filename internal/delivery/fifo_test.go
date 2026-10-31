//go:build darwin || linux

package delivery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type fifoHarness struct {
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan error
	ready    <-chan struct{}
	logs     *bytes.Buffer
	stopOnce sync.Once
	stopErr  error
}

func (h *fifoHarness) stop(t *testing.T) {
	t.Helper()
	h.stopOnce.Do(func() {
		h.cancel()
		select {
		case h.stopErr = <-h.done:
		case <-time.After(3 * time.Second):
			h.stopErr = errors.New("FIFO server did not stop")
		}
	})
	if h.stopErr != nil {
		t.Fatalf("server shutdown: %v", h.stopErr)
	}
}

func startFIFOHarness(t *testing.T, path string, gen GeneratorFunc) *fifoHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	var logs bytes.Buffer
	server := &FIFOServer{
		Path:      path,
		Generator: gen,
		Log:       &logs,
		OnReady:   func() { close(ready) },
	}
	go func() { done <- server.Run(ctx) }()
	h := &fifoHarness{ctx: ctx, cancel: cancel, done: done, ready: ready, logs: &logs}
	t.Cleanup(func() { h.stop(t) })
	select {
	case <-ready:
	case err := <-done:
		h.stopOnce.Do(func() {})
		t.Fatalf("FIFO server failed before ready: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("FIFO server did not become ready")
	}
	return h
}

func readFIFO(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open FIFO reader: %v", err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read FIFO: %v", err)
	}
	return data
}

func TestFIFOSequentialReadsAndLazyGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cat.fifo")
	var calls atomic.Int32
	h := startFIFOHarness(t, path, func(context.Context) ([]byte, error) {
		n := calls.Add(1)
		return []byte(fmt.Sprintf("complete-frame-%d", n)), nil
	})

	if got := calls.Load(); got != 0 {
		t.Fatalf("generator ran %d time(s) before a reader connected", got)
	}

	first := readFIFO(t, path)
	if string(first) != "complete-frame-1" {
		t.Fatalf("first frame = %q", first)
	}
	second := readFIFO(t, path)
	if string(second) != "complete-frame-2" {
		t.Fatalf("second frame = %q", second)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("generator calls = %d", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("FIFO permissions = %o", info.Mode().Perm())
	}
	if !modeIsFIFO(info) {
		t.Fatal("path is not a FIFO")
	}

	h.stop(t)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("FIFO path still exists after shutdown: %v", err)
	}
}

func TestFIFOFailureDoesNotStopServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cat.fifo")
	var calls atomic.Int32
	h := startFIFOHarness(t, path, func(context.Context) ([]byte, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("network unavailable\nplease retry")
		}
		return []byte("second-frame"), nil
	})
	failure := readFIFO(t, path)
	if !strings.Contains(string(failure), "network unavailable") {
		t.Fatalf("error frame = %q", failure)
	}
	if !strings.HasSuffix(string(failure), "\n") {
		t.Fatalf("error frame is not newline terminated: %q", failure)
	}
	if strings.Contains(string(failure), "\x1b") {
		t.Fatal("error frame contains an escape sequence")
	}
	if second := readFIFO(t, path); string(second) != "second-frame" {
		t.Fatalf("second frame = %q", second)
	}
	h.stop(t)
}

// readFIFOFD reads a non-blocking FIFO fd with poll so the test does not depend
// on Go's os.File poller state when multiple reader fds share a FIFO.
func readFIFOFD(fd int) ([]byte, error) {
	var out []byte
	buf := make([]byte, 32*1024)
	for {
		n, err := unix.Read(fd, buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if errors.Is(err, unix.EAGAIN) {
				pfds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP}}
				if _, pollErr := unix.Poll(pfds, -1); pollErr != nil && !errors.Is(pollErr, unix.EINTR) {
					return out, pollErr
				}
				continue
			}
			return out, err
		}
		if n == 0 {
			return out, nil
		}
	}
}

func TestFIFOConcurrentReadersShareOneUnframedStream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cat.fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	// Attach both readers to the initial FIFO inode before the server rotates
	// the path. This makes the concurrency test deterministic with respect to
	// which inode each reader sees.
	readerA, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(readerA)
	readerB, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(readerB)

	payload := []byte("one-unframed-image")
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	h := startFIFOHarness(t, path, func(ctx context.Context) ([]byte, error) {
		once.Do(func() { close(started) })
		select {
		case <-release:
			return payload, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("generator did not start")
	}
	close(release)

	var wg sync.WaitGroup
	results := make([][]byte, 2)
	errs := make([]error, 2)
	for i, fd := range []int{readerA, readerB} {
		wg.Add(1)
		go func(i, fd int) {
			defer wg.Done()
			results[i], errs[i] = readFIFOFD(fd)
		}(i, fd)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("reader %d: %v", i, errs[i])
		}
	}
	total := len(results[0]) + len(results[1])
	if total != len(payload) {
		t.Fatalf("readers consumed %d bytes total, want one frame of %d; this would mean duplication", total, len(payload))
	}
	if (len(results[0]) == len(payload)) && (len(results[1]) == len(payload)) {
		t.Fatal("both readers received a complete frame despite the shared FIFO stream")
	}
	h.stop(t)
}

func TestFIFOReaderDisconnectDoesNotStopServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cat.fifo")
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	h := startFIFOHarness(t, path, func(ctx context.Context) ([]byte, error) {
		n := calls.Add(1)
		if n == 1 {
			close(started)
			select {
			case <-release:
				return []byte("abandoned-frame"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return []byte("second-frame"), nil
	})
	first, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("generator did not start")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)

	second := readFIFO(t, path)
	if string(second) != "second-frame" {
		t.Fatalf("second frame = %q", second)
	}
	h.stop(t)
	if !strings.Contains(h.logs.String(), "reader disconnected") {
		t.Fatalf("disconnect was not logged: %q", h.logs.String())
	}
}

func TestFIFOShutdownRemovesPathAndUnblocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cat.fifo")
	h := startFIFOHarness(t, path, func(context.Context) ([]byte, error) {
		return []byte("unused"), nil
	})
	h.stop(t)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("FIFO path still exists after shutdown: %v", err)
	}
}

func TestFIFORefusesNonFIFOPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cat.fifo")
	if err := os.WriteFile(path, []byte("important data"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &FIFOServer{Path: path, Generator: GeneratorFunc(func(context.Context) ([]byte, error) {
		return []byte("frame"), nil
	})}
	err := server.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "non-FIFO") {
		t.Fatalf("error = %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || string(got) != "important data" {
		t.Fatalf("existing object was modified: %q, %v", got, readErr)
	}
}

func TestFIFOReusesStaleFIFOAndSecuresMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cat.fifo")
	if err := unix.Mkfifo(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	h := startFIFOHarness(t, path, func(context.Context) ([]byte, error) {
		return []byte("stale-reused"), nil
	})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	if got := readFIFO(t, path); string(got) != "stale-reused" {
		t.Fatalf("frame = %q", got)
	}
	h.stop(t)
}

func TestFIFORejectsSecondServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cat.fifo")
	h := startFIFOHarness(t, path, func(context.Context) ([]byte, error) {
		return []byte("frame"), nil
	})
	second := &FIFOServer{Path: path, Generator: GeneratorFunc(func(context.Context) ([]byte, error) {
		return []byte("other"), nil
	})}
	err := second.Run(context.Background())
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("error = %v", err)
	}
	h.stop(t)
}

func TestFIFORemovesStaleRotationTemps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cat.fifo")
	stale := filepath.Join(dir, tempFIFOPrefix(path)+"deadbeef")
	if err := unix.Mkfifo(stale, 0o600); err != nil {
		t.Fatal(err)
	}
	h := startFIFOHarness(t, path, func(context.Context) ([]byte, error) {
		return []byte("frame"), nil
	})
	if _, err := os.Lstat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale rotation FIFO still exists: %v", err)
	}
	h.stop(t)
}
