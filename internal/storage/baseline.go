package storage

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"traffic-manager-lite/migrations"
)

const ApplicationID = 1414351922
const BaselineVersion = 20000

func baselineDigest() (string, error) {
	b, e := migrations.Files.ReadFile("001_v2_initial.sql")
	if e != nil {
		return "", e
	}
	canonical := strings.TrimPrefix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\ufeff")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(canonical))), nil
}

func validateBaseline(db *sql.DB) error {
	var tables, app, version int
	if e := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&tables); e != nil {
		return e
	}
	if e := db.QueryRow("PRAGMA application_id").Scan(&app); e != nil {
		return e
	}
	if e := db.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		return e
	}
	if tables == 0 && app == 0 && version == 0 {
		return nil
	}
	var marker string
	if app != ApplicationID || version != BaselineVersion {
		return fmt.Errorf("TML v2 requires a clean database (found application_id=%d, version=%d); existing data was not upgraded or reset; use the offline clean-baseline procedure", app, version)
	}
	if e := db.QueryRow("SELECT value FROM settings WHERE key='baseline'").Scan(&marker); e != nil || marker != "tml-v2" {
		return fmt.Errorf("invalid TML v2 database ownership marker; refusing to write")
	}
	expected, e := baselineDigest()
	if e != nil {
		return e
	}
	var actual string
	if e = db.QueryRow("SELECT value FROM settings WHERE key='baseline_schema_sha256'").Scan(&actual); e != nil || actual != expected {
		return fmt.Errorf("TML v2 baseline schema fingerprint mismatch; refusing to upgrade or write this database")
	}
	return nil
}
