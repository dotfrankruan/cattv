package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"text/tabwriter"

	"github.com/dotfrankruan/cattv/internal/catalog"
	"github.com/dotfrankruan/cattv/internal/library"
	"github.com/dotfrankruan/cattv/internal/version"
)

func (c *cli) runDoctor(args []string) error {
	flags, err := newDisplayFlags()
	if err != nil {
		return err
	}
	fs := newFlagSet("cattv doctor")
	flags.register(fs)
	positional, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if flags.help {
		c.printDoctorHelp()
		return nil
	}
	if len(positional) != 0 {
		return &usageError{err: fmt.Errorf("unexpected argument %q", positional[0])}
	}
	if err := flags.validate(); err != nil {
		return err
	}
	setup, err := c.setupDisplay(flags)
	if err != nil {
		return err
	}

	catalogStatus := "not initialized"
	stat, err := os.Stat(c.cfg.CatalogPath)
	if err == nil {
		cat, loadErr := catalog.Load(c.cfg.CatalogPath)
		if loadErr != nil {
			catalogStatus = "invalid: " + loadErr.Error()
		} else {
			total, curated := cat.Count()
			catalogStatus = fmt.Sprintf("ready: %s photos, %s curated, %s", formatCount(total), formatCount(curated), humanBytes(stat.Size()))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		catalogStatus = err.Error()
	}

	libraryStatus := "empty"
	if idx, loadErr := library.NewStore(c.cfg.LibraryPath).Load(); loadErr != nil {
		libraryStatus = "invalid: " + loadErr.Error()
	} else if len(idx.Items) > 0 {
		libraryStatus = fmt.Sprintf("%s local image(s)", formatCount(len(idx.Items)))
	}

	duckdbStatus := "not found (required only for cattv init)"
	duckdbName := c.cfg.DuckDBPath
	if duckdbName == "" {
		duckdbName = "duckdb"
	}
	if path, lookupErr := exec.LookPath(duckdbName); lookupErr == nil {
		duckdbStatus = path
	}

	parquet := c.cfg.ParquetPath
	if parquet == "" {
		parquet = defaultParquetPath()
	}
	parquetStatus := "not configured"
	if parquet != "" {
		parquetStatus = parquet
		if _, statErr := os.Stat(parquet); statErr != nil {
			parquetStatus += " (unavailable)"
		}
	}

	tty := "no"
	if setup.geometry.TTY {
		tty = "yes"
	}
	tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "CatTV:\t%s\n", version.Version)
	fmt.Fprintf(tw, "Platform:\t%s/%s (%s)\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
	fmt.Fprintf(tw, "Data directory:\t%s\n", c.cfg.DataDir)
	fmt.Fprintf(tw, "Catalog:\t%s\n", catalogStatus)
	fmt.Fprintf(tw, "Catalog path:\t%s\n", c.cfg.CatalogPath)
	fmt.Fprintf(tw, "Local library:\t%s (%s)\n", libraryStatus, c.cfg.LibraryPath)
	fmt.Fprintf(tw, "Terminal:\t%d×%d cells, %d×%d px, tty=%s\n",
		setup.geometry.Columns, setup.geometry.Rows, setup.geometry.PixelWidth, setup.geometry.PixelHeight, tty)
	fmt.Fprintf(tw, "Renderer:\t%s (%s)\n", setup.kind, setup.reason)
	fmt.Fprintf(tw, "DuckDB:\t%s\n", duckdbStatus)
	fmt.Fprintf(tw, "Parquet:\t%s\n", parquetStatus)
	fmt.Fprintf(tw, "Image cache:\tnone; one image fetched per invocation\n")
	if err := tw.Flush(); err != nil {
		return err
	}
	if catalogStatus == "not initialized" {
		fmt.Fprintln(c.out)
		fmt.Fprintln(c.out, "Initialize the metadata catalog with:")
		fmt.Fprintln(c.out, "  cattv init --parquet /path/to/unsplash_cats_clean.parquet")
	}
	return nil
}
