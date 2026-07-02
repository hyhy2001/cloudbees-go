package cache

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestGetTTL(t *testing.T) {
	cases := map[string]int{
		"jobs.list.foo":              15,
		"jobs.detail.myjob":          20,
		"controllers.capabilities.x": 300,
		"nodes.approved.agent1":      15,
		"unknown.key":                defaultTTL,
	}
	for key, want := range cases {
		if got := GetTTL(key); got != want {
			t.Errorf("GetTTL(%q) = %d, want %d", key, got, want)
		}
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE cache (key TEXT PRIMARY KEY, value TEXT NOT NULL, expires_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSetGetInvalidate(t *testing.T) {
	db := openTestDB(t)

	if err := SetCache(db, "jobs.list.root", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	raw, ok, err := GetCached(db, "jobs.list.root")
	if err != nil || !ok {
		t.Fatalf("expected cache hit, got ok=%v err=%v", ok, err)
	}
	if string(raw) != `["a","b"]` {
		t.Errorf("got %s", raw)
	}

	if err := Invalidate(db, "jobs.list.root"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := GetCached(db, "jobs.list.root"); ok {
		t.Error("expected cache miss after invalidate")
	}
}

func TestInvalidatePrefix(t *testing.T) {
	db := openTestDB(t)
	_ = SetCache(db, "jobs.list.a", 1)
	_ = SetCache(db, "jobs.detail.b", 2)
	_ = SetCache(db, "nodes.list", 3)

	if err := InvalidatePrefix(db, "jobs."); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := GetCached(db, "jobs.list.a"); ok {
		t.Error("jobs.list.a should be gone")
	}
	if _, ok, _ := GetCached(db, "jobs.detail.b"); ok {
		t.Error("jobs.detail.b should be gone")
	}
	if _, ok, _ := GetCached(db, "nodes.list"); !ok {
		t.Error("nodes.list should survive")
	}
}

func TestInvalidateResource(t *testing.T) {
	db := openTestDB(t)
	_ = SetCache(db, "jobs.list", 1)
	_ = SetCache(db, "nodes.list", 2)
	_ = SetCache(db, "credentials.list", 3)

	if err := InvalidateResource(db, "job"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := GetCached(db, "jobs.list"); ok {
		t.Error("jobs.list should be gone")
	}
	if _, ok, _ := GetCached(db, "nodes.list"); !ok {
		t.Error("nodes.list should survive")
	}

	if err := InvalidateResource(db, "all"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := GetCached(db, "nodes.list"); ok {
		t.Error("nodes.list should be gone after 'all'")
	}
	if _, ok, _ := GetCached(db, "credentials.list"); ok {
		t.Error("credentials.list should be gone after 'all'")
	}
}
