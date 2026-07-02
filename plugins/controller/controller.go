// Package controller implements bee controller commands.
package controller

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"bee/internal/api"
	"bee/internal/cli"
	"bee/internal/db"
	"bee/internal/session"
)

var controllerClassFragments = []string{"Master", "Controller", "ConnectedMaster", "ManagedMaster"}

type controllerDTO struct {
	Class       string `json:"_class"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Offline     bool   `json:"offline"`
}

func Register(root *cobra.Command, database *sql.DB, dbPath string) {
	grp := &cobra.Command{
		Use:   "controller",
		Short: "Manage CloudBees / Jenkins controller instances (masters)",
	}
	grp.AddCommand(
		listCmd(database, dbPath),
		infoCmd(database, dbPath),
		selectCmd(database, dbPath),
		currentCmd(database),
	)
	root.AddCommand(grp)
}

func newClient(database *sql.DB, dbPath string) (*api.Client, error) {
	s, err := session.LoadSession(database, dbPath)
	if err != nil {
		return nil, fmt.Errorf("%w\n\nRun: bee auth login", err)
	}
	return api.New(s.Profile.ServerURL, s.BasicToken), nil
}

func listControllers(ctx context.Context, database *sql.DB, client *api.Client) ([]controllerDTO, error) {
	var result struct {
		Jobs []controllerDTO `json:"jobs"`
	}
	if err := client.GetJSONCached(ctx, database, "/api/json?tree=jobs[_class,name,url,description,offline]", "controllers.list", &result); err != nil {
		return nil, err
	}
	var controllers []controllerDTO
	for _, j := range result.Jobs {
		for _, frag := range controllerClassFragments {
			if strings.Contains(j.Class, frag) {
				controllers = append(controllers, j)
				break
			}
		}
	}
	if len(controllers) == 0 {
		return result.Jobs, nil
	}
	return controllers, nil
}

func listCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all available controllers on this CloudBees server",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient(database, dbPath)
			if err != nil {
				return err
			}
			controllers, err := listControllers(cmd.Context(), database, client)
			if err != nil {
				return err
			}
			activeName, _, _ := getActiveController(database)
			rows := make([][]string, len(controllers))
			for i, c := range controllers {
				marker := " "
				if c.Name == activeName {
					marker = "*"
				}
				status := "ONLINE"
				if c.Offline {
					status = "OFFLINE"
				}
				desc := c.Description
				if len(desc) > 40 {
					desc = desc[:40]
				}
				rows[i] = []string{marker, c.Name, desc, status}
			}
			cli.Table([]string{"", "Name", "Description", "Status"}, rows)
			fmt.Printf("  %d controller(s)\n", len(controllers))
			return nil
		},
	}
}

func infoCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "View controller details: URL, type, online status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient(database, dbPath)
			if err != nil {
				return err
			}
			var detail struct {
				Class       string `json:"_class"`
				Name        string `json:"name"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Offline     bool   `json:"offline"`
			}
			if err := client.GetJSONCached(cmd.Context(), database, "/job/"+args[0]+"/api/json", "controllers.detail."+args[0], &detail); err != nil {
				return err
			}
			typeLabel := detail.Class
			if strings.Contains(detail.Class, "ManagedMaster") {
				typeLabel = "Managed Master"
			} else if strings.Contains(detail.Class, "ConnectedMaster") {
				typeLabel = "Connected Master"
			} else if idx := strings.LastIndex(detail.Class, "."); idx >= 0 {
				typeLabel = detail.Class[idx+1:]
			}
			status := "ONLINE"
			if detail.Offline {
				status = "OFFLINE"
			}
			pairs := [][]string{
				{"Name", detail.Name},
				{"URL", detail.URL},
				{"Type", typeLabel},
				{"Status", status},
				{"Description", detail.Description},
			}
			if caps, err := GetControllerCapabilities(cmd.Context(), database, client, args[0], detail.URL); err == nil {
				pairs = append(pairs,
					[]string{"Can Create Job", fmt.Sprintf("%v", caps.CanCreateJob)},
					[]string{"Can Create Node", fmt.Sprintf("%v", caps.CanCreateNode)},
					[]string{"Can Create Cred", fmt.Sprintf("%v", caps.CanCreateCred)},
				)
			}
			cli.KV(pairs)
			return nil
		},
	}
}

func selectCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "select <name>",
		Short: "Switch / change the active controller for subsequent commands",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			client, err := newClient(database, dbPath)
			if err != nil {
				return err
			}
			controllers, err := listControllers(cmd.Context(), database, client)
			if err != nil {
				return err
			}
			var match *controllerDTO
			for i, c := range controllers {
				if c.Name == name {
					match = &controllers[i]
					break
				}
			}
			if match == nil {
				return fmt.Errorf("controller '%s' not found", name)
			}
			// Resolve redirect to real URL
			resolvedURL := resolveURL(cmd.Context(), client, match.URL)
			profileName, _ := session.GetActiveProfileName(database)
			if err := db.SetSetting(database, "active_controller."+profileName, name); err != nil {
				return err
			}
			if err := db.SetSetting(database, "active_controller_url."+profileName, resolvedURL); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Active controller: %s", name))
			fmt.Printf("     Resolved URL: %s\n", resolvedURL)
			return nil
		},
	}
}

func currentCmd(database *sql.DB) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show which controller is currently active",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, url, ok := getActiveController(database)
			if !ok {
				cli.Info("No active controller selected. Use: bee controller select <name>")
				return nil
			}
			fmt.Printf("Active controller: %s\n", name)
			fmt.Printf("URL              : %s\n", url)
			return nil
		},
	}
}

func getActiveController(database *sql.DB) (name, url string, ok bool) {
	profileName, _ := session.GetActiveProfileName(database)
	name, ok1, _ := db.GetSetting(database, "active_controller."+profileName)
	if !ok1 {
		name, ok1, _ = db.GetSetting(database, "active_controller")
	}
	url, ok2, _ := db.GetSetting(database, "active_controller_url."+profileName)
	if !ok2 {
		url, ok2, _ = db.GetSetting(database, "active_controller_url")
	}
	return name, url, ok1 && ok2 && name != ""
}

// GetActiveControllerClient returns an API client pointed at the active controller.
// Falls back to root client if no controller selected.
func GetActiveControllerClient(database *sql.DB, dbPath string) (*api.Client, error) {
	s, err := session.LoadSession(database, dbPath)
	if err != nil {
		return nil, err
	}
	_, url, ok := getActiveController(database)
	if ok && url != "" {
		return api.New(url, s.BasicToken), nil
	}
	return api.New(s.Profile.ServerURL, s.BasicToken), nil
}

// resolveURL follows the CJOC 302 redirect to find the real controller URL,
// stripping SSO suffixes (mirrors TS resolveControllerUrl).
func resolveURL(ctx context.Context, client *api.Client, cjocURL string) string {
	httpClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // stop at first redirect
		},
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cjocURL, nil)
	if err != nil {
		return cjocURL
	}
	req.Header.Set("Authorization", "Basic "+client.BasicToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return cjocURL
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "" {
		if strings.Contains(loc, "operations-center-sso-navigate") {
			return loc[:strings.Index(loc, "operations-center-sso-navigate")]
		}
		return loc
	}
	return cjocURL
}
