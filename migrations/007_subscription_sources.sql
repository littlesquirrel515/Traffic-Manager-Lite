ALTER TABLE subscription_access_logs ADD COLUMN client_ip TEXT NOT NULL DEFAULT '';
ALTER TABLE subscription_access_logs ADD COLUMN peer_ip TEXT NOT NULL DEFAULT '';
ALTER TABLE subscription_access_logs ADD COLUMN ip_source TEXT NOT NULL DEFAULT 'legacy_hash_only';
ALTER TABLE subscriber_credentials ADD COLUMN usage_mode TEXT NOT NULL DEFAULT 'exclusive' CHECK(usage_mode IN ('exclusive','shared_native'));
