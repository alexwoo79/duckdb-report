CREATE TABLE IF NOT EXISTS analysis_params AS
SELECT
    DATE '2026-01-01' AS start_date,
    DATE '2026-12-31' AS end_date,
    8 AS top_n;

-- 月度、区域、品类汇总。
CREATE OR REPLACE TABLE sales_summary AS
SELECT
    date_trunc('month', order_date)::DATE AS month,
    region,
    category,
    count(DISTINCT order_id) AS orders,
    sum(quantity) AS units,
    round(sum(revenue), 2) AS revenue,
    round(sum(cost), 2) AS cost,
    round(sum(gross_profit), 2) AS gross_profit,
    round(100.0 * sum(gross_profit) / nullif(sum(revenue), 0), 2) AS margin_pct
FROM order_detail
WHERE order_date BETWEEN
    (SELECT start_date FROM analysis_params)
    AND (SELECT end_date FROM analysis_params)
GROUP BY 1, 2, 3
ORDER BY 1, 2, 3;

CREATE OR REPLACE TABLE kpi_summary AS
SELECT
    count(DISTINCT order_id) AS orders,
    count(DISTINCT customer_id) AS customers,
    sum(quantity) AS units,
    round(sum(revenue), 2) AS revenue,
    round(sum(gross_profit), 2) AS gross_profit,
    round(100.0 * sum(gross_profit) / nullif(sum(revenue), 0), 2) AS margin_pct
FROM order_detail
WHERE order_date BETWEEN
    (SELECT start_date FROM analysis_params)
    AND (SELECT end_date FROM analysis_params);
