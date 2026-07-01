// Package tui is the main Bubbletea TUI application for bee.
package tui

import (
	"database/sql"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"bee/tui/screens"
	"bee/tui/theme"
)

// Tab names.
var tabNames = []string{"Jobs", "Nodes", "Credentials", "Controllers"}

// App is the root TUI model.
type App struct {
	db         *sql.DB
	dbPath     string
	tabs       []string
	activeTab  int
	jobScreen  screens.JobScreen
	nodeScreen screens.NodeScreen
	credScreen screens.CredScreen
	ctrlScreen screens.ControllerScreen
	width      int
	height     int
	quitting   bool
}

// NewApp creates and initializes the TUI application.
func NewApp(db *sql.DB, dbPath string) App {
	return App{
		db:         db,
		dbPath:     dbPath,
		tabs:       tabNames,
		activeTab:  0,
		jobScreen:  screens.NewJobScreen(db, dbPath),
		nodeScreen: screens.NewNodeScreen(db, dbPath),
		credScreen: screens.NewCredScreen(db, dbPath),
		ctrlScreen: screens.NewControllerScreen(db, dbPath, ""),
	}
}

// Init fires all screen init commands in parallel.
func (a App) Init() tea.Cmd {
	return tea.Batch(
		a.jobScreen.Init(),
		a.nodeScreen.Init(),
		a.credScreen.Init(),
		a.ctrlScreen.Init(),
	)
}

// Update routes messages to the active screen and handles global keys.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.jobScreen, _ = a.jobScreen.Update(msg)
		a.nodeScreen, _ = a.nodeScreen.Update(msg)
		a.credScreen, _ = a.credScreen.Update(msg)
		a.ctrlScreen, _ = a.ctrlScreen.Update(msg)
		return a, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "ctrl+c":
			a.quitting = true
			return a, tea.Quit
		case "tab", "right":
			a.activeTab = (a.activeTab + 1) % len(a.tabs)
			return a, nil
		case "shift+tab", "left":
			a.activeTab = (a.activeTab - 1 + len(a.tabs)) % len(a.tabs)
			return a, nil
		case "1":
			a.activeTab = 0
			return a, nil
		case "2":
			a.activeTab = 1
			return a, nil
		case "3":
			a.activeTab = 2
			return a, nil
		case "4":
			a.activeTab = 3
			return a, nil
		}
		return a.delegateKey(msg)
	}

	// Route non-key messages to all screens (async fetches).
	var cmds []tea.Cmd
	var cmd tea.Cmd

	a.jobScreen, cmd = a.jobScreen.Update(msg)
	cmds = append(cmds, cmd)
	a.nodeScreen, cmd = a.nodeScreen.Update(msg)
	cmds = append(cmds, cmd)
	a.credScreen, cmd = a.credScreen.Update(msg)
	cmds = append(cmds, cmd)
	a.ctrlScreen, cmd = a.ctrlScreen.Update(msg)
	cmds = append(cmds, cmd)

	return a, tea.Batch(cmds...)
}

func (a App) delegateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch a.activeTab {
	case 0:
		a.jobScreen, cmd = a.jobScreen.Update(msg)
	case 1:
		a.nodeScreen, cmd = a.nodeScreen.Update(msg)
	case 2:
		a.credScreen, cmd = a.credScreen.Update(msg)
	case 3:
		a.ctrlScreen, cmd = a.ctrlScreen.Update(msg)
	}
	return a, cmd
}

// View renders the full TUI.
func (a App) View() string {
	if a.quitting {
		return ""
	}

	var sb strings.Builder

	// Tab bar.
	sb.WriteString(renderTabBar(a.tabs, a.activeTab, a.width))
	sb.WriteString("\n")

	// Separator.
	sepWidth := a.width
	if sepWidth < 2 {
		sepWidth = 80
	}
	sb.WriteString(theme.StyleSubtle.Render(strings.Repeat(theme.SymSep, sepWidth)))
	sb.WriteString("\n")

	// Active screen body.
	bodyHeight := a.height - 4
	if bodyHeight < 5 {
		bodyHeight = 5
	}
	body := a.activeScreenView()
	lines := strings.Split(body, "\n")
	if len(lines) > bodyHeight {
		lines = lines[:bodyHeight]
	}
	sb.WriteString(strings.Join(lines, "\n"))
	sb.WriteString("\n")

	// Status bar.
	sb.WriteString(renderStatusBar(a.activeTab, a.width))

	return sb.String()
}

func (a App) activeScreenView() string {
	switch a.activeTab {
	case 0:
		return a.jobScreen.View()
	case 1:
		return a.nodeScreen.View()
	case 2:
		return a.credScreen.View()
	case 3:
		return a.ctrlScreen.View()
	}
	return ""
}

func renderTabBar(tabs []string, active, width int) string {
	var parts []string
	for i, name := range tabs {
		num := fmt.Sprintf("%d:", i+1)
		if i == active {
			parts = append(parts, theme.StyleKeyHint.Render(num)+theme.StyleTabActive.Render(name+" ▾"))
		} else {
			parts = append(parts, theme.StyleTabInactive.Render(num+name))
		}
	}
	bar := "  " + strings.Join(parts, "  ")
	visLen := lipgloss.Width(bar)
	if width > visLen {
		bar += strings.Repeat(" ", width-visLen)
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color(theme.ColorHeaderBg)).
		Render(bar)
}

func renderStatusBar(activeTab, width int) string {
	hints := []string{
		theme.StyleKeyHint.Render("tab") + theme.StyleDim.Render(" next"),
		theme.StyleKeyHint.Render("^F") + theme.StyleDim.Render(" search"),
		theme.StyleKeyHint.Render("r") + theme.StyleDim.Render(" refresh"),
		theme.StyleKeyHint.Render("q") + theme.StyleDim.Render(" quit"),
	}
	switch activeTab {
	case 0:
		hints = append(hints,
			theme.StyleKeyHint.Render("^X")+theme.StyleDim.Render(" delete"),
			theme.StyleKeyHint.Render("enter")+theme.StyleDim.Render(" detail"),
		)
	case 1:
		hints = append(hints,
			theme.StyleKeyHint.Render("^O")+theme.StyleDim.Render(" toggle"),
			theme.StyleKeyHint.Render("^X")+theme.StyleDim.Render(" delete"),
		)
	case 2:
		hints = append(hints,
			theme.StyleKeyHint.Render("^X")+theme.StyleDim.Render(" delete"),
		)
	case 3:
		hints = append(hints,
			theme.StyleKeyHint.Render("enter")+theme.StyleDim.Render(" select"),
		)
	}
	bar := "  " + strings.Join(hints, "  ")
	return theme.StyleStatusBar.Width(width).Render(bar)
}

// Run launches the TUI program and blocks until the user quits.
func Run(db *sql.DB, dbPath string) error {
	app := NewApp(db, dbPath)
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
