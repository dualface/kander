# Kander build entry point. The binary is self-installing: run it to open the wizard.

GO ?= go
PKG := ./cmd/kander
SERVER_PKG := ./cmd/kander-server
GOFLAGS ?=
LDFLAGS ?=
# Local default is git describe --tags --always. A checkout with no tags still
# succeeds with --always (short hash only); treat that like "no tag" and use
# dev, matching a non-git directory where describe fails.
VERSION ?= $(shell if git describe --tags >/dev/null 2>&1; then git describe --tags --always; else echo dev; fi)
VERSION_PACKAGE := github.com/dualface/kander/internal/version
VERSION_LDFLAGS := -X $(VERSION_PACKAGE).Version=$(VERSION)

ifeq ($(OS),Windows_NT)
BIN ?= kander.exe
SERVER_BIN ?= kander-server.exe
else
BIN ?= kander
SERVER_BIN ?= kander-server
endif

.PHONY: all build test vet fmt fmt-check clean install help

all: build

## build: build the kander and kander-server binaries into the repository root
build:
	$(GO) build $(GOFLAGS) -ldflags '$(VERSION_LDFLAGS) $(LDFLAGS)' -o $(BIN) $(PKG)
	$(GO) build $(GOFLAGS) -ldflags '$(VERSION_LDFLAGS) $(LDFLAGS)' -o $(SERVER_BIN) $(SERVER_PKG)

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
	rm -f kander kander.exe kander-server kander-server.exe

## install: build, then run the interactive installer
install: build
	./$(BIN) install

## help: list the available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
