/* @bruin
name: fixture.daily_summary
type: duckdb.sql
depends:
  - fixture.raw_events
materialization:
  type: table
@bruin */
select date_trunc('day', ts) as day, count(*) as events
from fixture.raw_events
group by 1
