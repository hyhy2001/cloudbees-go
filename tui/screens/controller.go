package screens

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/internal/api"
	"bee/internal/session"
	"bee/plugins/controller"
	"bee/tui/components"
	"bee/tui/theme"
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

type ctrlInfoLoaded struct {
	name string
	caps controller.Capabilities
	err  error
}

// ControllerScreen is the TUI screen for listing/selecting controllers.
type ControllerScreen struct {
	db         *sql.DB
	dbPath     string
	table      components.DataTable
	search     components.SearchBox
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
	cols := []components.Column{
		{Header: " ", Width: 3},
		{Header: "Name", Width: 30, Flex: true},
		{Header: "Type", Width: 18},
		{Header: "URL", Width: 40, Flex: true},
		{Header: "Status", Width: 8},
	}
	return ControllerScreen{
		db:         database,
		dbPath:     dbPath,
		table:      components.NewDataTable(cols),
		loading:    true,
		activeName: activeName,
	}
}

// Init fires the initial data fetch.
func (s ControllerScreen) Init() tea.Cmd {
	return s.fetchControllers()
}

// InputCaptured reports whether the info/detail modal is currently visible or
// the search box is being edited, meaning this screen wants raw keys routed
// to it instead of being intercepted by the app shell for tab-switching/quit.
func (s ControllerScreen) InputCaptured() bool {
	return s.detail.Visible() || s.search.Editing()
}

func (s ControllerScreen) fetchControllers() tea.Cmd {
	database, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		sess, err := session.LoadSession(database, dbPath)
		if err != nil {
			return controllersLoaded{err: err}
		}
		client := api.New(sess.Profile.ServerURL, sess.BasicToken)
		dtos, err := controller.ListControllers(context.Background(), client)
		if err != nil {
			return controllersLoaded{err: err}
		}
		ctrls := make([]ctrlEntry, 0, len(dtos))
		for _, d := range dtos {
			ctrls = append(ctrls, ctrlEntry{
				Name:        d.Name,
				Class:       d.Class,
				URL:         d.URL,
				Description: d.Description,
				Offline:     d.Offline,
			})
		}
		return controllersLoaded{ctrls: ctrls}
	}
}

func (s ControllerScreen) doSelect(name, ctrlURL string) tea.Cmd {
	database := s.db
	return func() tea.Msg {
		profileName := controller.GetActiveProfileName(database)
		if err := controller.SetActiveController(database, profileName, name, ctrlURL); err != nil {
			return ctrlSelectDone{err: err}
		}
		return ctrlSelectDone{name: name}
	}
}

func (s ControllerScreen) doFetchInfo(name, ctrlURL string) tea.Cmd {
	database, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(database, dbPath)
		if err != nil {
			return ctrlInfoLoaded{name: name, err: err}
		}
		caps, err := controller.GetControllerCapabilities(context.Background(), database, client, name, ctrlURL)
		if err != nil {
			return ctrlInfoLoaded{name: name, err: err}
		}
		return ctrlInfoLoaded{name: name, caps: caps}
	}
}

// filteredControllers applies the search box filter to the full controller list.
func (s ControllerScreen) filteredControllers() []ctrlEntry {
	if !s.search.Active() && s.search.Query() == "" {
		return s.ctrls
	}
	out := make([]ctrlEntry, 0, len(s.ctrls))
	for _, c := range s.ctrls {
		if s.search.Matches(c.Name + " " + c.Description + " " + c.Class) {
			out = append(out, c)
		}
	}
	return out
}

func buildControllerRows(ctrls []ctrlEntry, active string) ([][]components.Cell, []string) {
	rows := make([][]components.Cell, len(ctrls))
	keys := make([]string, len(ctrls))
	for i, c := range ctrls {
		indicator := " "
		indicatorColor := ""
		if c.Name == active {
			indicator = theme.SymSelected
			indicatorColor = theme.ColorSuccess
		}
		statusText := "online"
		statusColor := theme.ColorSuccess
		if c.Offline {
			statusText = "offline"
			statusColor = theme.ColorWarning
		}
		rows[i] = []components.Cell{
			{Text: indicator, Color: indicatorColor},
			{Text: c.Name},
			{Text: typeLabelController(c.Class), Color: theme.ColorBlue},
			{Text: c.URL, Dim: true},
			{Text: statusText, Color: statusColor},
		}
		keys[i] = c.Name
	}
	return rows, keys
}

