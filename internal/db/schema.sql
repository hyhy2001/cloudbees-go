-- CloudBees CLI Database Schema
-- SQLite 3.7+ compatible

CREATE TABLE IF NOT EXISTS profiles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    server_url TEXT    NOT NULL,
    username   TEXT    NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS cache (
    key        TEXT    PRIMARY KEY,
    value      TEXT    NOT NULL,
    expires_at INTEGER NOT NULL
);

-- Index for time-based purge (purgeExpired scans WHERE expires_at <= now).
-- PRIMARY KEY already covers exact-key lookups; this index covers range scans.
CREATE INDEX IF NOT EXISTS idx_cache_expires ON cache(expires_at);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS user_resources (
    resource_type   TEXT NOT NULL,
    name            TEXT NOT NULL,
    profile_name    TEXT NOT NULL,
    controller_name TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    PRIMARY KEY (resource_type, name, profile_name, controller_name)
);
