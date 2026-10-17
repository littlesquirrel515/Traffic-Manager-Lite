-- Keep per-observation evidence, including baselines/zero deltas, separate from traffic totals.
CREATE TABLE traffic_counter_observations(
 identity_id INTEGER NOT NULL REFERENCES identities(id), collected_at TEXT NOT NULL,
 provider_type TEXT NOT NULL, metric_scope TEXT NOT NULL, capability_status TEXT NOT NULL,
 instance_version TEXT NOT NULL, config_revision TEXT NOT NULL, epoch_id TEXT NOT NULL,
 source_upload_name TEXT NOT NULL, source_download_name TEXT NOT NULL,
 raw_upload INTEGER NOT NULL, raw_download INTEGER NOT NULL,
 traffic_direction TEXT NOT NULL, mapped INTEGER NOT NULL, coverage TEXT NOT NULL,
 upload_delta INTEGER NOT NULL, download_delta INTEGER NOT NULL, date TEXT NOT NULL,
 PRIMARY KEY(identity_id,collected_at));
CREATE INDEX counter_observation_retention ON traffic_counter_observations(collected_at);
-- Evidence only: these deltas are NEVER added to traffic_hourly/daily again.
CREATE TABLE archived_counter_evidence(
 identity_id INTEGER NOT NULL REFERENCES identities(id), date TEXT NOT NULL,
 provider_type TEXT NOT NULL, instance_version TEXT NOT NULL, config_revision TEXT NOT NULL,
 traffic_direction TEXT NOT NULL, source_upload_name TEXT NOT NULL, source_download_name TEXT NOT NULL,
 metric_scope TEXT NOT NULL, capability_status TEXT NOT NULL, mapped INTEGER NOT NULL, coverage TEXT NOT NULL,
 first_observed_at TEXT NOT NULL, last_observed_at TEXT NOT NULL, observations INTEGER NOT NULL,
 upload_delta INTEGER NOT NULL, download_delta INTEGER NOT NULL,
 PRIMARY KEY(identity_id,date,provider_type,instance_version,config_revision,traffic_direction,source_upload_name,source_download_name,metric_scope,capability_status,mapped,coverage));
