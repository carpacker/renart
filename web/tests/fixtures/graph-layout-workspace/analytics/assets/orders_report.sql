/* @bruin
name: analytics.orders_report
type: duckdb.sql
materialization:
  type: view
meta:
  web_view: table
@bruin */

select *
from analytics.orders
