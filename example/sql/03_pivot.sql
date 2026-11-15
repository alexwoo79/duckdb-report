-- 区域 × 品类收入透视与月度趋势。
CREATE OR REPLACE TABLE region_category_pivot AS
SELECT
    region,
    round(sum(revenue) FILTER (WHERE category = '办公家具'), 2) AS "办公家具",
    round(sum(revenue) FILTER (WHERE category = '电子设备'), 2) AS "电子设备",
    round(sum(revenue) FILTER (WHERE category = '办公用品'), 2) AS "办公用品",
    round(sum(revenue), 2) AS total_revenue,
    round(sum(gross_profit), 2) AS gross_profit,
    round(100.0 * sum(gross_profit) / nullif(sum(revenue), 0), 2) AS margin_pct
FROM sales_summary
GROUP BY region
ORDER BY total_revenue DESC;

CREATE OR REPLACE TABLE monthly_trend AS
SELECT
    month,
    round(sum(revenue), 2) AS revenue,
    round(sum(gross_profit), 2) AS gross_profit,
    round(100.0 * sum(gross_profit) / nullif(sum(revenue), 0), 2) AS margin_pct
FROM sales_summary
GROUP BY month
ORDER BY month;

CREATE OR REPLACE TABLE product_ranking AS
SELECT
    product_name,
    category,
    sum(quantity) AS units,
    round(sum(revenue), 2) AS revenue,
    round(sum(gross_profit), 2) AS gross_profit,
    round(100.0 * sum(gross_profit) / nullif(sum(revenue), 0), 2) AS margin_pct
FROM order_detail
GROUP BY product_name, category
ORDER BY revenue DESC
LIMIT {{top_n}};
