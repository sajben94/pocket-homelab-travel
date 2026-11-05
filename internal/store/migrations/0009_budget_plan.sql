-- A trip's budget plan, made before it starts: how much each category may
-- cost, as a total or per day of the trip (amounts in the trip currency, minor
-- units). The trip budget is the sum of the plan once there is one.
CREATE TABLE IF NOT EXISTS budget_plan (
  trip_id  INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
  category TEXT NOT NULL,
  amount   INTEGER NOT NULL DEFAULT 0,
  per_day  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (trip_id, category)
);
