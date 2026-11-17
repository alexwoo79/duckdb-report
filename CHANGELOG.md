# Changelog

## Unreleased

## v0.1.5 - 2026-11-17

### Added

- Add a collapsible database/schema/table asset tree with attached-database status, search, and detach actions.
- Add a CodeMirror SQL editor with Tab-accepted DuckDB keyword, function, table, and column completion plus common keyboard shortcuts.
- Add a Quarto-aware QMD editor with YAML and Quarto completion plus new-document, save-as, and rendering shortcuts.
- Add editor font-size shortcuts and a persistent 11–17px application UI font-size setting.
- Add Parquet export for query results alongside UTF-8 BOM CSV export.
- Add Linux x64 desktop builds, CI verification, and release packaging for WebKitGTK 4.1.
- Discover common Linux Quarto, R, Miniforge, Mambaforge, and app-specific Jupyter installation paths for GUI-launched reports.
- Add `duckdb-report-cli` with JSON commands for workspace status, SQL, imports, report rendering, and workflow validation/execution/import/export.
- Add a read-only `doctor` command for workflow, SQL, QMD, asset, and runtime diagnostics, with opt-in `EXPLAIN`.
- Package the desktop app, CLI, documentation, and companion `duckdb-sql-analytics` Skill together for macOS, Windows, and Linux, with manual snapshot packaging support.
- Publish manually triggered snapshot packages as GitHub prereleases with all platform ZIPs and checksums.

## v0.1.4 - 2026-11-16

### Changed

- Replace sample sales owner names with clearly fictional English placeholders.
- Detect the legacy sample by SHA-256 and refresh both its CSV and DuckDB table without embedding the old names in source code.
- Add resizable and collapsible SQL editor/result panes.
- Add resizable and collapsible QMD/HTML report panes.
- Add syntax highlighting for SQL and QMD editors with synchronized scrolling and line numbers.

## v0.1.3 - 2026-11-15

### Added

- Support multiple report tables with `query_xxx` Preset parameters and `{{table_xxx}}` placeholders.
- Pass report Preset parameters into direct report rendering while preserving workflow runtime overrides.

## v0.1.2 - 2026-11-15

### Fixed

- Resolve QMD `params` defaults when rendering reports outside a workflow.
- Keep `top_n=8` as a compatibility default for earlier monthly report templates.
- Discover Jupyter and Python installed through Python.org Framework, pyenv, Conda, and Windows user installs.
- Force UTF-8 locale for GUI-launched R sessions so Chinese DuckDB strings render correctly.
- Prevent the starter workflow from overwriting the monthly example KPI table schema.

## v0.1.1 - 2026-11-15

### Fixed

- Publish a macOS Universal application for both Apple Silicon and Intel Macs.
- Select a Windows-compatible Chinese report font and normalize report database paths for Windows.

## v0.1.0 - 2026-11-15

### Added

- Local DuckDB SQL workbench with CSV, Excel, JSON, Parquet, and external DuckDB imports.
- Reusable data source, SQL, report, and export presets with editable workflow steps.
- Quarto HTML/PDF rendering with Python, R, and Go-injected report tables.
- Report preview, source download, CSV query export, sorting, search, and fullscreen result tables.
- Workflow ZIP import/export and report packages containing `analysis.duckdb`, QMD, assets, and rendered output.
- Monthly sales example covering table joins, summary tables, pivots, validation, and Python/R reports.
- macOS and Windows application builds based on the `duckui_notebook` icon.
