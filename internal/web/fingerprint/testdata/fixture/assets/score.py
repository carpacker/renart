""" @bruin
name: fixture.score
type: python
depends:
  - fixture.daily_summary
@bruin """

def materialize():
    return [1, 2, 3]
