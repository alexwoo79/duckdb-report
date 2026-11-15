-- 基础勾稽检查：明细收入应等于汇总收入，Join 不应改变明细行数。
CREATE OR REPLACE TABLE validation_results AS
WITH
base_rows AS (
    SELECT count(*) AS row_count
    FROM order_items AS i
    JOIN orders AS o USING (order_id)
    WHERE o.status <> 'cancelled'
),
joined_rows AS (
    SELECT count(*) AS row_count
    FROM order_detail
),
source_revenue AS (
    SELECT round(sum(revenue), 2) AS revenue
    FROM order_detail
),
summary_revenue AS (
    SELECT round(sum(revenue), 2) AS revenue
    FROM sales_summary
)
SELECT 'row_count_preserved' AS check_name,
       joined_rows.row_count AS actual_value,
       base_rows.row_count AS expected_value,
       joined_rows.row_count = base_rows.row_count AS passed
FROM joined_rows, base_rows
UNION ALL
SELECT 'revenue_reconciled',
       summary_revenue.revenue,
       source_revenue.revenue,
       summary_revenue.revenue = source_revenue.revenue
FROM summary_revenue, source_revenue;
