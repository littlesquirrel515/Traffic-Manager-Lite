package storage

import (
	"context"
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"
	"traffic-manager-lite/migrations"
)

type Store struct {
	DB       *sql.DB
	Location *time.Location
}

func Stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }
func Open(path, timezone string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Store, error) { db.Close(); return nil, e }
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA synchronous=FULL", "CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)"} {
		if _, err = db.Exec(q); err != nil {
			return fail(err)
		}
	}
	files, _ := migrations.Files.ReadDir(".")
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for _, f := range files {
		var n int
		if err = db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version=?", f.Name()).Scan(&n); err != nil {
			return fail(err)
		}
		if n > 0 {
			continue
		}
		b, e := migrations.Files.ReadFile(f.Name())
		if e != nil {
			return fail(e)
		}
		tx, e := db.Begin()
		if e != nil {
			return fail(e)
		}
		if _, e = tx.Exec(string(b)); e == nil {
			_, e = tx.Exec("INSERT INTO schema_migrations VALUES(?,?)", f.Name(), Stamp(time.Now()))
		}
		if e != nil {
			tx.Rollback()
			return fail(e)
		}
		if e = tx.Commit(); e != nil {
			return fail(e)
		}
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return fail(err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		return fail(err)
	}
	_, err = db.Exec("INSERT INTO settings(key,value) VALUES('aggregation_timezone',?) ON CONFLICT DO NOTHING", timezone)
	if err != nil {
		return fail(err)
	}
	var stored string
	if err = db.QueryRow("SELECT value FROM settings WHERE key='aggregation_timezone'").Scan(&stored); err != nil {
		return fail(err)
	}
	if stored != timezone {
		return fail(fmt.Errorf("aggregation timezone is fixed at %s; rebuild aggregates before changing it", stored))
	}
	return &Store{DB: db, Location: loc}, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) Rows(ctx context.Context, q string, args ...any) ([]map[string]any, error) {
	rows, e := s.DB.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	cols, e := rows.Columns()
	if e != nil {
		return nil, e
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range vals {
			ptr[i] = &vals[i]
		}
		if e = rows.Scan(ptr...); e != nil {
			return nil, e
		}
		m := map[string]any{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				m[c] = string(b)
			} else {
				m[c] = vals[i]
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// OpenReadOnly is used by audits: it never applies migrations or alters pragmas.
func OpenReadOnly(path, zone string) (*Store, error) {
	absolute, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	uri := "file:" + (&url.URL{Path: filepath.ToSlash(absolute)}).EscapedPath() + "?mode=ro"
	db, e := sql.Open("sqlite", uri)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	loc, e := time.LoadLocation(zone)
	if e != nil {
		db.Close()
		return nil, e
	}
	var stored string
	if e = db.QueryRow("SELECT value FROM settings WHERE key='aggregation_timezone'").Scan(&stored); e != nil || stored != zone {
		db.Close()
		return nil, fmt.Errorf("use the stored aggregation timezone")
	}
	return &Store{DB: db, Location: loc}, nil
}
