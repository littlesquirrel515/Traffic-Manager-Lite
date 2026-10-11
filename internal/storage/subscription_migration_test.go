package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"traffic-manager-lite/migrations"
)

func TestSubscriptionBundleMigrationPreservesLegacyTokensAndNodeOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec("PRAGMA foreign_keys=ON")
	exec("CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at TEXT NOT NULL)")
	files, _ := migrations.Files.ReadDir(".")
	for _, f := range files {
		if f.Name() >= "013" {
			continue
		}
		b, e := migrations.Files.ReadFile(f.Name())
		if e != nil {
			t.Fatal(e)
		}
		exec(string(b))
		exec("INSERT INTO schema_migrations VALUES(?,'old')", f.Name())
	}
	exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'old','localhost','old','old')")
	exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'old','xray','localhost:10085','old','old')")
	exec("INSERT INTO users(id,display_name,created_at,updated_at) VALUES(1,'old','old','old')")
	for _, id := range []string{"a", "b"} {
		exec("INSERT INTO nodes(id,instance_id,inbound_tag,user_id,protocol,discovered_json,effective_json,source_hash,updated_at) VALUES(?,1,'in',1,'vless','{}','{}','hash','old')", id)
	}
	exec("INSERT INTO subscriptions VALUES(7,'old','singbox','original-token-hash',1,1,'old','old')")
	exec("INSERT INTO subscription_nodes VALUES(7,'b',0),(7,'a',1)")
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	s, e := Open(path, "UTC")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var hash, scope string
	var user int
	if e = s.DB.QueryRow("SELECT token_hash,selection_scope,user_id FROM subscriptions WHERE id=7").Scan(&hash, &scope, &user); e != nil || hash != "original-token-hash" || scope != "user" || user != 1 {
		t.Fatal("legacy scope/token changed", hash, scope, user, e)
	}
	var ids string
	if e = s.DB.QueryRow("SELECT group_concat(node_id,',') FROM (SELECT node_id FROM subscription_nodes WHERE subscription_id=7 ORDER BY position)").Scan(&ids); e != nil || ids != "b,a" {
		t.Fatal("node set/order lost", ids, e)
	}
	rows, e := s.DB.Query("PRAGMA foreign_key_check")
	if e != nil {
		t.Fatal(e)
	}
	if rows.Next() {
		t.Fatal("migration broke foreign keys")
	}
	rows.Close()
	if _, e = s.DB.Exec("DELETE FROM subscriptions WHERE id=7"); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = s.DB.QueryRow("SELECT count(*) FROM subscription_nodes").Scan(&count); e != nil || count != 0 {
		t.Fatal("cascade lost", count, e)
	}
}
