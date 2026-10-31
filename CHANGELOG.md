# Changelog

All notable changes to CatTV are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Experimental `cattv fifo serve` delivery server with a user-only FIFO that
  downloads only after a reader connects, rotates to a fresh inode per reader,
  closes the writer for EOF, and survives reader disconnects.
- Strict `cattv fifo client` Unix socket transport with HTTP one-request/
  one-response framing for concurrent clients and exact payload boundaries.
- Muted capture-location and upload-date detail beside the photo attribution
  when the catalog contains the optional metadata (catalog schema 2).
- `cattv fifo path`, `CATTV_FIFO`, and `CATTV_SOCKET` configuration.
- Terminal-safe rendering of metadata, filenames, warnings, and delivery errors.

### Changed

- `cattv init` now imports location and date fields and writes catalog schema 2;
  schema 1 catalogs remain readable but need re-import for the new caption data.
- DuckDB import no longer depends on the 2.0-only `-no-agent` flag.

### Fixed

- Recent-photo history now actually excludes prior remote and local picks.
- Image fallback now tries larger supplied variants when the best size fails,
  not only smaller variants.
- Per-request HTTP client timeouts are retried as network failures instead of
  being misreported as caller cancellation.
- Unreadable state files are preserved instead of silently overwritten. State,
  library, catalog, socket, and FIFO paths are checked for format and endpoint
  collisions.
- Safe handling of terminal control characters in user-controlled paths and
  metadata.

## [0.1.0] - 2026-10-31

### Added

- On-demand random cat display from a local metadata catalog, with curated
  preference and recent-photo avoidance.
- Compact gzipped JSONL catalog and a DuckDB-backed, metadata-only importer for
  the extracted Unsplash cat Parquet dataset.
- Bounded HTTP image fetching with terminal-aware resolution, retries,
  cancellation, an 8 MiB response cap, and helpful offline errors.
- Kitty graphics protocol renderer with PNG encoding and 4096-byte chunking.
- iTerm2 inline image renderer.
- True-colour ANSI Unicode half-block fallback, including Apple Terminal.
- Automatic renderer selection with mux detection, terminal capability
  signals, and an active Kitty probe.
- `cattv show <file>` for fully offline local display.
- Local image import, index listing/removal/clearing, and offline random
  selection from the library.
- `cattv init`, `cattv catalog status`, `cattv doctor`, and `cattv version`.
- Atomic catalog/library/history writes, terminal-size-aware layouts, image
  decompression limits, and caption sanitization.
- Apache-2.0 license, third-party notices, CI, cross-platform build targets,
  and documentation including the source Parquet schema.

### Security

- No telemetry, accounts, background services, persistent image cache, or bulk
  image downloads.
- Attribution metadata is preserved and displayed by default.

[Unreleased]: https://github.com/dotfrankruan/cattv/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/dotfrankruan/cattv/releases/tag/v0.1.0
