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
WHERE order_date::DATE BETWEEN DATE '{{start_date}}' AND DATE '{{end_date}}'
GROUP BY 1, 2, 3
ORDER BY 1, 2, 3;

CREATE OR REPLACE TABLE kpi_summary AS
SELECT
    round(sum(revenue), 2) AS revenue,
    round(sum(cost), 2) AS cost,
    round(sum(revenue - cost), 2) AS gross_profit,
    round(100.0 * sum(revenue - cost) / nullif(sum(revenue), 0), 2) AS gross_margin_pct,
    count(*) AS orders
FROM sales
WHERE order_date::DATE BETWEEN DATE '{{start_date}}' AND DATE '{{end_date}}';
