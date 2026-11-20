GO ?= go
WAILS_VERSION ?= v2.13.0
WAILS ?= $(GO) run github.com/wailsapp/wails/v2/cmd/wails@$(WAILS_VERSION)
NPM ?= npm
UNAME_S := $(shell uname -s)
LINUX_WEBKIT_TAGS ?= webkit2_41
GO_TOOLCHAIN ?= go1.26.0
BUILD_LDFLAGS ?= -s -w
export GOTOOLCHAIN := $(GO_TOOLCHAIN)
CLI_NAME := duckdb-report-cli
ifeq ($(OS),Windows_NT)
CLI_NAME := duckdb-report-cli.exe
endif

GO_TEST_TAGS :=
ifeq ($(UNAME_S),Linux)
GO_TEST_TAGS := $(LINUX_WEBKIT_TAGS)
endif

.PHONY: all test frontend build build-cli build-linux build-windows dev run

all: test build build-cli

test:
ifeq ($(GO_TEST_TAGS),)
	go test ./...
else
	go test -tags "$(GO_TEST_TAGS)" ./...
endif
	cd frontend && $(NPM) run build

frontend:
	cd frontend && $(NPM) ci

build:
ifeq ($(UNAME_S),Darwin)
	CGO_LDFLAGS="$${CGO_LDFLAGS:--mmacosx-version-min=13.0}" $(WAILS) build -trimpath -ldflags="$(BUILD_LDFLAGS)"
else ifeq ($(UNAME_S),Linux)
	$(WAILS) build -trimpath -ldflags="$(BUILD_LDFLAGS)" -tags "$(LINUX_WEBKIT_TAGS)"
else
	$(WAILS) build -trimpath -ldflags="$(BUILD_LDFLAGS)"
endif

build-cli:
	go build -trimpath -ldflags="$(BUILD_LDFLAGS)" -o "build/bin/$(CLI_NAME)" ./cmd/duckdb-report-cli

build-linux:
	$(WAILS) build -platform linux/amd64 -trimpath -ldflags="$(BUILD_LDFLAGS)" -tags "$(LINUX_WEBKIT_TAGS)"

build-windows:
	$(WAILS) build -platform windows/amd64 -trimpath -ldflags="$(BUILD_LDFLAGS)"

dev:
ifeq ($(UNAME_S),Linux)
	$(WAILS) dev -tags "$(LINUX_WEBKIT_TAGS)"
else
	$(WAILS) dev
endif

run: build
ifeq ($(UNAME_S),Darwin)
	open build/bin/duckdb_report.app
else
	./build/bin/duckdb_report
endif
