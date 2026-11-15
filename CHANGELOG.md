# Changelog

## v0.1.2 - 2026-11-15

### Fixed

- Resolve QMD `params` defaults when rendering reports outside a workflow.
- Keep `top_n=8` as a compatibility default for earlier monthly report templates.
- Discover Jupyter and Python installed through Python.org Framework, pyenv, Conda, and Windows user installs.
- Force UTF-8 locale for GUI-launched R sessions so Chinese DuckDB strings render correctly.

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
