// Package cache implements a SQLite-backed TTL cache for Jenkins API reads,
// keyed by a dotted-prefix scheme (e.g. "jobs.list", "nodes.detail.<name>").
package cache

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// ttlTable maps key prefixes to their TTL in seconds. First match wins, so
// order matters: more specific prefixes must come before shorter ones only
// when they'd otherwise collide (none currently do).
var ttlTable = []struct {
	prefix string
	ttl    int
}{
	{"jobs.list", 15}, {"jobs.queue", 5}, {"jobs.detail", 20}, {"jobs.exists", 60},
	{"controllers.list", 60}, {"controllers.detail", 60}, {"controllers.capabilities", 300},
	{"credentials.list", 30}, {"credentials.detail", 30},
	{"nodes.list", 30}, {"nodes.detail", 30}, {"nodes.approved", 15},
}

const defaultTTL = 15

// GetTTL returns the TTL in seconds for a cache key, by longest-matching
// configured prefix, falling back to defaultTTL.
func GetTTL(key string) int {
	best := -1
	ttl := defaultTTL
	for _, e := range ttlTable {
		if strings.HasPrefix(key, e.prefix) && len(e.prefix) > best {
			best = len(e.prefix)
			ttl = e.ttl
		}
	}
	return ttl
}

// GetCached returns the raw JSON value for key if present and unexpired.
func GetCached(db *sql.DB, key string) (json.RawMessage, bool, error) {
	var value string
	var expiresAt int64
	err := db.QueryRow(`SELECT value, expires_at FROM cache WHERE key = ?`, key).Scan(&value, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if expiresAt <= time.Now().Unix() {
		return nil, false, nil
	}
	return json.RawMessage(value), true, nil
}

// SetCache stores value (JSON-marshaled) under key. An optional ttl override
// (seconds) may be passed; otherwise GetTTL(key) is used.
func SetCache(db *sql.DB, key string, value any, ttl ...int) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	t := GetTTL(key)
	if len(ttl) > 0 {
		t = ttl[0]
	}
	expiresAt := time.Now().Add(time.Duration(t) * time.Second).Unix()
	_, err = db.Exec(`INSERT INTO cache(key, value, expires_at) VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, expires_at = excluded.expires_at`,
		key, string(b), expiresAt)
	return err
}

// Invalidate removes a single cache key.
func Invalidate(db *sql.DB, key string) error {
	_, err := db.Exec(`DELETE FROM cache WHERE key = ?`, key)
	return err
}

// escapeLike escapes LIKE metacharacters (\, %, _) in a literal prefix.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

// InvalidatePrefix removes every cache key starting with prefix.
func InvalidatePrefix(db *sql.DB, prefix string) error {
	_, err := db.Exec(`DELETE FROM cache WHERE key LIKE ? ESCAPE '\'`, escapeLike(prefix)+"%")
	return err
}

// InvalidateResource clears all cache entries for a resource type
// ("job", "node", "credential", or "all").
func InvalidateResource(db *sql.DB, resourceType string) error {
	prefixes := map[string]string{
		"job": "jobs.", "node": "nodes.", "credential": "credentials.",
	}
	if resourceType == "all" {
		for _, p := range prefixes {
			if err := InvalidatePrefix(db, p); err != nil {
				return err
			}
		}
		return nil
	}
	if p, ok := prefixes[resourceType]; ok {
		return InvalidatePrefix(db, p)
	}
	return nil
}

// PurgeExpired deletes all expired cache rows and returns how many were removed.
func PurgeExpired(db *sql.DB) (int64, error) {
	res, err := db.Exec(`DELETE FROM cache WHERE expires_at <= ?`, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ClearAll wipes every row from the cache table, expired or not.
func ClearAll(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM cache`)
	return err
}
