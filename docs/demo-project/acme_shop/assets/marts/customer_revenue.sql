/* @bruin
name: marts.customer_revenue
type: duckdb.sql
description: Total revenue and order count per customer.
materialization:
  type: table
meta:
  renart_inferred_upstreams: marts.order_items_enriched,staging.customers
depends:
  - marts.order_items_enriched
  - staging.customers
columns:
  - name: customer_id
    type: INTEGER
  - name: customer_name
    type: VARCHAR
  - name: country
    type: VARCHAR
  - name: orders
    type: BIGINT
  - name: total_revenue
    type: DECIMAL(12,2)
@bruin */

SELECT
  c.customer_id,
  c.customer_name,
  c.country,
  COUNT(DISTINCT e.order_id) AS orders,
  SUM(e.line_revenue) AS total_revenue
FROM marts.order_items_enriched AS e
JOIN staging.customers AS c ON e.customer_id = c.customer_id
GROUP BY c.customer_id, c.customer_name, c.country
ORDER BY total_revenue DESC
