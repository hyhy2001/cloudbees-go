// Package session manages authentication profiles and token storage.
package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/scrypt"
)

const secretFile = ".bee_secret"
const keyLen = 32

// Profile holds a saved login target.
type Profile struct {
	ID        int64
	Name      string
	ServerURL string
	Username  string
	IsDefault bool
}

// Session holds the decrypted credentials for a profile.
type Session struct {
	Profile   Profile
	BasicToken string // base64(user:apitoken)
}

// secretPath returns the path of the machine secret file alongside the DB.
func secretPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), secretFile)
}

// getMachineSecret returns (or creates) the per-machine 32-byte secret.
func getMachineSecret(dbPath string) ([]byte, error) {
	p := secretPath(dbPath)
	if data, err := os.ReadFile(p); err == nil {
		return data, nil
	}
	secret := make([]byte, keyLen)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, secret, 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}

// deriveKey derives a 32-byte AES key from the machine secret + uid.
func deriveKey(dbPath string) ([]byte, error) {
	secret, err := getMachineSecret(dbPath)
	if err != nil {
		return nil, err
	}
	uid := fmt.Sprintf("bee:%d", os.Getuid())
	salt := []byte(uid)
	// ponytail: scrypt params are modest (N=32768) — upgrade when threat model requires it
	return scrypt.Key(secret, salt, 32768, 8, 1, keyLen)
}

// encryptToken encrypts a plaintext string → base64(iv|tag|ciphertext).
func encryptToken(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	ct := gcm.Seal(iv, iv, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// decryptToken decrypts a base64(iv|tag|ciphertext) string.
func decryptToken(encoded string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	iv, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, iv, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// SaveProfile upserts a profile in the database.
func SaveProfile(db *sql.DB, name, serverURL, username string, isDefault bool) error {
	_, err := db.Exec(`
		INSERT INTO profiles (name, server_url, username, is_default, created_at)
		VALUES (?, ?, ?, ?, strftime('%s','now'))
		ON CONFLICT(name) DO UPDATE SET
			server_url=excluded.server_url,
			username=excluded.username,
			is_default=excluded.is_default`,
		name, serverURL, username, isDefault)
	return err
}

// SaveToken encrypts and stores the API token for a profile in settings.
func SaveToken(db *sql.DB, dbPath, profileName, token string) error {
	key, err := deriveKey(dbPath)
	if err != nil {
		return err
	}
	// basic token = base64(username:token) — we store the raw token and encode at use time
	enc, err := encryptToken(token, key)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		"token:"+profileName, enc)
	return err
}

// LoadToken decrypts the API token for a profile.
func LoadToken(db *sql.DB, dbPath, profileName string) (string, error) {
	var enc string
	err := db.QueryRow(`SELECT value FROM settings WHERE key=?`, "token:"+profileName).Scan(&enc)
	if err != nil {
		return "", err
	}
	key, err := deriveKey(dbPath)
	if err != nil {
		return "", err
	}
	return decryptToken(enc, key)
}

// GetActiveProfileName returns the name of the currently active profile.
func GetActiveProfileName(db *sql.DB) (string, error) {
	var name string
	err := db.QueryRow(`SELECT value FROM settings WHERE key='active_profile'`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "default", nil
	}
	return name, err
}

// SetActiveProfile stores the active profile name.
func SetActiveProfile(db *sql.DB, name string) error {
	_, err := db.Exec(`
		INSERT INTO settings (key, value) VALUES ('active_profile', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, name)
	return err
}

// ListProfiles returns all saved profiles.
func ListProfiles(db *sql.DB) ([]Profile, error) {
	rows, err := db.Query(`SELECT id, name, server_url, username, is_default FROM profiles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var profiles []Profile
	for rows.Next() {
		var p Profile
		var isDefault int
		if err := rows.Scan(&p.ID, &p.Name, &p.ServerURL, &p.Username, &isDefault); err != nil {
			return nil, err
		}
		p.IsDefault = isDefault == 1
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// GetProfile returns a single profile by name.
func GetProfile(db *sql.DB, name string) (Profile, error) {
	var p Profile
	var isDefault int
	err := db.QueryRow(`SELECT id, name, server_url, username, is_default FROM profiles WHERE name=?`, name).
		Scan(&p.ID, &p.Name, &p.ServerURL, &p.Username, &isDefault)
	p.IsDefault = isDefault == 1
	return p, err
}

// DeleteProfile removes a profile and its token.
func DeleteProfile(db *sql.DB, name string) error {
	if _, err := db.Exec(`DELETE FROM profiles WHERE name=?`, name); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM settings WHERE key=?`, "token:"+name)
	return err
}

// SetInsecureTLS stores whether TLS verification should be skipped for a profile.
func SetInsecureTLS(db *sql.DB, profileName string, insecure bool) error {
	val := "0"
	if insecure {
		val = "1"
	}
	_, err := db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		"tls_insecure:"+profileName, val)
	return err
}

// GetInsecureTLS returns true if TLS verification should be skipped for this profile.
func GetInsecureTLS(db *sql.DB, profileName string) bool {
	var val string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key=?`, "tls_insecure:"+profileName).Scan(&val); err != nil {
		return false
	}
	return val == "1"
}

func BuildBasicToken(username, token string) string {
	raw := username + ":" + token
	_ = sha256.New() // ensure crypto/sha256 is linked for future use
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// HasToken reports whether a stored (encrypted) token exists for the profile.
// Lighter than LoadToken — no decryption, just presence.
func HasToken(db *sql.DB, profileName string) bool {
	var enc string
	err := db.QueryRow(`SELECT value FROM settings WHERE key=?`, "token:"+profileName).Scan(&enc)
	return err == nil
}

// ClearToken logs a profile out: it removes the stored token and the profile's
// active-controller selection, but keeps the profile row so it can log back in.
// If the cleared profile was active, the active pointer moves to another
// profile that still has a token, or is dropped when none remain.
func ClearToken(db *sql.DB, profileName string) error {
	if _, err := db.Exec(`DELETE FROM settings WHERE key IN (?, ?, ?)`,
		"token:"+profileName,
		"active_controller."+profileName,
		"active_controller_url."+profileName,
	); err != nil {
		return err
	}
	active, _ := GetActiveProfileName(db)
	if active != profileName {
		return nil
	}
	// Repoint active_profile at the next logged-in profile, if any.
	profiles, _ := ListProfiles(db)
	for _, p := range profiles {
		if p.Name != profileName && HasToken(db, p.Name) {
			return SetActiveProfile(db, p.Name)
		}
	}
	_, err := db.Exec(`DELETE FROM settings WHERE key='active_profile'`)
	return err
}

// LoadSession returns the active profile + decoded basic token.
func LoadSession(db *sql.DB, dbPath string) (*Session, error) {
	name, err := GetActiveProfileName(db)
	if err != nil {
		return nil, err
	}
	p, err := GetProfile(db, name)
	if err != nil {
		return nil, fmt.Errorf("no active profile %q — run: bee auth login", name)
	}
	token, err := LoadToken(db, dbPath, name)
	if err != nil {
		return nil, fmt.Errorf("no token for profile %q — run: bee auth login", name)
	}
	return &Session{
		Profile:    p,
		BasicToken: BuildBasicToken(p.Username, token),
	}, nil
}
