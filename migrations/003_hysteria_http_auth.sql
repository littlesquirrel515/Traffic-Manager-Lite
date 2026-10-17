CREATE TABLE hysteria_auth_backends (
 inbound_id TEXT PRIMARY KEY REFERENCES catalog_inbounds(id),
 instance_id INTEGER NOT NULL UNIQUE REFERENCES instances(id),
 token_hash TEXT NOT NULL UNIQUE,
 allowed_cidrs TEXT NOT NULL,
 target_endpoint TEXT NOT NULL,
 target_tag TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 1,
 confirmed INTEGER NOT NULL DEFAULT 0,
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL,
 configured_at TEXT,
 last_callback_at TEXT,
 last_success_at TEXT
);
CREATE TABLE hysteria_auth_accounts (
 inbound_id TEXT NOT NULL REFERENCES hysteria_auth_backends(inbound_id),
 credential_id TEXT NOT NULL UNIQUE REFERENCES credential_material(id),
 auth_hash TEXT NOT NULL,
 origin TEXT NOT NULL CHECK(origin IN ('managed','native')),
 match_mode TEXT NOT NULL DEFAULT 'exact' CHECK(match_mode IN ('exact','userpass')),
 last_success_at TEXT,
 PRIMARY KEY(inbound_id,auth_hash)
);
