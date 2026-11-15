SELECT
    strftime(month, '%Y-%m') AS month,
    region,
    product,
    orders,
    revenue,
    cost,
    gross_profit,
    gross_margin_pct
FROM monthly_summary
ORDER BY month, region, product;
