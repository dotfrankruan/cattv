# Changelog

All notable changes to CatTV are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
