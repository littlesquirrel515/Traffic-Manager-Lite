-- Traffic Manager Lite v2: fresh databases only. No legacy upgrades.
CREATE TABLE servers(id INTEGER PRIMARY KEY, name TEXT NOT NULL, address TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE instances(id INTEGER PRIMARY KEY, server_id INTEGER NOT NULL REFERENCES servers(id), name TEXT NOT NULL, core_type TEXT NOT NULL CHECK(core_type IN ('xray','v2fly','singbox','hysteria2')), api_endpoint TEXT NOT NULL, api_secret TEXT NOT NULL DEFAULT '', config_path TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, last_collected_at TEXT, last_error TEXT NOT NULL DEFAULT '', capabilities_json TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, control_endpoint TEXT NOT NULL DEFAULT '', detected_version TEXT NOT NULL DEFAULT '', clash_endpoint TEXT NOT NULL DEFAULT '', clash_secret TEXT NOT NULL DEFAULT '');
CREATE TABLE users(id INTEGER PRIMARY KEY, display_name TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE identities(id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), instance_id INTEGER NOT NULL REFERENCES instances(id), inbound_tag TEXT NOT NULL DEFAULT '', core_user_key TEXT NOT NULL, scope TEXT NOT NULL CHECK(scope IN ('user','inbound','instance')), last_active_at TEXT, UNIQUE(instance_id,inbound_tag,core_user_key,scope));
CREATE TABLE traffic_cursors(identity_id INTEGER PRIMARY KEY REFERENCES identities(id), raw_upload INTEGER NOT NULL CHECK(raw_upload>=0), raw_download INTEGER NOT NULL CHECK(raw_download>=0), epoch_id TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL, provider_type TEXT NOT NULL DEFAULT '', instance_version TEXT NOT NULL DEFAULT '');
CREATE TABLE traffic_samples(id INTEGER PRIMARY KEY, identity_id INTEGER NOT NULL REFERENCES identities(id), instance_id INTEGER NOT NULL REFERENCES instances(id), inbound_tag TEXT NOT NULL, upload_delta INTEGER NOT NULL CHECK(upload_delta>=0), download_delta INTEGER NOT NULL CHECK(download_delta>=0), collected_at TEXT NOT NULL, hour_start TEXT NOT NULL, date TEXT NOT NULL, batch_id TEXT NOT NULL DEFAULT '', UNIQUE(identity_id,collected_at));
CREATE TABLE traffic_hourly(hour_start TEXT NOT NULL, identity_id INTEGER NOT NULL REFERENCES identities(id), instance_id INTEGER NOT NULL REFERENCES instances(id), upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0), download_bytes INTEGER NOT NULL CHECK(download_bytes>=0), PRIMARY KEY(hour_start,identity_id));
CREATE TABLE traffic_daily(date TEXT NOT NULL, identity_id INTEGER NOT NULL REFERENCES identities(id), instance_id INTEGER NOT NULL REFERENCES instances(id), upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0), download_bytes INTEGER NOT NULL CHECK(download_bytes>=0), PRIMARY KEY(date,identity_id));
CREATE TABLE archive_ledger(hour_start TEXT NOT NULL, identity_id INTEGER NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, PRIMARY KEY(hour_start,identity_id));
CREATE TABLE archive_daily_ledger(date TEXT NOT NULL, identity_id INTEGER NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, PRIMARY KEY(date,identity_id));
CREATE TABLE collector_errors(id INTEGER PRIMARY KEY, instance_id INTEGER NOT NULL REFERENCES instances(id), message TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE online_snapshots(instance_id INTEGER PRIMARY KEY REFERENCES instances(id), records_json TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE archive_runs(id INTEGER PRIMARY KEY, cutoff TEXT NOT NULL, deleted INTEGER NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE collection_batches(instance_id INTEGER PRIMARY KEY REFERENCES instances(id), batch_id TEXT NOT NULL);
CREATE TABLE instance_epochs(instance_id INTEGER PRIMARY KEY REFERENCES instances(id), epoch_id TEXT NOT NULL, boot_estimate TEXT NOT NULL, observed_at TEXT NOT NULL);
CREATE TABLE xray_clients(instance_id INTEGER NOT NULL REFERENCES instances(id), inbound_tag TEXT NOT NULL, asset_key TEXT NOT NULL, email TEXT NOT NULL, protocol TEXT NOT NULL DEFAULT '', level INTEGER NOT NULL DEFAULT 0, source TEXT NOT NULL, present INTEGER NOT NULL DEFAULT 1, observed_at TEXT NOT NULL, credential_fingerprint TEXT NOT NULL DEFAULT '', PRIMARY KEY(instance_id,inbound_tag,asset_key,source));
CREATE TABLE collector_health(instance_id INTEGER NOT NULL REFERENCES instances(id), collector TEXT NOT NULL, status TEXT NOT NULL, result_count INTEGER NOT NULL DEFAULT 0, checked_at TEXT NOT NULL, success_at TEXT, error_code TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '', failures INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(instance_id,collector));
CREATE TABLE xray_user_states(instance_id INTEGER NOT NULL REFERENCES instances(id), email TEXT NOT NULL, stats_state TEXT NOT NULL DEFAULT 'unknown', online_state TEXT NOT NULL DEFAULT 'unknown', online_count INTEGER, online_basis TEXT NOT NULL DEFAULT '', checked_at TEXT NOT NULL, PRIMARY KEY(instance_id,email));
CREATE TABLE xray_diagnostics(id INTEGER PRIMARY KEY, instance_id INTEGER NOT NULL REFERENCES instances(id), checked_at TEXT NOT NULL, trigger TEXT NOT NULL, report_json TEXT NOT NULL);
CREATE TABLE xray_runtime(instance_id INTEGER PRIMARY KEY REFERENCES instances(id), version TEXT NOT NULL DEFAULT 'Unknown', build_info TEXT NOT NULL DEFAULT 'Unknown', version_source TEXT NOT NULL DEFAULT 'unavailable', verified_at TEXT, evidence_json TEXT NOT NULL DEFAULT '{}');
CREATE TABLE core_clients(instance_id INTEGER NOT NULL REFERENCES instances(id),inbound_tag TEXT NOT NULL,asset_key TEXT NOT NULL,email TEXT NOT NULL,protocol TEXT NOT NULL DEFAULT '',level INTEGER NOT NULL DEFAULT 0,source TEXT NOT NULL,present INTEGER NOT NULL DEFAULT 1,observed_at TEXT NOT NULL,PRIMARY KEY(instance_id,inbound_tag,asset_key,source));
CREATE TABLE core_user_states(instance_id INTEGER NOT NULL REFERENCES instances(id),email TEXT NOT NULL,stats_state TEXT NOT NULL DEFAULT 'unknown',online_state TEXT NOT NULL DEFAULT 'unknown',online_count INTEGER,online_basis TEXT NOT NULL DEFAULT '',checked_at TEXT NOT NULL,online_kind TEXT NOT NULL DEFAULT '',PRIMARY KEY(instance_id,email));
CREATE TABLE core_diagnostics(id INTEGER PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),checked_at TEXT NOT NULL,trigger TEXT NOT NULL,report_json TEXT NOT NULL);
CREATE TABLE core_runtime(instance_id INTEGER PRIMARY KEY REFERENCES instances(id),version TEXT NOT NULL DEFAULT 'Unknown',build_info TEXT NOT NULL DEFAULT 'Unknown',version_source TEXT NOT NULL DEFAULT 'unavailable',verified_at TEXT);
CREATE TABLE traffic_provenance(identity_id INTEGER PRIMARY KEY REFERENCES identities(id), provider_type TEXT NOT NULL,metric_scope TEXT NOT NULL,capability_status TEXT NOT NULL,collected_at TEXT NOT NULL,instance_version TEXT NOT NULL,config_revision TEXT NOT NULL,source_upload_name TEXT NOT NULL,source_download_name TEXT NOT NULL,raw_upload INTEGER NOT NULL,raw_download INTEGER NOT NULL,traffic_direction TEXT NOT NULL,mapped INTEGER NOT NULL,coverage TEXT NOT NULL);
CREATE TABLE native_connection_cursors(instance_id INTEGER NOT NULL REFERENCES instances(id),epoch_id TEXT NOT NULL,connection_id TEXT NOT NULL,inbound_tag TEXT NOT NULL,user_key TEXT NOT NULL,raw_upload INTEGER NOT NULL,raw_download INTEGER NOT NULL,observed_at TEXT NOT NULL,PRIMARY KEY(instance_id,epoch_id,connection_id));
CREATE TABLE native_user_totals(instance_id INTEGER NOT NULL REFERENCES instances(id),inbound_tag TEXT NOT NULL,user_key TEXT NOT NULL,upload INTEGER NOT NULL,download INTEGER NOT NULL,PRIMARY KEY(instance_id,inbound_tag,user_key));
CREATE TABLE core_asset_states(instance_id INTEGER NOT NULL REFERENCES instances(id),inbound_tag TEXT NOT NULL,email TEXT NOT NULL,online_state TEXT NOT NULL,online_count INTEGER,online_kind TEXT NOT NULL,online_basis TEXT NOT NULL,checked_at TEXT NOT NULL,PRIMARY KEY(instance_id,inbound_tag,email));
CREATE TABLE provider_observations(instance_id INTEGER NOT NULL REFERENCES instances(id),provider_type TEXT NOT NULL,metric_scope TEXT NOT NULL,capability_status TEXT NOT NULL,collected_at TEXT NOT NULL,instance_version TEXT NOT NULL,config_revision TEXT NOT NULL,summary_json TEXT NOT NULL,PRIMARY KEY(instance_id,provider_type,metric_scope));
CREATE TABLE config_operations(id INTEGER PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),operation TEXT NOT NULL,inbound_tag TEXT NOT NULL,old_revision TEXT NOT NULL,new_revision TEXT NOT NULL,apply_status TEXT NOT NULL,created_at TEXT NOT NULL,summary TEXT NOT NULL);
CREATE TABLE direction_audits(id INTEGER PRIMARY KEY,created_at TEXT NOT NULL,instance_id INTEGER NOT NULL,affected_records INTEGER NOT NULL,status TEXT NOT NULL,backup_path TEXT NOT NULL DEFAULT '');
CREATE TABLE traffic_direction_repairs(instance_id INTEGER NOT NULL REFERENCES instances(id),source_profile TEXT NOT NULL,cutoff TEXT NOT NULL,affected_records INTEGER NOT NULL,backup_path TEXT NOT NULL,evidence TEXT NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(instance_id,source_profile,cutoff));
CREATE TABLE core_connection_snapshots(instance_id INTEGER NOT NULL REFERENCES instances(id),provider_type TEXT NOT NULL,collected_at TEXT NOT NULL,capability_status TEXT NOT NULL,metric_scope TEXT NOT NULL,connections_json TEXT NOT NULL,PRIMARY KEY(instance_id,provider_type));
CREATE TABLE traffic_counter_observations(
 identity_id INTEGER NOT NULL REFERENCES identities(id), collected_at TEXT NOT NULL,
 provider_type TEXT NOT NULL, metric_scope TEXT NOT NULL, capability_status TEXT NOT NULL,
 instance_version TEXT NOT NULL, config_revision TEXT NOT NULL, epoch_id TEXT NOT NULL,
 source_upload_name TEXT NOT NULL, source_download_name TEXT NOT NULL,
 raw_upload INTEGER NOT NULL, raw_download INTEGER NOT NULL,
 traffic_direction TEXT NOT NULL, mapped INTEGER NOT NULL, coverage TEXT NOT NULL,
 upload_delta INTEGER NOT NULL, download_delta INTEGER NOT NULL, date TEXT NOT NULL,
 PRIMARY KEY(identity_id,collected_at));
CREATE TABLE archived_counter_evidence(
 identity_id INTEGER NOT NULL REFERENCES identities(id), date TEXT NOT NULL,
 provider_type TEXT NOT NULL, instance_version TEXT NOT NULL, config_revision TEXT NOT NULL,
 traffic_direction TEXT NOT NULL, source_upload_name TEXT NOT NULL, source_download_name TEXT NOT NULL,
 metric_scope TEXT NOT NULL, capability_status TEXT NOT NULL, mapped INTEGER NOT NULL, coverage TEXT NOT NULL,
 first_observed_at TEXT NOT NULL, last_observed_at TEXT NOT NULL, observations INTEGER NOT NULL,
 upload_delta INTEGER NOT NULL, download_delta INTEGER NOT NULL,
 PRIMARY KEY(identity_id,date,provider_type,instance_version,config_revision,traffic_direction,source_upload_name,source_download_name,metric_scope,capability_status,mapped,coverage));
CREATE TABLE credential_material(id TEXT PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),inbound_tag TEXT NOT NULL,core_client_ref TEXT NOT NULL,user_id INTEGER REFERENCES users(id),auth_json TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'available',source TEXT NOT NULL DEFAULT 'manual_unverified',verified_at TEXT NOT NULL);
CREATE TABLE catalog_inbounds(id TEXT PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),source_ref TEXT NOT NULL,tag TEXT NOT NULL,protocol TEXT NOT NULL,listen_address TEXT NOT NULL DEFAULT '',listen_port INTEGER NOT NULL,profile_json TEXT NOT NULL,structure_hash TEXT NOT NULL,config_hash TEXT NOT NULL,present INTEGER NOT NULL DEFAULT 1,review INTEGER NOT NULL DEFAULT 0,runtime_state TEXT NOT NULL DEFAULT 'unknown',discovered_at TEXT NOT NULL, authentication_type TEXT NOT NULL DEFAULT 'unknown', source_capabilities_json TEXT NOT NULL DEFAULT '{"configured_clients":"source_only","runtime_loaded":"unknown","dynamic_management":"unknown"}', display_name TEXT NOT NULL DEFAULT '', listen_ports_json TEXT NOT NULL DEFAULT '[]', retired INTEGER NOT NULL DEFAULT 0,UNIQUE(instance_id,tag,protocol));
CREATE TABLE node_endpoints(id TEXT PRIMARY KEY,name TEXT NOT NULL,profile_json TEXT NOT NULL,enabled INTEGER NOT NULL DEFAULT 1,sort_order INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', endpoint_type TEXT NOT NULL DEFAULT 'custom');
CREATE TABLE node_profiles(id TEXT PRIMARY KEY,inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),endpoint_id TEXT NOT NULL REFERENCES node_endpoints(id),name TEXT NOT NULL,description TEXT NOT NULL DEFAULT '',source_type TEXT NOT NULL CHECK(source_type IN ('discovered')),profile_json TEXT NOT NULL DEFAULT '{}',overrides_json TEXT NOT NULL DEFAULT '{}',tags_json TEXT NOT NULL DEFAULT '[]',enabled INTEGER NOT NULL DEFAULT 1,sort_order INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL, binding_id TEXT NOT NULL REFERENCES inbound_endpoint_bindings(id), variant_key TEXT NOT NULL DEFAULT 'default');
CREATE TABLE node_groups(id TEXT PRIMARY KEY,name TEXT NOT NULL,description TEXT NOT NULL DEFAULT '',enabled INTEGER NOT NULL DEFAULT 1,sort_order INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE node_group_members(group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,node_id TEXT NOT NULL REFERENCES node_profiles(id),sort_order INTEGER NOT NULL,PRIMARY KEY(group_id,node_id));
CREATE TABLE subscribers(id TEXT PRIMARY KEY,name TEXT NOT NULL,external_ref TEXT NOT NULL DEFAULT '',enabled INTEGER NOT NULL DEFAULT 1,created_at TEXT NOT NULL,updated_at TEXT NOT NULL, traffic_limit_bytes INTEGER NOT NULL DEFAULT 0 CHECK(traffic_limit_bytes>=0), expires_at TEXT, activated_at TEXT, quota_enabled INTEGER NOT NULL DEFAULT 0, expiry_enabled INTEGER NOT NULL DEFAULT 0, quota_revision INTEGER NOT NULL DEFAULT 1, quota_initialized INTEGER NOT NULL DEFAULT 0, quota_reset_at TEXT, quota_policy TEXT NOT NULL DEFAULT 'report_partial');
CREATE TABLE subscriber_credentials(id TEXT PRIMARY KEY,subscriber_id TEXT NOT NULL REFERENCES subscribers(id),inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),credential_id TEXT NOT NULL REFERENCES credential_material(id),enabled INTEGER NOT NULL DEFAULT 1,created_at TEXT NOT NULL,billable INTEGER NOT NULL DEFAULT 1,UNIQUE(subscriber_id,inbound_id));
CREATE TABLE catalog_subscriptions(id TEXT PRIMARY KEY,subscriber_id TEXT NOT NULL REFERENCES subscribers(id),name TEXT NOT NULL,token_hash TEXT NOT NULL UNIQUE,token_version INTEGER NOT NULL DEFAULT 1,enabled INTEGER NOT NULL DEFAULT 1,expires_at TEXT,format TEXT NOT NULL,options_json TEXT NOT NULL DEFAULT '{}',last_access_at TEXT,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE catalog_subscription_nodes(subscription_id TEXT NOT NULL REFERENCES catalog_subscriptions(id) ON DELETE CASCADE,node_id TEXT NOT NULL REFERENCES node_profiles(id),sort_order INTEGER NOT NULL,enabled INTEGER NOT NULL DEFAULT 1,PRIMARY KEY(subscription_id,node_id));
CREATE TABLE catalog_subscription_groups(subscription_id TEXT NOT NULL REFERENCES catalog_subscriptions(id) ON DELETE CASCADE,group_id TEXT NOT NULL REFERENCES node_groups(id),sort_order INTEGER NOT NULL,PRIMARY KEY(subscription_id,group_id));
CREATE TABLE subscription_access_logs(id INTEGER PRIMARY KEY,subscription_id TEXT REFERENCES catalog_subscriptions(id) ON DELETE SET NULL,format TEXT NOT NULL,accessed_at TEXT NOT NULL,status_code INTEGER NOT NULL,error_code TEXT NOT NULL,user_agent TEXT NOT NULL,client_ip_hash TEXT NOT NULL);
CREATE TABLE catalog_sync_runs(id INTEGER PRIMARY KEY,instance_id INTEGER REFERENCES instances(id),config_hash TEXT NOT NULL DEFAULT '',status TEXT NOT NULL,summary_json TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE inbound_identity_aliases(instance_id INTEGER NOT NULL REFERENCES instances(id),source_locator TEXT NOT NULL,tag TEXT NOT NULL,protocol TEXT NOT NULL,inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),first_seen_at TEXT NOT NULL,last_seen_at TEXT NOT NULL,PRIMARY KEY(instance_id,source_locator,tag,protocol));
CREATE TABLE inbound_identity_history(id INTEGER PRIMARY KEY,inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),instance_id INTEGER NOT NULL REFERENCES instances(id),old_tag TEXT NOT NULL,new_tag TEXT NOT NULL,old_source TEXT NOT NULL,new_source TEXT NOT NULL,config_hash TEXT NOT NULL,method TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE inbound_identity_pending(id TEXT PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),source_locator TEXT NOT NULL,tag TEXT NOT NULL,protocol TEXT NOT NULL,profile_json TEXT NOT NULL,listen_address TEXT NOT NULL,candidates_json TEXT NOT NULL,config_hash TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'needs_review',resolved_inbound_id TEXT REFERENCES catalog_inbounds(id),created_at TEXT NOT NULL,UNIQUE(instance_id,source_locator,tag,protocol));
CREATE TABLE inbound_client_keys(inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),client_ref TEXT NOT NULL,credential_id TEXT NOT NULL UNIQUE REFERENCES credential_material(id) DEFERRABLE INITIALLY DEFERRED,PRIMARY KEY(inbound_id,client_ref));
CREATE TABLE inbound_merge_audit(id TEXT PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),primary_id TEXT NOT NULL,secondary_id TEXT NOT NULL,backup_path TEXT NOT NULL,summary_json TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE inbound_retirements(inbound_id TEXT PRIMARY KEY REFERENCES catalog_inbounds(id),canonical_id TEXT REFERENCES catalog_inbounds(id),created_at TEXT NOT NULL);
CREATE TABLE subscriber_usage_sources(subscriber_id TEXT NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,instance_id INTEGER NOT NULL REFERENCES instances(id),inbound_id TEXT NOT NULL DEFAULT '',client_ref TEXT NOT NULL,baseline_upload INTEGER NOT NULL DEFAULT 0,baseline_download INTEGER NOT NULL DEFAULT 0,baseline_known INTEGER NOT NULL DEFAULT 0,active INTEGER NOT NULL DEFAULT 1,frozen_upload INTEGER,frozen_download INTEGER,frozen_state TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,PRIMARY KEY(subscriber_id,instance_id,inbound_id,client_ref));
CREATE TABLE subscriber_quota_audit(id INTEGER PRIMARY KEY,subscriber_id TEXT NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,action TEXT NOT NULL,before_json TEXT NOT NULL,after_json TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE inbound_endpoint_bindings(
 id TEXT PRIMARY KEY,
 inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),
 endpoint_id TEXT NOT NULL REFERENCES node_endpoints(id),
 enabled INTEGER NOT NULL DEFAULT 1,
 sort_order INTEGER NOT NULL DEFAULT 0,
 overrides_json TEXT NOT NULL DEFAULT '{}',
 validation_status TEXT NOT NULL DEFAULT 'ready',
 validated_config_hash TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 UNIQUE(inbound_id,endpoint_id)
);
CREATE TABLE endpoint_binding_audit(id INTEGER PRIMARY KEY,binding_id TEXT REFERENCES inbound_endpoint_bindings(id),action TEXT NOT NULL,summary_json TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE endpoint_connection_requests(request_id TEXT PRIMARY KEY,request_hash TEXT NOT NULL,binding_id TEXT NOT NULL REFERENCES inbound_endpoint_bindings(id));
CREATE INDEX samples_time ON traffic_samples(collected_at);
CREATE INDEX samples_identity_time ON traffic_samples(identity_id,collected_at);
CREATE INDEX daily_instance_date ON traffic_daily(instance_id,date);
CREATE INDEX samples_batch ON traffic_samples(instance_id,batch_id);
CREATE INDEX xray_diagnostics_instance_time ON xray_diagnostics(instance_id,id);
CREATE INDEX core_diagnostics_instance_time ON core_diagnostics(instance_id,id);
CREATE INDEX counter_observation_retention ON traffic_counter_observations(collected_at);
CREATE INDEX credential_inbound ON credential_material(instance_id,inbound_tag,core_client_ref);
CREATE INDEX node_profiles_inbound ON node_profiles(inbound_id);
CREATE INDEX subscription_access_time ON subscription_access_logs(accessed_at);
CREATE UNIQUE INDEX node_binding_variant ON node_profiles(binding_id,variant_key);
CREATE TRIGGER node_binding_insert BEFORE INSERT ON node_profiles WHEN NEW.binding_id IS NOT NULL BEGIN
 SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM inbound_endpoint_bindings b WHERE b.id=NEW.binding_id AND b.inbound_id=NEW.inbound_id AND b.endpoint_id=NEW.endpoint_id) THEN RAISE(ABORT,'node binding ownership mismatch') END;
