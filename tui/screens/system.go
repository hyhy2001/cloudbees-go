package screens

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/internal/api"
	"bee/internal/cache"
	"bee/internal/session"
	"bee/plugins/controller"
	"bee/plugins/system"
	"bee/tui/theme"
)

// systemInfoLoaded carries the fetched system info snapshot.
type systemInfoLoaded struct {
	activeController string
	username         string
	version          string
	health           system.HealthResult
	healthErr        error
	plugins          []system.PluginInfo
	err              error // session/client resolution failure — nothing loaded
}

// cacheClearedMsg signals the cache-clear action completed.
type cacheClearedMsg struct{ err error }

// SystemScreen is the read-only TUI "Info" tab: version/health/plugins + cache clear.
type SystemScreen struct {
	db      *sql.DB
	dbPath  string
	version string
	loading bool
	info    *systemInfoLoaded
	notice  string
	width   int
	height  int
}

// NewSystemScreen creates a new SystemScreen.
func NewSystemScreen(database *sql.DB, dbPath string, cliVersion string) SystemScreen {
	return SystemScreen{
		db:      database,
		dbPath:  dbPath,
		version: cliVersion,
		loading: true,
	}
}

// Init fires the initial data fetch.
func (s SystemScreen) Init() tea.Cmd {
	return s.fetchInfo()
}

// InputCaptured always reports false — the System screen has no overlay,
// form, or menu that needs to capture raw key input.
func (s SystemScreen) InputCaptured() bool { return false }

func (s SystemScreen) fetchInfo() tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		name, _, _ := controller.GetActiveController(db)
		var username string
		if sess, err := session.LoadSession(db, dbPath); err == nil {
			username = sess.Profile.Username
		}
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return systemInfoLoaded{activeController: name, username: username, err: err}
		}
		return fetchSystemInfo(client, name, username)
	}
}

func fetchSystemInfo(client *api.Client, activeController, username string) systemInfoLoaded {
	ctx := context.Background()
	version := system.GetVersion(ctx, client)
	health, healthErr := system.HealthCheck(ctx, client)
	var plugins []system.PluginInfo
	if activeController != "" {
		plugins = system.GetInstalledPlugins(ctx, client)
	}
	return systemInfoLoaded{
		activeController: activeController,
		username:         username,
		version:          version,
		health:           health,
		healthErr:        healthErr,
		plugins:          plugins,
	}
}

func (s SystemScreen) doClearCache() tea.Cmd {
	db := s.db
	return func() tea.Msg {
		return cacheClearedMsg{err: cache.ClearAll(db)}
	}
}

// Update handles messages and key input.
func (s SystemScreen) Update(msg tea.Msg) (SystemScreen, tea.Cmd) {
	switch msg := msg.(type) {
	case systemInfoLoaded:
		s.loading = false
		s.info = &msg
		return s, nil

	case cacheClearedMsg:
		if msg.err != nil {
			s.notice = "Error clearing cache: " + msg.err.Error()
		} else {
			s.notice = "Cache cleared — refresh each tab with r to reload"
		}
		return s, nil

	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+x":
			s.notice = ""
			return s, s.doClearCache()
		case "r":
			s.loading = true
			s.notice = ""
			return s, s.fetchInfo()
		}
	}
	return s, nil
}

func fixWidth(s string, width int) string {
	if len(s) <= width {
		return s + strings.Repeat(" ", width-len(s))
	}
	if width <= 1 {
		return s[:width]
	}
	return s[:width-1] + "…"
}

// View renders the system info screen.
func (s SystemScreen) View() string {
	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(theme.SymGear + " Info"))
	sb.WriteString("\n\n")

	if s.loading {
		sb.WriteString(theme.StyleDim.Render(theme.SymLoading + " Loading system info..."))
		return sb.String()
	}

	info := s.info
	if info == nil {
		return sb.String()
	}

	sb.WriteString(theme.StyleDim.Render("CLI version:         ") + s.version + "\n")
	active := info.activeController
	if active == "" {
		sb.WriteString(theme.StyleDim.Render("Active controller:   (none)") + "\n")
	} else {
		sb.WriteString(theme.StyleDim.Render("Active controller:   ") + theme.StyleSuccess.Render(active) + "\n")
	}
	sb.WriteString(theme.StyleDim.Render("DB path:             ") + theme.StyleDim.Render(s.dbPath) + "\n")
	if info.username != "" {
		sb.WriteString(theme.StyleDim.Render("Logged in as:        ") + info.username + "\n")
	}

	if info.err != nil {
		sb.WriteString(theme.StyleWarning.Render(theme.SymWarn+" Not logged in — server info unavailable: ") + info.err.Error() + "\n")
	} else {
		sb.WriteString(theme.StyleDim.Render("Server version:      ") + info.version + "\n")
		if info.healthErr != nil {
			sb.WriteString(theme.StyleDim.Render("Health status:       ") + theme.StyleError.Render("ERROR") +
				theme.StyleDim.Render(" — "+info.healthErr.Error()) + "\n")
		} else {
			sb.WriteString(theme.StyleDim.Render("Health status:       ") + theme.StyleSuccess.Render("OK") + "\n")
			sb.WriteString(theme.StyleDim.Render("Mode:                ") + info.health.Mode + "\n")
			sb.WriteString(theme.StyleDim.Render("Executors:           ") + fmt.Sprint(info.health.NumExecutors) + "\n")
			if info.health.NodeDescription != "" {
				sb.WriteString(theme.StyleDim.Render("Description:         ") + theme.StyleDim.Render(info.health.NodeDescription) + "\n")
			}
		}

		if len(info.plugins) > 0 {
			sb.WriteString("\n")
			sb.WriteString(fmt.Sprintf(" %s Plugins (%d)\n", theme.SymArrow, len(info.plugins)))
			sb.WriteString(theme.StyleDim.Render("  " + fixWidth("Name", 36) + "  " + fixWidth("Version", 12) + "  Status\n"))
			max := len(info.plugins)
			if max > 15 {
				max = 15
			}
			for _, p := range info.plugins[:max] {
				status := "inactive"
				if p.Active {
					status = "active"
				}
				statusStyle := theme.StyleDim
				if p.Active {
					statusStyle = theme.StyleSuccess
				}
				sb.WriteString("  " + fixWidth(p.ShortName, 36) + "  " + theme.StyleDim.Render(fixWidth(p.Version, 12)) + "  " + statusStyle.Render(status) + "\n")
			}
			if len(info.plugins) > max {
				sb.WriteString(theme.StyleDim.Render(fmt.Sprintf("  ... and %d more\n", len(info.plugins)-max)))
			}
		} else if active != "" {
			sb.WriteString("\n" + theme.StyleWarning.Render(theme.SymWarn+" Plugin list requires admin/manage permissions on the Jenkins server") + "\n")
		}
	}

	if s.notice != "" {
		sb.WriteString("\n" + theme.StyleSuccess.Render(s.notice) + "\n")
	}

	sb.WriteString("\n" + theme.StyleDim.Render("Ctrl+x clear cache  ·  r refresh"))
	return sb.String()
}
