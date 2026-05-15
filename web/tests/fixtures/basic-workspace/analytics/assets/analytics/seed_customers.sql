/* @bruin
type: duckdb.sql
materialization:
  type: view
depends:
  - analytics.customer_seed
@bruin */

select customer_id, customer_name, segment
from analytics.customer_seed
