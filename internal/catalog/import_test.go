package catalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportPipelineWithFakeDuckDB(t *testing.T) {
	dir := t.TempDir()
	parquet := filepath.Join(dir, "cats.parquet")
	if err := os.WriteFile(parquet, []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}
	duckdb := filepath.Join(dir, "fake-duckdb")
	script := `#!/bin/sh
cat <<'EOF'
{"id":"alpha","page_url":"https://unsplash.com/photos/alpha","url_320":"https://images.example/alpha?w=320","curated":true,"quality_score":81.2,"attribution":"Photo by A"}
{"id":"beta","page_url":"https://unsplash.com/photos/beta","image_url":"https://images.example/beta","curated":false,"quality_score":40}
EOF
`
	if err := os.WriteFile(duckdb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "runtime", "catalog.jsonl.gz")
	meta, err := Import(context.Background(), path, ImportOptions{
		ParquetPath: parquet,
		DuckDBPath:  duckdb,
		AppVersion:  "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Records != 2 || meta.Curated != 1 || meta.Source != parquet {
		t.Fatalf("meta = %+v", meta)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	total, curated := loaded.Count()
	if total != 2 || curated != 1 {
		t.Fatalf("counts = %d/%d", total, curated)
	}
	if loaded.Photos[0].Credit() == "" {
		t.Fatal("import did not preserve attribution")
	}
}

func TestImportReportsDuckDBFailure(t *testing.T) {
	dir := t.TempDir()
	parquet := filepath.Join(dir, "cats.parquet")
	if err := os.WriteFile(parquet, []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}
	duckdb := filepath.Join(dir, "fake-duckdb")
	script := "#!/bin/sh\necho 'Catalog Error: read_parquet failed' >&2\nexit 7\n"
	if err := os.WriteFile(duckdb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Import(context.Background(), filepath.Join(dir, "catalog.gz"), ImportOptions{
		ParquetPath: parquet,
		DuckDBPath:  duckdb,
	})
	if err == nil || !strings.Contains(err.Error(), "read_parquet failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestImportRefusesToOverwriteParquet(t *testing.T) {
	dir := t.TempDir()
	parquet := filepath.Join(dir, "cats.parquet")
	if err := os.WriteFile(parquet, []byte("dataset"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Import(context.Background(), parquet, ImportOptions{ParquetPath: parquet})
	if err == nil || !strings.Contains(err.Error(), "must not") {
		t.Fatalf("error = %v", err)
	}
	got, readErr := os.ReadFile(parquet)
	if readErr != nil || string(got) != "dataset" {
		t.Fatalf("dataset was modified: %q, %v", got, readErr)
	}
}
