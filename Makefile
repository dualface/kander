# Kander 构建入口. 产物与 install.sh / install.ps1 期望的路径一致,
# 安装器会直接复用仓库根目录下已构建的二进制.

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

## build: 构建 kander 二进制到仓库根目录
build:
	$(GO) build $(GOFLAGS) -ldflags '$(VERSION_LDFLAGS) $(LDFLAGS)' -o $(BIN) $(PKG)

## test: 运行全部测试
test:
	$(GO) test ./...

## vet: 静态检查
vet:
	$(GO) vet ./...

## fmt: 格式化全部 Go 源码
fmt:
	$(GO) fmt ./...

## fmt-check: 只报告未格式化的文件, 不改写
fmt-check:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then echo "$$out"; exit 1; fi

## clean: 删除构建产物
clean:
	rm -f kander kander.exe

## install: 构建后调用安装脚本安装到当前作用域
install: build
	sh install.sh

## help: 列出可用目标
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
