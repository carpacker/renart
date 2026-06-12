/* @bruin
name: fixture.raw_events
type: duckdb.sql
materialization:
  type: table
  strategy: append
  incremental_key: ts
@bruin */
select * from source_events where region = '{{ var.region }}'
