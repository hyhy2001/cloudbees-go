// Package controller — exported service layer for TUI and other consumers.
package controller

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"

	"bee/internal/api"
	"bee/internal/cache"
	"bee/internal/db"
	"bee/internal/session"
)

// ControllerDTO is the exported controller view.
type ControllerDTO struct {
	Class       string
	Name        string
	URL         string
	Description string
	Offline     bool
}

// ListControllers fetches all controllers from the CJOC root API.
func ListControllers(ctx context.Context, client *api.Client) ([]ControllerDTO, error) {
	dtos, err := listControllers(ctx, nil, client)
	if err != nil {
		return nil, err
	}
	out := make([]ControllerDTO, len(dtos))
	for i, d := range dtos {
		out[i] = ControllerDTO{Class: d.Class, Name: d.Name, URL: d.URL, Description: d.Description, Offline: d.Offline}
	}
	return out, nil
}

// GetActiveController returns the active controller name, URL, and whether one is set.
func GetActiveController(database *sql.DB) (name, url string, ok bool) {
	return getActiveController(database)
}

// SetActiveController persists the selected controller to the database.
func SetActiveController(database *sql.DB, profileName, name, ctrlURL string) error {
	if err := db.SetSetting(database, "active_controller."+profileName, name); err != nil {
		return err
	}
	return db.SetSetting(database, "active_controller_url."+profileName, ctrlURL)
}

// GetActiveProfileName returns the active profile name from the session.
func GetActiveProfileName(database *sql.DB) string {
	name, _ := session.GetActiveProfileName(database)
	return name
}

// Capabilities reports what the current credentials can create on a controller.
type Capabilities struct {
	TypeLabel     string
	CanCreateJob  bool
	CanCreateNode bool
	CanCreateCred bool
}

// probeOKStatuses are HTTP responses Jenkins gives for an authorized create
// attempt. 400/405 means "allowed but incomplete", 302 means "created/redirected".
// 401/403/404 means no permission or endpoint missing.
var probeOKStatuses = map[int]bool{302: true, 400: true, 405: true}

func probeCanCreate(ctx context.Context, client *api.Client, path string) bool {
	resp, err := client.DoNoRedirect(ctx, "POST", path, nil, "application/x-www-form-urlencoded")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return probeOKStatuses[resp.StatusCode]
}

// GetControllerCapabilities probes whether the active credentials can create
// jobs/nodes/credentials on the named controller, caching the result for 5
// minutes (capability checks are expensive: 3 network round-trips plus a
// redirect resolution).
func GetControllerCapabilities(ctx context.Context, database *sql.DB, cjocClient *api.Client, name, cjocURL string) (Capabilities, error) {
	cacheKey := "controllers.capabilities." + name
	if database != nil {
		if raw, ok, err := cache.GetCached(database, cacheKey); err == nil && ok {
			var c Capabilities
			if err := json.Unmarshal(raw, &c); err == nil {
				return c, nil
			}
		}
	}

	var detail struct {
		Class   string `json:"_class"`
		Offline bool   `json:"offline"`
	}
	if err := cjocClient.GetJSON(ctx, "/job/"+name+"/api/json?tree=_class,offline", &detail); err != nil {
		return Capabilities{}, err
	}

	typeLabel := detail.Class
	switch {
	case strings.Contains(detail.Class, "ManagedMaster"):
		typeLabel = "Managed Master"
	case strings.Contains(detail.Class, "ConnectedMaster"):
		typeLabel = "Connected Master"
	case strings.Contains(detail.Class, "Upgrading"):
		typeLabel = "Upgrading"
	case strings.LastIndex(detail.Class, ".") >= 0:
		typeLabel = detail.Class[strings.LastIndex(detail.Class, ".")+1:]
	case detail.Class == "":
		typeLabel = "Unknown"
	}

	result := Capabilities{TypeLabel: typeLabel}
	if detail.Offline || strings.Contains(detail.Class, "Beekeeper") {
		return result, nil
	}

	realURL := resolveURL(ctx, cjocClient, cjocURL)
	client := api.CloneWithBaseURL(cjocClient, realURL)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		result.CanCreateJob = probeCanCreate(ctx, client, "/createItem?name=probe_test")
	}()
	go func() {
		defer wg.Done()
		result.CanCreateNode = probeCanCreate(ctx, client, "/computer/doCreateItem?name=probe_tester&type=hudson.slaves.DumbSlave")
	}()
	go func() {
		defer wg.Done()
		result.CanCreateCred = probeCanCreate(ctx, client, "/credentials/store/system/domain/_/createCredentials")
	}()
	wg.Wait()

	if database != nil {
		_ = cache.SetCache(database, cacheKey, result, 300)
	}
	return result, nil
}

// Info is a best-effort snapshot of the current controller and user identity.
// Fields default to zero values on a failed sub-request rather than failing
// the whole call.
type Info struct {
	Class           string
	NodeDescription string
	NumExecutors    int
	UserID          string
	UserFullName    string
}

// GetControllerInfo fetches basic controller + current-user identity info,
// best-effort (each sub-request failing independently leaves its fields zero).
func GetControllerInfo(ctx context.Context, client *api.Client) Info {
	var info Info
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		var raw struct {
			Class           string `json:"_class"`
			NodeDescription string `json:"nodeDescription"`
			NumExecutors    int    `json:"numExecutors"`
		}
		if err := client.GetJSON(ctx, "/api/json?tree=_class,nodeDescription,numExecutors", &raw); err == nil {
			info.Class, info.NodeDescription, info.NumExecutors = raw.Class, raw.NodeDescription, raw.NumExecutors
		}
	}()
	go func() {
		defer wg.Done()
		var raw struct {
			ID       string `json:"id"`
			FullName string `json:"fullName"`
		}
		if err := client.GetJSON(ctx, "/me/api/json?tree=id,fullName", &raw); err == nil {
			info.UserID, info.UserFullName = raw.ID, raw.FullName
		}
	}()
	wg.Wait()
	return info
}
