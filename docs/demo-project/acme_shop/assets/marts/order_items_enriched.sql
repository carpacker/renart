/* @bruin
name: marts.order_items_enriched
type: duckdb.sql
description: Order line items joined to orders and products, with line revenue.
materialization:
  type: table
meta:
  renart_inferred_upstreams: staging.order_items,staging.orders,staging.products
depends:
  - staging.order_items
  - staging.orders
  - staging.products
columns:
  - name: order_id
    type: INTEGER
  - name: order_date
    type: DATE
  - name: customer_id
    type: INTEGER
  - name: product_id
    type: INTEGER
  - name: product_name
    type: VARCHAR
  - name: category
    type: VARCHAR
  - name: quantity
    type: INTEGER
  - name: unit_price
    type: DECIMAL(10,2)
  - name: line_revenue
    type: DECIMAL(12,2)
@bruin */

SELECT
  oi.order_id,
  o.order_date,
  o.customer_id,
  oi.product_id,
  p.product_name,
  p.category,
  oi.quantity,
  p.unit_price,
  oi.quantity * p.unit_price AS line_revenue
FROM staging.order_items AS oi
JOIN staging.orders AS o ON oi.order_id = o.order_id
JOIN staging.products AS p ON oi.product_id = p.product_id
