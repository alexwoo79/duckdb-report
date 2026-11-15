# Contributing

## Development setup

1. Install Go 1.26+, Node.js 20+, Wails 2.13, and optionally Quarto, R, and Python. On Linux, also install GCC, GTK 3, and the WebKitGTK 4.1 development package.
2. Clone the repository.
3. Install frontend dependencies with `cd frontend && npm ci`.
4. Run `make test`.
5. Start the development app with `make dev`.

## Before opening a pull request

Run:

```bash
gofmt -w .
go test ./...
cd frontend && npm run build
```

On Linux with WebKitGTK 4.1, use `go test -tags webkit2_41 ./...`; `make test` selects this tag automatically.

Keep generated binaries, `frontend/dist`, DuckDB files, and report output out of commits. Explain any workflow format changes and include a migration path for existing `workflows/current.json` files.
