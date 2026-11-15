# DuckDB Report Studio

[![CI](https://github.com/alexwoo79/duckdb-report/actions/workflows/ci.yml/badge.svg)](https://github.com/alexwoo79/duckdb-report/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/alexwoo79/duckdb-report)](https://github.com/alexwoo79/duckdb-report/releases)
[![License](https://img.shields.io/github/license/alexwoo79/duckdb-report)](LICENSE)

<p align="center">
  <img src="build/appicon.png" width="160" alt="DuckDB Report Studio icon">
</p>

一个面向本地分析交付的 DuckDB + SQL + Quarto 工作台。界面使用 Vue 3 + TypeScript，桌面壳与执行层使用 Go + Wails。

## 能力

- 接入 CSV、JSON、Parquet、Excel（`.xlsx` / `.xlsm`）和 DuckDB 数据库。
- SQL 编辑、保存、执行与结果表格分页展示。
- 查询结果支持全屏查看并导出 UTF-8 BOM CSV，便于 Excel 直接打开。
- QMD 编辑、HTML/PDF 渲染及应用内预览。
- 用 `Preset → Step → Workflow` 组合可复用分析流程。
- Preset 支持查看参数、新建、编辑、复制 ID、删除及一键加入步骤。
- SQL Preset 支持 `file` 文件模式与 `inline` 完整 SQL 模式，并可打开原生 SQL 文件选择器。
- 工作流支持 ZIP 包导出/导入，包含工作流定义及引用的 SQL、QMD、模板样式和数据文件。
- 报告后可追加输出节点，把 HTML/PDF、QMD、assets 与该次报告的 `analysis.duckdb` 快照一起打包为 ZIP。
- macOS、Windows 与 Linux 发布包支持原生桌面运行，界面沿用 `duckui_notebook` 的图标与视觉风格。
- 使用 `{{variable}}` 在 SQL、QMD、路径和查询中共享参数。
- 工作流步骤实时状态、运行日志和独立产物目录。
- 设置页可修改应用显示名、目标工作区和主题，并查看报告运行时状态。
- 工作区可导出为独立 `.duckdb` 发布快照，使用 R DuckDB 驱动可读取的兼容存储格式。
- 深色/浅色主题，布局延续 `duckui_notebook` 的三栏工作台和低干扰工具栏风格。

## 技术结构

```text
main.go / app.go    Wails 桌面入口与前端绑定
internal/cli        CLI 命令树与无界面工作区执行
internal/database   DuckDB 会话、数据导入、SQL 执行
internal/report     Quarto 渲染与报告表格生成
internal/runner     Workflow 步骤编排
internal/workspace  工作区、模板、参数与产物管理
internal/preview    受控 HTML/PDF 预览路由
frontend            Vue 3 + TypeScript + Vite
example             四表 Join → 汇总 → 透视 → Python/R 报告完整样例
```

依赖采用显式构造器手工注入，没有引入 DI 框架。工作台是单用户本地应用，SQL 与 QMD 被设计为可直接执行，因此不应作为多租户远程服务暴露。

## 安装

普通用户请从 [GitHub Releases](https://github.com/alexwoo79/duckdb-report/releases) 下载：

- `duckdb-report-v*-macos-universal.zip`
- `duckdb-report-v*-windows-amd64.zip`
- `duckdb-report-v*-linux-amd64.zip`

详细环境、Quarto、R、Python、PDF 中文字体及资源目录说明见 [安装文档](docs/INSTALL.md)。

## 源码构建环境

- Go 1.26+
- Node.js 20+
- Wails CLI 2.13+
- Quarto 1.8+
- PDF 默认使用 Quarto 内置 Typst；只有自定义 LaTeX 格式时才需要 LaTeX 发行版
- 示例提供 Python/Jupyter 与 R 两套 Quarto 报告
- Linux 构建需要 GCC、GTK 3 与 WebKitGTK 4.1 开发包；Ubuntu 24.04/Debian 13 可安装 `build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev`

当前机器上的检测结果：

```text
DuckDB  v2.0.0 / go-duckdb v2.20000.0
Quarto  1.8.26
Wails   2.13.0
```

## 构建与运行

```bash
make          # 类型检查、测试并构建当前平台应用
make dev      # Wails 热重载开发
make test     # Go 测试 + Vue TypeScript 构建
make run      # 构建后打开应用
make build-cli # 构建独立 CLI
make build-linux  # 在 Linux 原生环境构建 x64 程序
make build-windows # 在 Windows 原生环境构建 x64 程序
```

输出：

```text
build/bin/duckdb_report.app
build/bin/duckdb_report.exe
build/bin/duckdb_report
build/bin/duckdb-report-cli
build/bin/duckdb-report-cli.exe
```

也可以直接使用：

```bash
wails build
wails build -platform linux/amd64 -tags webkit2_41
wails build -platform windows/amd64
```

Linux 默认使用 `webkit2_41` 构建标签，并通过 `GOTOOLCHAIN=go1.26.0` 固定已验证的 Wails 2.13 工具链。需要兼容仍只提供 WebKitGTK 4.0 的旧发行版时，可使用 `make build-linux LINUX_WEBKIT_TAGS=webkit2_40`，并在目标发行版上验证对应开发包。

GitHub Actions 的 `v*` 标签发布会分别编译 macOS Universal（Apple Silicon + Intel）、Windows x64 和 Linux x64。也可以手动触发 `Release` 工作流：默认上传三平台 snapshot ZIP，并创建 `snapshot-<short-sha>` 预发布页面；每个包都包含桌面程序、CLI、安装文档和同一提交的 `duckdb-sql-analytics` Skill。

## CLI 自动化

桌面二进制在带子命令时进入 CLI 模式；发布包同时提供独立的
`duckdb-report-cli`，避免 Windows GUI 子系统影响控制台输出。CLI 与 GUI
共用工作区、DuckDB、Quarto 和 Workflow 执行层。

```bash
duckdb-report-cli --workspace /path/to/workspace status --json
duckdb-report-cli --workspace /path/to/workspace query \
  --file sql/monthly_analysis.sql --limit 100 --json
duckdb-report-cli --workspace /path/to/workspace doctor --json
duckdb-report-cli --workspace /path/to/workspace workflow validate --json
duckdb-report-cli --workspace /path/to/workspace workflow run \
  --param start_date=2026-01-01 --param end_date=2026-12-31 --json
duckdb-report-cli --workspace /path/to/workspace render \
  --template templates/monthly_report.qmd --format pdf \
  --query-file sql/monthly_result.sql --json
```

主要命令：

- `status`：工作区、DuckDB、Quarto、R、Python、Jupyter、表和数据源状态。
- `query`：执行文件或内联 SQL，支持 `.read`。
- `doctor`：只读检查 Workflow、SQL、QMD、资源路径和运行时；`--explain`
  还会验证报告查询能否在当前工作库中绑定执行计划。
- `import`：导入 CSV、JSON、Parquet、Excel 或附加 DuckDB。
- `render`：渲染 HTML/PDF，并支持 `query_*` 命名表格。
- `workflow validate|run|import|export`：无界面验证、运行与迁移工作流。

`--json` 将机器可读结果写到 stdout，诊断和步骤进度写到 stderr；退出码 `0`
表示成功，`1` 表示运行错误，`2` 表示参数或命令使用错误。应用正在运行时不要用
CLI 并发写同一个 `workspace.duckdb`。

`doctor` 发现 error 时返回退出码 `1`；warning 默认不阻断，使用 `--strict`
可将 warning 也视为失败。它不自动修改 SQL 或业务口径。

## 工作区

默认工作区：

```text
macOS:   ~/Library/Application Support/duckdb_report/
Windows: %APPDATA%\duckdb_report\
Linux:   ${XDG_CONFIG_HOME:-~/.config}/duckdb_report/
```

可用环境变量切换：

```bash
DUCKDB_REPORT_HOME=/path/to/workspace wails build
```

目录内容：

```text
workspace.duckdb
data/
sql/
templates/
workflows/current.json
runs/<run-id>/report.qmd
runs/<run-id>/report.html
runs/<run-id>/report.pdf
runs/<run-id>/quarto.log
exports/<package-name>.zip
examples/monthly_sales/
```

首次启动会复制内置示例和 `example/` 中的月度销售组合分析样例，不覆盖已存在的用户文件。

设置保存在独立文件：

```text
~/Library/Application Support/duckdb_report/settings.json
```

Windows 对应 `%APPDATA%\duckdb_report\settings.json`，Linux 对应 `${XDG_CONFIG_HOME:-~/.config}/duckdb_report/settings.json`。

设置页保存新的工作区目录后，会在下次启动时启用。现有工作区数据不会自动迁移，需要时自行复制目录。通过 `DUCKDB_REPORT_HOME` 启动时，环境变量优先于设置文件中的工作区。

数据管理支持两种清理方式：

- 点击数据源旁的 `×` 只移除该导入表或外部数据库连接。
- “清空工作库”会删除当前工作库中的用户表、视图和外部连接；SQL、QMD、工作流和报告文件不会删除。

工作区设置中的“导出工作库 `.duckdb`”会生成完整数据库快照，便于组合分析结果并作为发布数据交付。导出使用兼容存储版本，可供 R DuckDB 驱动和常见 DuckDB 客户端读取。

“数据与分析资产”中的导入行为：

- CSV、JSON、Parquet、Excel 会进入工作库 `workspace.duckdb`，成为可查询表。
- DuckDB 文件以外部 catalog 附加，不会复制数据库内容。
- 导入完成后会创建数据源 Preset，并自动向当前工作流加入对应导入步骤。

## QMD 运行时

QMD 不一定需要 R 或 Python：

- 纯 Markdown / HTML 报告不需要脚本运行时。
- 使用 `{{table}}` 时，Go 后端先执行 SQL 并把结果表注入 QMD，不需要 R/Python。
- 显式包含 ```` ```{r} ```` 代码块时，需要 Quarto 能找到 R/Rscript。
- 显式包含 ```` ```{python} ```` 代码块时，需要 Python 与 Jupyter。
- PDF 默认由 Quarto 内置 Typst 生成。
- PDF 中文字体会根据 macOS、Windows 或 Linux 自动选择，也可用运行参数 `report_font` 覆盖。

报告页会实时识别当前 QMD 的依赖；缺少运行时时会禁用渲染按钮并说明替代方案。

## Workflow 约定

工作流保存在 `workflows/current.json`：

- Preset 固化可复用参数，例如数据源、SQL 集合和报告模板。
- Step 按顺序执行 `import`、`sql`、`render`、`export`。
- 参数覆盖顺序为：运行参数 > Step overrides > Preset 默认值。
- 模板变量统一使用 `{{name}}`。
- QMD 中的 `{{table}}` 是保留插入点，报告渲染时替换为 SQL 查询结果表。

SQL Preset 的 `mode` 参数支持：

- `file`：从 `file` 读取工作区中的 `.sql` 文件，可通过“选择 SQL 文件”按钮选择。
- `inline`：直接执行 `inline_sql` 中保存的完整 SQL，支持多条语句和 `.read`。
- 未设置 `mode` 的旧 Preset 自动按 `file` 处理。

工作流页面的“导出工作流包”会生成可迁移的 `.zip`：

- `workflow.json` 保存 Preset、Step、运行参数和标题。
- 引用的 SQL 文件会连同同目录脚本一起打包，以保留 `.read` 依赖。
- QMD 会连同相邻 `assets/` 等资源一起打包。
- 数据源 Preset 引用的 CSV、Excel、Parquet 或 DuckDB 文件会一并打包。

导入时文件解压到 `workflows/imports/<id>/`，资源路径自动改写，并设置为当前工作流，不会覆盖已有的同名 SQL 或模板。

报告发布包使用 `output` Preset 与 `export` Step 配置：

- `filename`：ZIP 文件名，最终会自动附加本次工作流运行 ID。
- `directory`：相对工作区的输出目录，默认 `exports`。
- `include_report`：是否包含渲染生成的 HTML/PDF。
- `include_database`：是否包含同一报告运行目录中的 `analysis.duckdb` 快照。
- `include_source`：是否包含 `report.qmd` 与 `assets/`。

`export` 必须位于 `render` 之后；如果没有前置渲染步骤，工作流会明确报错。该数据库快照可独立打开，不会打包正在被应用锁定的 `workspace.duckdb`。

报告 Preset 可以配置多个命名查询，在同一份 QMD 中输出多张表：

```text
query           → {{table}}
query_sales     → {{table_sales}}
query_products  → {{table_products}}
```

例如 `query_sales` 的值是 `SELECT * FROM sales_summary`，QMD 可以同时写：

```markdown
## 月度汇总

{{table}}

## 产品排名

{{table_products}}
```

命名查询与正文占位符必须一一对应；缺少 `query_products` 时，渲染会直接指出缺失参数。

示例 Preset：

```json
{
  "id": "sql-monthly",
  "type": "sqlset",
  "name": "月度经营口径",
  "params": {
    "file": "sql/monthly_analysis.sql",
    "query_file": "sql/monthly_result.sql",
    "limit": "500"
  }
}
```

## 验证

```bash
gofmt -w .
make test
```

在 Linux 上直接调用 Go 工具链时需带桌面后端标签：`go test -tags webkit2_41 ./...`。

当前测试覆盖：

- Workflow 校验与参数优先级
- `{{variable}}` 替换和路径越界防护
- DuckDB 多语句查询
- `.read` SQL 嵌套文件展开
- Excel 导入
- 报告表格 HTML 转义
- `{{table}}` 注入顺序
- 预览文件路由和目录穿越拦截
- 报告数据库快照与数据库发布包
- 跨平台 PDF 中文字体选择

## Agent 协作

推荐配合 [DuckDB SQL Analytics Skill](https://github.com/alexwoo79/skills_plugins/tree/main/codex-skills/duckdb-sql-analytics)
使用。Skill 负责 SQL 口径、时间序列方法、工作区发现和 Workflow/QMD 校验；桌面程序
提供实际的 DuckDB、Quarto 和发布包执行能力。

安装 Skill：

```bash
git clone https://github.com/alexwoo79/skills_plugins.git
mkdir -p "${CODEX_HOME:-$HOME/.codex}/skills"
cp -a skills_plugins/codex-skills/duckdb-sql-analytics \
  "${CODEX_HOME:-$HOME/.codex}/skills/"
```

典型 Agent 工作流：

```text
$duckdb-sql-analytics
  → status / doctor
  → 修改 SQL、QMD、workflows/current.json
  → workflow validate
  → workflow run --json
  → 用户按需在 GUI 审阅报告
```

对应命令：

```bash
duckdb-report-cli --workspace /path/to/workspace status --json
duckdb-report-cli --workspace /path/to/workspace doctor --json
duckdb-report-cli --workspace /path/to/workspace workflow validate --json
duckdb-report-cli --workspace /path/to/workspace workflow run --json
```

Agent 也可以直接生成或修改工作区中的：

- `workflows/*.json`
- `sql/*.sql`
- `templates/*.qmd`

CLI 适合自动化执行，GUI 适合人工审核、编辑和预览产物。运行前建议先检查
SQL/QMD 权限，因为该工作台刻意保留了本地分析工具的完整表达能力。