// typeLabelController mirrors typeLabel() in controller/screen.tsx: the last
// dot-separated segment of the Jenkins _class name, or "Unknown" if blank.
func typeLabelController(class string) string {
	if class == "" {
		return "Unknown"
	}
	if strings.Contains(class, "ManagedMaster") {
		return "Managed Master"
	}
	if strings.Contains(class, "ConnectedMaster") {
		return "Connected Master"
	}
	if idx := strings.LastIndex(class, "."); idx >= 0 {
		return class[idx+1:]
	}
	return class
}

// current returns the controller entry at the table cursor, or nil.
func (s ControllerScreen) current() *ctrlEntry {
	filtered := s.filteredControllers()
	i := s.table.Cursor()
	if i < 0 || i >= len(filtered) {
		return nil
	}
	return &filtered[i]
}

// Update handles messages and key input.
func (s ControllerScreen) Update(msg tea.Msg) (ControllerScreen, tea.Cmd) {
	if s.detail.Visible() {
		var cmd tea.Cmd
		s.detail, cmd = s.detail.Update(msg)
		return s, cmd
	}
	if s.search.Editing() {
		var cmd tea.Cmd
		s.search, cmd = s.search.Update(msg)
		if !s.search.Editing() {
			rows, keys := buildControllerRows(s.filteredControllers(), s.activeName)
			s.table.SetRows(rows, keys)
		}
		return s, cmd
	}

	switch msg := msg.(type) {
	case controllersLoaded:
		s.loading = false
		s.err = msg.err
		if msg.err == nil {
			s.ctrls = msg.ctrls
			rows, keys := buildControllerRows(s.filteredControllers(), s.activeName)
			s.table.SetRows(rows, keys)
		}
		return s, nil

	case ctrlSelectDone:
		if msg.err != nil {
			s.detail.Show("Error", msg.err.Error())
		} else {
			s.activeName = msg.name
			s.detail.Show("Selected", fmt.Sprintf("Active controller: %s", msg.name))
			rows, keys := buildControllerRows(s.filteredControllers(), s.activeName)
			s.table.SetRows(rows, keys)
		}
		return s, nil

	case ctrlInfoLoaded:
		if msg.err != nil {
			s.detail.Show("Error", msg.err.Error())
			return s, nil
		}
		body := fmt.Sprintf(
			"Type: %s\nCan create job:  %v\nCan create node: %v\nCan create cred: %v",
			msg.caps.TypeLabel, msg.caps.CanCreateJob, msg.caps.CanCreateNode, msg.caps.CanCreateCred,
		)
		s.detail.Show("Controller: "+msg.name, body)
		return s, nil

	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.table.SetSize(msg.Width, maxInt(5, msg.Height-12))
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "/":
			s.search.Open()
			return s, nil
		case "enter":
			c := s.current()
			if c == nil {
				return s, nil
			}
			return s, s.doFetchInfo(c.Name, c.URL)
		case "s":
			c := s.current()
			if c == nil {
				return s, nil
			}
			return s, s.doSelect(c.Name, c.URL)
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
	if sv := s.search.View(); sv != "" {
		sb.WriteString(sv + "\n")
	}
	sb.WriteString(s.table.View())
	sb.WriteString("\n")

	if c := s.current(); c != nil {
		sb.WriteString("\n")
		sb.WriteString(theme.StyleTitle.Render(c.Name))
		sb.WriteString("  ")
		if c.Offline {
			sb.WriteString(theme.StyleWarning.Render(theme.SymOffline + " offline"))
		} else {
			sb.WriteString(theme.StyleSuccess.Render(theme.SymOnline + " online"))
		}
		if c.Name == s.activeName {
			sb.WriteString(theme.StyleTitle.Render("  " + theme.SymSelected + " active"))
		}
		sb.WriteString("\n")
		sb.WriteString(theme.StyleDim.Render("type ") + theme.StyleBlue.Render(typeLabelController(c.Class)))
		sb.WriteString("\n")
		if c.URL != "" {
			sb.WriteString(theme.StyleSubtle.Render(c.URL))
			sb.WriteString("\n")
		}
		if c.Description != "" {
			sb.WriteString(theme.StyleDim.Render(c.Description))
			sb.WriteString("\n")
		}
	}

	sb.WriteString(theme.StyleDim.Render("Enter info  ·  s select  ·  ↑↓ move  ·  r refresh  ·  / search"))
	return sb.String()
}
