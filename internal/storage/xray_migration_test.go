package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
	"traffic-manager-lite/migrations"
)

func TestXrayMigrationPreservesLegacyData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	db.Exec("CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at TEXT NOT NULL)")
	files, _ := migrations.Files.ReadDir(".")
	for _, f := range files {
		if f.Name() >= "005" {
			continue
		}
		b, _ := migrations.Files.ReadFile(f.Name())
		if _, e = db.Exec(string(b)); e != nil {
			t.Fatal(e)
		}
		db.Exec("INSERT INTO schema_migrations VALUES(?,?)", f.Name(), Stamp(time.Now()))
	}
	db.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'legacy','localhost','old','old')")
	db.Close()
	store, e := Open(path, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	var name string
	if e := store.DB.QueryRow("SELECT name FROM servers WHERE id=1").Scan(&name); e != nil || name != "legacy" {
		t.Fatal("legacy data lost", e)
	}
	var n int
	if e := store.DB.QueryRow("SELECT count(*) FROM xray_clients").Scan(&n); e != nil || n != 0 {
		t.Fatal("new inventory initialized incorrectly", e)
	}
}
