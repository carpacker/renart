/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as customer_id,'Ada' as customer_name union all select 2 as customer_id,'Grace' as customer_name
