CREATE TABLE preferred_sync_runs (
 id INTEGER PRIMARY KEY,
 job_id TEXT NOT NULL REFERENCES preferred_sync_jobs(id) ON DELETE CASCADE,
 trigger TEXT NOT NULL CHECK(trigger IN ('initial','manual','scheduled')),
 started_at TEXT NOT NULL,
 finished_at TEXT NOT NULL,
 duration_ms INTEGER NOT NULL CHECK(duration_ms>=0),
 status TEXT NOT NULL,
 candidate_count INTEGER NOT NULL,
 active_count INTEGER NOT NULL,
 slot_count INTEGER NOT NULL,
 limit_value INTEGER NOT NULL,
 sources_json TEXT NOT NULL,
 error_code TEXT NOT NULL DEFAULT ''
);
CREATE INDEX preferred_sync_runs_job_time ON preferred_sync_runs(job_id,id DESC);
