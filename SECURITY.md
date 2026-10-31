# Security policy

## Reporting a vulnerability

Please report security issues privately through GitHub Security Advisories:

<https://github.com/dotfrankruan/cattv/security/advisories/new>

Do not open a public issue for a vulnerability. Include reproduction steps,
affected versions/platforms, and any suggested mitigation.

## Scope and expectations

CatTV is a single-shot terminal image viewer. It does not run a server, store
accounts, or collect telemetry. Relevant security areas include:

- image decoder and decompression-bomb handling
- terminal escape injection through metadata, filenames, or URLs
- unsafe URL handling or oversized downloads
- path handling in the local library importer
- state/index corruption and atomicity
- third-party dependency vulnerabilities

CatTV does not control content hosted by Unsplash or other image hosts.
