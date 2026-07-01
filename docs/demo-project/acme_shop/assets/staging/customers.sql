/* @bruin
name: staging.customers
type: duckdb.sql
description: Cleaned, typed customers — one row per customer.
materialization:
  type: view
meta:
  renart_inferred_upstreams: raw.customers
depends:
  - raw.customers
columns:
  - name: customer_id
    type: INTEGER
  - name: customer_name
    type: VARCHAR
  - name: country
    type: VARCHAR
  - name: signup_date
    type: DATE
@bruin */

SELECT
  customer_id,
  customer_name,
  country,
  CAST(signup_date AS DATE) AS signup_date
FROM raw.customers
