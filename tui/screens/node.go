package screens

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/plugins/controller"
	nodepkg "bee/plugins/node"
	"bee/tui/components"
	"bee/tui/theme"
)

// nodeEntry is a single node row.
type nodeEntry struct {
	Name        string
	DisplayName string
	Offline     bool
	Executors   int
	Labels      string
	Description string
}

type nodesLoaded struct {
	nodes []nodeEntry
	err   error
}

type nodeActionDone struct{ err error }

// nodeConfigLoaded carries the parsed config.xml digest for the highlighted
// node's detail panel (fetch-on-cursor-move, cached by caller).
type nodeConfigLoaded struct {
	name string
	cfg  nodepkg.NodeConfig
	ok   bool
}

// NodeScreen is the TUI screen for listing/managing Jenkins nodes.
type NodeScreen struct {
	db      *sql.DB
	dbPath  string
	table   components.DataTable
	search  components.SearchBox
	modal   components.ConfirmModal
	detail  components.MessageModal
	loading bool
	err     error
	nodes   []nodeEntry
	width   int
	height  int
	pending string // name of node targeted for action
	action  string // "delete" | "toggle"

	// detail panel: config.xml digest for the highlighted node
	nodeConfig      *nodepkg.NodeConfig
	nodeConfigCache map[string]nodepkg.NodeConfig
}

// NewNodeScreen creates a new NodeScreen.
func NewNodeScreen(db *sql.DB, dbPath string) NodeScreen {
	cols := []components.Column{
		{Header: "Status", Width: 10},
		{Header: "Name", Width: 32, Flex: true},
		{Header: "Exec", Width: 6},
		{Header: "Labels", Width: 24, Flex: true},
		{Header: "Description", Width: 22, Flex: true},
	}
	return NodeScreen{
		db:              db,
		dbPath:          dbPath,
		table:           components.NewDataTable(cols),
		loading:         true,
		nodeConfigCache: make(map[string]nodepkg.NodeConfig),
	}
}

// Init fires the initial data fetch.
func (s NodeScreen) Init() tea.Cmd {
	return s.fetchNodes()
}

// InputCaptured reports whether the confirm modal, detail message, or search
// box is currently capturing input, meaning this screen wants raw keys routed
// to it instead of being intercepted by the app shell for tab-switching/quit.
func (s NodeScreen) InputCaptured() bool {
	return s.modal.Visible() || s.detail.Visible() || s.search.Editing()
}

func (s NodeScreen) fetchNodes() tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodesLoaded{err: err}
		}
		rawNodes, err := nodepkg.ListNodes(context.Background(), client)
		if err != nil {
			return nodesLoaded{err: err}
		}
		nodes := make([]nodeEntry, 0, len(rawNodes))
		for _, n := range rawNodes {
			nodes = append(nodes, nodeEntry{
				Name:        n.DisplayName,
				DisplayName: n.DisplayName,
				Offline:     n.Offline,
				Executors:   n.NumExecutors,
				Labels:      n.Labels,
				Description: n.Description,
			})
		}
		return nodesLoaded{nodes: nodes}
	}
}

func (s NodeScreen) doDeleteNode(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodeActionDone{err: err}
		}
		if err := nodepkg.DeleteNode(context.Background(), client, name); err != nil {
			return nodeActionDone{err: err}
		}
		return nodeActionDone{}
	}
}

func (s NodeScreen) doToggleOffline(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodeActionDone{err: err}
		}
		if err := nodepkg.ToggleOffline(context.Background(), client, name, ""); err != nil {
			return nodeActionDone{err: err}
		}
		return nodeActionDone{}
	}
}

// fetchNodeConfig loads and parses a node's config.xml for the detail panel.
func (s NodeScreen) fetchNodeConfig(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodeConfigLoaded{name: name}
		}
		xmlStr, err := nodepkg.GetNodeConfigXML(context.Background(), client, name)
		if err != nil {
			return nodeConfigLoaded{name: name}
		}
		return nodeConfigLoaded{name: name, cfg: nodepkg.ParseNodeConfig(xmlStr), ok: true}
	}
}

// filteredNodes applies the search box filter to the full node list.
func (s NodeScreen) filteredNodes() []nodeEntry {
	if s.search.Query() == "" {
		return s.nodes
	}
	out := make([]nodeEntry, 0, len(s.nodes))
	for _, n := range s.nodes {
		if s.search.Matches(n.DisplayName + " " + n.Labels + " " + n.Description) {
			out = append(out, n)
		}
	}
	return out
}

func buildNodeRows(nodes []nodeEntry) ([][]components.Cell, []string) {
	rows := make([][]components.Cell, len(nodes))
	keys := make([]string, len(nodes))
	for i, n := range nodes {
		statusText := theme.SymOnline + " on"
		statusColor := theme.ColorSuccess
		if n.Offline {
			statusText = theme.SymOffline + " off"
			statusColor = theme.ColorWarning
		}
		rows[i] = []components.Cell{
			{Text: statusText, Color: statusColor},
			{Text: n.DisplayName},
			{Text: strconv.Itoa(n.Executors)},
			{Text: n.Labels},
			{Text: n.Description},
		}
		keys[i] = n.Name
	}
	return rows, keys
}

// current returns the node entry at the table cursor, or nil.
func (s NodeScreen) current() *nodeEntry {
	filtered := s.filteredNodes()
	i := s.table.Cursor()
	if i < 0 || i >= len(filtered) {
		return nil
	}
	return &filtered[i]
}

