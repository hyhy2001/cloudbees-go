package session

import (
	"testing"

	beedb "bee/internal/db"
)

func TestClearTokenRepointsActiveProfile(t *testing.T) {
	// File-backed (not :memory:) so token encryption's secret file has a
	// directory to live in alongside the DB. db.Open applies the schema.
	dbPath := t.TempDir() + "/cb.db"
	conn, err := beedb.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// Two logged-in profiles; "a" is active.
	for _, name := range []string{"a", "b"} {
		if err := SaveProfile(conn, name, "https://ci", "user-"+name, name == "a"); err != nil {
			t.Fatal(err)
		}
		if err := SaveToken(conn, dbPath, name, "tok-"+name); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetActiveProfile(conn, "a"); err != nil {
		t.Fatal(err)
	}

	// Logging out the active profile should repoint active → the other logged-in one.
	if err := ClearToken(conn, "a"); err != nil {
		t.Fatalf("ClearToken: %v", err)
	}
	if HasToken(conn, "a") {
		t.Error("expected token for 'a' to be cleared")
	}
	if got, _ := GetActiveProfileName(conn); got != "b" {
		t.Errorf("active profile = %q, want b", got)
	}

	// Profile row survives logout so it can be re-listed / logged back in.
	if _, err := GetProfile(conn, "a"); err != nil {
		t.Errorf("profile row for 'a' should survive logout: %v", err)
	}

	// Logging out the last profile drops the active pointer → falls back to default.
	if err := ClearToken(conn, "b"); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetActiveProfileName(conn); got != "default" {
		t.Errorf("active profile after last logout = %q, want default", got)
	}
}
