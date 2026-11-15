# DuckDB Report Studio 安装与运行环境

本文档面向直接使用 GitHub Release 的用户，也包含从源码构建和准备报告运行时的步骤。

## 1. 支持平台

- macOS 13 或更高版本，Universal 包同时支持 Apple Silicon 与 Intel。
- Windows 10/11 x64，需要 Microsoft Edge WebView2 Runtime。
- Linux x64，需要 GTK 3 与 WebKitGTK 4.1。发布包面向 Ubuntu 24.04、Debian 13、Fedora 40+、Arch Linux 等同代或更新的 glibc 发行版。
- 应用内置 DuckDB 2.0 和前端资源，不要求用户单独安装 DuckDB 或 Node.js。

## 2. 从 Release 安装

从 [Releases](https://github.com/alexwoo79/duckdb-report/releases) 下载对应压缩包：

- `duckdb-report-v0.1.4-macos-universal.zip`
- `duckdb-report-v0.1.4-windows-amd64.zip`
- `duckdb-report-v*-linux-amd64.zip`

macOS：

1. 解压后把 `duckdb_report.app` 移入“应用程序”。
2. 当前发布包未做 Apple Developer 公证。首次启动请右键应用并选择“打开”。

Windows：

1. 解压 ZIP，运行 `duckdb_report.exe`。
2. Windows 10/11 通常已包含 WebView2。若启动提示缺少 WebView2，请安装 Microsoft Edge WebView2 Evergreen Runtime。
3. 当前发布包未做代码签名，SmartScreen 可能要求选择“更多信息”后确认运行。

Linux：

1. 解压 `duckdb-report-*-linux-amd64.zip`。
2. 确认已安装 GTK 3 与 WebKitGTK 4.1：

   ```bash
   # Ubuntu 24.04 / Debian 13
   sudo apt install libgtk-3-0t64 libwebkit2gtk-4.1-0

   # Fedora
   sudo dnf install gtk3 webkit2gtk4.1

   # Arch Linux
   sudo pacman -S gtk3 webkit2gtk-4.1
   ```

3. 运行解压后的可执行文件：

   ```bash
   ./duckdb_report
   ```

应用首次启动后会在以下目录创建工作区：

```text
macOS:   ~/Library/Application Support/duckdb_report
Windows: %APPDATA%\duckdb_report
Linux:   ${XDG_CONFIG_HOME:-~/.config}/duckdb_report
```

可以通过设置页切换工作区，或使用环境变量临时覆盖：

```text
DUCKDB_REPORT_HOME=/path/to/workspace
```

## 3. 报告运行时

应用本体和 SQL 工作台只需要内置 DuckDB。以下组件仅在使用 Quarto、R、Python 或 PDF 报告时需要。

### Quarto

所有 QMD 报告都需要 Quarto 1.8 或更高版本，并确保 `quarto` 位于 `PATH`。

从 [Quarto 下载页](https://quarto.org/docs/get-started/) 安装，安装后验证：

```bash
quarto --version
```

### Python 报告

需要 Python 3.10 或更高版本、Jupyter，以及示例 QMD 使用的包：

```bash
python3 -m pip install jupyter duckdb pandas matplotlib
python3 -m jupyter --version
```

Windows 可将 `python3` 替换为 `python` 或 `py`。

Linux 的系统 Python 可能被发行版标记为 externally managed。使用 `uv` 创建应用专用环境时：

```bash
uv venv --python /usr/bin/python3 ~/.local/share/duckdb-report/jupyter
uv pip install --python ~/.local/share/duckdb-report/jupyter/bin/python \
  jupyterlab ipykernel duckdb pandas matplotlib
~/.local/share/duckdb-report/jupyter/bin/python -m ipykernel install \
  --user --name duckdb-report --display-name "Python (DuckDB Report Studio)"
```

应用会自动发现这个 Linux Jupyter 环境，无需修改系统 Python。

### R 报告

需要 R 4.2 或更高版本。启动 R 后安装依赖：

```r
install.packages(c(
  "DBI", "duckdb", "dplyr", "ggplot2",
  "knitr", "scales", "rmarkdown"
))
```

验证：

```bash
Rscript --version
```

### PDF 与中文字体

Quarto 使用内置 Typst 生成 PDF，不强制安装 LaTeX。PDF 可使用与操作系统匹配的中文字体：

- macOS：PingFang SC
- Windows：Microsoft YaHei
- Linux：Noto Sans CJK SC

应用会把当前系统对应的字体名注入报告模板。若系统缺少建议字体，请安装 `Noto Sans CJK SC` 或设置工作流运行参数 `report_font` 覆盖默认字体。

只有自定义 XeLaTeX/LaTeX 模板才需要 TeX 发行版。Linux 可安装用户级 TinyTeX 与中文排版包：

```bash
Rscript -e 'tinytex::install_tinytex(add_path = TRUE)'
tlmgr install collection-latexrecommended xecjk
```

## 4. 内置资源

发布程序内嵌以下资源，用户不需要从源码目录复制：

- Vue 前端生产构建。
- `example/` 月度销售示例数据、SQL、QMD 和组合工作流。
- `internal/workspace/starters/` 基础模板、SQL、示例销售数据和默认工作流。
- `build/appicon.png`、`build/appicon.icns` 与 `build/windows/icon.ico`。

运行后产生的资源位于工作区：

```text
data/                 导入与示例数据
sql/                  用户 SQL
templates/            QMD 模板及 assets
workflows/current.json
runs/<run-id>/        报告、QMD、日志和 analysis.duckdb
exports/              报告与数据库发布包
workspace.duckdb      当前工作库
```

## 5. 从源码构建

准备：

- Go 1.26+
- Node.js 20+
- Wails CLI 2.13
- 对应平台 C 编译器

Linux 还需要 GTK 3 与 WebKitGTK 4.1 开发包。Ubuntu 24.04/Debian 13：

```bash
sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
```

Fedora：`sudo dnf install gcc pkg-config gtk3-devel webkit2gtk4.1-devel`。Arch Linux：`sudo pacman -S base-devel pkgconf gtk3 webkit2gtk-4.1`。

安装 Wails：

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
```

获取并构建：

```bash
git clone https://github.com/alexwoo79/duckdb-report.git
cd duckdb-report
make test
make build
```

macOS 输出：

```text
build/bin/duckdb_report.app
```

构建 macOS Universal 包：

```bash
wails build -platform darwin/universal
```

Windows 原生构建：

```powershell
make build-windows
```

Windows 输出：

```text
build\bin\duckdb_report.exe
```

Linux 原生构建：

```bash
make build-linux
```

Linux 输出：

```text
build/bin/duckdb_report
build/bin/duckdb-report-cli
```

也可以直接运行 `GOTOOLCHAIN=go1.26.0 wails build -platform linux/amd64 -tags webkit2_41`。`make` 与 GitHub Actions 默认使用 WebKitGTK 4.1，并固定 Wails 2.13 已验证的 Go 1.26 工具链；若目标系统只有 WebKitGTK 4.0，可在 Linux 原生环境使用 `make build-linux LINUX_WEBKIT_TAGS=webkit2_40`。

构建独立 CLI：

```bash
make build-cli
```

发布包中的 CLI 可直接用于 Agent、脚本和定时任务：

```bash
duckdb-report-cli --workspace /path/to/workspace status --json
duckdb-report-cli --workspace /path/to/workspace doctor --json
duckdb-report-cli --workspace /path/to/workspace workflow run --json
```

CLI 与桌面应用不能同时写入同一个 `workspace.duckdb`。运行 CLI 前应关闭对应窗口。

`doctor` 会检查工作流引用、Step 顺序、参数占位符、SQL 语法、QMD 表格映射、
报告资源与运行时依赖。加入 `--explain` 会额外检查查询计划绑定；`--strict`
会把 warning 作为失败返回。

Agent 场景建议配合
[DuckDB SQL Analytics Skill](https://github.com/alexwoo79/skills_plugins/tree/main/codex-skills/duckdb-sql-analytics)。
Skill 的入口、SQL 模式和 CLI 调用协议说明位于该仓库的 `SKILL.md` 与
`references/duckdb-report-integration.md`。

Release 与手动触发的三平台包都包含：

```text
duckdb_report       # macOS 为 .app，Windows 为 .exe
duckdb-report-cli   # macOS 为调用 app 内二进制的启动脚本
skill/duckdb-sql-analytics/
README.md
docs/INSTALL.md
LICENSE
```

手动触发 `Release` 工作流时，默认会创建 `snapshot-<short-sha>` 预发布页面并上传三个 ZIP 与
`checksums.txt`。若只需要 Actions Artifact，可在触发时关闭 `publish_release`。

从 macOS 交叉编译 Windows 需要 MinGW-w64。仓库的 GitHub Actions Release 流程使用原生 Windows Runner 构建，可避免本地 CGO 交叉编译问题。

## 6. 常见问题

### 报告按钮被禁用

确认 QMD 中的代码块类型与已安装运行时一致：

- Python 代码块需要 Python、Jupyter 和 `jupyter` 命令。
- R 代码块需要 `Rscript`。
- 只使用 `{{table}}` 的 QMD 由 Go 注入数据，不需要 R 或 Python。

### PDF 中文显示为方框

确认系统已安装 Microsoft YaHei、PingFang SC 或 Noto Sans CJK SC。也可以在报告工作流中增加运行参数：

```json
{
  "report_font": "Noto Sans CJK SC"
}
```

### Windows 无法启动

先确认 WebView2 Runtime 已安装。若界面出现 DuckDB 文件锁错误，请关闭重复启动的应用实例后再打开。

### Linux 无法启动或缺少 WebKitGTK

发布包动态依赖 GTK 3 与 WebKitGTK 4.1。可根据发行版安装 `libgtk-3-0t64`、`libwebkit2gtk-4.1-0`（Ubuntu/Debian），或 `gtk3`、`webkit2gtk4.1`（Fedora/Arch）。没有可用图形会话的服务器环境无法启动桌面界面。旧发行版如果只提供 WebKitGTK 4.0，需要自行从源码使用 `webkit2_40` 标签构建。

### Quarto 能执行但应用显示未找到

确保在启动应用前，`quarto`、`python3` 或 `Rscript` 已加入当前用户的 `PATH`。macOS 从 Finder 启动时不会读取交互式 shell 的临时 PATH 修改。
