// Package db manages the SQLite connection and schema for bee's local state.
package db

import (
	"database/sql"
	_ "embed"
	"os"
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

// DefaultPath returns ~/.local/share/bee/cb.db (matching the TS binary's location).
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "bee", "cb.db")
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
