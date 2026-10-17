-- Generalized observability for non-Xray cores; legacy Xray history remains intact.
ALTER TABLE instances ADD COLUMN clash_endpoint TEXT NOT NULL DEFAULT '';
CREATE TABLE core_clients(instance_id INTEGER NOT NULL REFERENCES instances(id),inbound_tag TEXT NOT NULL,asset_key TEXT NOT NULL,email TEXT NOT NULL,protocol TEXT NOT NULL DEFAULT '',level INTEGER NOT NULL DEFAULT 0,source TEXT NOT NULL,present INTEGER NOT NULL DEFAULT 1,observed_at TEXT NOT NULL,PRIMARY KEY(instance_id,inbound_tag,asset_key,source));
CREATE TABLE core_user_states(instance_id INTEGER NOT NULL REFERENCES instances(id),email TEXT NOT NULL,stats_state TEXT NOT NULL DEFAULT 'unknown',online_state TEXT NOT NULL DEFAULT 'unknown',online_count INTEGER,online_basis TEXT NOT NULL DEFAULT '',checked_at TEXT NOT NULL,online_kind TEXT NOT NULL DEFAULT '',PRIMARY KEY(instance_id,email));
CREATE TABLE core_diagnostics(id INTEGER PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),checked_at TEXT NOT NULL,trigger TEXT NOT NULL,report_json TEXT NOT NULL);
CREATE INDEX core_diagnostics_instance_time ON core_diagnostics(instance_id,id);
CREATE TABLE core_runtime(instance_id INTEGER PRIMARY KEY REFERENCES instances(id),version TEXT NOT NULL DEFAULT 'Unknown',build_info TEXT NOT NULL DEFAULT 'Unknown',version_source TEXT NOT NULL DEFAULT 'unavailable',verified_at TEXT);
