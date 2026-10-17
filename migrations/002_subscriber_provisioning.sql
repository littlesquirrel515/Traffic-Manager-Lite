CREATE TABLE subscriber_provisioning (
 request_id TEXT PRIMARY KEY,
 request_hash TEXT NOT NULL,
 subscriber_id TEXT NOT NULL REFERENCES subscribers(id),
 inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),
 client_ref TEXT NOT NULL,
 target_endpoint TEXT NOT NULL,
 target_tag TEXT NOT NULL,
 target_protocol TEXT NOT NULL,
 origin TEXT NOT NULL CHECK(origin IN ('managed','native')),
 credential_id TEXT REFERENCES credential_material(id),
 desired_sealed TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL,
 message TEXT NOT NULL DEFAULT '',
 requested_enabled INTEGER NOT NULL DEFAULT 1,
 checked_at TEXT NOT NULL,
 UNIQUE(subscriber_id,inbound_id)
);
