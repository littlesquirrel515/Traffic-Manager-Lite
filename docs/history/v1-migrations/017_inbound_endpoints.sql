-- Keep every existing node, override, authorization, group and subscription ID.
ALTER TABLE catalog_inbounds ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE catalog_inbounds ADD COLUMN listen_ports_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE catalog_inbounds ADD COLUMN retired INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_endpoints ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE node_endpoints ADD COLUMN endpoint_type TEXT NOT NULL DEFAULT 'custom';
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
ALTER TABLE node_profiles ADD COLUMN binding_id TEXT REFERENCES inbound_endpoint_bindings(id);
ALTER TABLE node_profiles ADD COLUMN variant_key TEXT NOT NULL DEFAULT 'default';
CREATE UNIQUE INDEX node_binding_variant ON node_profiles(binding_id,variant_key);
INSERT INTO node_endpoints(id,name,profile_json,enabled,sort_order,created_at,updated_at)
 SELECT 'endpoint_'||n.id,n.name,'{}',1,n.sort_order,n.created_at,n.updated_at
 FROM node_profiles n WHERE n.inbound_id IS NOT NULL AND n.endpoint_id IS NULL;
UPDATE node_profiles SET endpoint_id='endpoint_'||id WHERE inbound_id IS NOT NULL AND endpoint_id IS NULL;
INSERT INTO inbound_endpoint_bindings(id,inbound_id,endpoint_id,enabled,sort_order,validated_config_hash,created_at,updated_at)
 SELECT 'binding_'||MIN(n.id),n.inbound_id,n.endpoint_id,1,MIN(n.sort_order),i.config_hash,MIN(n.created_at),MAX(n.updated_at)
 FROM node_profiles n JOIN catalog_inbounds i ON i.id=n.inbound_id WHERE n.endpoint_id IS NOT NULL
 GROUP BY n.inbound_id,n.endpoint_id;
UPDATE node_profiles SET binding_id=(SELECT b.id FROM inbound_endpoint_bindings b WHERE b.inbound_id=node_profiles.inbound_id AND b.endpoint_id=node_profiles.endpoint_id),variant_key='legacy:'||id WHERE inbound_id IS NOT NULL;
UPDATE catalog_inbounds SET listen_ports_json=json_array(listen_port) WHERE listen_port>0;
CREATE TABLE endpoint_binding_audit(id INTEGER PRIMARY KEY,binding_id TEXT REFERENCES inbound_endpoint_bindings(id),action TEXT NOT NULL,summary_json TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE endpoint_connection_requests(request_id TEXT PRIMARY KEY,request_hash TEXT NOT NULL,binding_id TEXT NOT NULL REFERENCES inbound_endpoint_bindings(id));
CREATE TRIGGER node_binding_insert BEFORE INSERT ON node_profiles WHEN NEW.binding_id IS NOT NULL BEGIN
 SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM inbound_endpoint_bindings b WHERE b.id=NEW.binding_id AND b.inbound_id=NEW.inbound_id AND b.endpoint_id=NEW.endpoint_id) THEN RAISE(ABORT,'node binding ownership mismatch') END;
END;
CREATE TRIGGER node_binding_update BEFORE UPDATE OF binding_id,inbound_id,endpoint_id ON node_profiles WHEN NEW.binding_id IS NOT NULL BEGIN
 SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM inbound_endpoint_bindings b WHERE b.id=NEW.binding_id AND b.inbound_id=NEW.inbound_id AND b.endpoint_id=NEW.endpoint_id) THEN RAISE(ABORT,'node binding ownership mismatch') END;
END;
