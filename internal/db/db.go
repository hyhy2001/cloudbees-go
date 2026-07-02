// Package db manages the SQLite connection and schema for bee's local state.
package db

import (
	"database/sql"
	_ "embed"
	"os"
	"os/user"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSql string

var (
	mu   sync.Mutex
	pool = map[string]*sql.DB{}
)

// beeRoot returns the directory bee's data lives under: BEE_DIR if set and
// it exists, else the directory containing the running binary — matching
// the TS build's detectBeeRoot() (data sits next to the binary, not in
// ~/.local/share, so a portable/USB install carries its own DB with it).
func beeRoot() string {
	if d := os.Getenv("BEE_DIR"); d != "" {
		if _, err := os.Stat(d); err == nil {
			return d
		}
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	home, _ := os.UserHomeDir()
	return home
}

// DefaultPath returns <bee_root>/data/<username>/cb.db, matching the TS
// binary's layout — a DB file per OS user, next to wherever bee lives.
func DefaultPath() string {
	username := "default"
	if u, err := user.Current(); err == nil && u.Username != "" {
		username = u.Username
	}
	dir := filepath.Join(beeRoot(), "data", username)
	os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "cb.db")
}

// Open returns (or lazily creates) a pooled *sql.DB for the given path.
// In-memory paths (":memory:") are never pooled.
func Open(path string) (*sql.DB, error) {
	if path == "" {
		path = DefaultPath()
	}
	if path != ":memory:" {
		mu.Lock()
		if db, ok := pool[path]; ok {
			mu.Unlock()
			return db, nil
		}
		mu.Unlock()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer

	if _, err := db.Exec("PRAGMA journal_mode = WAL; PRAGMA synchronous = NORMAL; PRAGMA foreign_keys = ON;"); err != nil {
		return nil, err
	}
	if _, err := db.Exec(schemaSql); err != nil {
		return nil, err
	}

	if path != ":memory:" {
		mu.Lock()
		pool[path] = db
		mu.Unlock()
	}
	return db, nil
}
