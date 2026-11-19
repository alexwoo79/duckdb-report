-- 手工分析参数：修改这里即可调整后续汇总区间与产品排名数量。
CREATE OR REPLACE TABLE analysis_params AS
SELECT
    DATE '2026-01-01' AS start_date,
    DATE '2026-12-31' AS end_date,
    8 AS top_n;

-- 将订单、客户、明细和产品四张子表连接为订单事实表。
CREATE OR REPLACE TABLE order_detail AS
SELECT
    o.order_id,
    o.order_date::DATE AS order_date,
    o.status,
    o.channel,
    c.customer_id,
    c.customer_name,
    c.region,
    c.segment,
    p.product_id,
    p.product_name,
    p.category,
    i.quantity,
    p.unit_price,
    p.unit_cost,
    i.discount_rate,
    round(i.quantity * p.unit_price * (1 - i.discount_rate), 2) AS revenue,
    round(i.quantity * p.unit_cost, 2) AS cost,
    round(
        i.quantity * p.unit_price * (1 - i.discount_rate)
        - i.quantity * p.unit_cost,
        2
    ) AS gross_profit
FROM orders AS o
JOIN customers AS c
  ON c.customer_id = o.customer_id
JOIN order_items AS i
  ON i.order_id = o.order_id
JOIN products AS p
  ON p.product_id = i.product_id
WHERE o.status <> 'cancelled';
