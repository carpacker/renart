/* @bruin
name: staging.orders
type: duckdb.sql
description: Cleaned order headers — one row per order.
materialization:
  type: view
meta:
  renart_inferred_upstreams: raw.orders
depends:
  - raw.orders
columns:
  - name: order_id
    type: INTEGER
  - name: customer_id
    type: INTEGER
  - name: order_date
    type: DATE
@bruin */

SELECT
  order_id,
  customer_id,
  CAST(ordered_at AS DATE) AS order_date
FROM raw.orders
