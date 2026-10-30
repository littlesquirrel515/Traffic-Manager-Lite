-- v2.1 adds explicit node authorization without reinterpreting legacy grants.
ALTER TABLE subscribers ADD COLUMN node_authorization_required INTEGER NOT NULL DEFAULT 0;
CREATE TABLE subscriber_node_grants (
 subscriber_id TEXT NOT NULL REFERENCES subscribers(id),
 node_id TEXT NOT NULL REFERENCES node_profiles(id),
 inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),
 created_at TEXT NOT NULL,
 PRIMARY KEY(subscriber_id,node_id)
);
CREATE TABLE subscriber_node_requests (
 request_id TEXT PRIMARY KEY,
 request_hash TEXT NOT NULL,
 subscriber_id TEXT NOT NULL REFERENCES subscribers(id),
 plan_json TEXT NOT NULL,
 requested_enabled INTEGER NOT NULL,
 activation_revision INTEGER NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending',
 message TEXT NOT NULL DEFAULT '',
 checked_at TEXT NOT NULL
);
CREATE TABLE subscriber_node_tasks (
 request_id TEXT NOT NULL REFERENCES subscriber_node_requests(request_id),
 child_request_id TEXT NOT NULL UNIQUE,
 inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),
 status TEXT NOT NULL DEFAULT 'pending',
 message TEXT NOT NULL DEFAULT '',
 checked_at TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(request_id,inbound_id)
);
