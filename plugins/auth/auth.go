// Package auth implements bee auth commands.
package auth

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"bee/internal/cli"
	"bee/internal/session"
)

// Register adds auth subcommands to root.
func Register(root *cobra.Command, db *sql.DB, dbPath string) {
	auth := &cobra.Command{
		Use:   "auth",
		Short: "Authentication and profile management",
	}

	auth.AddCommand(
		loginCmd(db, dbPath),
		logoutCmd(db),
		profilesCmd(db),
		useCmd(db),
		deleteCmd(db),
	)
	root.AddCommand(auth)
}

func loginCmd(db *sql.DB, dbPath string) *cobra.Command {
	var (
		serverURL string
		username  string
		token     string
		profile   string
		insecure  bool
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login (sign in / authenticate) — save server URL, username, and API token",
		RunE: func(cmd *cobra.Command, args []string) error {
			if serverURL == "" {
				var err error
				serverURL, err = cli.ReadLine("Server URL: ")
				if err != nil || serverURL == "" {
					return fmt.Errorf("server URL required")
				}
			}
			if username == "" {
				var err error
				username, err = cli.ReadLine("Username: ")
				if err != nil || username == "" {
					return fmt.Errorf("username required")
				}
			}
			if token == "" {
				var err error
				token, err = cli.ReadHidden("API Token: ")
				if err != nil || token == "" {
					return fmt.Errorf("API token required")
				}
			}

			// Validate credentials with a test request
			serverURL = strings.TrimRight(serverURL, "/")
			if err := validateCredentials(serverURL, username, token, insecure); err != nil {
				return fmt.Errorf("login failed: %w", err)
			}

			if err := session.SaveProfile(db, profile, serverURL, username, profile == "default"); err != nil {
				return fmt.Errorf("save profile: %w", err)
			}
			if err := session.SaveToken(db, dbPath, profile, token); err != nil {
				return fmt.Errorf("save token: %w", err)
			}
			if err := session.SetActiveProfile(db, profile); err != nil {
				return err
			}
			if insecure {
				_ = session.SetInsecureTLS(db, profile, true)
			}

			cli.Success(fmt.Sprintf("Logged in as '%s' on %s (profile: %s)", username, serverURL, profile))
			if insecure {
				cli.Warn("TLS verification disabled — only use on trusted networks.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&serverURL, "url", "", "CloudBees / Jenkins server URL")
	cmd.Flags().StringVar(&username, "username", "", "Your login username")
	cmd.Flags().StringVar(&token, "token", "", "Your API Token")
	cmd.Flags().StringVar(&profile, "profile", "default", "Named profile to save this login under")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "Skip TLS certificate verification (self-signed certs)")
	return cmd
}

func validateCredentials(serverURL, username, token string, insecure bool) error {
	basicToken := session.BuildBasicToken(username, token)
	req, err := http.NewRequest(http.MethodGet, serverURL+"/api/json?tree=url,nodeName", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Basic "+basicToken)
	httpClient := &http.Client{}
	if insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// Try parsing the URL to give a clearer error
		if _, e := url.Parse(serverURL); e != nil {
			return fmt.Errorf("invalid URL: %s", serverURL)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("wrong username or token")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func logoutCmd(db *sql.DB) *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Sign out and remove stored credentials for a profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			if profile == "" {
				var err error
				profile, err = session.GetActiveProfileName(db)
				if err != nil {
					return err
				}
			}
			if err := session.DeleteProfile(db, profile); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Logged out profile '%s'", profile))
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Profile to sign out (default: active profile)")
	return cmd
}

func profilesCmd(db *sql.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "profiles",
		Short: "List all saved profiles and show which one is active",
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := session.ListProfiles(db)
			if err != nil {
				return err
			}
			active, _ := session.GetActiveProfileName(db)
			if len(profiles) == 0 {
				cli.Info("No profiles saved. Run: bee auth login")
				return nil
			}
			rows := make([][]string, len(profiles))
			for i, p := range profiles {
				marker := " "
				if p.Name == active {
					marker = "▸"
				}
				rows[i] = []string{marker, p.Name, p.Username, p.ServerURL}
			}
			cli.Table([]string{"", "Profile", "Username", "Server"}, rows)
			return nil
		},
	}
}

func useCmd(db *sql.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "use <profile>",
		Short: "Switch / change the active profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if _, err := session.GetProfile(db, name); err != nil {
				return fmt.Errorf("profile '%s' not found", name)
			}
			if err := session.SetActiveProfile(db, name); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Switched to profile '%s'", name))
			return nil
		},
	}
}

func deleteCmd(db *sql.DB) *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete (remove) a login profile and its stored token",
		RunE: func(cmd *cobra.Command, args []string) error {
			if profile == "" {
				return fmt.Errorf("--profile required")
			}
			if err := session.DeleteProfile(db, profile); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Deleted profile '%s'", profile))
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Profile to delete")
	_ = cmd.MarkFlagRequired("profile")
	return cmd
}

// Login validates credentials, then saves the profile, token, and marks it
// active. Used by the TUI login modal. profileName defaults to "default".
func Login(db *sql.DB, dbPath, serverURL, username, token, profileName string, insecure bool) error {
	if profileName == "" {
		profileName = "default"
	}
	serverURL = strings.TrimRight(serverURL, "/")
	if err := validateCredentials(serverURL, username, token, insecure); err != nil {
		return err
	}
	if err := session.SaveProfile(db, profileName, serverURL, username, profileName == "default"); err != nil {
		return err
	}
	if err := session.SaveToken(db, dbPath, profileName, token); err != nil {
		return err
	}
	if err := session.SetActiveProfile(db, profileName); err != nil {
		return err
	}
	if insecure {
		_ = session.SetInsecureTLS(db, profileName, true)
	}
	return nil
}

// Logout clears the active session for a profile (defaults to the active one),
// keeping the profile row so the user can log back in.
func Logout(db *sql.DB, profileName string) error {
	if profileName == "" {
		var err error
		profileName, err = session.GetActiveProfileName(db)
		if err != nil {
			return err
		}
	}
	return session.ClearToken(db, profileName)
}

// SwitchProfile points the active-profile pointer at a saved, logged-in
// profile. Returns an error if the profile has no stored token.
func SwitchProfile(db *sql.DB, profileName string) error {
	if _, err := session.GetProfile(db, profileName); err != nil {
		return fmt.Errorf("profile '%s' not found", profileName)
	}
	if !session.HasToken(db, profileName) {
		return fmt.Errorf("no session for profile '%s'", profileName)
	}
	return session.SetActiveProfile(db, profileName)
}

// CurrentSession is a helper for other plugins to get the active session.
func CurrentSession(db *sql.DB, dbPath string) (*session.Session, error) {
	s, err := session.LoadSession(db, dbPath)
	if err != nil {
		return nil, fmt.Errorf("%w\n\nRun: bee auth login", err)
	}
	return s, nil
}

// GetDB returns db and dbPath (used by other plugins via package auth).
var _ = os.Getenv // ensure os is used
