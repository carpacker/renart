/* @bruin
name: staging.products
type: duckdb.sql
description: Cleaned, typed product catalog — one row per product.
materialization:
  type: view
meta:
  renart_inferred_upstreams: raw.products
depends:
  - raw.products
columns:
  - name: product_id
    type: INTEGER
  - name: product_name
    type: VARCHAR
  - name: category
    type: VARCHAR
  - name: unit_price
    type: DECIMAL(10,2)
@bruin */

SELECT
  product_id,
  product_name,
  category,
  CAST(unit_price AS DECIMAL(10, 2)) AS unit_price
FROM raw.products
