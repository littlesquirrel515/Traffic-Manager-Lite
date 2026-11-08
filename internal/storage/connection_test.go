package storage

import (
	"path/filepath"
	"testing"
)

func TestConnectionSettingsSurvivePoolReconnect(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "database with spaces.db"), "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.DB.Exec("CREATE TABLE reconnect_parent(id INTEGER PRIMARY KEY)"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("CREATE TABLE reconnect_child(parent_id INTEGER REFERENCES reconnect_parent(id))"); e != nil {
		t.Fatal(e)
	}
	// Simulate replacement of the original connection after an IO/driver failure.
	s.DB.SetMaxIdleConns(0)
	for i := 0; i < 3; i++ {
		var enabled, timeout int
		if e = s.DB.QueryRow("PRAGMA foreign_keys").Scan(&enabled); e != nil || enabled != 1 {
			t.Fatalf("foreign keys lost on reconnect: %d %v", enabled, e)
		}
		if e = s.DB.QueryRow("PRAGMA busy_timeout").Scan(&timeout); e != nil || timeout != 5000 {
			t.Fatal(timeout, e)
		}
		if _, e = s.DB.Exec("INSERT INTO reconnect_child VALUES(999)"); e == nil {
			t.Fatal("orphan insert accepted after reconnect")
		}
	}
}
