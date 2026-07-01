/* @bruin
name: staging.order_items
type: duckdb.sql
description: Cleaned order line items — one row per order/product.
materialization:
  type: view
meta:
  renart_inferred_upstreams: raw.order_items
depends:
  - raw.order_items
columns:
  - name: order_id
    type: INTEGER
  - name: product_id
    type: INTEGER
  - name: quantity
    type: INTEGER
@bruin */

SELECT
  order_id,
  product_id,
  quantity
FROM raw.order_items
