package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/dotfrankruan/cattv/internal/app"
	"github.com/dotfrankruan/cattv/internal/catalog"
	"github.com/dotfrankruan/cattv/internal/version"
)

func (c *cli) runInit(ctx context.Context, args []string) error {
	fs := newFlagSet("cattv init")
	var (
		parquet string
		duckdb  string
		help    bool
	)
	fs.StringVar(&parquet, "parquet", "", "path to unsplash_cats_clean.parquet")
	fs.StringVar(&duckdb, "duckdb", "", "path to the duckdb executable")
	addHelpFlag(fs, &help)
	positional, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if help {
		c.printInitHelp()
		return nil
	}
	if len(positional) > 1 {
		return &usageError{err: errors.New("init accepts at most one positional Parquet path")}
	}
	if parquet == "" && len(positional) == 1 {
		parquet = positional[0]
	}
	if parquet == "" {
		parquet = c.cfg.ParquetPath
	}
	if parquet == "" {
		parquet = defaultParquetPath()
	}
	if parquet == "" {
		return &usageError{err: errors.New(
			"no Parquet dataset supplied; run `cattv init --parquet /path/to/unsplash_cats_clean.parquet` or set CATTV_PARQUET")}
	}
	if duckdb == "" {
		duckdb = c.cfg.DuckDBPath
	}

	meta, err := catalog.Import(ctx, c.cfg.CatalogPath, catalog.ImportOptions{
		ParquetPath: parquet,
		DuckDBPath:  duckdb,
		AppVersion:  version.Version,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Catalog ready: %s\n", c.cfg.CatalogPath)
	fmt.Fprintf(c.out, "%s photos, %s curated picks\n", formatCount(meta.Records), formatCount(meta.Curated))
	fmt.Fprintln(c.out, "No image pixels were downloaded; photos are fetched on demand.")
	fmt.Fprintln(c.out, "Run `cattv` to meet a cat. 🐱")
	return nil
}

func (c *cli) runCatalog(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return c.runCatalogStatus(nil)
	}
	switch args[0] {
	case "status":
		return c.runCatalogStatus(args[1:])
	case "init", "import":
		return c.runInit(ctx, args[1:])
	case "path":
		fmt.Fprintln(c.out, c.cfg.CatalogPath)
		return nil
	case "help", "-h", "--help":
		c.printCatalogHelp()
		return nil
	default:
		return &usageError{err: fmt.Errorf("unknown catalog command %q", args[0])}
	}
}

func (c *cli) runCatalogStatus(args []string) error {
	fs := newFlagSet("cattv catalog status")
	var help bool
	addHelpFlag(fs, &help)
	positional, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if help {
		c.printCatalogHelp()
		return nil
	}
	if len(positional) != 0 {
		return &usageError{err: fmt.Errorf("unexpected argument %q", positional[0])}
	}
	info, statErr := os.Stat(c.cfg.CatalogPath)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", app.ErrCatalogMissing, c.cfg.CatalogPath)
		}
		return statErr
	}
	cat, err := catalog.Load(c.cfg.CatalogPath)
	if err != nil {
		return fmt.Errorf("%w: %v", app.ErrCatalogInvalid, err)
	}
	total, curated := cat.Count()
	tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "Catalog:\t%s\n", c.cfg.CatalogPath)
	fmt.Fprintf(tw, "Photos:\t%s (%s curated)\n", formatCount(total), formatCount(curated))
	fmt.Fprintf(tw, "Size:\t%s\n", humanBytes(info.Size()))
	fmt.Fprintf(tw, "Imported:\t%s\n", cat.Meta.CreatedAt.Local().Format("2006-01-02 15:04 MST"))
	fmt.Fprintf(tw, "Source:\t%s\n", cat.Meta.Source)
	fmt.Fprintf(tw, "Schema:\t%d\n", cat.Meta.Schema)
	return tw.Flush()
}

func defaultParquetPath() string {
	path := filepath.Join("data", "unsplash_cats_clean.parquet")
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		abs, err := filepath.Abs(path)
		if err == nil {
			return abs
		}
		return path
	}
	return ""
}

func formatCount(n int) string {
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
