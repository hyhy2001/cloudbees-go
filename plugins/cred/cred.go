// Package cred implements bee cred commands.
package cred

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"bee/internal/api"
	"bee/internal/cache"
	"bee/internal/cli"
	"bee/internal/db"
	"bee/internal/session"
	"bee/plugins/controller"
)

// credDTO mirrors the Jenkins credentials JSON fields.
type credDTO struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	TypeName    string `json:"typeName"`
	Scope       string `json:"scope"`
	Description string `json:"description"`
}

// getUserSeg returns the credential store path segment.
// store="user" maps to /user/<username>/credentials/store/user/domain/_
// store="system" maps to /credentials/store/system/domain/_
func getUserSeg(username, store string) string {
	if store == "user" && username != "" && !strings.EqualFold(username, "system") {
		return "/user/" + url.PathEscape(username) + "/credentials/store/user/domain/_"
	}
	return "/credentials/store/system/domain/_"
}

func getUsername(database *sql.DB, dbPath string) string {
	s, err := session.LoadSession(database, dbPath)
	if err != nil {
		return ""
	}
	return s.Profile.Username
}

func getProfileName(database *sql.DB) string {
	name, _ := session.GetActiveProfileName(database)
	return name
}

func warnUserStore(store, username string) {
	if store == "user" && username == "" {
		cli.Warn("--store user requested but not logged in; using the system store.")
	}
}

// validateStore enforces the only two credential stores Jenkins exposes here.
func validateStore(store string) error {
	if store != "system" && store != "user" {
		return fmt.Errorf("invalid store '%s'. Choose from: system, user", store)
	}
	return nil
}

// validateScope enforces the only two credential scopes Jenkins exposes here.
func validateScope(scope string) error {
	if scope != "GLOBAL" && scope != "SYSTEM" {
		return fmt.Errorf("invalid scope '%s'. Choose from: GLOBAL, SYSTEM", scope)
	}
	return nil
}

// listCredentials fetches credentials from the store.
func listCredentials(database *sql.DB, client *api.Client, store, username string) ([]credDTO, error) {
	seg := getUserSeg(username, store)
	var result struct {
		Credentials []credDTO `json:"credentials"`
	}
	if err := client.GetJSONCached(context.Background(), database, seg+"/api/json?tree=credentials[id,typeName,description,scope,displayName]", "credentials.list."+store, &result); err != nil {
		// 404 means plugin not installed — treat as empty
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}
	return result.Credentials, nil
}

