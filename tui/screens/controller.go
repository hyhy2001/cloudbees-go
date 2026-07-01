package screens

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hyhy2001/bee/internal/db"
	"github.com/hyhy2001/bee/internal/api"
	"github.com/hyhy2001/bee/internal/session"
	"github.com/hyhy2001/bee/tui/components"
	"github.com/hyhy2001/bee/tui/theme"
)

// ctrlEntry is a single controller row.
type ctrlEntry struct {
	Name        string
	Class       string
	URL         string
	Description string
	Offline     bool
}

type controllersLoaded struct {
	ctrls []ctrlEntry
	err   error
}

type ctrlSelectDone struct {
	name string
	err  error
}

// ControllerScreen is the TUI screen for listing/selecting controllers.
type ControllerScreen struct {
	db         *sql.DB
	dbPath     string
	table      components.TableModel
	detail     components.MessageModal
	loading    bool
	err        error
	ctrls      []ctrlEntry
	width      int
	height     int
	activeName string // currently selected controller name
}

// NewControllerScreen creates a new ControllerScreen.
func NewControllerScreen(database *sql.DB, dbPath string, activeName string) ControllerScreen {
	cols := []table.Column{
		{Title: "Active", Width: 7},
		{Title: "Name", Width: 30},
		{Title: "Description", Width: 28},
		{Title: "Status", Width: 8},
	}
	return ControllerScreen{
		db:         database,
		dbPath:     dbPath,
		table:      components.New(cols, nil),
		loading:    true,
		activeName: activeName,
	}
}

// Init fires the initial data fetch.
func (s ControllerScreen) Init() tea.Cmd {
	return s.fetchControllers()
}

func (s ControllerScreen) fetchControllers() tea.Cmd {
	database, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		sess, err := session.LoadSession(database, dbPath)
		if err != nil {
			return controllersLoaded{err: err}
		}
		client := api.New(sess.Profile.ServerURL, sess.BasicToken)

		var result struct {
			Jobs []struct {
				Class       string `json:"_class"`
				Name        string `json:"name"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Offline     bool   `json:"offline"`
			} `json:"jobs"`
		}
		if err := client.GetJSON(context.Background(),
			"/api/json?tree=jobs[_class,name,url,description,offline]", &result); err != nil {
			return controllersLoaded{err: err}
		}

		var ctrls []ctrlEntry
		classFrags := []string{"Master", "Controller", "ConnectedMaster", "ManagedMaster"}
		for _, j := range result.Jobs {
			for _, frag := range classFrags {
				if strings.Contains(j.Class, frag) {
					ctrls = append(ctrls, ctrlEntry{
						Name:        j.Name,
						Class:       j.Class,
						URL:         j.URL,
						Description: j.Description,
						Offline:     j.Offline,
					})
					break
				}
			}
		}
		if len(ctrls) == 0 {
			for _, j := range result.Jobs {
				ctrls = append(ctrls, ctrlEntry{
					Name:        j.Name,
					Class:       j.Class,
					URL:         j.URL,
					Description: j.Description,
					Offline:     j.Offline,
				})
			}
		}
		return controllersLoaded{ctrls: ctrls}
	}
}

func (s ControllerScreen) doSelect(name, ctrlURL string) tea.Cmd {
	database, _ := s.db, s.dbPath
	return func() tea.Msg {
		profileName, _ := session.GetActiveProfileName(database)
		if err := db.SetSetting(database, "active_controller."+profileName, name); err != nil {
			return ctrlSelectDone{err: err}
		}
		if err := db.SetSetting(database, "active_controller_url."+profileName, ctrlURL); err != nil {
			return ctrlSelectDone{err: err}
		}
		return ctrlSelectDone{name: name}
	}
}

func buildControllerRows(ctrls []ctrlEntry, active string) []table.Row {
	rows := make([]table.Row, len(ctrls))
	for i, c := range ctrls {
		indicator := " "
		if c.Name == active {
			indicator = theme.SymSelected
		}
		status := "online"
		if c.Offline {
			status = "offline"
		}
		desc := c.Description
		if len(desc) > 28 {
			desc = desc[:25] + "..."
		}
		rows[i] = table.Row{indicator, c.Name, desc, status}
	}
	return rows
}

// Update handles messages and key input.
func (s ControllerScreen) Update(msg tea.Msg) (ControllerScreen, tea.Cmd) {
	if s.detail.Visible() {
		var cmd tea.Cmd
		s.detail, cmd = s.detail.Update(msg)
		return s, cmd
	}

	switch msg := msg.(type) {
	case controllersLoaded:
		s.loading = false
		s.err = msg.err
		if msg.err == nil {
			s.ctrls = msg.ctrls
			s.table.SetRows(buildControllerRows(s.ctrls, s.activeName))
		}
		return s, nil

	case ctrlSelectDone:
		if msg.err != nil {
			s.detail.Show("Error", msg.err.Error())
		} else {
			s.activeName = msg.name
			s.detail.Show("Selected", fmt.Sprintf("Active controller: %s", msg.name))
			s.table.SetRows(buildControllerRows(s.ctrls, s.activeName))
		}
		return s, nil

	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.table.SetSize(msg.Width, maxInt(5, msg.Height-8))
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			row := s.table.SelectedRow()
			if row == nil {
				return s, nil
			}
			name := row[1]
			var ctrlURL string
			for _, c := range s.ctrls {
				if c.Name == name {
					ctrlURL = c.URL
					break
				}
			}
			return s, s.doSelect(name, ctrlURL)
		case "r":
			s.loading = true
			return s, s.fetchControllers()
		}
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

// View renders the controller screen.
func (s ControllerScreen) View() string {
	if s.detail.Visible() {
		return s.detail.View()
	}
	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(theme.SymGear + " Controllers"))
	if s.activeName != "" {
		sb.WriteString("  " + theme.StyleSuccess.Render("["+s.activeName+"]"))
	}
	sb.WriteString("\n")
	if s.loading {
		sb.WriteString(theme.StyleDim.Render(theme.SymLoading + " Loading controllers..."))
		return sb.String()
	}
	if s.err != nil {
		sb.WriteString(theme.StyleError.Render(theme.SymFail + " " + s.err.Error()))
		return sb.String()
	}
	if len(s.ctrls) == 0 {
		sb.WriteString(theme.StyleDim.Render("No controllers found."))
		return sb.String()
	}
	sb.WriteString(s.table.View())
	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("enter=select  r=refresh  ^F=search"))
	return sb.String()
}
