package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ImportOptions controls construction of a local catalog from the extracted
// Parquet dataset.
type ImportOptions struct {
	ParquetPath string
	DuckDBPath  string
	AppVersion  string
}

// Import reads the cat dataset with DuckDB and atomically writes a compact
// runtime catalog. This is deliberately a separate operation from viewing:
// normal display never opens the Parquet file.
func Import(ctx context.Context, catalogPath string, opts ImportOptions) (Meta, error) {
	parquet, err := filepath.Abs(opts.ParquetPath)
	if err != nil {
		return Meta{}, fmt.Errorf("resolve dataset path: %w", err)
	}
	info, err := os.Stat(parquet)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Meta{}, fmt.Errorf("dataset not found at %s; pass --parquet or set CATTV_PARQUET", parquet)
		}
		return Meta{}, fmt.Errorf("inspect dataset: %w", err)
	}
	if info.IsDir() {
		return Meta{}, fmt.Errorf("dataset path is a directory, expected a Parquet file: %s", parquet)
	}
	catalogAbs, err := filepath.Abs(catalogPath)
	if err != nil {
		return Meta{}, fmt.Errorf("resolve catalog path: %w", err)
	}
	if catalogAbs == parquet {
		return Meta{}, errors.New("catalog path must not be the Parquet dataset path")
	}
	if catalogInfo, statErr := os.Stat(catalogAbs); statErr == nil && os.SameFile(catalogInfo, info) {
		return Meta{}, errors.New("catalog path must not overwrite the Parquet dataset")
	}

	duckdb, err := findDuckDB(opts.DuckDBPath)
	if err != nil {
		return Meta{}, err
	}

	query, err := buildImportQuery(parquet)
	if err != nil {
		return Meta{}, err
	}
	photos, err := runDuckDB(ctx, duckdb, query)
	if err != nil {
		return Meta{}, err
	}
	if len(photos) == 0 {
		return Meta{}, errors.New("the dataset query returned no usable cat photos")
	}

	meta := Meta{
		CreatedAt:   time.Now().UTC(),
		Source:      parquet,
		AppVersion:  opts.AppVersion,
		Description: "Unsplash cat subset metadata; images fetched on demand",
	}
	saved, err := Save(catalogPath, meta, photos)
	if err != nil {
		return Meta{}, err
	}
	return saved, nil
}

func findDuckDB(configured string) (string, error) {
	if configured != "" {
		path, err := exec.LookPath(configured)
		if err != nil {
			return "", fmt.Errorf("DuckDB executable %q is not available: %w", configured, err)
		}
		return path, nil
	}
	path, err := exec.LookPath("duckdb")
	if err != nil {
		return "", errors.New("duckdb not found; install DuckDB (https://duckdb.org) or set CATTV_DUCKDB")
	}
	return path, nil
}

func buildImportQuery(parquet string) (string, error) {
	literal, err := sqlLiteral(parquet)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`
SELECT
    trim(photo_id) AS id,
    COALESCE(photo_url, '') AS page_url,
    COALESCE(photo_image_url, '') AS image_url,
    COALESCE(image_url_320, '') AS url_320,
    COALESCE(image_url_640, '') AS url_640,
    COALESCE(image_url_1280, '') AS url_1280,
    CAST(COALESCE(photo_width, 0) AS BIGINT) AS width,
    CAST(COALESCE(photo_height, 0) AS BIGINT) AS height,
    COALESCE(orientation, '') AS orientation,
    COALESCE(NULLIF(trim(ai_description), ''), NULLIF(trim(photo_description), ''), '') AS description,
    COALESCE(attribution, '') AS attribution,
    COALESCE(photographer_name, '') AS photographer,
    COALESCE(photographer_username, '') AS photographer_username,
    COALESCE(strftime(photo_submitted_at, '%%Y-%%m-%%d'), '') AS date,
    COALESCE(photo_location_name, '') AS location_name,
    COALESCE(photo_location_country, '') AS country,
    COALESCE(photo_location_city, '') AS city,
    photo_location_latitude AS latitude,
    photo_location_longitude AS longitude,
    CAST(COALESCE(quality_score, 0) AS DOUBLE) AS quality_score,
    COALESCE(quality_tier, '') AS quality_tier,
    COALESCE(is_curated_pick, false) AS curated
FROM read_parquet(%s)
WHERE photo_id IS NOT NULL
  AND trim(photo_id) <> ''
  AND (
      NULLIF(trim(image_url_320), '') IS NOT NULL
      OR NULLIF(trim(image_url_640), '') IS NOT NULL
      OR NULLIF(trim(image_url_1280), '') IS NOT NULL
      OR NULLIF(trim(photo_image_url), '') IS NOT NULL
  )
QUALIFY row_number() OVER (
    PARTITION BY photo_id
    ORDER BY COALESCE(is_curated_pick, false) DESC, COALESCE(quality_score, 0) DESC
) = 1
ORDER BY photo_id
`, literal), nil
}

func sqlLiteral(s string) (string, error) {
	if strings.ContainsRune(s, 0) {
		return "", errors.New("dataset path contains a NUL byte")
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
}

func runDuckDB(ctx context.Context, duckdb, query string) ([]Photo, error) {
	// Explicit output mode disables DuckDB's newer agent-oriented formatting
	// without requiring the 2.0-only `-no-agent` flag. This keeps the importer
	// compatible with the DuckDB 1.4 LTS CLI.
	cmd := exec.CommandContext(ctx, duckdb,
		"-no-init",
		"-jsonlines",
		"-noheader",
		"-c", query,
	)
	cmd.Env = append(os.Environ(), "DUCKDB_AGENT_MODE=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("prepare DuckDB output: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start DuckDB: %w", err)
	}

	photos, parseErr := parseDuckDBPhotos(stdout)
	if parseErr != nil {
		_ = cmd.Process.Kill()
		_ = stdout.Close()
		_ = cmd.Wait()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("read DuckDB output: %w", parseErr)
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = waitErr.Error()
		}
		if len(detail) > 4000 {
			detail = detail[:4000] + "…"
		}
		return nil, fmt.Errorf("DuckDB import failed: %s", detail)
	}
	return photos, nil
}

func parseDuckDBPhotos(r io.Reader) ([]Photo, error) {
	dec := json.NewDecoder(r)
	seen := make(map[string]bool)
	var photos []Photo
	for {
		var p Photo
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode photo record: %w", err)
		}
		p.Kind = PhotoKind
		p.ID = strings.TrimSpace(p.ID)
		if p.ID == "" {
			return nil, errors.New("dataset returned a photo with an empty id")
		}
		if err := p.validate(); err != nil {
			return nil, err
		}
		if seen[p.ID] {
			return nil, fmt.Errorf("dataset returned duplicate photo id %s", p.ID)
		}
		seen[p.ID] = true
		photos = append(photos, p)
	}
	return photos, nil
}
