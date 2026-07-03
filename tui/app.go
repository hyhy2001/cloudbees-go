// Package tui is the main Bubbletea TUI application for bee.
package tui

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"bee/plugins/controller"
	"bee/tui/components"
	"bee/tui/screens"
	"bee/tui/theme"
)

// serverTypeMsg is fired on startup to tell the app whether the active server
// is an Operations Center (has Controllers) or a plain Jenkins/Managed Controller.
type serverTypeMsg struct{ isOC bool }

func detectServerType(db *sql.DB, dbPath string) tea.Cmd {
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return serverTypeMsg{isOC: false}
		}
		var result struct {
			Class string `json:"_class"`
		}
		if err := client.GetJSON(context.Background(), "/api/json?tree=_class", &result); err != nil {
			return serverTypeMsg{isOC: false}
		}
		// Operations Center classes contain "opscenter" or "cjoc" (case-insensitive)
		lower := strings.ToLower(result.Class)
		isOC := strings.Contains(lower, "opscenter") || strings.Contains(lower, "cjoc")
		return serverTypeMsg{isOC: isOC}
	}
}

// Tab names — Controllers tab is always in the list but hidden on plain Jenkins.
var allTabNames = []string{"Jobs", "Nodes", "Credentials", "Controllers", "Info"}

const controllerTabIndex = 3

// inputCapturer is implemented by screens that can report whether they
// currently own an overlay/form/menu that needs raw key input — while true,
// the app shell must not steal digit/tab/quit keys for global navigation.
type inputCapturer interface {
	InputCaptured() bool
}

// App is the root TUI model.
type App struct {
	db         *sql.DB
	dbPath     string
	tabs       []string // visible tab names (may exclude Controllers)
	tabMap     []int    // tabMap[visible index] = allTabNames index
	activeTab  int      // index into tabs (visible)
	jobScreen  screens.JobScreen
	nodeScreen screens.NodeScreen
	credScreen screens.CredScreen
	ctrlScreen screens.ControllerScreen
	sysScreen  screens.SystemScreen
	width      int
	height     int
	quitting   bool
	isOC       bool // true = server is Operations Center, show Controllers tab
	help       components.HelpOverlay
	cmdLog     components.CommandLog
	toast      components.Toast
}

// activeInputCaptured reports whether the currently active screen owns an
// overlay/form/menu that wants raw keys — in that case the app-global
// tab-switch/digit-jump/quit bindings must not intercept the keystroke.
func (a App) activeInputCaptured() bool {
	if a.help.Visible() {
		return true
	}
	switch a.allTabIndex() {
	case 0:
		return a.jobScreen.InputCaptured()
	case 1:
		return a.nodeScreen.InputCaptured()
	case 2:
		return a.credScreen.InputCaptured()
	case 3:
		return a.ctrlScreen.InputCaptured()
	case 4:
		return a.sysScreen.InputCaptured()
	}
	return false
}

// NewApp creates and initializes the TUI application.
func NewApp(db *sql.DB, dbPath, version string) App {
	a := App{
		db:         db,
		dbPath:     dbPath,
		activeTab:  0,
		jobScreen:  screens.NewJobScreen(db, dbPath),
		nodeScreen: screens.NewNodeScreen(db, dbPath),
		credScreen: screens.NewCredScreen(db, dbPath),
		ctrlScreen: screens.NewControllerScreen(db, dbPath, ""),
		sysScreen:  screens.NewSystemScreen(db, dbPath, version),
	}
	a.rebuildTabs(false)
	a.help.Title = "Keyboard Shortcuts"
	a.help.SetEntries(
		[]string{"Global", "Jobs", "Nodes", "Credentials", "Controllers"},
		[][]components.HelpEntry{
			{
				{Key: "←→ / Tab", Desc: "switch tab"},
				{Key: "1–5", Desc: "jump to tab"},
				{Key: "?", Desc: "toggle help"},
				{Key: "L", Desc: "toggle command log"},
				{Key: "^Q", Desc: "quit"},
			},
			{
				{Key: "↑↓", Desc: "navigate"},
				{Key: "/", Desc: "search"},
				{Key: "Enter", Desc: "open menu"},
				{Key: "^N", Desc: "new job"},
				{Key: "^D", Desc: "delete"},
				{Key: "r", Desc: "refresh"},
			},
			{
				{Key: "↑↓", Desc: "navigate"},
				{Key: "/", Desc: "search"},
				{Key: "Enter", Desc: "open menu"},
				{Key: "^N", Desc: "new node"},
				{Key: "^D", Desc: "delete"},
				{Key: "^O", Desc: "toggle offline"},
				{Key: "r", Desc: "refresh"},
			},
			{
				{Key: "↑↓", Desc: "navigate"},
				{Key: "/", Desc: "search"},
				{Key: "Enter", Desc: "open menu"},
				{Key: "^N", Desc: "new credential"},
				{Key: "^D", Desc: "delete"},
				{Key: "r", Desc: "refresh"},
			},
			{
				{Key: "↑↓", Desc: "navigate"},
				{Key: "Enter", Desc: "view info"},
				{Key: "s", Desc: "select controller"},
				{Key: "r", Desc: "refresh"},
			},
		},
	)
	return a
}

