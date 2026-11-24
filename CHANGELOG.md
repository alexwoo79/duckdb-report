# Changelog

## v2.0.9 - 2026-11-24

### Changed

- Rebalance the Settings page into two independently stacked columns so cards no longer leave blank gaps from row-height mismatches.

## v2.0.8 - 2026-11-24

### Added

- Track the latest stable GitHub Release on startup and every six hours, show an update badge on the Settings rail button, and expose release details in Settings.
- Download the matching macOS, Windows, or Linux package to `Downloads/duckdb-report-updates`, verify it against the published SHA-256 checksum, and open the verified package from Settings.

## v2.0.7 - 2026-11-23

### Changed

- Keep SQL and QMD editing inside CodeMirror and defer Vue model synchronization until typing pauses, while forcing a final synchronization before run, render, save, and blur.
- Refresh only the database catalog after SQL execution and rebuild completions only when the table catalog changes.
- Cache report runtime detection for one minute and reuse the number formatter in query results.
- Enable Linux WebKit GPU acceleration on demand by default, with `DUCKDB_REPORT_WEBVIEW_GPU=never` as a compatibility fallback.
- Default DuckDB to half the available CPU threads and a 60% memory limit, configurable through `DUCKDB_REPORT_THREADS` and `DUCKDB_REPORT_MEMORY_LIMIT`.

## v2.0.6 - 2026-11-21

### Fixed

- Prevent Windows console windows from flashing when the desktop app probes or starts Quarto, R, Python, and system file handlers.

## v2.0.5 - 2026-11-21

### Changed

- Remove the CLI `workflow` command group and its runner wiring. `status` no longer reads workflow metadata, and `doctor` now audits workspace SQL and QMD files directly while ignoring legacy `workflows/current.json`.

## v2.0.4 - 2026-11-20

### Fixed

- Resolve relative SQL file paths against the active workspace for `COPY`, common DuckDB readers, `ATTACH`, and `EXPORT DATABASE`, instead of the desktop process working directory.
- Resolve relative data import paths against the active workspace while leaving absolute paths, URLs, path expressions, and SQL-file-relative `.read` includes unchanged.

## v2.0.3 - 2026-11-20

### Changed

- Move workspace switching and DuckDB export controls into the data asset sidebar, remove the workspace card from Settings, and rebalance the remaining Settings cards into a two-column layout.

## v2.0.2 - 2026-11-20

### Fixed

- Treat only the application default directory as a seeded workspace. User-selected workspaces no longer receive starter folders or `workflows/current.json`; optional directories are created only when files or reports are written.
- Remove the SQL parameter strip so SQL executes exactly as edited and report rendering does not inject preset parameters.
- Make bundled SQL and QMD samples self-contained with `analysis_params`, fixed QMD defaults, and a one-click `run_all.sql` so older demo assets remain usable.
- Render the R sample with `ragg_png` so report generation works when macOS R cannot load X11 or Cairo graphics libraries.
- Remove the obsolete starter `templates/monthly_report.qmd` and provide non-null empty SQL, template, and workflow collections when switching to an empty workspace.

## v2.0.1 - 2026-11-20

### Changed

- Refocus the desktop interface on manual SQL analysis and report generation. Remove the Workflow entry point, Preset sidebar, workflow inspector, and workflow shortcuts while keeping the CLI-compatible backend for existing workspaces.
- Keep report rendering free of preset data SQL; all analysis and chart code lives directly in the QMD script.
- Make the data asset sidebar horizontally resizable, persist the selected width, and expose keyboard resizing plus double-click reset.

## v0.1.11 - 2026-11-19

### Changed

- Tighten dashboard spacing, align the log drawer with the main content gutters, soften card and overlay shadows, brighten the light palette, and use the new application icon in the title bar.

## v0.1.10 - 2026-11-18

### Changed

- Expand the remaining SQL or report pane when its counterpart is collapsed, and return the inspector's grid space to the main workspace when the inspector is hidden.

## v0.1.9 - 2026-11-18

### Fixed

- Exclude bundled example directories from SQL and QMD lists in existing workspaces, remove unchanged unreferenced starter files, and hide known sample tables injected by earlier workspace-switch versions.

## v0.1.8 - 2026-11-18

### Fixed

- Keep existing workspaces isolated by skipping built-in starter SQL, QMD, CSV, and sample tables unless the selected directory is new or is the application's default workspace.

## v0.1.7 - 2026-11-18

### Fixed

- Apply a workspace selected through the directory picker immediately, without requiring a separate settings save from the page header.

## v0.1.6 - 2026-11-18

### Fixed

- Apply workspace path changes immediately by reinitializing the DuckDB session, workflow runtime, and report preview against the selected workspace, while preserving the active workspace if switching fails.

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
