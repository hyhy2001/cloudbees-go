// Package db provides resource tracking queries.
package db

import (
	"database/sql"
	"time"
)

// TrackResource adds a resource to the user's "Mine" list.
func TrackResource(db *sql.DB, resourceType, name, profileName, controllerName string) error {
	_, err := db.Exec(`
		INSERT OR IGNORE INTO user_resources (resource_type, name, profile_name, controller_name, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		resourceType, name, profileName, controllerName, time.Now().Unix())
	return err
}

// UntrackResource removes a resource from the user's "Mine" list.
func UntrackResource(db *sql.DB, resourceType, name, profileName, controllerName string) error {
	_, err := db.Exec(`
		DELETE FROM user_resources WHERE resource_type=? AND name=? AND profile_name=? AND controller_name=?`,
		resourceType, name, profileName, controllerName)
	return err
}

// ListTracked returns tracked resource names for a given type/profile/controller.
func ListTracked(db *sql.DB, resourceType, profileName, controllerName string) ([]string, error) {
	rows, err := db.Query(`
		SELECT name FROM user_resources
		WHERE resource_type=? AND profile_name=? AND controller_name=?
		ORDER BY created_at DESC`,
		resourceType, profileName, controllerName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// GetSetting retrieves a setting value.
func GetSetting(db *sql.DB, key string) (string, bool, error) {
	var val string
	err := db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return val, err == nil, err
}

// SetSetting stores a setting value.
func SetSetting(db *sql.DB, key, value string) error {
	_, err := db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value)
	return err
}
