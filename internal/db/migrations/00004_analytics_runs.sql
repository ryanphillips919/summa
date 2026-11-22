-- +goose Up
CREATE TABLE analytics_runs (
  id                    INTEGER PRIMARY KEY,
  organization_id       INTEGER NOT NULL REFERENCES organizations(id),
  created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  source_transaction_id INTEGER NOT NULL CHECK (source_transaction_id >= 0),
  forecast_days         INTEGER NOT NULL CHECK (forecast_days BETWEEN 1 AND 90),
  observation_days      INTEGER NOT NULL CHECK (observation_days > 0),
  model                 TEXT NOT NULL,
  result_json           TEXT NOT NULL CHECK (json_valid(result_json))
);

CREATE INDEX analytics_runs_organization_id_id
  ON analytics_runs (organization_id, id DESC);

-- +goose Down
DROP INDEX analytics_runs_organization_id_id;
DROP TABLE analytics_runs;