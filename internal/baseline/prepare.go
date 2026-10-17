// Package baseline prepares an offline, reversible TML-only reset. It has no
// core/network dependencies and deliberately cannot replace the source database.
package baseline

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/migrations"
)

type Report struct {
	Source          string           `json:"source"`
	Backup          string           `json:"backup"`
	Candidate       string           `json:"candidate"`
	BackupSHA256    string           `json:"backup_sha256"`
	CandidateSHA256 string           `json:"candidate_sha256"`
	Ownership       string           `json:"ownership"`
	Counts          map[string]int64 `json:"affected_table_counts"`
	CreatedAt       string           `json:"created_at"`
	ResetExecuted   bool             `json:"reset_executed"`
}

func openReadOnly(path string) (*sql.DB, error) {
	uri := "file:" + (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath() + "?mode=ro"
	db, e := sql.Open("sqlite", uri)
	if e == nil {
		db.SetMaxOpenConns(1)
	}
	return db, e
}

func inspect(ctx context.Context, db *sql.DB) (string, map[string]int64, error) {
	allowed := map[string]bool{"schema_migrations": true, "managed_user_state": true, "reconciliation_plans": true, "reconciliation_actions": true, "reconciliation_instance_policy": true, "nodes": true, "subscriptions": true, "subscription_nodes": true, "legacy_node_redirects": true}
	files, e := migrations.Files.ReadDir(".")
	if e != nil {
		return "", nil, e
	}
	for _, file := range files {
		b, err := migrations.Files.ReadFile(file.Name())
		if err != nil {
			return "", nil, err
		}
		for _, m := range regexp.MustCompile(`(?i)CREATE TABLE\s+(?:"|\[)?([a-z0-9_]+)`).FindAllStringSubmatch(string(b), -1) {
			allowed[m[1]] = true
		}
	}
	rows, e := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if e != nil {
		return "", nil, e
	}
	tables := []string{}
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			rows.Close()
			return "", nil, e
		}
		tables = append(tables, name)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return "", nil, e
	}
	seen := map[string]bool{}
	for _, t := range tables {
		if !allowed[t] {
			return "", nil, fmt.Errorf("ownership unclear: unrecognized table %q; preparation refused", t)
		}
		seen[t] = true
	}
	for _, name := range []string{"servers", "instances", "settings", "schema_migrations", "traffic_daily", "identities"} {
		if !seen[name] {
			return "", nil, fmt.Errorf("not an exclusively owned TML database: missing %s", name)
		}
	}
	var app, version int
	if e = db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); e != nil {
		return "", nil, e
	}
	if e = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); e != nil {
		return "", nil, e
	}
	owner := "tml-v1"
	if app == storage.ApplicationID && version == storage.BaselineVersion {
		var marker string
		if e = db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='baseline'").Scan(&marker); e != nil || marker != "tml-v2" {
			return "", nil, fmt.Errorf("invalid v2 ownership marker")
		}
		owner = "tml-v2"
	} else if app != 0 || version != 0 {
		return "", nil, fmt.Errorf("unknown database application/version")
	} else {
		var initial int
		if e = db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='001_initial.sql'").Scan(&initial); e != nil || initial != 1 {
			return "", nil, fmt.Errorf("legacy TML migration ownership evidence missing")
		}
		var timezone string
		if e = db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='aggregation_timezone'").Scan(&timezone); e != nil {
			return "", nil, fmt.Errorf("TML timezone evidence missing")
		}
		if _, e = time.LoadLocation(timezone); e != nil {
			return "", nil, fmt.Errorf("invalid TML timezone evidence")
		}
	}
	counts := map[string]int64{}
	for _, t := range tables {
		var n int64
		if e = db.QueryRowContext(ctx, `SELECT count(*) FROM "`+strings.ReplaceAll(t, `"`, `""`)+`"`).Scan(&n); e != nil {
			return "", nil, e
		}
		counts[t] = n
	}
	return owner, counts, nil
}

func fileHash(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func Prepare(ctx context.Context, source, destination, timezone string) (Report, error) {
	var out Report
	abs, e := filepath.Abs(source)
	if e != nil {
		return out, e
	}
	info, e := os.Lstat(abs)
	if e != nil {
		return out, e
	}
	if !info.Mode().IsRegular() {
		return out, fmt.Errorf("source must be a regular database file, not a symlink")
	}
	db, e := openReadOnly(abs)
	if e != nil {
		return out, e
	}
	defer db.Close()
	if _, _, e = inspect(ctx, db); e != nil {
		return out, e
	}
	dir, e := filepath.Abs(destination)
	if e != nil {
		return out, e
	}
	// Exclusive creation prevents overwriting any previous recovery material.
	if e = os.Mkdir(dir, 0700); e != nil {
		return out, e
	}
	out = Report{Source: abs, Backup: filepath.Join(dir, "before.db"), Candidate: filepath.Join(dir, "v2-empty.db"), CreatedAt: storage.Stamp(time.Now()), ResetExecuted: false}
	if _, e = db.ExecContext(ctx, "VACUUM INTO ?", out.Backup); e != nil {
		return out, e
	}
	if e = os.Chmod(out.Backup, 0600); e != nil {
		return out, e
	}
	copyDB, e := openReadOnly(out.Backup)
	if e != nil {
		return out, e
	}
	defer copyDB.Close()
	var integrity string
	if e = copyDB.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); e != nil || integrity != "ok" {
		return out, fmt.Errorf("backup integrity check failed")
	}
	out.Ownership, out.Counts, e = inspect(ctx, copyDB)
	if e != nil {
		return out, e
	}
	out.BackupSHA256, e = fileHash(out.Backup)
	if e != nil {
		return out, e
	}
	candidate, e := storage.Open(out.Candidate, timezone)
	if e != nil {
		return out, e
	}
	if _, e = candidate.DB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); e != nil {
		candidate.Close()
		return out, e
	}
	if e = candidate.Close(); e != nil {
		return out, e
	}
	out.CandidateSHA256, e = fileHash(out.Candidate)
	if e != nil {
		return out, e
	}
	raw, e := json.MarshalIndent(out, "", "  ")
	if e != nil {
		return out, e
	}
	f, e := os.OpenFile(filepath.Join(dir, "manifest.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return out, e
	}
	if _, e = f.Write(raw); e != nil {
		f.Close()
		return out, e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return out, e
	}
	return out, f.Close()
}