// rebuildTabs rebuilds the visible tab list based on isOC.
func (a *App) rebuildTabs(isOC bool) {
	a.isOC = isOC
	a.tabs = nil
	a.tabMap = nil
	for i, name := range allTabNames {
		if i == controllerTabIndex && !isOC {
			continue
		}
		a.tabs = append(a.tabs, name)
		a.tabMap = append(a.tabMap, i)
	}
}

// allTabIndex returns the allTabNames index for the current activeTab.
func (a App) allTabIndex() int {
	if a.activeTab >= 0 && a.activeTab < len(a.tabMap) {
		return a.tabMap[a.activeTab]
	}
	return 0
}

// Init fires all screen init commands in parallel.
func (a App) Init() tea.Cmd {
	return tea.Batch(
		detectServerType(a.db, a.dbPath),
		a.jobScreen.Init(),
		a.nodeScreen.Init(),
		a.credScreen.Init(),
		a.ctrlScreen.Init(),
		a.sysScreen.Init(),
	)
}

// Update routes messages to the active screen and handles global keys.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case serverTypeMsg:
		prevIsOC := a.isOC
		a.rebuildTabs(msg.isOC)
		// Clamp activeTab if tab count shrank.
		if a.activeTab >= len(a.tabs) {
			a.activeTab = len(a.tabs) - 1
		}
		_ = prevIsOC
		return a, nil

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.help.Width = msg.Width
		a.cmdLog.Width = msg.Width
		a.toast.Width = msg.Width
		a.jobScreen, _ = a.jobScreen.Update(msg)
		a.nodeScreen, _ = a.nodeScreen.Update(msg)
		a.credScreen, _ = a.credScreen.Update(msg)
		a.ctrlScreen, _ = a.ctrlScreen.Update(msg)
		a.sysScreen, _ = a.sysScreen.Update(msg)
		return a, nil

	case tea.KeyMsg:
		captured := a.activeInputCaptured()
		if !captured {
			switch msg.String() {
			case "ctrl+q", "q", "Q", "ctrl+c":
				a.quitting = true
				return a, tea.Quit
			case "?":
				a.help.Toggle()
				return a, nil
			case "L":
				a.cmdLog.Toggle()
				return a, nil
			case "tab", "right":
				a.activeTab = (a.activeTab + 1) % len(a.tabs)
				return a, nil
			case "shift+tab", "left":
				a.activeTab = (a.activeTab - 1 + len(a.tabs)) % len(a.tabs)
				return a, nil
			}
			// Numeric shortcuts: 1–5 map to visible tab indices.
			for i := range a.tabs {
				if msg.String() == fmt.Sprintf("%d", i+1) {
					a.activeTab = i
					return a, nil
				}
			}
		}
		// Route key to help overlay first when it is visible.
		if a.help.Visible() {
			a.help, _ = a.help.Update(msg)
			return a, nil
		}
		return a.delegateKey(msg)
	}

	// Route non-key messages to all screens (async fetches) and app-level components.
	var cmds []tea.Cmd
	var cmd tea.Cmd

	a.toast, _ = a.toast.Update(msg)
	a.cmdLog, _ = a.cmdLog.Update(msg)

	a.jobScreen, cmd = a.jobScreen.Update(msg)
	cmds = append(cmds, cmd)
	a.nodeScreen, cmd = a.nodeScreen.Update(msg)
	cmds = append(cmds, cmd)
	a.credScreen, cmd = a.credScreen.Update(msg)
	cmds = append(cmds, cmd)
	a.ctrlScreen, cmd = a.ctrlScreen.Update(msg)
	cmds = append(cmds, cmd)
	a.sysScreen, cmd = a.sysScreen.Update(msg)
	cmds = append(cmds, cmd)

	return a, tea.Batch(cmds...)
}

