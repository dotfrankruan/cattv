SHELL := /bin/sh

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/frandustry/cattv/internal/version.Version=$(VERSION)
GO ?= go

.PHONY: build install test vet check clean

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/cattv ./cmd/cattv

install:
	$(GO) install -trimpath -ldflags "$(LDFLAGS)" ./cmd/cattv

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

check: vet test

clean:
	rm -rf bin coverage.out
