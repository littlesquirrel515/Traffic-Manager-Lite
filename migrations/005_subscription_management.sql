CREATE TABLE subscriber_node_revocations (
 subscriber_id TEXT NOT NULL REFERENCES subscribers(id),
 node_id TEXT NOT NULL REFERENCES node_profiles(id),
 created_at TEXT NOT NULL,
 PRIMARY KEY(subscriber_id,node_id)
);
CREATE TABLE subscriber_node_edits (
 request_id TEXT PRIMARY KEY,
 request_hash TEXT NOT NULL,
 subscriber_id TEXT NOT NULL REFERENCES subscribers(id),
 addition_json TEXT NOT NULL,
 status TEXT NOT NULL,
 message TEXT NOT NULL,
 checked_at TEXT NOT NULL
);
CREATE TABLE subscription_link_secrets (
 subscription_id TEXT PRIMARY KEY REFERENCES catalog_subscriptions(id) ON DELETE CASCADE,
 token_hash TEXT NOT NULL UNIQUE,
 token_sealed TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE TABLE subscriber_unified_links (
 subscriber_id TEXT PRIMARY KEY REFERENCES subscribers(id),
 subscription_id TEXT NOT NULL UNIQUE REFERENCES catalog_subscriptions(id) ON DELETE CASCADE
);
CREATE TABLE subscription_admin_audit (
 id INTEGER PRIMARY KEY,
 subscription_id TEXT REFERENCES catalog_subscriptions(id) ON DELETE SET NULL,
 subscriber_id TEXT REFERENCES subscribers(id),
 action TEXT NOT NULL,
 created_at TEXT NOT NULL
);
