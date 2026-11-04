package storage

import (
	"path/filepath"
	"testing"
)

func TestSubscriptionManagementMigrationPreservesLegacyLinksAndQuotas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v21.db")
	s, e := Open(path, "UTC")
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{
		"DROP TABLE subscription_admin_audit", "DROP TABLE subscriber_unified_links", "DROP TABLE subscription_link_secrets", "DROP TABLE subscriber_node_edits", "DROP TABLE subscriber_node_revocations",
		"DELETE FROM schema_migrations WHERE version='005_subscription_management.sql'",
		`INSERT INTO subscribers(id,name,enabled,created_at,updated_at,traffic_limit_bytes,quota_revision,node_authorization_required) VALUES('old','Keep',1,'before','before',987654321,8,0)`,
		`INSERT INTO catalog_subscriptions(id,subscriber_id,name,token_hash,token_version,enabled,format,created_at,updated_at) VALUES('old-url','old','Old URL','old-sha256-preserved',9,1,'mihomo','before','before')`,
	} {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	s.Close()
	s, e = Open(path, "UTC")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var hash, format string
	var version int
	var quota int64
	e = s.DB.QueryRow(`SELECT c.token_hash,c.token_version,c.format,s.traffic_limit_bytes FROM catalog_subscriptions c JOIN subscribers s ON s.id=c.subscriber_id WHERE c.id='old-url'`).Scan(&hash, &version, &format, &quota)
	if e != nil || hash != "old-sha256-preserved" || version != 9 || format != "mihomo" || quota != 987654321 {
		t.Fatal("upgrade rewrote legacy resources", e)
	}
	for _, table := range []string{"subscription_link_secrets", "subscriber_unified_links", "subscriber_node_revocations", "subscriber_node_edits"} {
		var n int
		if e = s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatal("migration manufactured links or changed permissions", table, e)
		}
	}
}
