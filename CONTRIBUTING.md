# Contributing to CatTV

Thanks for helping put cats in terminals.

## Development setup

Requirements:

- Go 1.26+
- DuckDB CLI only if you want to test the real Parquet import
- No photo dataset is needed for unit tests

```console
git clone https://github.com/dotfrankruan/cattv.git
cd cattv
make check
make build
```

## Ground rules

- Keep the default command network-light: one image per invocation.
- Do not add persistent image caching, prefetching, telemetry, accounts, or a
  background daemon.
- Do not commit Parquet files, photos, generated catalogs, secrets, or large
  binary fixtures. Use generated images and `httptest` in tests.
- Preserve attribution and respect Unsplash's terms.
- Add focused tests for behavior changes. Renderer changes should include
  protocol-level tests that decode/validate the emitted payload.
- Run `gofmt`, `go vet ./...`, and `go test ./...` before sending a change.

## Pull requests

Describe the user-visible behavior, terminal compatibility impact, and test
coverage. Changes that affect renderer selection should explain how failures
fall back safely. Changes that touch networking need explicit bounds
(timeouts/size/retries) and cancellation behavior.

## Security and privacy

Please do not file public issues for security vulnerabilities. Use GitHub's
private security advisory feature for the repository. CatTV should never send
telemetry or identifiers to third parties.
