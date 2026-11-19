CREATE OR REPLACE TABLE analysis_params AS
SELECT
    DATE '2026-01-01' AS start_date,
    DATE '2026-12-31' AS end_date,
    8 AS top_n;

CREATE OR REPLACE TABLE monthly_summary AS
SELECT
    date_trunc('month', order_date::DATE)::DATE AS month,
    region,
    product,
    count(*) AS orders,
    round(sum(revenue), 2) AS revenue,
    round(sum(cost), 2) AS cost,
    round(sum(revenue - cost), 2) AS gross_profit,
    round(100.0 * sum(revenue - cost) / nullif(sum(revenue), 0), 2) AS gross_margin_pct
FROM sales
WHERE order_date::DATE BETWEEN
    (SELECT start_date FROM analysis_params)
    AND (SELECT end_date FROM analysis_params)
GROUP BY 1, 2, 3
ORDER BY 1, 2, 3;

CREATE OR REPLACE TABLE monthly_kpi_summary AS
SELECT
    round(sum(revenue), 2) AS revenue,
    round(sum(cost), 2) AS cost,
    round(sum(revenue - cost), 2) AS gross_profit,
    round(100.0 * sum(revenue - cost) / nullif(sum(revenue), 0), 2) AS gross_margin_pct,
    count(*) AS orders
FROM sales
WHERE order_date::DATE BETWEEN
    (SELECT start_date FROM analysis_params)
    AND (SELECT end_date FROM analysis_params);
