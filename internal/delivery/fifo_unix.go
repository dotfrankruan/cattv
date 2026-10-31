//go:build darwin || linux

package delivery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/dotfrankruan/cattv/internal/textsafe"
	"golang.org/x/sys/unix"
)

// FIFOServer delivers one complete rendered frame per sequential FIFO reader.
//
// After a reader is connected, the server atomically rotates the public FIFO
// path to a fresh inode before rendering. This lets the current reader observe
// EOF when the writer closes, even if the next reader is already opening the
// path. Concurrent readers that attach to the same inode still share one byte
// stream; the FIFO transport cannot provide per-reader framing.
type FIFOServer struct {
	Path      string
	Generator Generator
	Log       io.Writer
	OnReady   func()
}

// Run creates the FIFO and serves frames until ctx is canceled or a fatal
// endpoint error occurs. A clean context shutdown returns nil after removing
// the FIFO path and releasing the server lock.
func (s *FIFOServer) Run(ctx context.Context) (retErr error) {
	if err := validateGenerator(s.Generator); err != nil {
		return err
	}
	if s.Path == "" {
		return errors.New("FIFO path is required")
	}
	if s.Log == nil {
		s.Log = io.Discard
	}
	path, err := filepath.Abs(s.Path)
	if err != nil {
		return fmt.Errorf("resolve FIFO path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create FIFO directory: %w", err)
	}

	// A reader disappearing mid-write causes EPIPE. The server handles the
	// error and must not be killed by SIGPIPE on platforms that deliver it.
	signal.Ignore(syscall.SIGPIPE)

	lock, err := acquireFileLock(path + ".lock")
	if err != nil {
		return err
	}
	defer lock.release()

	cleanupStaleTempFIFOs(path)
	info, err := prepareFIFOPath(path)
	if err != nil {
		return err
	}
	defer func() {
		if cleanupErr := cleanupFIFOPath(path, info); cleanupErr != nil {
			s.logf("cleanup FIFO: %v", cleanupErr)
			if retErr == nil {
				retErr = cleanupErr
			}
		}
	}()
	if s.OnReady != nil {
		s.OnReady()
	}

	for {
		if ctx.Err() != nil {
			return nil
		}
		writer, err := openFIFOWriter(ctx, path, info)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wait for FIFO reader: %w", err)
		}

		nextInfo, err := rotateFIFO(path, info)
		if err != nil {
			_ = writer.Close()
			return fmt.Errorf("rotate FIFO for the next reader: %w", err)
		}
		info = nextInfo

		payload, genErr := checkedFrame(s.Generator, ctx)
		if ctx.Err() != nil {
			_ = writer.Close()
			return nil
		}
		if genErr != nil {
			s.logf("request failed: %v", genErr)
			payload = frameForError(genErr)
		}

		writeErr := writeAll(writer, payload)
		closeErr := writer.Close()
		if ctx.Err() != nil {
			return nil
		}
		if writeErr != nil {
			if errors.Is(writeErr, syscall.EPIPE) || errors.Is(writeErr, syscall.ECONNRESET) {
				s.logf("reader disconnected before the frame completed")
			} else {
				s.logf("write FIFO frame: %v", writeErr)
			}
		}
		if closeErr != nil {
			s.logf("close FIFO writer: %v", closeErr)
		}
	}
}

func (s *FIFOServer) logf(format string, args ...any) {
	fmt.Fprintln(s.Log, "cattv fifo: "+textsafe.Terminal(fmt.Sprintf(format, args...)))
}

func prepareFIFOPath(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := unix.Mkfifo(path, 0o600); err != nil {
			return nil, fmt.Errorf("create FIFO: %w", err)
		}
		info, err = os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect new FIFO: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("inspect FIFO path: %w", err)
	} else if !modeIsFIFO(info) {
		return nil, fmt.Errorf("refusing to replace existing non-FIFO path: %s", path)
	} else if !ownedByCurrentUser(info) {
		return nil, fmt.Errorf("refusing to use FIFO not owned by the current user: %s", path)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("secure FIFO permissions: %w", err)
	}
	info, err = os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("re-inspect FIFO: %w", err)
	}
	if !modeIsFIFO(info) || !ownedByCurrentUser(info) {
		return nil, fmt.Errorf("unsafe FIFO path after preparation: %s", path)
	}
	return info, nil
}

