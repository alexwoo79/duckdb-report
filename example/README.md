# 月度销售组合分析示例

这套示例演示一条手工可控的数据分析路径：

```text
客户/产品/订单/订单明细子表
  -> 四表 Join
  -> 月度区域品类汇总
  -> 区域×品类透视、趋势和产品排名
  -> 勾稽验证
  -> Python 或 R 报告
```

## 数据文件

- `data/customers.csv`：客户维度。
- `data/products.csv`：产品及其成本、售价。
- `data/orders.csv`：订单日期、状态、渠道和客户。
- `data/order_items.csv`：订单商品、数量、折扣。

已取消订单不会进入订单事实表；其余订单通过四表 Join 生成 `order_detail`。

## SQL 流程

1. `sql/01_join.sql`：生成 `order_detail`。
2. `sql/02_summary.sql`：生成 `sales_summary` 和 `kpi_summary`。
3. `sql/03_pivot.sql`：生成 `region_category_pivot`、`monthly_trend`、`product_ranking`。
4. `sql/04_validation.sql`：验证 Join 行数和收入汇总。

`01_join.sql` 会建立 `analysis_params` 表，统一保存样例日期区间与产品排名数量。
后续 SQL 从该表读取参数，因此不再依赖应用预设参数栏或工作流运行参数。

## 手工运行

在桌面版 SQL 页按顺序打开并运行 `01_join.sql`、`02_summary.sql`、
`03_pivot.sql` 和 `04_validation.sql`。每份 SQL 都会物化独立结果表，便于逐表检查。
也可以直接打开 `run_all.sql` 一键复跑全部步骤。

完成 SQL 后，在报告页：

1. 选择 Python 或 R QMD。
2. 在 QMD 脚本块中直接读取需要的结果表，分析、图表和表格代码全部写在 QMD 中。
3. 直接连接报告运行目录中的 `analysis.duckdb`。
4. 生成 HTML 或 PDF。

## 报告

- `reports/monthly_sales_python.qmd`
- `reports/monthly_sales_r.qmd`
- `reports/assets/report.css`

两份报告直接读取报告运行目录中的 `analysis.duckdb` 快照，因此不会与主工作库争用文件锁。
报告标题、产品排名数量、数据查询和图表代码都直接写在 QMD 中，不需要额外参数预设。

## 使用方式

首次启动应用时，示例会复制到工作区：

```text
examples/monthly_sales/
```

Python 报告需要 Python、Jupyter 和 Matplotlib；R 报告需要 R、DBI、DuckDB、dplyr、ggplot2、knitr。
