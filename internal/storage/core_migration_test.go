package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
	"traffic-manager-lite/migrations"
)

func TestCoreMigrationPreservesV13InventoryAndHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v13.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	db.Exec("CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at TEXT NOT NULL)")
	files, _ := migrations.Files.ReadDir(".")
	for _, f := range files {
		if f.Name() >= "007" {
			continue
		}
		b, _ := migrations.Files.ReadFile(f.Name())
		if _, e = db.Exec(string(b)); e != nil {
			t.Fatal(e)
		}
		db.Exec("INSERT INTO schema_migrations VALUES(?,?)", f.Name(), Stamp(time.Now()))
	}
	db.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'old','localhost','old','old')")
	db.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'old','xray','localhost:10085','old','old')")
	db.Exec("INSERT INTO xray_clients(instance_id,inbound_tag,asset_key,email,protocol,source,observed_at) VALUES(1,'in','email:alice','alice','vless','runtime_api','old')")
	db.Exec("INSERT INTO xray_diagnostics(instance_id,checked_at,trigger,report_json) VALUES(1,'old','manual','{}')")
	db.Close()
	store, e := Open(path, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	var n int
	for _, table := range []string{"xray_clients", "xray_diagnostics"} {
		if e := store.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 1 {
			t.Fatal("lost v1.3 records", table, e)
		}
	}
	var endpoint, secret string
	if e = store.DB.QueryRow("SELECT clash_endpoint,clash_secret FROM instances WHERE id=1").Scan(&endpoint, &secret); e != nil || endpoint != "" || secret != "" {
		t.Fatal("new optional fields invalid", e)
	}
}
