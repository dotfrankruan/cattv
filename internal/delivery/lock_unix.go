//go:build darwin || linux

package delivery

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// fileLock provides process-level exclusion for one delivery endpoint. The
// lock file itself is kept after shutdown; flock ownership disappears with the
// process, so a leftover file is harmless and cannot block a later restart.
type fileLock struct {
	file *os.File
}

func acquireFileLock(path string) (*fileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create endpoint directory: %w", err)
	}
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open endpoint lock: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	closeOnError := func(err error) (*fileLock, error) {
		_ = f.Close()
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		return closeOnError(fmt.Errorf("inspect endpoint lock: %w", err))
	}
	if !info.Mode().IsRegular() || !ownedByCurrentUser(info) {
		return closeOnError(fmt.Errorf("unsafe endpoint lock file: %s", path))
	}
	if err := f.Chmod(0o600); err != nil {
		return closeOnError(fmt.Errorf("secure endpoint lock: %w", err))
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return closeOnError(ErrAlreadyRunning)
		}
		return closeOnError(fmt.Errorf("lock delivery endpoint: %w", err))
	}
	return &fileLock{file: f}, nil
}

func (l *fileLock) release() {
	if l == nil || l.file == nil {
		return
	}
	_ = unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	_ = l.file.Close()
	l.file = nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func modeIsFIFO(info os.FileInfo) bool {
	return info.Mode()&os.ModeNamedPipe != 0
}

func modeIsSocket(info os.FileInfo) bool {
	return info.Mode()&os.ModeSocket != 0
}
