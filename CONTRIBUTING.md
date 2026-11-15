# Contributing

## Development setup

1. Install Go 1.26+, Node.js 20+, Wails 2.13, and optionally Quarto, R, and Python.
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

Keep generated binaries, `frontend/dist`, DuckDB files, and report output out of commits. Explain any workflow format changes and include a migration path for existing `workflows/current.json` files.
