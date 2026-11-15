WAILS ?= wails
NPM ?= npm

.PHONY: all test frontend build build-windows dev run

all: test build

test:
	go test ./...
	cd frontend && $(NPM) run build

frontend:
	cd frontend && $(NPM) ci

build:
ifeq ($(shell uname -s),Darwin)
	CGO_LDFLAGS="$${CGO_LDFLAGS:--mmacosx-version-min=13.0}" $(WAILS) build
else
	$(WAILS) build
endif

build-windows:
	$(WAILS) build -platform windows/amd64

dev:
	$(WAILS) dev

run: build
	open build/bin/duckdb_report.app
