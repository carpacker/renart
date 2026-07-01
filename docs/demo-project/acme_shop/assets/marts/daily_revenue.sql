/* @bruin
name: marts.daily_revenue
type: duckdb.sql
description: Revenue and order count per day.
materialization:
  type: table
meta:
  renart_inferred_upstreams: marts.order_items_enriched
depends:
  - marts.order_items_enriched
columns:
  - name: order_date
    type: DATE
  - name: orders
    type: BIGINT
  - name: revenue
    type: DECIMAL(12,2)
@bruin */

SELECT
  order_date,
  COUNT(DISTINCT order_id) AS orders,
  SUM(line_revenue) AS revenue
FROM marts.order_items_enriched
GROUP BY order_date
ORDER BY order_date
