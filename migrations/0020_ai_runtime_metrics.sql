-- +goose Up
-- +goose StatementBegin
--
-- Final subset of the P1-B-02 extraction: the ai_runtime_metrics
-- hypertable. Originally lived in a separate Pool.Exec call after the
-- main inline block so a Timescale-specific failure (e.g. extension
-- not loaded) wouldn't cascade into the main schema migrations.
--
-- One row per (service, sample_ts) tick; we record deltas (calls,
-- confirmed, filtered) since the previous tick rather than cumulative
-- counters, so range queries can SUM() without worrying about api
-- restarts that reset in-process atomics. GPU fields are absolute
-- readings sampled at tick time.
--
-- Note: `SELECT create_hypertable(...)` is gated on the timescaledb
-- extension being installed. On a vanilla Postgres this would fail.
-- We rely on the 0001 baseline having already established the
-- extension; goose runs migrations in a transaction, so a missing
-- extension would fail this whole migration cleanly with a clear
-- error rather than producing a half-applied state.

CREATE TABLE IF NOT EXISTS ai_runtime_metrics (
    ts                  TIMESTAMPTZ NOT NULL,
    service             TEXT NOT NULL,
    site_id             UUID,
    gpu_util_pct        INT,
    gpu_memory_used_mb  INT,
    gpu_memory_total_mb INT,
    gpu_temperature_c   INT,
    calls_delta         INT NOT NULL DEFAULT 0,
    confirmed_delta     INT NOT NULL DEFAULT 0,
    filtered_delta      INT NOT NULL DEFAULT 0,
    avg_inference_ms    INT
);
ALTER TABLE ai_runtime_metrics ADD COLUMN IF NOT EXISTS site_id UUID;
SELECT create_hypertable('ai_runtime_metrics', 'ts', if_not_exists => TRUE);
CREATE INDEX IF NOT EXISTS idx_ai_metrics_service_ts
    ON ai_runtime_metrics (service, ts DESC);
CREATE INDEX IF NOT EXISTS idx_ai_metrics_site_ts
    ON ai_runtime_metrics (site_id, ts DESC) WHERE site_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
--
-- Down drops the hypertable cleanly. Timescale handles DROP TABLE on
-- a hypertable by removing all chunks; no extra cleanup needed.
DROP INDEX IF EXISTS idx_ai_metrics_site_ts;
DROP INDEX IF EXISTS idx_ai_metrics_service_ts;
DROP TABLE IF EXISTS ai_runtime_metrics;
-- +goose StatementEnd
