package storage

import (
	"path/filepath"
	"testing"
)

func TestNodeWorkflowMigrationKeepsLegacyUserQuotaAndToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v20.db")
	s, e := Open(path, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	// Reconstruct the previous v2 schema, then exercise the real startup upgrade.
	for _, q := range []string{
		"DROP TABLE subscriber_node_tasks", "DROP TABLE subscriber_node_requests", "DROP TABLE subscriber_node_grants",
		"ALTER TABLE subscribers DROP COLUMN node_authorization_required",
		"DELETE FROM schema_migrations WHERE version='004_subscriber_node_workflow.sql'",
		`INSERT INTO subscribers(id,name,enabled,created_at,updated_at,traffic_limit_bytes,quota_revision) VALUES('legacy','Preserved',1,'before','before',123456789,7)`,
		`INSERT INTO catalog_subscriptions(id,subscriber_id,name,token_hash,enabled,format,created_at,updated_at) VALUES('link','legacy','Existing link','preserve-token-hash',1,'mihomo','before','before')`,
	} {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(q, e)
		}
	}
	s.Close()
	s, e = Open(path, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var name, token string
	var enabled, required bool
	var quota, revision int64
	e = s.DB.QueryRow(`SELECT s.name,s.enabled,s.node_authorization_required,s.traffic_limit_bytes,s.quota_revision,c.token_hash FROM subscribers s JOIN catalog_subscriptions c ON c.subscriber_id=s.id WHERE s.id='legacy'`).Scan(&name, &enabled, &required, &quota, &revision, &token)
	if e != nil || name != "Preserved" || !enabled || required || quota != 123456789 || revision != 7 || token != "preserve-token-hash" {
		t.Fatal("legacy user changed", name, enabled, required, quota, revision, token, e)
	}
	var n int
	if e = s.DB.QueryRow("SELECT count(*) FROM subscriber_node_grants").Scan(&n); e != nil || n != 0 {
		t.Fatal("upgrade fabricated authorization", n, e)
	}
}
