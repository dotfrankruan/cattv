// Package fileutil provides crash-safe local file helpers.
package fileutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteAtomic writes path through a callback using a temporary file in the
// same directory. The destination is replaced only after the callback and the
// file sync succeed. Existing permissions are preserved when possible.
func WriteAtomic(path string, perm os.FileMode, write func(io.Writer) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("set temporary file permissions: %w", err)
	}
	if err := write(tmp); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}

// ReadFile reads a file and returns an empty byte slice for a missing file
// when allowMissing is true.
func ReadFile(path string, allowMissing bool) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if allowMissing && errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return b, nil
}