END;
CREATE TRIGGER node_binding_update BEFORE UPDATE OF binding_id,inbound_id,endpoint_id ON node_profiles WHEN NEW.binding_id IS NOT NULL BEGIN
 SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM inbound_endpoint_bindings b WHERE b.id=NEW.binding_id AND b.inbound_id=NEW.inbound_id AND b.endpoint_id=NEW.endpoint_id) THEN RAISE(ABORT,'node binding ownership mismatch') END;
END;
INSERT INTO settings(key,value) VALUES('baseline','tml-v2');
INSERT INTO settings(key,value) VALUES('subscription_catalog','{"base_url":"","default_format":"mihomo","update_interval":24,"sync_interval_seconds":300,"log_retention_days":30,"rate_per_minute":60,"default_sort":"explicit"}');
PRAGMA application_id=1414351922;
PRAGMA user_version=20000;
CREATE TABLE remote_inbound_registry(inbound_id TEXT PRIMARY KEY REFERENCES catalog_inbounds(id),source_type TEXT NOT NULL CHECK(source_type IN ('native_api','manual','agent','optional_config')),verification TEXT NOT NULL,manual_profile_json TEXT NOT NULL DEFAULT '{}',revision INTEGER NOT NULL DEFAULT 1,checked_at TEXT NOT NULL);
CREATE TABLE remote_inventory_runs(id INTEGER PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),status TEXT NOT NULL,summary TEXT NOT NULL,checked_at TEXT NOT NULL);
CREATE UNIQUE INDEX meter_single_owner ON subscriber_usage_sources(instance_id,inbound_id,client_ref) WHERE active=1;
CREATE UNIQUE INDEX inbound_tag_one_live ON catalog_inbounds(instance_id,tag) WHERE retired=0;
CREATE TABLE remote_runtime_users(inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),client_ref TEXT NOT NULL,desired_sealed TEXT NOT NULL,desired_enabled INTEGER NOT NULL,restore_authorized INTEGER NOT NULL DEFAULT 0,api_endpoint TEXT NOT NULL,core_tag TEXT NOT NULL,protocol TEXT NOT NULL,revision INTEGER NOT NULL DEFAULT 1,status TEXT NOT NULL,persistence TEXT NOT NULL DEFAULT 'runtime_only',checked_at TEXT NOT NULL,PRIMARY KEY(inbound_id,client_ref));
