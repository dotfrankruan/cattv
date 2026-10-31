// Package config resolves CatTV's data locations and environment defaults.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appDirName = "cattv"

// Config contains user-configurable locations. Empty optional values are
// resolved by the command that needs them.
type Config struct {
	DataDir     string
	CatalogPath string
	LibraryPath string
	StatePath   string
	ParquetPath string
	DuckDBPath  string
}

// Load resolves configuration from CATTV_* environment variables and
// platform-appropriate data directories.
func Load() (Config, error) {
	dir, err := DataDir()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		DataDir:     dir,
		CatalogPath: filepath.Join(dir, "catalog.jsonl.gz"),
		LibraryPath: filepath.Join(dir, "library.json"),
		StatePath:   filepath.Join(dir, "state.json"),
		ParquetPath: os.Getenv("CATTV_PARQUET"),
		DuckDBPath:  os.Getenv("CATTV_DUCKDB"),
	}
	override(&cfg.CatalogPath, "CATTV_CATALOG")
	override(&cfg.LibraryPath, "CATTV_LIBRARY")
	override(&cfg.StatePath, "CATTV_STATE")
	if err := ensureDistinct(
		namedPath{"catalog", cfg.CatalogPath},
		namedPath{"library", cfg.LibraryPath},
		namedPath{"state", cfg.StatePath},
	); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

type namedPath struct {
	label string
	path  string
}

// ensureDistinct prevents a misconfigured override from making two writers
// share one file. Catalog, library, and history formats are not interchangeable.
func ensureDistinct(paths ...namedPath) error {
	seen := make(map[string]string, len(paths))
	for _, p := range paths {
		key, err := filepath.Abs(p.path)
		if err != nil {
			key = filepath.Clean(p.path)
		}
		if prior, ok := seen[key]; ok {
			return fmt.Errorf("%s path and %s path are the same file: %s", prior, p.label, key)
		}
		seen[key] = p.label
	}
	return nil
}

func override(dst *string, env string) {
	if v := os.Getenv(env); v != "" {
		*dst = v
	}
}

// DataDir returns CATTV_DATA_DIR when set, otherwise the conventional
// application data directory for the current OS.
func DataDir() (string, error) {
	if dir := os.Getenv("CATTV_DATA_DIR"); dir != "" {
		return filepath.Clean(dir), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", appDirName), nil
	case "linux", "freebsd", "openbsd", "netbsd":
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, appDirName), nil
		}
		return filepath.Join(home, ".local", "share", appDirName), nil
	default:
		return "", errors.New("unsupported operating system; set CATTV_DATA_DIR")
	}
}
