// Package controller — exported service layer for TUI and other consumers.
package controller

import (
	"context"
	"database/sql"

	"github.com/hyhy2001/bee/internal/api"
	"github.com/hyhy2001/bee/internal/db"
	"github.com/hyhy2001/bee/internal/session"
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
	dtos, err := listControllers(ctx, client)
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
