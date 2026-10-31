package cli

import (
	"fmt"
	"strings"
)

func (c *cli) printHelp() {
	fmt.Fprint(c.out, `CatTV — Your terminal deserves a cat. 🐱

Usage:
  cattv [next] [flags]               Display one random cat (downloads one image)
  cattv show <file> [flags]          Display a local image (works offline)
  cattv local [flags]                Display a random local library image (offline)
  cattv import <path>...             Index local images or directories
  cattv library list                List indexed local images
  cattv library remove <id|path>    Remove one local library entry
  cattv library clear               Empty the local library index
  cattv init [flags]                Build the metadata catalog from Parquet
  cattv catalog status              Show catalog statistics
  cattv catalog path                Print the catalog path
  cattv doctor [flags]              Diagnose terminal, renderer, and setup
  cattv version                     Print version information

Setup:
  CatTV ships no photo dataset. Build the compact metadata catalog once from
  the extracted Unsplash Parquet file, then run cattv normally:

    cattv init --parquet /path/to/unsplash_cats_clean.parquet
    cattv

  init reads metadata only. It does not download image pixels.

Display flags:
  --renderer auto|kitty|iterm2|ansi
  --width N            maximum image width in terminal cells
  --height N           maximum image height in terminal cells
  --no-caption         hide photographer attribution
  --timeout 20s        overall network deadline for one remote image
  --retries 2          retries for transient image-server errors

Environment:
  CATTV_DATA_DIR       override the application data directory
  CATTV_CATALOG        override the catalog path
  CATTV_LIBRARY        override the local library index path
  CATTV_STATE          override recent-photo history path
  CATTV_PARQUET        default Parquet path for init
  CATTV_DUCKDB         DuckDB executable path
  CATTV_RENDERER       default renderer
  CATTV_TIMEOUT        default network deadline (e.g. 20s)
  CATTV_RETRIES        default retry count
  CATTV_NO_PROBE=1     disable active terminal capability probing

Rendering:
  kitty and iTerm2 use native terminal image protocols. Other terminals,
  including Apple Terminal.app, use a true-colour Unicode half-block renderer.
  tmux/screen default to the portable renderer for safety.

Docs and issues: https://github.com/dotfrankruan/cattv
`)
}

func (c *cli) printHelpTopic(args []string) {
	if len(args) == 0 {
		c.printHelp()
		return
	}
	switch args[0] {
	case "next":
		c.printNextHelp()
	case "show":
		c.printShowHelp()
	case "local":
		c.printLocalHelp()
	case "import":
		c.printImportHelp()
	case "init", "catalog":
		c.printInitHelp()
	case "library":
		c.printLibraryHelp()
	case "doctor":
		c.printDoctorHelp()
	case "version":
		fmt.Fprintln(c.out, "Usage: cattv version")
	default:
		c.printHelp()
	}
}

func (c *cli) printDisplayFlags() {
	fmt.Fprint(c.out, `
Display flags:
  --renderer auto|kitty|iterm2|ansi   force a backend (default: auto)
  --width N                            maximum width in terminal cells
  --height N                           maximum height in terminal cells
  --no-caption                         hide photographer attribution
  --timeout duration                   overall network deadline (remote only)
  --retries N                          transient-error retries (remote only)
`)
}

func (c *cli) printNextHelp() {
	fmt.Fprint(c.out, `Usage: cattv [next] [flags]

Select a random cat from the local metadata catalog, download one appropriately
sized image on demand, render it, and release the image data. Curated picks are
preferred and recently shown photos are avoided when alternatives exist.

This command needs a network connection. Run `+"`cattv init`"+` once beforehand.
`)
	c.printDisplayFlags()
}

func (c *cli) printShowHelp() {
	fmt.Fprint(c.out, `Usage: cattv show <file> [flags]

Display an explicit local image with no network access. JPEG, PNG, GIF, and
WebP are supported. Aspect ratio and terminal fit are preserved.
`)
	c.printDisplayFlags()
}

func (c *cli) printLocalHelp() {
	fmt.Fprint(c.out, `Usage: cattv local [flags]

Display a random image from the local library index, with no network access.
Use `+"`cattv import`"+` to add files and directories first.
`)
	c.printDisplayFlags()
}

func (c *cli) printImportHelp() {
	fmt.Fprint(c.out, `Usage: cattv import <file-or-directory>...

Recursively index local JPEG, PNG, GIF, and WebP files. Files are referenced in
place; CatTV does not copy them. Import existing image files or directories:

  cattv import ~/Pictures/cats
`)
}

func (c *cli) printInitHelp() {
	fmt.Fprint(c.out, `Usage: cattv init [--parquet PATH] [--duckdb PATH]

Build or refresh CatTV's compact local metadata catalog from the extracted
Unsplash cat Parquet dataset. This operation uses DuckDB, reads metadata only,
and downloads no image pixels.

The dataset path is resolved from --parquet, a positional path, CATTV_PARQUET,
or ./data/unsplash_cats_clean.parquet.

  cattv init --parquet /data/unsplash_cats_clean.parquet
  cattv

Run `+"`cattv catalog status`"+` to inspect the resulting index.
`)
}

func (c *cli) printCatalogHelp() {
	fmt.Fprint(c.out, `Usage:
  cattv catalog status          Show catalog path, counts, and import source
  cattv catalog path            Print the catalog path
  cattv catalog init [flags]    Alias for cattv init
  cattv init [flags]            Build/refresh the metadata catalog
`)
}

func (c *cli) printLibraryHelp() {
	fmt.Fprint(c.out, `Usage:
  cattv import <path>...          Index local image files/directories
  cattv library list              List indexed images
  cattv library remove <id|path>  Remove one index entry
  cattv library clear             Empty the local library index
  cattv local [flags]             Show a random indexed local image

The index stores paths and metadata, not copies of image files.
`)
}

func (c *cli) printDoctorHelp() {
	fmt.Fprint(c.out, `Usage: cattv doctor [flags]

Print CatTV version, paths, catalog and library status, terminal dimensions,
automatic renderer choice, and external tool availability. Doctor performs no
network image download.
`)
	c.printDisplayFlags()
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}
