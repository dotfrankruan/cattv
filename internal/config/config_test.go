package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsesExplicitEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CATTV_DATA_DIR", dir)
	t.Setenv("CATTV_CATALOG", filepath.Join(dir, "custom.gz"))
	t.Setenv("CATTV_LIBRARY", filepath.Join(dir, "custom-library.json"))
	t.Setenv("CATTV_STATE", filepath.Join(dir, "custom-state.json"))
	t.Setenv("CATTV_PARQUET", "/data/cats.parquet")
	t.Setenv("CATTV_DUCKDB", "/opt/duckdb")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != dir {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
	if cfg.CatalogPath != filepath.Join(dir, "custom.gz") {
		t.Fatalf("CatalogPath = %q", cfg.CatalogPath)
	}
	if cfg.LibraryPath != filepath.Join(dir, "custom-library.json") {
		t.Fatalf("LibraryPath = %q", cfg.LibraryPath)
	}
	if cfg.StatePath != filepath.Join(dir, "custom-state.json") {
		t.Fatalf("StatePath = %q", cfg.StatePath)
	}
	if cfg.ParquetPath != "/data/cats.parquet" || cfg.DuckDBPath != "/opt/duckdb" {
		t.Fatalf("unexpected import config: %+v", cfg)
	}
}

func TestLoadDefaultsInsideDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CATTV_DATA_DIR", dir)
	for _, name := range []string{"CATTV_CATALOG", "CATTV_LIBRARY", "CATTV_STATE"} {
		old, ok := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(name, old)
			}
		})
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CatalogPath != filepath.Join(dir, "catalog.jsonl.gz") {
		t.Fatalf("CatalogPath = %q", cfg.CatalogPath)
	}
	if cfg.LibraryPath != filepath.Join(dir, "library.json") {
		t.Fatalf("LibraryPath = %q", cfg.LibraryPath)
	}
}

func TestLoadRejectsSharedDataPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CATTV_DATA_DIR", dir)
	shared := filepath.Join(dir, "shared.json")
	t.Setenv("CATTV_CATALOG", shared)
	t.Setenv("CATTV_LIBRARY", shared)
	t.Setenv("CATTV_STATE", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected duplicate data path error")
	}
}
