# CatTV

**Your terminal deserves a cat. 🐱**

[![CI](https://github.com/dotfrankruan/cattv/actions/workflows/ci.yml/badge.svg)](https://github.com/dotfrankruan/cattv/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

CatTV is a fast, terminal-native cat image viewer written in Go, part of the
FRANDUSTRY CAT ecosystem. One command, one random cat, one on-demand image
download:

```console
$ cattv
                         ...a cat appears in your terminal...
Photo by Helena Lopes on Unsplash - https://unsplash.com/photos/uFt1IWGqKYc
```

It prefers curated photographs from a local metadata catalog, asks the image
CDN for a terminal-appropriate resolution, decodes and resizes the image in
memory, renders it with the best available terminal graphics protocol, and
releases the image bytes. There is no image cache, no background daemon, no
account, and no telemetry.

## Why CatTV

- **Actually native.** Kitty graphics, iTerm2 inline images, or a true-colour
  Unicode half-block renderer. No browser, Electron, or background service.
- **On demand by design.** The default command fetches exactly one image. There
  are no mandatory bulk downloads, prefetches, or persistent image caches.
- **Local metadata, fast starts.** A compact gzipped JSONL index is built once
  from Parquet. Normal viewing never scans the source dataset.
- **Curated, not just top-scored.** Random selection is uniform among curated
  picks. CatTV does not repeatedly choose the same maximum-scored photo.
- **Rich, quiet captions.** Photographer attribution and the source link are
  always shown by default; when the dataset provides a location and upload date,
  they appear as muted secondary detail beside the link.
- **Streamable, experimentally.** `cattv fifo serve` can deliver fresh cats to
  `cat ~/.cache/cat.fifo`; a framed Unix socket client is available when strict
  one-response-per-request semantics matter.
- **Polite networking.** One image per invocation, bounded response size,
  timeouts, cancellation, a few transient-error retries, and no tracking.
- **Useful offline.** `cattv show <file>` is always offline. `cattv import`
  indexes your own collection for `cattv local`.
- **Small and portable.** Pure Go, no CGO, no SQLite driver, macOS and Linux.

## Requirements

- Go 1.26 or newer to build.
- DuckDB CLI 1.4+ **only** for `cattv init`. DuckDB is not used at runtime.
- A network connection for the default `cattv` command.
- A terminal. Kitty/Ghostty/WezTerm/iTerm2 unlock native image protocols;
  everything else gets the portable renderer.

The supported build targets are macOS arm64/amd64 and Linux arm64/amd64.

## Install

```console
go install github.com/dotfrankruan/cattv/cmd/cattv@latest
```

Or build from a clone:

```console
git clone https://github.com/dotfrankruan/cattv.git
cd cattv
make build
./bin/cattv version
```

Add `bin/` to your `PATH`, or use `make install` to install through `go install`.

## Quick start

CatTV deliberately does **not** redistribute the Unsplash dataset or any photo
files. Obtain the already-prepared cat Parquet dataset, then build the local
metadata catalog once:

```console
# DuckDB is needed only for this step.
cattv init --parquet "/path/to/unsplash_cats_clean.parquet"

# Meet a cat. This fetches one image from the image URLs in the catalog.
cattv
```

You can also set the dataset path once:

```console
export CATTV_PARQUET="/path/to/unsplash_cats_clean.parquet"
cattv init
```

If `./data/unsplash_cats_clean.parquet` exists, `cattv init` finds it
automatically. Parquet files and the entire `data/` directory are git-ignored
so the dataset cannot be accidentally committed.

Check your setup at any time:

```console
cattv doctor
cattv catalog status
```

## Commands

```text
cattv [next] [flags]               Fetch and display one random remote cat
cattv show <file> [flags]          Display a local image without network access
cattv local [flags]                Display a random local-library image offline
cattv import <path>...             Index local image files and directories
cattv library list                 List indexed local images
cattv library remove <id|path>     Remove one local-library entry
cattv library clear                Empty the local-library index
cattv init [flags]                 Build/refresh the Parquet metadata catalog
cattv catalog status               Show catalog counts and source metadata
cattv catalog path                 Print the catalog path
cattv doctor [flags]               Diagnose setup, terminal, and renderer
cattv fifo serve [flags]           Experimental FIFO cat delivery server
cattv fifo client [flags]          Strict one-response Unix socket client
cattv fifo path [flags]            Print default FIFO/socket paths
cattv version                      Print version and platform
```

### Examples

```console
# Random remote cat, limited to 60 columns
cattv --width 60

# An explicit local file; flags may appear before or after the path
cattv show ~/Pictures/cats/mochi.jpg --height 30

# Portable renderer for a screenshot or a limited terminal
cattv next --renderer ansi

# Local offline collection
cattv import ~/Pictures/cats
cattv local

# Remove one entry by ID or path
cattv library remove 4f8c02ad9b12e7d1

# Experimental FIFO delivery; default renderer is kitty
cattv fifo serve
cat ~/.cache/cat.fifo

# Strict framed alternative for concurrent clients
cattv fifo client
```

### Display flags

| Flag | Default | Meaning |
|---|---:|---|
| `--renderer auto\|kitty\|iterm2\|ansi` | `auto` | Force a rendering backend |
| `--width N` | terminal width | Maximum image width in terminal cells |
| `--height N` | terminal height | Maximum image height in terminal cells |
| `--no-caption` | off | Suppress the attribution line |
| `--timeout 20s` | `20s` | Overall network deadline for one remote display |
| `--retries 2` | `2` | Retries for transient image-server errors |

`--renderer ansi` still uses 24-bit colour escapes; it is an image renderer, not
a text-only mode. Use `--no-caption` if you want only the image in a pipeline.

## How the on-demand path works

```text
cattv
  │
  ├─ load compact gzipped JSONL catalog          (local, ~2.4 MiB for 22k records)
  ├─ choose random curated photo                 (uniform, avoids recent 32)
  ├─ choose 320/640/1280 URL from terminal size  (never fetch originals)
  ├─ HTTP GET with 20s budget, retries, 8 MiB cap
  ├─ decode PNG/JPEG/GIF/WebP in memory
  ├─ resize to terminal cell/pixel dimensions
  ├─ render image; print attribution and muted place/date detail
  └─ release image bytes; persist only recent photo IDs
```

The only persistent artifacts are the metadata catalog, the optional local
library index, and a small recent-photo list. Image pixels are never written to
disk by the default command.

If the selected photo's CDN URL is unavailable or invalid, CatTV tries the
other supplied sizes and then up to five other random photos within the same
network budget. A DNS or transport failure stops further fallback photos and
prints an offline explanation instead of hammering the network. Context
cancellation (Ctrl-C) propagates through the request.

When the catalog contains optional `photo_submitted_at` and location metadata,
the attribution line gains muted detail, for example:

```text
Photo by Jane Doe on Unsplash - https://unsplash.com/photos/...  ·  Cape Town, South Africa · 2020-11-30
```

The photographer/link portion and the place/date portion are both terminal-safe;
the latter is dimmed when the output is a TTY. `--no-caption` suppresses both.

## Rendering

| Backend | Terminals | Transport |
|---|---|---|
| `kitty` | Kitty, Ghostty, WezTerm, Konsole, Rio | Kitty graphics APC, f=100 PNG, base64 chunks ≤4096 bytes |
| `iterm2` | iTerm2 | OSC 1337 `File=inline=1` with a PNG payload |
| `ansi` | Any true-colour terminal, including Apple Terminal | 24-bit ANSI plus Unicode upper-half blocks (`▀`) |

Automatic selection uses more than `$TERM`:

1. An explicit `--renderer` or `CATTV_RENDERER` wins.
2. Non-TTY output uses the portable renderer.
3. `tmux`/GNU screen use the portable renderer for safety.
4. Capability variables and terminal markers identify Kitty-family and iTerm2
   terminals.
5. On an otherwise unknown interactive terminal, CatTV sends a bounded Kitty
   graphics query plus DA1 and uses the response. This is authoritative, not
   name-based. Disable it with `CATTV_NO_PROBE=1`.
6. The ANSI fallback is always available.

CatTV preserves image aspect ratio and fits the drawable area derived from the
terminal's reported cell and device-pixel dimensions. ANSI output uses two
vertical samples per character and adapts to non-square cell geometry.

Apple Terminal supports 24-bit colour on macOS Tahoe (26) and later; older
releases may not render true-colour output correctly. Kitty and iTerm2 are
recommended where available. Multiplexer passthrough is intentionally not
wrapped by CatTV; the safe default is ANSI inside tmux/screen.

## FIFO cat delivery (experimental)

The experimental delivery server reuses the normal catalog, downloader, and
renderers:

```console
cattv fifo serve
cat ~/.cache/cat.fifo
```

Each sequential open starts a fresh download only after a reader is present,
writes one complete rendered response, and closes the write end so the reader
sees EOF. The FIFO is created with mode `0600`, never opens read-write, and
refuses to replace a non-FIFO path. Ctrl-C, stale paths, SIGPIPE, network
failures, and reader disconnects are handled without killing the server. The
default renderer is Kitty for the initial Ghostty implementation; use
`--renderer ansi` or `--renderer iterm2` for other terminals.

**FIFOs have no message boundaries.** If multiple `cat` processes read at once,
the kernel may split one frame between them, give it all to one, or give one
reader nothing. CatTV cannot detect concurrent readers or make `cat` frame a
request. The exact guarantee "every successful concurrent `cat` gets one
complete image" is therefore impossible with a plain FIFO. Sequential use is
supported; concurrent use is best-effort.

For strict request-response semantics, `cattv fifo serve` also exposes a Unix
socket:

```console
cattv fifo client
```

It uses one HTTP request per connection with an exact `Content-Length`, so
concurrent clients each receive a complete independent frame or an error. Use
`--no-socket` to disable it. Custom paths and full limitations are documented in
[docs/fifo-delivery.md](docs/fifo-delivery.md).

## Local library

`cattv show <file>` never needs a catalog or network. For a random offline
collection, index images in place:

```console
cattv import ~/Pictures/cats ~/Downloads/favorite-kitten.png
cattv local
```

- Supported extensions: `.jpg`, `.jpeg`, `.jfif`, `.png`, `.gif`, `.webp`.
- Directories are scanned recursively; directory symlinks are not followed.
- Files are **not copied**. The index stores absolute paths, dimensions,
  format, size, and an identifier.
- Duplicate imports update existing entries instead of adding duplicates.
- Missing or corrupt library files are skipped gracefully on display.
- The index and recent history are atomically written with `0600` permissions.

Animated GIFs render their first frame. Images above 60 megapixels are rejected
before the full bitmap is decoded. EXIF orientation is not currently applied to
local JPEGs.

## Dataset integration and Parquet schema

`cattv init` invokes DuckDB to read metadata from the extracted
`unsplash_cats_clean.parquet` file. It deduplicates by `photo_id`, preserves
attribution and URL metadata, adds optional capture location and upload date,
and writes the compact runtime catalog. No image pixels are downloaded, and the
Parquet file is not needed for normal viewing.

The runtime catalog is currently schema 2. Schema 1 catalogs remain readable,
but only schema 2 catalogs contain the new location/date fields; rerun
`cattv init` after upgrading to populate them.

The development extraction contains 22,265 unique cat photos, including 15,215
curated picks. Those counts are properties of the dataset, not constants in the
code.

### Full source schema

The source Parquet schema inspected during implementation is:

| Column | DuckDB type |
|---|---|
| `photo_id` | `VARCHAR` |
| `photo_url` | `VARCHAR` |
| `photo_image_url` | `VARCHAR` |
| `image_url_320` | `VARCHAR` |
| `image_url_640` | `VARCHAR` |
| `image_url_1280` | `VARCHAR` |
| `photo_submitted_at` | `TIMESTAMP` |
| `photo_featured` | `BOOLEAN` |
| `photo_width` | `BIGINT` |
| `photo_height` | `BIGINT` |
| `photo_aspect_ratio` | `DOUBLE` |
| `orientation` | `VARCHAR` |
| `megapixels` | `DOUBLE` |
| `resolution_tier` | `VARCHAR` |
| `photo_description` | `VARCHAR` |
| `ai_description` | `VARCHAR` |
| `photographer_username` | `VARCHAR` |
| `photographer_first_name` | `VARCHAR` |
| `photographer_last_name` | `VARCHAR` |
| `photographer_name` | `VARCHAR` |
| `attribution` | `VARCHAR` |
| `exif_camera_make` | `VARCHAR` |
| `exif_camera_model` | `VARCHAR` |
| `exif_iso` | `BIGINT` |
| `exif_aperture_value` | `VARCHAR` |
| `exif_focal_length` | `VARCHAR` |
| `exif_exposure_time` | `VARCHAR` |
| `photo_location_name` | `VARCHAR` |
| `photo_location_latitude` | `DOUBLE` |
| `photo_location_longitude` | `DOUBLE` |
| `photo_location_country` | `VARCHAR` |
| `photo_location_city` | `VARCHAR` |
| `stats_views` | `BIGINT` |
| `stats_downloads` | `BIGINT` |
| `downloads_per_1k_views` | `DOUBLE` |
| `blur_hash` | `VARCHAR` |
| `keywords` | `VARCHAR[]` |
| `keyword_count` | `BIGINT` |
| `matched_cat_keywords` | `VARCHAR[]` |
| `n_cat_keywords` | `BIGINT` |
| `n_bigcat_keywords` | `BIGINT` |
| `match_tier` | `VARCHAR` |
| `cat_confidence` | `DECIMAL(3,2)` |
| `match_reason` | `VARCHAR` |
| `has_ai_description` | `BOOLEAN` |
| `has_caption` | `BOOLEAN` |
| `caption_ai_conflict` | `BOOLEAN` |
| `source_dataset` | `VARCHAR` |
| `extraction_date` | `DATE` |
| `quality_score` | `DOUBLE` |
| `quality_tier` | `VARCHAR` |
| `quality_flags` | `VARCHAR[]` |
| `is_curated_pick` | `BOOLEAN` |

CatTV imports only the fields needed for display and attribution: photo ID and
URLs, dimensions/orientation, description, photographer/attribution fields,
upload date, location name/country/city/coordinates, quality score/tier, and the
curated flag. Optional values are tolerated.

The metadata was prepared with heuristic tiering and quality flags. It was not
visually validated end-to-end, so an occasional false positive is possible.
`quality_score` measures metadata quality (resolution, engagement, completeness),
not visual charm, which is why CatTV does not sort by it.

### Dataset and image licensing

- **No Parquet file and no Unsplash image is stored in this repository.**
- Photos remain subject to the Unsplash License and Unsplash's current terms.
  CatTV hotlinks the supplied `images.unsplash.com` URLs and displays the
  supplied attribution.
- The generated catalog is local metadata for personal use. Review the terms
  before redistributing metadata or building a public service from it.
- CatTV is not affiliated with or endorsed by Unsplash.
- The CatTV source code is Apache-2.0; that license does not grant rights to
  third-party photos or datasets.

## Privacy and network behavior

- No telemetry, analytics, accounts, cookies, or background services.
- The default command contacts only the image CDN URL for the selected photo.
  It does not contact Unsplash APIs, an analytics endpoint, or a CatTV server.
- The HTTP `User-Agent` is `cattv/<version> (+https://github.com/dotfrankruan/cattv)`,
  with no user identifiers.
- No prefetching, bulk downloads, prefetch queue, or persistent image cache.
- Local library and catalog operations make no network requests.
- `cattv doctor` does not test connectivity or download images.

## Configuration

Platform data directories:

| OS | Default |
|---|---|
| macOS | `~/Library/Application Support/cattv` |
| Linux | `$XDG_DATA_HOME/cattv`, or `~/.local/share/cattv` |

Files in that directory:

- `catalog.jsonl.gz` — compact remote metadata catalog
- `library.json` — local image index (paths only)
- `state.json` — most recent 32 photo identifiers

Environment variables:

| Variable | Purpose |
|---|---|
| `CATTV_DATA_DIR` | Override the data directory |
| `CATTV_CATALOG` | Override catalog file path |
| `CATTV_LIBRARY` | Override local-library index path |
| `CATTV_STATE` | Override recent-history path |
| `CATTV_PARQUET` | Default Parquet path for `cattv init` |
| `CATTV_DUCKDB` | DuckDB executable |
| `CATTV_RENDERER` | Default renderer (`auto`, `kitty`, `iterm2`, `ansi`) |
| `CATTV_TIMEOUT` | Default network deadline (`20s`, `2m`, or bare seconds) |
| `CATTV_RETRIES` | Default retry count |
| `CATTV_NO_PROBE=1` | Disable active terminal capability probing |
| `CATTV_CELL_WIDTH`, `CATTV_CELL_HEIGHT` | Override cell pixel dimensions when the terminal does not report them |
| `CATTV_FIFO` | Default FIFO endpoint for `cattv fifo` |
| `CATTV_SOCKET` | Default strict Unix socket endpoint |

## Exit codes

| Code | Meaning |
|---:|---|
| `0` | Image displayed, or command completed successfully |
| `1` | Runtime/local error |
| `2` | Invalid command-line usage |
| `3` | Catalog missing or invalid (run `cattv init`) |
| `4` | Network failure or timeout |
| `5` | No displayable image / empty library |
| `130` | Interrupted with Ctrl-C |

## Architecture

```text
cmd/cattv            process entry point and signal handling
internal/cli         argument parsing, help, doctor, friendly errors
internal/app         display orchestration and fallback
internal/catalog     compact JSONL catalog, random selection, DuckDB import
internal/fetch       bounded HTTP downloader with retries and cancellation
internal/render      renderer interface, Kitty, iTerm2, ANSI, image codec/fit
internal/terminal    TTY geometry, pixel dimensions, active Kitty probe
internal/delivery    experimental FIFO and strict Unix socket servers
internal/textsafe    terminal-control sanitization for metadata and paths
internal/library     local image index and import scanner
internal/state       recent-photo history
internal/config      XDG/platform paths and environment overrides
internal/fileutil    atomic writes
internal/version     build-time version
```

The runtime catalog is gzipped JSONL with one metadata record at the end. This
avoids any database runtime dependency and loads quickly for a single-shot
CLI. Writes use temporary files, `fsync`, and atomic rename. A
catalog is validated (schema, counts, duplicate IDs, usable URLs) before use.

The chain is deliberately layered: the CLI knows about flags and user guidance;
the app knows about display policy; the catalog, fetcher, renderer, and delivery
packages are independently testable and do not depend on the CLI. The FIFO
transport is documented in [docs/fifo-delivery.md](docs/fifo-delivery.md).

## Development

```console
make check       # go vet + go test
make race        # race detector
make build       # bin/cattv with version metadata
make build-all   # macOS/Linux arm64 and amd64 binaries
```

The test suite uses generated images and `httptest`; it does not require the
Parquet dataset, DuckDB, a network connection, or a real terminal. `cattv init`
is tested in development against the extracted Parquet file and exercised
manually with the real dataset.

To run the **real** import path locally:

```console
duckdb --version
cattv init --parquet ./data/unsplash_cats_clean.parquet
cattv catalog status
cattv doctor
cattv --renderer ansi --width 60 --height 24
```

Never commit `.parquet` files, generated catalogs, or downloaded images. The
repository's `.gitignore` covers Parquet files, the `data/` directory, and build
outputs.

## Limitations and non-goals

- CatTV does not bundle a photo hosting service, API key, or dataset.
- Every default invocation needs the network; there is intentionally no
  persistent image cache or prefetching.
- Selection is metadata-driven, not a visual cat classifier.
- The tool renders one image per invocation rather than providing a slideshow
  or a full-screen TUI.
- Kitty/iTerm2 protocol support inside tmux/screen is left to explicit terminal
  configuration; CatTV's safe default is ANSI.
- FIFO delivery is experimental and has no per-reader message boundaries; use
  the Unix socket client for strict concurrent request-response semantics. The
  server's renderer and geometry are fixed at startup, not negotiated by `cat`.
- GIF playback is static (first frame), HEIC is not supported, and EXIF
  orientation is not applied locally.
- Remote images larger than 8 MiB or 60 megapixels are rejected.

## License and attribution

CatTV is licensed under the [Apache License 2.0](LICENSE).
Third-party Go modules are listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

Photos are by their respective Unsplash photographers. CatTV displays the
attribution metadata supplied by the dataset. See the dataset licensing section
above. Unsplash is a trademark of Unsplash, Inc.; CatTV is an independent
project.
