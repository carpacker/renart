/* @bruin
name: analytics.orders
type: duckdb.sql
materialization:
  type: view
meta:
  web_view: table
@bruin */

select 100 as order_id, 1 as customer_id, 42 as total_amount
union all
select 101 as order_id, 2 as customer_id, 84 as total_amount