func rotateFIFO(path string, current os.FileInfo) (os.FileInfo, error) {
	now, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect current FIFO: %w", err)
	}
	if !modeIsFIFO(now) || !ownedByCurrentUser(now) || !os.SameFile(now, current) {
		return nil, fmt.Errorf("FIFO path changed to an unexpected object: %s", path)
	}

	tempPath, err := createTempFIFO(path)
	if err != nil {
		return nil, err
	}
	installed := false
	defer func() {
		if !installed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := os.Rename(tempPath, path); err != nil {
		return nil, fmt.Errorf("install next FIFO: %w", err)
	}
	installed = true
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect rotated FIFO: %w", err)
	}
	if !modeIsFIFO(info) || !ownedByCurrentUser(info) {
		return nil, fmt.Errorf("unsafe FIFO after rotation: %s", path)
	}
	return info, nil
}

func tempFIFOPrefix(path string) string {
	return "." + filepath.Base(path) + ".next-"
}

// cleanupStaleTempFIFOs removes only hidden next-inode FIFOs belonging to this
// endpoint. A previous server may have crashed between mkfifo and rename.
func cleanupStaleTempFIFOs(path string) {
	dir := filepath.Dir(path)
	prefix := tempFIFOPrefix(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !modeIsFIFO(info) || !ownedByCurrentUser(info) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

func createTempFIFO(targetPath string) (string, error) {
	dir := filepath.Dir(targetPath)
	prefix := tempFIFOPrefix(targetPath)
	var suffix [8]byte
	for tries := 0; tries < 16; tries++ {
		if _, err := rand.Read(suffix[:]); err != nil {
			return "", fmt.Errorf("generate FIFO name: %w", err)
		}
		path := filepath.Join(dir, prefix+hex.EncodeToString(suffix[:]))
		err := unix.Mkfifo(path, 0o600)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create next FIFO: %w", err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			_ = os.Remove(path)
			return "", fmt.Errorf("secure next FIFO: %w", err)
		}
		return path, nil
	}
	return "", errors.New("could not allocate a unique FIFO path")
}

type fifoOpenResult struct {
	file *os.File
	err  error
}

func openFIFOWriter(ctx context.Context, path string, expected os.FileInfo) (*os.File, error) {
	result := make(chan fifoOpenResult, 1)
	go func() {
		fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			result <- fifoOpenResult{err: err}
			return
		}
		f := os.NewFile(uintptr(fd), path)
		info, statErr := f.Stat()
		if statErr != nil {
			_ = f.Close()
			result <- fifoOpenResult{err: statErr}
			return
		}
		if !modeIsFIFO(info) || !ownedByCurrentUser(info) || !os.SameFile(info, expected) {
			_ = f.Close()
			result <- fifoOpenResult{err: fmt.Errorf("FIFO path changed before a reader connected: %s", path)}
			return
		}
		result <- fifoOpenResult{file: f}
	}()

	select {
	case opened := <-result:
		return opened.file, opened.err
	case <-ctx.Done():
		// A blocking open(2) cannot be interrupted directly. Opening the read
		// end non-blocking completes the pending writer open so it can be
		// closed and cleaned up. No sleep or polling loop is involved.
		unblockFD, unblockErr := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		opened := <-result
		if unblockErr == nil {
			_ = unix.Close(unblockFD)
		}
		if opened.file != nil {
			_ = opened.file.Close()
		}
		return nil, ctx.Err()
	}
}

func cleanupFIFOPath(path string, expected os.FileInfo) error {
	now, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !modeIsFIFO(now) || !ownedByCurrentUser(now) || !os.SameFile(now, expected) {
		return nil
	}

	// If a reader is waiting, briefly opening the write end succeeds and,
	// after unlinking the path, closing it delivers EOF. If no reader is
	// waiting, open returns ENXIO and unlinking is sufficient.
	fd, openErr := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if openErr != nil && !errors.Is(openErr, unix.ENXIO) {
		return fmt.Errorf("open FIFO for cleanup: %w", openErr)
	}
	if fd >= 0 {
		f := os.NewFile(uintptr(fd), path)
		openedInfo, statErr := f.Stat()
		if statErr != nil || !os.SameFile(openedInfo, expected) {
			_ = f.Close()
			return nil
		}
		removeErr := os.Remove(path)
		_ = f.Close()
		return removeErr
	}
	return os.Remove(path)
}