// Update handles messages and key input.
func (s NodeScreen) Update(msg tea.Msg) (NodeScreen, tea.Cmd) {
	if s.modal.Visible() {
		var cmd tea.Cmd
		s.modal, cmd = s.modal.Update(msg)
		return s, cmd
	}
	if s.detail.Visible() {
		var cmd tea.Cmd
		s.detail, cmd = s.detail.Update(msg)
		return s, cmd
	}
	if s.search.Editing() {
		var cmd tea.Cmd
		prevQuery := s.search.Query()
		s.search, cmd = s.search.Update(msg)
		if s.search.Query() != prevQuery || !s.search.Editing() {
			rows, keys := buildNodeRows(s.filteredNodes())
			s.table.SetRows(rows, keys)
		}
		return s, cmd
	}

	switch msg := msg.(type) {
	case nodesLoaded:
		s.loading = false
		s.err = msg.err
		if msg.err == nil {
			s.nodes = msg.nodes
			rows, keys := buildNodeRows(s.filteredNodes())
			s.table.SetRows(rows, keys)
		}
		return s, s.maybeFetchDetail()

	case nodeConfigLoaded:
		if msg.ok {
			cfg := msg.cfg
			s.nodeConfigCache[msg.name] = cfg
			if c := s.current(); c != nil && c.Name == msg.name {
				s.nodeConfig = &cfg
			}
		}
		return s, nil

	case nodeActionDone:
		if msg.err != nil {
			s.detail.Show("Error", msg.err.Error())
		} else {
			s.detail.Show("Done", fmt.Sprintf("Action on '%s' completed.", s.pending))
			delete(s.nodeConfigCache, s.pending)
			s.pending = ""
			s.action = ""
			return s, s.fetchNodes()
		}
		return s, nil

	case components.ConfirmResultMsg:
		if msg.Yes && s.pending != "" {
			switch s.action {
			case "delete":
				return s, s.doDeleteNode(s.pending)
			case "toggle":
				return s, s.doToggleOffline(s.pending)
			}
		}
		s.pending = ""
		s.action = ""
		return s, nil

	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.table.SetSize(msg.Width, maxInt(5, msg.Height-12))
		s.modal.SetWidth(msg.Width)
		s.detail.SetWidth(msg.Width)
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "/":
			s.search.Open()
			return s, nil
		case "ctrl+d":
			if c := s.current(); c != nil {
				s.pending = c.Name
				s.action = "delete"
				s.modal.Show("Delete node", fmt.Sprintf("Delete node '%s'? This cannot be undone.", c.Name))
			}
			return s, nil
		case "ctrl+o":
			if c := s.current(); c != nil {
				s.pending = c.Name
				s.action = "toggle"
				s.modal.Show("Toggle offline", fmt.Sprintf("Toggle offline state of '%s'?", c.Name))
			}
			return s, nil
		case "r":
			s.loading = true
			return s, s.fetchNodes()
		}
	}

	before := s.table.Cursor()
	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	if s.table.Cursor() != before {
		return s, tea.Batch(cmd, s.maybeFetchDetail())
	}
	return s, cmd
}

// maybeFetchDetail serves the detail-panel config from cache, or kicks off a
// background fetch when the highlighted node hasn't been seen yet.
func (s *NodeScreen) maybeFetchDetail() tea.Cmd {
	c := s.current()
	if c == nil {
		s.nodeConfig = nil
		return nil
	}
	if cached, ok := s.nodeConfigCache[c.Name]; ok {
		cfg := cached
		s.nodeConfig = &cfg
		return nil
	}
	s.nodeConfig = nil
	return s.fetchNodeConfig(c.Name)
}

// View renders the node screen.
func (s NodeScreen) View() string {
	if s.modal.Visible() {
		return s.modal.View()
	}
	if s.detail.Visible() {
		return s.detail.View()
	}
	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(theme.SymOnline+" Nodes") + "\n")
	if s.loading {
		sb.WriteString(theme.StyleDim.Render(theme.SymLoading + " Loading nodes..."))
		return sb.String()
	}
	if s.err != nil {
		sb.WriteString(theme.StyleError.Render(theme.SymFail + " " + s.err.Error()))
		return sb.String()
	}
	if len(s.nodes) == 0 {
		sb.WriteString(theme.StyleDim.Render("No nodes. Press Ctrl+n to create one."))
		return sb.String()
	}
	if sv := s.search.View(); sv != "" {
		sb.WriteString(sv + "\n")
	}
	sb.WriteString(s.table.View())
	sb.WriteString("\n")

	if c := s.current(); c != nil {
		sb.WriteString("\n")
		sb.WriteString(theme.StyleTitle.Render(c.DisplayName))
		sb.WriteString("  ")
		if c.Offline {
			sb.WriteString(theme.StyleWarning.Render(theme.SymOffline + " offline"))
		} else {
			sb.WriteString(theme.StyleSuccess.Render(theme.SymOnline + " online"))
		}
		sb.WriteString(theme.StyleDim.Render(fmt.Sprintf("  exec %d", c.Executors)))
		sb.WriteString("\n")
		if s.nodeConfig != nil {
			cfg := s.nodeConfig
			sb.WriteString(theme.StyleDim.Render("launcher ") + cfg.LauncherType)
			if cfg.LauncherType == "ssh" && cfg.Host != "" {
				sb.WriteString(theme.StyleDim.Render("   host ") + fmt.Sprintf("%s:%d", cfg.Host, cfg.Port))
			}
			if cfg.RemoteDir != "" {
				sb.WriteString(theme.StyleDim.Render("   remote ") + cfg.RemoteDir)
			}
			sb.WriteString("\n")
		}
		if c.Labels != "" {
			sb.WriteString(theme.StyleDim.Render("labels " + c.Labels))
			sb.WriteString("\n")
		}
	}

	sb.WriteString(theme.StyleDim.Render("Enter menu  ·  ^D delete  ·  ^O offline  ·  r refresh  ·  / search"))
	return sb.String()
}
