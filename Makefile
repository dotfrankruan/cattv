SHELL := /bin/sh

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/dotfrankruan/cattv/internal/version.Version=$(VERSION)
GO ?= go
BINARY := bin/cattv

.PHONY: build install test vet check race build-all clean

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/cattv

install:
	$(GO) install -trimpath -ldflags "$(LDFLAGS)" ./cmd/cattv

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

check: vet test

build-all:
	GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/cattv-darwin-arm64 ./cmd/cattv
	GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/cattv-darwin-amd64 ./cmd/cattv
	GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/cattv-linux-arm64 ./cmd/cattv
	GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/cattv-linux-amd64 ./cmd/cattv

clean:
	rm -rf bin coverage.out
