# Kander build entry point. Artifacts land where install.sh / install.ps1 expect them,
# so the installers reuse the binary already built in the repository root.

GO ?= go
PKG := ./cmd/kander
GOFLAGS ?=
LDFLAGS ?=
BUILD_TIMESTAMP ?= $(shell date -u +%Y%m%dT%H%M%SZ)
GIT_HASH ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
VERSION_PACKAGE := github.com/dualface/kander/internal/version
VERSION_LDFLAGS := -X $(VERSION_PACKAGE).BuildTimestamp=$(BUILD_TIMESTAMP) -X $(VERSION_PACKAGE).GitHash=$(GIT_HASH)

ifeq ($(OS),Windows_NT)
BIN ?= kander.exe
else
BIN ?= kander
endif

.PHONY: all build test vet fmt fmt-check clean install help

all: build

## build: build the kander binary into the repository root
build:
	$(GO) build $(GOFLAGS) -ldflags '$(VERSION_LDFLAGS) $(LDFLAGS)' -o $(BIN) $(PKG)

## test: run the whole test suite
test:
	$(GO) test ./...

## vet: run static analysis
vet:
	$(GO) vet ./...

## fmt: format all Go sources
fmt:
	$(GO) fmt ./...

## fmt-check: only report unformatted files, never rewrite them
fmt-check:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then echo "$$out"; exit 1; fi

## clean: remove build artifacts
clean:
	rm -f kander kander.exe

## install: build, then run the install script for the current scope
install: build
	sh install.sh

## help: list the available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
