//go:build darwin || linux

package delivery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dotfrankruan/cattv/internal/textsafe"
)

const defaultShutdownTimeout = 3 * time.Second

// SocketServer exposes strict one-request/one-response semantics over a Unix
// domain socket using HTTP/1.1 framing. It is the reliable alternative to the
// FIFO's shared, unframed byte stream.
type SocketServer struct {
	Path            string
	Generator       Generator
	Log             io.Writer
	OnReady         func()
	ShutdownTimeout time.Duration

	logMu sync.Mutex
}

// Run listens and serves requests until ctx is canceled.
func (s *SocketServer) Run(ctx context.Context) (retErr error) {
	if err := validateGenerator(s.Generator); err != nil {
		return err
	}
	if s.Path == "" {
		return errors.New("Unix socket path is required")
	}
	if s.Log == nil {
		s.Log = io.Discard
	}
	timeout := s.ShutdownTimeout
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}
	path, err := filepath.Abs(s.Path)
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}

	lock, err := acquireFileLock(path + ".lock")
	if err != nil {
		return err
	}
	defer lock.release()

	if err := prepareSocketPath(path); err != nil {
		return err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("listen on Unix socket: %w", err)
	}
	defer listener.Close()
	if unixListener, ok := listener.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("secure Unix socket: %w", err)
	}
	socketInfo, err := os.Lstat(path)
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("inspect Unix socket: %w", err)
	}
	if !modeIsSocket(socketInfo) || !ownedByCurrentUser(socketInfo) {
		_ = os.Remove(path)
		return fmt.Errorf("unsafe Unix socket path: %s", path)
	}
	defer func() {
		_ = listener.Close()
		if cleanupErr := cleanupSocketPath(path, socketInfo); cleanupErr != nil {
			s.logf("cleanup socket: %v", cleanupErr)
			if retErr == nil {
				retErr = cleanupErr
			}
		}
	}()

	baseCtx, cancelBase := context.WithCancel(ctx)
	defer cancelBase()
	var frameSem sync.Mutex
	handler := http.NewServeMux()
	handler.HandleFunc("/cat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		frameSem.Lock()
		payload, err := checkedFrame(s.Generator, r.Context())
		frameSem.Unlock()
		if err != nil {
			http.Error(w, "cattv: "+textsafe.Terminal(err.Error()), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/x-cattv-terminal-frame")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		if _, err := w.Write(payload); err != nil {
			s.logf("socket response write failed: %v", err)
		}
	})
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return baseCtx
		},
	}

	if s.OnReady != nil {
		s.OnReady()
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		cancelBase()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
		shutdownErr := server.Shutdown(shutdownCtx)
		cancel()
		if shutdownErr != nil {
			_ = server.Close()
		}
		serveErr := <-serveResult
		if shutdownErr != nil && !errors.Is(shutdownErr, context.DeadlineExceeded) {
			return fmt.Errorf("shut down Unix socket server: %w", shutdownErr)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("serve Unix socket: %w", serveErr)
		}
		return nil
	case serveErr := <-serveResult:
		cancelBase()
		_ = server.Close()
		if serveErr == nil || errors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve Unix socket: %w", serveErr)
	}
}

func (s *SocketServer) logf(format string, args ...any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	fmt.Fprintf(s.Log, "cattv socket: "+format+"\n", args...)
}

func prepareSocketPath(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect socket path: %w", err)
	}
	if !modeIsSocket(info) {
		return fmt.Errorf("refusing to replace existing non-socket path: %s", path)
	}
	if !ownedByCurrentUser(info) {
		return fmt.Errorf("refusing to replace socket not owned by the current user: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale Unix socket: %w", err)
	}
	return nil
}

func cleanupSocketPath(path string, expected os.FileInfo) error {
	now, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !modeIsSocket(now) || !ownedByCurrentUser(now) || !os.SameFile(now, expected) {
		return nil
	}
	return os.Remove(path)
}

// FetchFrame requests exactly one framed terminal response from a Unix socket
// server. The HTTP body boundary is what makes this transport reliable for
// concurrent callers.
func FetchFrame(ctx context.Context, socketPath string, timeout time.Duration) ([]byte, error) {
	if socketPath == "" {
		return nil, errors.New("Unix socket path is required")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		Proxy:              nil,
		DisableCompression: true,
		DisableKeepAlives:  true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	client := &http.Client{Transport: transport}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://unix/cat", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "cattv-fifo-client")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to CatTV socket: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		msg := textsafe.Terminal(strings.TrimSpace(string(body)))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("CatTV socket returned %s: %s", resp.Status, msg)
	}
	if resp.ContentLength > MaxFrameSize {
		return nil, fmt.Errorf("CatTV socket response is too large (%d bytes)", resp.ContentLength)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, MaxFrameSize+1))
	if err != nil {
		return nil, fmt.Errorf("read CatTV socket response: %w", err)
	}
	if len(payload) > MaxFrameSize {
		return nil, errors.New("CatTV socket response exceeds the frame limit")
	}
	if len(payload) == 0 {
		return nil, errors.New("CatTV socket returned an empty response")
	}
	return payload, nil
}