// getCredential fetches a single credential by ID.
func getCredential(client *api.Client, credID, username, store string) (*credDTO, error) {
	seg := getUserSeg(username, store)
	var c credDTO
	if err := client.GetJSON(context.Background(), seg+"/credential/"+url.PathEscape(credID)+"/api/json", &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// buildUsernamePasswordXML builds the XML for a username+password credential.
func buildUsernamePasswordXML(id, username, password, desc, scope string) string {
	return `<?xml version='1.1' encoding='UTF-8'?>` + "\n" +
		`<com.cloudbees.plugins.credentials.impl.UsernamePasswordCredentialsImpl>` +
		`<scope>` + scope + `</scope>` +
		`<id>` + xmlEscape(id) + `</id>` +
		`<description>` + xmlEscape(desc) + `</description>` +
		`<username>` + xmlEscape(username) + `</username>` +
		`<password>` + xmlEscape(password) + `</password>` +
		`</com.cloudbees.plugins.credentials.impl.UsernamePasswordCredentialsImpl>`
}

// buildSecretTextXML builds the XML for a secret-text credential.
func buildSecretTextXML(id, secret, desc, scope string) string {
	return `<?xml version='1.1' encoding='UTF-8'?>` + "\n" +
		`<org.jenkinsci.plugins.plaincredentials.impl.StringCredentialsImpl>` +
		`<scope>` + scope + `</scope>` +
		`<id>` + xmlEscape(id) + `</id>` +
		`<description>` + xmlEscape(desc) + `</description>` +
		`<secret>` + xmlEscape(secret) + `</secret>` +
		`</org.jenkinsci.plugins.plaincredentials.impl.StringCredentialsImpl>`
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// extractTagCred extracts the text content of a simple non-nested XML tag,
// unescaping basic entities. Returns "" when the tag is absent.
func extractTagCred(xmlStr, tag string) string {
	open := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	i := strings.Index(xmlStr, open)
	if i < 0 {
		return ""
	}
	rest := xmlStr[i+len(open):]
	j := strings.Index(rest, closeTag)
	if j < 0 {
		return ""
	}
	v := rest[:j]
	v = strings.ReplaceAll(v, "&lt;", "<")
	v = strings.ReplaceAll(v, "&gt;", ">")
	v = strings.ReplaceAll(v, "&quot;", `"`)
	v = strings.ReplaceAll(v, "&amp;", "&")
	return v
}

// getCredentialXML fetches the config.xml for a credential.
func getCredentialXML(client *api.Client, credID, username, store string) (string, error) {
	seg := getUserSeg(username, store)
	resp, err := client.Do(context.Background(), "GET", seg+"/credential/"+url.PathEscape(credID)+"/config.xml", nil, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GET config.xml: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

var rootCloseAnchorRe = regexp.MustCompile(`(\n?)(</[A-Za-z][\w.-]*>\s*)$`)

// setXMLElement replaces or inserts a simple XML element value, tolerating
// attributes on the opening tag (e.g. <secret plugin="...">) unlike a plain
// strings.Index on "<tag>".
func setXMLElement(xmlStr, tag, value string) string {
	escaped := xmlEscape(value)
	openRe := regexp.MustCompile(`(<` + regexp.QuoteMeta(tag) + `(?:\s[^>]*)?>)([\s\S]*?)(</` + regexp.QuoteMeta(tag) + `>)`)
	if loc := openRe.FindStringSubmatchIndex(xmlStr); loc != nil {
		open := xmlStr[loc[2]:loc[3]]
		closeTag := xmlStr[loc[6]:loc[7]]
		return xmlStr[:loc[0]] + open + escaped + closeTag + xmlStr[loc[1]:]
	}
	// Insert before the last closing tag (the document root), not just any "</".
	if m := rootCloseAnchorRe.FindStringSubmatchIndex(xmlStr); m != nil {
		rootClose := xmlStr[m[4]:m[5]]
		insert := "\n  <" + tag + ">" + escaped + "</" + tag + ">" + rootClose
		return xmlStr[:m[4]] + insert
	}
	return xmlStr
}

// Register wires up the cred command group.
func Register(root *cobra.Command, database *sql.DB, dbPath string) {
	grp := &cobra.Command{
		Use:   "cred",
		Short: "Manage CloudBees credentials (secrets, tokens, passwords, API keys, SSH keys)",
	}
	grp.AddCommand(
		credListCmd(database, dbPath),
		credGetCmd(database, dbPath),
		credCreateCmd(database, dbPath),
		credDeleteCmd(database, dbPath),
		credUpdateCmd(database, dbPath),
		credTrackCmd(database, dbPath),
		credUntrackCmd(database, dbPath),
	)
	root.AddCommand(grp)
}

func credListCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagAll bool
	var flagStore, flagOutput string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored credentials from the selected store",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateStore(flagStore); err != nil {
				return err
			}
			username := getUsername(database, dbPath)
			warnUserStore(flagStore, username)
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			allCreds, err := listCredentials(database, client, flagStore, username)
			if err != nil {
				return err
			}
			profile := getProfileName(database)
			ctrlKey := client.BaseURL + "." + flagStore

			var creds []credDTO
			if flagAll {
				creds = allCreds
			} else {
				tracked, _ := db.ListTracked(database, "credential", profile, ctrlKey)
				trackedSet := map[string]bool{}
				for _, id := range tracked {
					trackedSet[id] = true
				}
				serverIDs := map[string]bool{}
				for _, c := range allCreds {
					serverIDs[c.ID] = true
				}
				for _, c := range allCreds {
					if trackedSet[c.ID] {
						creds = append(creds, c)
					}
				}
				for id := range trackedSet {
					if !serverIDs[id] {
						creds = append(creds, credDTO{
							ID:          id,
							TypeName:    "[DELETED]",
							Description: "[DELETED_ON_SERVER]",
						})
					}
				}
			}

			if flagOutput == "json" {
				b, err := json.MarshalIndent(creds, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(b))
				return nil
			}

			rows := make([][]string, len(creds))
			for i, c := range creds {
				typeName := c.TypeName
				if len(typeName) > 25 {
					typeName = typeName[:25]
				}
				desc := c.Description
				if len(desc) > 35 {
					desc = desc[:35]
				}
				rows[i] = []string{c.ID, typeName, desc, c.Scope}
			}
			cli.Table([]string{"ID", "Type", "Description", "Scope"}, rows)
			fmt.Printf("  %d credential(s)  [store: %s]\n", len(creds), flagStore)
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagAll, "all", false, "Show all credentials (default: only yours)")
	cmd.Flags().StringVar(&flagStore, "store", "system", "Credential store: system or user")
	cmd.Flags().StringVar(&flagOutput, "output", "", "Output format: table (default) or json")
	return cmd
}

func credGetCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagStore string
	cmd := &cobra.Command{
		Use:   "get <cred_id>",
		Short: "View a credential's details (secret values are masked)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateStore(flagStore); err != nil {
				return err
			}
			username := getUsername(database, dbPath)
			warnUserStore(flagStore, username)
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			cred, err := getCredential(client, args[0], username, flagStore)
			if err != nil {
				return err
			}
			// Mask sensitive fields by name
			pairs := [][]string{
				{"ID", cred.ID},
				{"DisplayName", cred.DisplayName},
				{"TypeName", cred.TypeName},
				{"Scope", cred.Scope},
				{"Description", cred.Description},
			}
			cli.KV(pairs)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagStore, "store", "system", "Credential store: system or user")
	return cmd
}

func credCreateCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagID, flagUsername, flagPassword, flagSecretText, flagDesc, flagScope, flagStore string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new credential (Username+Password, SecretText, or SSH key)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateStore(flagStore); err != nil {
				return err
			}
			if err := validateScope(flagScope); err != nil {
				return err
			}
			username := getUsername(database, dbPath)
			warnUserStore(flagStore, username)

			if flagSecretText != "" && flagUsername != "" {
				return fmt.Errorf("--secret-text and --username are mutually exclusive")
			}

			credID := flagID
			if credID == "" {
				credID = uuid.New().String()
			}

			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}

			if _, err := getCredential(client, credID, username, flagStore); err == nil {
				return fmt.Errorf("credential %q already exists", credID)
			}

			seg := getUserSeg(username, flagStore)
			var xmlBody string
			if flagSecretText != "" {
				xmlBody = buildSecretTextXML(credID, flagSecretText, flagDesc, flagScope)
			} else {
				if flagUsername == "" {
					return fmt.Errorf("--username is required for Username+Password credentials (or use --secret-text)")
				}
				password := flagPassword
				if password == "" {
					var readErr error
					password, readErr = cli.ReadHidden("Password for '" + flagUsername + "': ")
					if readErr != nil {
						return readErr
					}
				}
				xmlBody = buildUsernamePasswordXML(credID, flagUsername, password, flagDesc, flagScope)
			}

			if err := client.PostXML(cmd.Context(), seg+"/createCredentials", xmlBody); err != nil {
				return err
			}
			_ = cache.InvalidateResource(database, "credential")

			profile := getProfileName(database)
			ctrlKey := client.BaseURL + "." + flagStore
			_ = db.TrackResource(database, "credential", credID, profile, ctrlKey)

			cli.Success(fmt.Sprintf("Credential '%s' created in %s store.", credID, flagStore))
			base := strings.TrimRight(client.BaseURL, "/")
			if flagStore == "user" {
				fmt.Printf("  Link: %s/user/%s/credentials/store/user/domain/_/credential/%s/\n",
					base, url.PathEscape(username), credID)
			} else {
				fmt.Printf("  Link: %s/credentials/store/system/domain/_/credential/%s/\n", base, credID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagID, "id", "", "Unique credential ID (auto-generated if omitted)")
	cmd.Flags().StringVar(&flagUsername, "username", "", "Username (creates Username+Password credential)")
	cmd.Flags().StringVar(&flagPassword, "password", "", "Password or API key (prompted if omitted)")
	cmd.Flags().StringVar(&flagSecretText, "secret-text", "", "Plain-text secret — creates SecretText type")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Human-readable label")
	cmd.Flags().StringVar(&flagScope, "scope", "GLOBAL", "Visibility: GLOBAL or SYSTEM")
	cmd.Flags().StringVar(&flagStore, "store", "system", "Credential store: system or user")
	return cmd
}

func credDeleteCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagYes bool
	var flagStore string
	cmd := &cobra.Command{
		Use:   "delete <cred_id> [cred_id...]",
		Short: "Delete one or more credentials permanently",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateStore(flagStore); err != nil {
				return err
			}
			username := getUsername(database, dbPath)
			warnUserStore(flagStore, username)

			if !flagYes {
				label := fmt.Sprintf("credential '%s'", args[0])
				if len(args) > 1 {
					label = fmt.Sprintf("%d credentials", len(args))
				}
				fmt.Printf("Delete %s from %s store? [y/N] ", label, flagStore)
				var answer string
				fmt.Scanln(&answer)
				if strings.ToLower(strings.TrimSpace(answer)) != "y" {
					cli.Info("Cancelled.")
					return nil
				}
			}

			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			profile := getProfileName(database)
			ctrlKey := client.BaseURL + "." + flagStore
			seg := getUserSeg(username, flagStore)

			for _, credID := range args {
				if err := client.PostForm(cmd.Context(), seg+"/credential/"+url.PathEscape(credID)+"/doDelete", map[string]string{}); err != nil {
					cli.Error(fmt.Sprintf("Failed to delete '%s': %s", credID, err))
					continue
				}
				_ = db.UntrackResource(database, "credential", credID, profile, ctrlKey)
				cli.Success(fmt.Sprintf("Credential '%s' deleted from %s store.", credID, flagStore))
			}
			_ = cache.InvalidateResource(database, "credential")
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagYes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&flagStore, "store", "system", "Credential store: system or user")
	return cmd
}

func credUpdateCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagUsername, flagPassword, flagSecretText, flagDesc, flagStore string
	cmd := &cobra.Command{
		Use:   "update <cred_id>",
		Short: "Update an existing credential (rotate password, API token, secret, or description)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateStore(flagStore); err != nil {
				return err
			}
			username := getUsername(database, dbPath)
			warnUserStore(flagStore, username)

			if cmd.Flags().Changed("password") && cmd.Flags().Changed("secret-text") {
				return fmt.Errorf("--password and --secret-text are mutually exclusive")
			}

			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}

			credID := args[0]
			xmlStr, err := getCredentialXML(client, credID, username, flagStore)
			if err != nil {
				return err
			}

			if cmd.Flags().Changed("username") {
				xmlStr = setXMLElement(xmlStr, "username", flagUsername)
			}
			if cmd.Flags().Changed("password") {
				xmlStr = setXMLElement(xmlStr, "password", flagPassword)
			}
			if cmd.Flags().Changed("secret-text") {
				xmlStr = setXMLElement(xmlStr, "secret", flagSecretText)
			}
			if cmd.Flags().Changed("description") {
				xmlStr = setXMLElement(xmlStr, "description", flagDesc)
			}

			seg := getUserSeg(username, flagStore)
			if err := client.PostXML(cmd.Context(), seg+"/credential/"+url.PathEscape(credID)+"/config.xml", xmlStr); err != nil {
				return err
			}
			_ = cache.InvalidateResource(database, "credential")
			cli.Success(fmt.Sprintf("Credential '%s' updated.", credID))
			return nil
		},
	}
	cmd.Flags().StringVar(&flagUsername, "username", "", "New username value")
	cmd.Flags().StringVar(&flagPassword, "password", "", "New password or API key")
	cmd.Flags().StringVar(&flagSecretText, "secret-text", "", "New secret value (rotate/refresh token or key)")
	cmd.Flags().StringVar(&flagDesc, "description", "", "New human-readable label")
	cmd.Flags().StringVar(&flagStore, "store", "system", "Credential store: system or user")
	return cmd
}

func credTrackCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagStore string
	cmd := &cobra.Command{
		Use:   "track <cred_id> [cred_id...]",
		Short: "Track existing server credentials — add them to your Mine",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateStore(flagStore); err != nil {
				return err
			}
			username := getUsername(database, dbPath)
			warnUserStore(flagStore, username)
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			profile := getProfileName(database)
			ctrlKey := client.BaseURL + "." + flagStore
			tracked, _ := db.ListTracked(database, "credential", profile, ctrlKey)
			trackedSet := map[string]bool{}
			for _, id := range tracked {
				trackedSet[id] = true
			}

			for _, credID := range args {
				if _, err := getCredential(client, credID, username, flagStore); err != nil {
					if strings.Contains(err.Error(), "404") {
						cli.Error(fmt.Sprintf("Credential '%s' not found in %s store. Skipping.", credID, flagStore))
					} else {
						cli.Error(fmt.Sprintf("Could not verify credential '%s': %s", credID, err))
					}
					continue
				}
				if trackedSet[credID] {
					cli.Info(fmt.Sprintf("Credential '%s' is already tracked.", credID))
					continue
				}
				_ = db.TrackResource(database, "credential", credID, profile, ctrlKey)
				trackedSet[credID] = true
				cli.Success(fmt.Sprintf("Tracked '%s' into Mine.", credID))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagStore, "store", "system", "Credential store: system or user")
	return cmd
}

func credUntrackCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagStore string
	cmd := &cobra.Command{
		Use:   "untrack <cred_id> [cred_id...]",
		Short: "Stop tracking credentials — remove from your Mine (does not delete from server)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateStore(flagStore); err != nil {
				return err
			}
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			profile := getProfileName(database)
			ctrlKey := client.BaseURL + "." + flagStore
			tracked, _ := db.ListTracked(database, "credential", profile, ctrlKey)
			trackedSet := map[string]bool{}
			for _, id := range tracked {
				trackedSet[id] = true
			}

			for _, credID := range args {
				if !trackedSet[credID] {
					cli.Info(fmt.Sprintf("Credential '%s' is not in Mine.", credID))
					continue
				}
				_ = db.UntrackResource(database, "credential", credID, profile, ctrlKey)
				delete(trackedSet, credID)
				cli.Success(fmt.Sprintf("Removed '%s' from Mine.", credID))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagStore, "store", "system", "Credential store: system or user")
	return cmd
}
