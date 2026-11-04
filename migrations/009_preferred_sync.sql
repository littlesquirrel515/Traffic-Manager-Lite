CREATE TABLE preferred_sync_jobs(
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL UNIQUE,
 template_node_id TEXT NOT NULL REFERENCES node_profiles(id),
 group_id TEXT NOT NULL UNIQUE REFERENCES node_groups(id),
 config_json TEXT NOT NULL,
 state_json TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 1,
 revision INTEGER NOT NULL DEFAULT 1,
 next_run INTEGER NOT NULL,
 last_attempt TEXT NOT NULL,
 last_success TEXT NOT NULL,
 status TEXT NOT NULL
);
CREATE TABLE preferred_sync_slots(
 job_id TEXT NOT NULL REFERENCES preferred_sync_jobs(id),
 position INTEGER NOT NULL,
 node_id TEXT NOT NULL UNIQUE REFERENCES node_profiles(id),
 endpoint_id TEXT NOT NULL UNIQUE REFERENCES node_endpoints(id),
 binding_id TEXT NOT NULL UNIQUE REFERENCES inbound_endpoint_bindings(id),
 expected_revision TEXT NOT NULL,
 expected_profile TEXT NOT NULL,
 active INTEGER NOT NULL DEFAULT 1,
 PRIMARY KEY(job_id,position)
);
CREATE INDEX preferred_sync_due ON preferred_sync_jobs(enabled,next_run);
