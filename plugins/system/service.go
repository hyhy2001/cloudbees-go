// Package system is a service-only layer for TUI use — no CLI command is
// registered, matching the TS original (system info is a TUI-only surface).
package system

import (
	"context"
	"fmt"
	"strings"

	"bee/internal/api"
)

// HealthResult is a snapshot of controller health.
type HealthResult struct {
	Class           string
	Mode            string
	NodeDescription string
	NumExecutors    int
}

// HealthCheck fetches basic controller identity/health info.
func HealthCheck(ctx context.Context, client *api.Client) (HealthResult, error) {
	var raw struct {
		Class           string `json:"_class"`
		Mode            string `json:"mode"`
		NodeDescription string `json:"nodeDescription"`
		NumExecutors    int    `json:"numExecutors"`
	}
	if err := client.GetJSON(ctx, "/api/json?tree=_class,mode,nodeDescription,numExecutors", &raw); err != nil {
		return HealthResult{}, err
	}
	return HealthResult{Class: raw.Class, Mode: raw.Mode, NodeDescription: raw.NodeDescription, NumExecutors: raw.NumExecutors}, nil
}

// PluginInfo is one installed plugin's identity and state.
type PluginInfo struct {
	ShortName string
	Version   string
	Active    bool
}

// GetInstalledPlugins lists installed plugins. Any error (including 403,
// when the current user lacks admin rights) yields an empty slice rather
// than failing — plugin listing is a nice-to-have, not load-bearing.
func GetInstalledPlugins(ctx context.Context, client *api.Client) []PluginInfo {
	var raw struct {
		Plugins []struct {
			ShortName string `json:"shortName"`
			Version   string `json:"version"`
			Active    bool   `json:"active"`
		} `json:"plugins"`
	}
	if err := client.GetJSON(ctx, "/pluginManager/api/json?tree=plugins[shortName,version,active]", &raw); err != nil {
		return nil
	}
	out := make([]PluginInfo, len(raw.Plugins))
	for i, p := range raw.Plugins {
		out[i] = PluginInfo{ShortName: p.ShortName, Version: p.Version, Active: p.Active}
	}
	return out
}

// HasPlugin reports whether shortName is installed and active. Fails open:
// if the plugin list can't be fetched at all, assume it might be present
// rather than blocking a feature that depends on it.
func HasPlugin(ctx context.Context, client *api.Client, shortName string) bool {
	plugins := GetInstalledPlugins(ctx, client)
	if len(plugins) == 0 {
		return true
	}
	for _, p := range plugins {
		if p.ShortName == shortName && p.Active {
			return true
		}
	}
	return false
}

// GetVersion returns the Jenkins/CloudBees version class string, or an
// "Error: ..." string on failure (never fails the caller).
func GetVersion(ctx context.Context, client *api.Client) string {
	var raw struct {
		Class string `json:"_class"`
	}
	if err := client.GetJSON(ctx, "/api/json?tree=_class", &raw); err != nil {
		return "Error: " + strings.TrimSpace(fmt.Sprint(err))
	}
	return raw.Class
}
