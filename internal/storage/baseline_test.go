package storage

import (
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestV2EmptyInstallReopenAndNoLegacyTables(t *testing.T) {
	p := filepath.Join(t.TempDir(), "new.db")
	s, e := Open(p, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"servers", "instances", "catalog_inbounds", "node_profiles", "subscribers", "traffic_samples", "subscriber_usage_sources"} {
		var n int
		if e = s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatalf("%s not empty: %d %v", table, n, e)
		}
	}
	var old int
	if e = s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('nodes','subscriptions','subscription_nodes','legacy_node_redirects','managed_user_state','reconciliation_plans','reconciliation_actions','reconciliation_instance_policy')").Scan(&old); e != nil || old != 0 {
		t.Fatal("legacy model remains", e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(p, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var migrations int
	if e = s.DB.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&migrations); e != nil || migrations != 3 {
		t.Fatal("baseline and additive migrations were not applied exactly once", migrations, e)
	}
}

func TestV2RejectsExistingDatabasesBeforeAnyWrite(t *testing.T) {
	for _, fixture := range []string{"CREATE TABLE unrelated(private TEXT); INSERT INTO unrelated VALUES('preserve')", "CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at TEXT);INSERT INTO schema_migrations VALUES('001_initial.sql','old');CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT);"} {
		p := filepath.Join(t.TempDir(), "old.db")
		db, e := sql.Open("sqlite", p)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(fixture); e != nil {
			t.Fatal(e)
		}
		db.Close()
		before, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		if s, e := Open(p, "Asia/Shanghai"); e == nil {
			s.Close()
			t.Fatal("old/foreign database accepted")
		} else if !strings.Contains(e.Error(), "clean database") {
			t.Fatal(e)
		}
		after, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		if sha256.Sum256(before) != sha256.Sum256(after) {
			t.Fatal("rejected database was modified")
		}
	}
}

func TestSubscriberMigrationUpgradesExistingV2WithoutReset(t *testing.T) {
	p := filepath.Join(t.TempDir(), "existing-v2.db")
	s, e := Open(p, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{`DROP TABLE subscriber_provisioning`, `DELETE FROM schema_migrations WHERE version='002_subscriber_provisioning.sql'`, `INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(42,'preserve','fixture','before','before')`} {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	var before string
	s.DB.QueryRow("SELECT value FROM settings WHERE key='baseline_schema_sha256'").Scan(&before)
	s.Close()
	upgraded, e := Open(p, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer upgraded.Close()
	var name, after string
	if e = upgraded.DB.QueryRow("SELECT name FROM servers WHERE id=42").Scan(&name); e != nil || name != "preserve" {
		t.Fatal("existing asset lost")
	}
	upgraded.DB.QueryRow("SELECT value FROM settings WHERE key='baseline_schema_sha256'").Scan(&after)
	if before != after {
		t.Fatal("initial schema evidence changed")
	}
	var n int
	if e = upgraded.DB.QueryRow("SELECT count(*) FROM subscriber_provisioning").Scan(&n); e != nil || n != 0 {
		t.Fatal("additive migration failed")
	}
}

func TestHTTPAuthMigrationPreservesV2TokensQuotaAndTraffic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	s, e := Open(path, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	statements := []string{
		`DROP TABLE hysteria_auth_accounts`, `DROP TABLE hysteria_auth_backends`, `DELETE FROM schema_migrations WHERE version='003_hysteria_http_auth.sql'`,
		`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(7,'preserve','fixture','before','before')`,
		`INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(7,7,'preserve','hysteria2','http://127.0.0.1:1','before','before')`,
		`INSERT INTO users(id,display_name,created_at,updated_at) VALUES(7,'preserve','before','before')`,
		`INSERT INTO identities(id,user_id,instance_id,inbound_tag,core_user_key,scope) VALUES(7,7,7,'','preserve','user')`,
		`INSERT INTO traffic_cursors(identity_id,raw_upload,raw_download,epoch_id,updated_at) VALUES(7,123,456,'preserve','before')`,
		`INSERT INTO traffic_daily VALUES('2026-01-01',7,7,12,34)`,
		`INSERT INTO subscribers(id,name,enabled,traffic_limit_bytes,quota_revision,created_at,updated_at) VALUES('owner','preserve',1,777,9,'before','before')`,
		`INSERT INTO catalog_subscriptions(id,subscriber_id,name,token_hash,format,created_at,updated_at) VALUES('sub','owner','preserve','unchanged-fixture-token-hash','mihomo','before','before')`,
	}
	for _, q := range statements {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	s.Close()
	s, e = Open(path, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var token string
	if e = s.DB.QueryRow("SELECT token_hash FROM catalog_subscriptions WHERE id='sub'").Scan(&token); e != nil || token != "unchanged-fixture-token-hash" {
		t.Fatal("existing token changed")
	}
	var a, b int
	if e = s.DB.QueryRow("SELECT traffic_limit_bytes,quota_revision FROM subscribers WHERE id='owner'").Scan(&a, &b); e != nil || a != 777 || b != 9 {
		t.Fatal("existing quota changed")
	}
	if e = s.DB.QueryRow("SELECT raw_upload,raw_download FROM traffic_cursors WHERE identity_id=7").Scan(&a, &b); e != nil || a != 123 || b != 456 {
		t.Fatal("counter baseline changed")
	}
	if e = s.DB.QueryRow("SELECT upload_bytes,download_bytes FROM traffic_daily WHERE identity_id=7").Scan(&a, &b); e != nil || a != 12 || b != 34 {
		t.Fatal("historical ledger changed")
	}
	if e = s.DB.QueryRow("SELECT count(*) FROM hysteria_auth_backends").Scan(&a); e != nil || a != 0 {
		t.Fatal("migration enabled a backend without authorization")
	}
}

func TestV1FullDatabaseRejectionPreservesTokensAndTraffic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v1.db")
	db, e := sql.Open("sqlite", p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at TEXT NOT NULL)"); e != nil {
		t.Fatal(e)
	}
	files, e := filepath.Glob("../../docs/history/v1-migrations/*.sql")
	if e != nil || len(files) != 17 {
		t.Fatal("historical fixture incomplete", len(files), e)
	}
	sort.Strings(files)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, e = db.Exec(string(b)); e != nil {
			t.Fatal(f, e)
		}
		if _, e = db.Exec("INSERT INTO schema_migrations VALUES(?,'before-v2')", filepath.Base(f)); e != nil {
			t.Fatal(e)
		}
	}
	for _, statement := range []string{
		"INSERT INTO settings VALUES('aggregation_timezone','Asia/Shanghai')",
		"INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'preserve','fixture','before','before')",
		"INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'preserve','xray','127.0.0.1:1','before','before')",
		"INSERT INTO users(id,display_name,created_at,updated_at) VALUES(1,'preserve','before','before')",
		"INSERT INTO identities(id,user_id,instance_id,core_user_key,scope) VALUES(1,1,1,'preserve','user')",
		"INSERT INTO traffic_daily VALUES('2026-10-01',1,1,123,456)",
		"INSERT INTO subscriptions(id,name,format,token_hash,user_id,enabled,created_at,updated_at,selection_scope) VALUES(1,'preserve','mihomo','fixture-hash-only',1,1,'before','before','user')",
	} {
		if _, e = db.Exec(statement); e != nil {
			t.Fatal(e)
		}
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if s, e := Open(p, "Asia/Shanghai"); e == nil {
		s.Close()
		t.Fatal("v1 accepted")
	}
	after, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("rejection changed v1 contents")
	}
}

func TestV2RejectsMismatchedBaselineFingerprintWithoutWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "wrong-schema.db")
	s, e := Open(p, "UTC")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("UPDATE settings SET value='wrong-build' WHERE key='baseline_schema_sha256'"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	before, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if s, e := Open(p, "UTC"); e == nil {
		s.Close()
		t.Fatal("mismatched v2 schema accepted")
	}
	after, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("mismatched baseline modified")
	}
}