func (a App) delegateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch a.allTabIndex() {
	case 0:
		a.jobScreen, cmd = a.jobScreen.Update(msg)
	case 1:
		a.nodeScreen, cmd = a.nodeScreen.Update(msg)
	case 2:
		a.credScreen, cmd = a.credScreen.Update(msg)
	case 3:
		a.ctrlScreen, cmd = a.ctrlScreen.Update(msg)
	case 4:
		a.sysScreen, cmd = a.sysScreen.Update(msg)
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

	// Overlays take priority over the screen body when visible.
	if v := a.help.View(); v != "" {
		sb.WriteString(v)
		sb.WriteString("\n")
		sb.WriteString(renderStatusBar(a.allTabIndex(), a.width))
		return sb.String()
	}

	body := a.activeScreenView()
	lines := strings.Split(body, "\n")
	if len(lines) > bodyHeight {
		lines = lines[:bodyHeight]
	}
	sb.WriteString(strings.Join(lines, "\n"))
	sb.WriteString("\n")

	// App-level chrome: toast and command log rendered below the body.
	if v := a.toast.View(); v != "" {
		sb.WriteString(v)
		sb.WriteString("\n")
	}
	if v := a.cmdLog.View(); v != "" {
		sb.WriteString(v)
		sb.WriteString("\n")
	}

	// Status bar.
	sb.WriteString(renderStatusBar(a.allTabIndex(), a.width))

	return sb.String()
}

func (a App) activeScreenView() string {
	switch a.allTabIndex() {
	case 0:
		return a.jobScreen.View()
	case 1:
		return a.nodeScreen.View()
	case 2:
		return a.credScreen.View()
	case 3:
		return a.ctrlScreen.View()
	case 4:
		return a.sysScreen.View()
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
	// Global hints — always visible (right side in TS, merged here)
	hints := []string{
		theme.StyleKeyHint.Render("←→/Tab") + theme.StyleDim.Render(" tab"),
		theme.StyleKeyHint.Render("L") + theme.StyleDim.Render(" log"),
		theme.StyleKeyHint.Render("?") + theme.StyleDim.Render(" help"),
		theme.StyleKeyHint.Render("^Q") + theme.StyleDim.Render(" quit"),
	}
	switch activeTab {
	case 0: // Jobs
		hints = append(hints,
			theme.StyleKeyHint.Render("Enter")+theme.StyleDim.Render(" menu"),
			theme.StyleKeyHint.Render("^N")+theme.StyleDim.Render(" new"),
			theme.StyleKeyHint.Render("^D")+theme.StyleDim.Render(" delete"),
			theme.StyleKeyHint.Render("r")+theme.StyleDim.Render(" refresh"),
		)
	case 1: // Nodes
		hints = append(hints,
			theme.StyleKeyHint.Render("Enter")+theme.StyleDim.Render(" menu"),
			theme.StyleKeyHint.Render("^N")+theme.StyleDim.Render(" new"),
			theme.StyleKeyHint.Render("^D")+theme.StyleDim.Render(" delete"),
			theme.StyleKeyHint.Render("^O")+theme.StyleDim.Render(" offline"),
			theme.StyleKeyHint.Render("r")+theme.StyleDim.Render(" refresh"),
		)
	case 2: // Credentials
		hints = append(hints,
			theme.StyleKeyHint.Render("Enter")+theme.StyleDim.Render(" menu"),
			theme.StyleKeyHint.Render("^N")+theme.StyleDim.Render(" new"),
			theme.StyleKeyHint.Render("^D")+theme.StyleDim.Render(" delete"),
			theme.StyleKeyHint.Render("r")+theme.StyleDim.Render(" refresh"),
		)
	case 3: // Controllers
		hints = append(hints,
			theme.StyleKeyHint.Render("Enter")+theme.StyleDim.Render(" info"),
			theme.StyleKeyHint.Render("s")+theme.StyleDim.Render(" select"),
			theme.StyleKeyHint.Render("r")+theme.StyleDim.Render(" refresh"),
		)
	case 4: // System
		hints = append(hints,
			theme.StyleKeyHint.Render("^X")+theme.StyleDim.Render(" clear cache"),
			theme.StyleKeyHint.Render("r")+theme.StyleDim.Render(" refresh"),
		)
	}
	bar := "  " + strings.Join(hints, "  ")
	return theme.StyleStatusBar.Width(width).Render(bar)
}

// Run launches the TUI program and blocks until the user quits.
func Run(db *sql.DB, dbPath, version string) error {
	app := NewApp(db, dbPath, version)
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
