# 月度销售组合分析示例

这套示例演示一条完整的数据分析流水线：

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

日期使用 `{{start_date}}`、`{{end_date}}`，产品排名数量使用 `{{top_n}}`。

## 工作流

- `workflows/monthly_sales_python.json`：完整流水线并使用 Python/Jupyter 报告。
- `workflows/monthly_sales_r.json`：相同分析口径并使用 R 报告。

工作流包含：

- 4 个 CSV 导入步骤。
- 四表 Join。
- 汇总和透视。
- 勾稽验证。
- Quarto 报告渲染。

## 报告

- `reports/monthly_sales_python.qmd`
- `reports/monthly_sales_r.qmd`
- `reports/assets/report.css`

两份报告读取报告运行目录中的 DuckDB 快照。应用会在 Quarto 执行前替换：

```text
{{database}}
{{top_n}}
```

应用程序会把 `database` 覆盖为当前运行的只读快照路径，因此不会与主工作库争用文件锁，
也不需要 Python papermill 或 Quarto `params` 运行时。

## 使用方式

首次启动应用时，示例会复制到工作区：

```text
examples/monthly_sales/
```

可以在“工作流 → 导入工作流”中选择对应的 JSON 文件，或通过工作流包导入功能加载完整资产。

Python 报告需要 Python、Jupyter 和 Matplotlib；R 报告需要 R、DBI、DuckDB、dplyr、ggplot2、knitr。
