package screens

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"bee/plugins/controller"
	internaldb "bee/internal/db"
	nodepkg "bee/plugins/node"
	"bee/tui/components"
	"bee/tui/theme"
)

// nodeAutoRefreshMsg is the tick for the node screen's auto-refresh.
type nodeAutoRefreshMsg struct{}

// nodeGrantsLoaded carries approved folders for GrantListOverlay.
type nodeGrantsLoaded struct {
	nodeName string
	items    []components.GrantItem
	err      error
}

// nodeGrantActionDone reports result of approve/revoke.
type nodeGrantActionDone struct {
	nodeName string
	err      error
}

// nodeEntry is a single node row.
type nodeEntry struct {
	Name        string
	DisplayName string
	Offline     bool
	Executors   int
	Labels      string
	Description string
	Gone        bool // synthesized placeholder for a tracked node missing on the server
}

type nodesLoaded struct {
	nodes   []nodeEntry
	tracked map[string]bool
	baseURL string
	err     error
}

type nodeActionDone struct {
	label string
	err   error
}

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
	form    formOverlay
	menu    menuOverlay
	loading bool
	err     error
	nodes   []nodeEntry
	width   int
	height  int
	pending string // name of node targeted for action
	action  string // "delete" | "toggle"
	activeNode string // name being edited

	autoRefresh    bool
	nodeFormIntent string // "create" | "edit"
	grantList      components.GrantListOverlay
	grantNodeName  string // node currently shown in grantList
	approveInput   formOverlay // single-field "folder name" form for adding approval
	showAll        bool
	tracked        map[string]bool
	baseURL        string // active controller URL for track/untrack

	// detail panel: config.xml digest for the highlighted node
	nodeConfig      *nodepkg.NodeConfig
	nodeConfigCache map[string]nodepkg.NodeConfig

	pendingBulkDelete bool // confirm targets the multi-selection
	refresh           components.AutoRefresh
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
	tbl := components.NewDataTable(cols)
	tbl.SetSelectable(true)
	return NodeScreen{
		db:              db,
		dbPath:          dbPath,
		table:           tbl,
		loading:         true,
		showAll:         internaldb.GetScopeShowAll(db, "node"),
		nodeConfigCache: make(map[string]nodepkg.NodeConfig),
	}
}

// Init fires the initial data fetch.
func (s NodeScreen) Init() tea.Cmd {
	return s.fetchNodes()
}

// InputCaptured reports whether any overlay is capturing input.
func (s NodeScreen) InputCaptured() bool {
	return s.modal.Visible() || s.detail.Visible() || s.search.Editing() ||
		s.form.Visible() || s.menu.Visible() || s.grantList.Visible() || s.approveInput.Visible()
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
		profileName := controller.GetActiveProfileName(db)
		trackedNames, _ := internaldb.ListTracked(db, "node", profileName, client.BaseURL)
		tracked := make(map[string]bool, len(trackedNames))
		for _, n := range trackedNames {
			tracked[n] = true
		}
		return nodesLoaded{nodes: nodes, tracked: tracked, baseURL: client.BaseURL}
	}
}

func (s NodeScreen) doDeleteNode(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodeActionDone{label: "delete", err: err}
		}
		if err := nodepkg.DeleteNode(context.Background(), client, name); err != nil {
			return nodeActionDone{label: "delete", err: err}
		}
		return nodeActionDone{label: "delete " + name}
	}
}

func (s NodeScreen) doBulkDeleteNodes(names []string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodeActionDone{label: "delete", err: err}
		}
		var failed int
		var firstErr error
		for _, n := range names {
			if e := nodepkg.DeleteNode(context.Background(), client, n); e != nil {
				failed++
				if firstErr == nil {
					firstErr = e
				}
			}
		}
		if firstErr != nil {
			return nodeActionDone{label: "delete", err: fmt.Errorf("%d of %d failed: %w", failed, len(names), firstErr)}
		}
		return nodeActionDone{label: fmt.Sprintf("delete %d nodes", len(names))}
	}
}

// selectionOrCursor returns the multi-selection when non-empty, else the cursor
// row's name as a one-element slice (empty when nothing is under the cursor).
func (s NodeScreen) selectionOrCursor() []string {
	if s.table.SelectedCount() > 0 {
		out := make([]string, 0, s.table.SelectedCount())
		for k := range s.table.Selected() {
			out = append(out, k)
		}
		return out
	}
	if c := s.current(); c != nil {
		return []string{c.Name}
	}
	return nil
}

func (s *NodeScreen) trackNames(names []string, track bool) {
	if s.baseURL == "" || len(names) == 0 {
		return
	}
	profileName := controller.GetActiveProfileName(s.db)
	if s.tracked == nil {
		s.tracked = make(map[string]bool)
	}
	for _, n := range names {
		if track {
			_ = internaldb.TrackResource(s.db, "node", n, profileName, s.baseURL)
			s.tracked[n] = true
		} else {
			_ = internaldb.UntrackResource(s.db, "node", n, profileName, s.baseURL)
			delete(s.tracked, n)
		}
	}
}

func (s *NodeScreen) rebuildRows() {
	rows, keys := buildNodeRows(s.filteredNodes(), s.tracked)
	s.table.SetRows(rows, keys)
}

func (s NodeScreen) doToggleOffline(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodeActionDone{label: "toggle", err: err}
		}
		if err := nodepkg.ToggleOffline(context.Background(), client, name, ""); err != nil {
			return nodeActionDone{label: "toggle", err: err}
		}
		return nodeActionDone{label: "toggle " + name}
	}
}

func (s NodeScreen) doCreateNode(opts nodepkg.CreateNodeOpts, name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodeActionDone{label: "create", err: err}
		}
		if err := nodepkg.CreateNode(context.Background(), client, name, opts); err != nil {
			return nodeActionDone{label: "create", err: err}
		}
		return nodeActionDone{label: "create " + name}
	}
}

func (s NodeScreen) doUpdateNode(name string, opts nodepkg.UpdateNodeOpts) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodeActionDone{label: "edit", err: err}
		}
		if err := nodepkg.UpdateNode(context.Background(), client, name, opts); err != nil {
			return nodeActionDone{label: "edit", err: err}
		}
		return nodeActionDone{label: "edit " + name}
	}
}

func (s *NodeScreen) nodeScheduleAutoRefresh() tea.Cmd {
	return tea.Tick(s.refresh.Next(), func(_ time.Time) tea.Msg { return nodeAutoRefreshMsg{} })
}

func (s NodeScreen) fetchNodeGrants(nodeName string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodeGrantsLoaded{nodeName: nodeName, err: err}
		}
		approved, err := nodepkg.ListApprovedFolders(context.Background(), client, nodeName)
		if err != nil {
			return nodeGrantsLoaded{nodeName: nodeName, err: err}
		}
		items := make([]components.GrantItem, len(approved))
		for i, a := range approved {
			items[i] = components.GrantItem{Label: a.FolderName, ID: a.TokenID}
		}
		return nodeGrantsLoaded{nodeName: nodeName, items: items}
	}
}

func (s NodeScreen) doApproveFolder(nodeName, folderName string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodeGrantActionDone{nodeName: nodeName, err: err}
		}
		err = nodepkg.ApproveFolder(context.Background(), client, nodeName, folderName)
		return nodeGrantActionDone{nodeName: nodeName, err: err}
	}
}

func (s NodeScreen) doRevokeGrant(nodeName, tokenID string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodeGrantActionDone{nodeName: nodeName, err: err}
		}
		err = nodepkg.DeleteAgentToken(context.Background(), client, nodeName, tokenID)
		return nodeGrantActionDone{nodeName: nodeName, err: err}
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

// filteredNodes applies search + Mine/All filter to the full node list.
func (s NodeScreen) filteredNodes() []nodeEntry {
	nodes := s.nodes
	if !s.showAll && len(s.tracked) > 0 {
		present := make(map[string]bool, len(nodes))
		out := make([]nodeEntry, 0, len(nodes))
		for _, n := range nodes {
			if s.tracked[n.Name] {
				out = append(out, n)
				present[n.Name] = true
			}
		}
		// Synthesize placeholders for tracked nodes missing from the server.
		for name := range s.tracked {
			if !present[name] {
				out = append(out, nodeEntry{Name: name, DisplayName: name, Gone: true})
			}
		}
		nodes = out
	}
	if s.search.Query() == "" {
		return nodes
	}
	out := make([]nodeEntry, 0, len(nodes))
	for _, n := range nodes {
		if s.search.Matches(n.DisplayName + " " + n.Labels + " " + n.Description) {
			out = append(out, n)
		}
	}
	return out
}

func buildNodeRows(nodes []nodeEntry, tracked map[string]bool) ([][]components.Cell, []string) {
	rows := make([][]components.Cell, len(nodes))
	keys := make([]string, len(nodes))
	for i, n := range nodes {
		if n.Gone {
			rows[i] = []components.Cell{
				{Text: "GONE", Color: theme.ColorError},
				{Text: "★ " + n.DisplayName, Color: theme.ColorError},
				{Text: "—", Dim: true},
				{Text: "", Dim: true},
				{Text: "deleted on server", Dim: true},
			}
			keys[i] = n.Name
			continue
		}
		statusText := theme.SymOnline + " on"
		statusColor := theme.ColorSuccess
		if n.Offline {
			statusText = theme.SymOffline + " off"
			statusColor = theme.ColorWarning
		}
		name := n.DisplayName
		if tracked[n.Name] {
			name = "★ " + name
		}
		rows[i] = []components.Cell{
			{Text: statusText, Color: statusColor},
			{Text: name},
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
	if s.approveInput.Visible() {
		var cmd tea.Cmd
		s.approveInput, cmd = s.approveInput.Update(msg)
		if sub, ok := msg.(FormSubmitMsg); ok {
			folderName := strGet(sub.Values, 0)
			if folderName != "" {
				return s, s.doApproveFolder(s.grantNodeName, folderName)
			}
		}
		return s, cmd
	}
	if s.grantList.Visible() {
		var cmd tea.Cmd
		s.grantList, cmd = s.grantList.Update(msg)
		switch m := msg.(type) {
		case components.GrantAddMsg:
			s.approveInput.Show("Approve Folder", []formField{
				{Label: "Folder name", Required: true, Placeholder: "my-folder"},
			})
		case components.GrantRevokeMsg:
			return s, s.doRevokeGrant(s.grantNodeName, m.Item.ID)
		case components.GrantRefreshMsg:
			s.grantList.SetItems(nil)
			s.grantList.Loaded = false
			return s, s.fetchNodeGrants(s.grantNodeName)
		case components.GrantCloseMsg:
			// already hidden by component
		}
		return s, cmd
	}
	if s.form.Visible() {
		var cmd tea.Cmd
		s.form, cmd = s.form.Update(msg)
		switch msg.(type) {
		case FormSubmitMsg:
			res := msg.(FormSubmitMsg)
			return s, tea.Batch(cmd, s.handleNodeFormSubmit(res.Values))
		case FormCancelMsg:
			s.nodeFormIntent = ""
		}
		return s, cmd
	}
	if s.menu.Visible() {
		var cmd tea.Cmd
		s.menu, cmd = s.menu.Update(msg)
		if sel, ok := msg.(MenuSelectMsg); ok {
			return s.handleNodeMenuSelect(sel.Index)
		}
		return s, cmd
	}
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
			s.rebuildRows()
		}
		return s, cmd
	}

	switch msg := msg.(type) {
	case nodeGrantsLoaded:
		if msg.nodeName == s.grantNodeName {
			if msg.err != nil {
				s.grantList.SetItems([]components.GrantItem{})
			} else {
				s.grantList.SetItems(msg.items)
			}
		}
		return s, nil

	case nodeGrantActionDone:
		if msg.err != nil {
			s.detail.Show("Error", msg.err.Error())
		} else if s.grantList.Visible() {
			// refresh the grant list
			s.grantList.Loaded = false
			s.grantList.Items = nil
			return s, s.fetchNodeGrants(s.grantNodeName)
		}
		return s, nil

	case nodesLoaded:
		s.loading = false
		s.err = msg.err
		if msg.err == nil {
			s.nodes = msg.nodes
			s.tracked = msg.tracked
			s.baseURL = msg.baseURL
			s.refresh.Reset()
			s.rebuildRows()
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
			s.detail.Show("Done", msg.label+" succeeded.")
			if strings.HasPrefix(msg.label, "delete") {
				delete(s.nodeConfigCache, s.pending)
				s.pending = ""
				s.action = ""
			}
			return s, s.fetchNodes()
		}
		return s, nil

	case components.ConfirmResultMsg:
		if msg.Yes && s.pendingBulkDelete && s.action == "delete" {
			s.pendingBulkDelete = false
			s.action = ""
			names := make([]string, 0, s.table.SelectedCount())
			for k := range s.table.Selected() {
				names = append(names, k)
			}
			s.table.ClearSelection()
			return s, s.doBulkDeleteNodes(names)
		}
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
		s.pendingBulkDelete = false
		return s, nil

	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.table.SetSize(msg.Width, maxInt(5, msg.Height-12))
		s.modal.SetWidth(msg.Width)
		s.detail.SetWidth(msg.Width)
		s.form.width = msg.Width
		s.menu.width = msg.Width
		s.grantList.Width = msg.Width
		s.approveInput.width = msg.Width
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+a":
			s.showAll = !s.showAll
			_ = internaldb.SetScopeShowAll(s.db, "node", s.showAll)
			s.rebuildRows()
			return s, nil
		case "esc":
			if s.table.SelectedCount() > 0 {
				s.table.ClearSelection()
			}
			return s, nil
		case "i":
			s.trackNames(s.selectionOrCursor(), true)
			s.table.ClearSelection()
			s.rebuildRows()
			return s, nil
		case "u":
			s.trackNames(s.selectionOrCursor(), false)
			s.table.ClearSelection()
			s.rebuildRows()
			return s, nil
		case "/":
			s.search.Open()
			return s, nil
		case "enter":
			if c := s.current(); c != nil && !c.Gone {
				s.activeNode = c.Name
				s.menu.Show("Node: "+c.Name, []string{"Edit", "Toggle Offline", "Manage Approvals", "Delete"})
			}
			return s, nil
		case "ctrl+n":
			s.nodeFormIntent = "create"
			s.form.Show("Create New Node", []formField{
				{Label: "Name", Required: true, Placeholder: "my-agent"},
				{Label: "Remote Dir", Required: true, Placeholder: "/home/jenkins"},
				{Label: "Executors", Value: "1"},
				{Label: "Labels", Placeholder: "linux docker"},
				{Label: "Description"},
				{Label: "Launcher", Value: "ssh", Options: []string{"ssh", "jnlp"}},
				{Label: "SSH Host", Placeholder: "192.168.1.100"},
				{Label: "SSH Port", Value: "22"},
				{Label: "Credential ID", Placeholder: "ssh-cred-id"},
				{Label: "Availability", Value: "always", Options: []string{"always", "demand"}},
				{Label: "In-demand Delay", Value: "0", Placeholder: "minutes (demand only)"},
				{Label: "Idle Delay", Value: "1", Placeholder: "minutes (demand only)"},
			})
			return s, nil
		case "ctrl+d":
			if n := s.table.SelectedCount(); n > 0 {
				s.action = "delete"
				s.pendingBulkDelete = true
				s.modal.Show("Delete nodes", fmt.Sprintf("Delete %d selected node(s)? This cannot be undone.", n))
				return s, nil
			}
			if c := s.current(); c != nil {
				s.pending = c.Name
				s.action = "delete"
				s.modal.Show("Delete node", fmt.Sprintf("Delete node '%s'? This cannot be undone.", c.Name))
			}
			return s, nil
		case "f", "F":
			s.autoRefresh = !s.autoRefresh
			if s.autoRefresh {
				s.refresh.Reset()
				return s, s.nodeScheduleAutoRefresh()
			}
			return s, nil
		case "r":
			s.loading = true
			return s, s.fetchNodes()
		}
	}

	if _, ok := msg.(nodeAutoRefreshMsg); ok && s.autoRefresh {
		return s, tea.Batch(s.fetchNodes(), s.nodeScheduleAutoRefresh())
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
	if c == nil || c.Gone {
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

func (s NodeScreen) handleNodeMenuSelect(idx int) (NodeScreen, tea.Cmd) {
	switch idx {
	case 0: // Edit
		c := s.current()
		if c == nil {
			return s, nil
		}
		s.activeNode = c.Name
		s.nodeFormIntent = "edit"
		cfg := s.nodeConfig
		remoteDir := ""
		launcher := "jnlp"
		host := ""
		port := "22"
		credID := ""
		availability := "always"
		inDemand := "0"
		idleDelay := "1"
		controlled := "no"
		if cfg != nil {
			launcher = cfg.LauncherType
			host = cfg.Host
			if cfg.Port > 0 {
				port = strconv.Itoa(cfg.Port)
			}
			credID = cfg.CredID
			availability = cfg.Availability
			remoteDir = cfg.RemoteDir
			inDemand = strconv.Itoa(cfg.InDemandDelay)
			idleDelay = strconv.Itoa(cfg.IdleDelay)
			if cfg.ControlledAgent {
				controlled = "yes"
			}
		}
		s.form.Show("Edit Node: "+c.Name, []formField{
			{Label: "Remote Dir", Value: remoteDir, Placeholder: "/home/jenkins"},
			{Label: "Executors", Value: strconv.Itoa(c.Executors)},
			{Label: "Labels", Value: c.Labels},
			{Label: "Description", Value: c.Description},
			{Label: "Launcher", Value: launcher, Options: []string{"ssh", "jnlp"}},
			{Label: "SSH Host", Value: host},
			{Label: "SSH Port", Value: port},
			{Label: "Credential ID", Value: credID},
			{Label: "Availability", Value: availability, Options: []string{"always", "demand"}},
			{Label: "In-demand Delay", Value: inDemand, Placeholder: "minutes (demand only)"},
			{Label: "Idle Delay", Value: idleDelay, Placeholder: "minutes (demand only)"},
			{Label: "Controlled Agent", Value: controlled, Options: []string{"no", "yes"}},
		})
	case 1: // Toggle Offline
		if c := s.current(); c != nil {
			s.pending = c.Name
			s.action = "toggle"
			s.modal.Show("Toggle offline", fmt.Sprintf("Toggle offline state of '%s'?", c.Name))
		}
	case 2: // Manage Approvals
		if c := s.current(); c != nil {
			// Precondition: approvals only apply to controlled agents. Warn (but
			// still allow viewing) when the node isn't controlled — enabling it
			// is done via Edit → Controlled Agent = yes.
			if cfg := s.nodeConfig; cfg != nil && !cfg.ControlledAgent {
				s.detail.Show("Not a controlled agent",
					fmt.Sprintf("Node %q is not a controlled agent, so folder approvals have no effect.\n\nEnable it first via Edit → Controlled Agent = yes.", c.Name))
				return s, nil
			}
			s.grantNodeName = c.Name
			s.grantList.Show(
				"Approved Folders — "+c.Name,
				"Folders this agent is allowed to run jobs from",
				"Folder",
				"No approved folders yet.",
				"approve folder",
			)
			return s, s.fetchNodeGrants(c.Name)
		}
	case 3: // Delete
		if c := s.current(); c != nil {
			s.pending = c.Name
			s.action = "delete"
			s.modal.Show("Delete node", fmt.Sprintf("Delete node '%s'? This cannot be undone.", c.Name))
		}
	}
	return s, nil
}

func (s NodeScreen) handleNodeFormSubmit(vals []string) tea.Cmd {
	intent := s.nodeFormIntent
	s.nodeFormIntent = ""
	switch intent {
	case "create":
		if len(vals) < 2 || strings.TrimSpace(vals[0]) == "" || strings.TrimSpace(vals[1]) == "" {
			return nil
		}
		name := strings.TrimSpace(vals[0])
		exec := 1
		if len(vals) > 2 {
			if n, err := strconv.Atoi(strings.TrimSpace(vals[2])); err == nil && n > 0 {
				exec = n
			}
		}
		port := 22
		if len(vals) > 7 {
			if p, err := strconv.Atoi(strings.TrimSpace(vals[7])); err == nil && p > 0 {
				port = p
			}
		}
		launcher := "ssh"
		if len(vals) > 5 {
			launcher = strings.TrimSpace(vals[5])
		}
		inDemand := 0
		if len(vals) > 10 {
			if n, err := strconv.Atoi(strings.TrimSpace(vals[10])); err == nil {
				inDemand = n
			}
		}
		idleDelay := 1
		if len(vals) > 11 {
			if n, err := strconv.Atoi(strings.TrimSpace(vals[11])); err == nil && n >= 0 {
				idleDelay = n
			}
		}
		opts := nodepkg.CreateNodeOpts{
			RemoteDir:     strings.TrimSpace(vals[1]),
			NumExecutors:  exec,
			Labels:        strGet(vals, 3),
			Desc:          strGet(vals, 4),
			LauncherType:  launcher,
			Host:          strGet(vals, 6),
			Port:          port,
			CredID:        strGet(vals, 8),
			Availability:  strGetDef(vals, 9, "always"),
			InDemandDelay: inDemand,
			IdleDelay:     idleDelay,
		}
		return s.doCreateNode(opts, name)
	case "edit":
		if len(vals) < 1 {
			return nil
		}
		remoteDir := strings.TrimSpace(vals[0])
		exec := 0
		if len(vals) > 1 {
			if n, err := strconv.Atoi(strings.TrimSpace(vals[1])); err == nil && n > 0 {
				exec = n
			}
		}
		port := 0
		if len(vals) > 6 {
			if p, err := strconv.Atoi(strings.TrimSpace(vals[6])); err == nil && p > 0 {
				port = p
			}
		}
		launcher := strGet(vals, 4)
		host := strGet(vals, 5)
		credID := strGet(vals, 7)
		avail := strGet(vals, 8)
		labels := strGet(vals, 2)
		desc := strGet(vals, 3)
		opts := nodepkg.UpdateNodeOpts{}
		if remoteDir != "" {
			opts.RemoteDir = &remoteDir
		}
		if exec > 0 {
			opts.NumExecutors = &exec
		}
		if labels != "" {
			opts.Labels = &labels
		}
		if desc != "" {
			opts.Desc = &desc
		}
		if launcher != "" {
			opts.LauncherType = &launcher
		}
		if host != "" {
			opts.Host = &host
		}
		if port > 0 {
			opts.Port = &port
		}
		if credID != "" {
			opts.CredID = &credID
		}
		if avail != "" {
			opts.Availability = &avail
		}
		if n, err := strconv.Atoi(strGet(vals, 9)); err == nil {
			opts.InDemandDelay = &n
		}
		if n, err := strconv.Atoi(strGet(vals, 10)); err == nil {
			opts.IdleDelay = &n
		}
		if v := strGet(vals, 11); v == "yes" || v == "no" {
			ctrl := v == "yes"
			opts.ControlledAgent = &ctrl
		}
		return s.doUpdateNode(s.activeNode, opts)
	}
	return nil
}

// View renders the node screen.
func (s NodeScreen) View() string {
	if s.approveInput.Visible() {
		return s.approveInput.View()
	}
	if s.grantList.Visible() {
		return s.grantList.View()
	}
	if s.form.Visible() {
		return s.form.View()
	}
	if s.menu.Visible() {
		return s.menu.View()
	}
	if s.modal.Visible() {
		return s.modal.View()
	}
	if s.detail.Visible() {
		return s.detail.View()
	}
	var sb strings.Builder
	title := theme.StyleTitle.Render(theme.SymOnline + " Nodes")
	if !s.showAll {
		title += " " + theme.StyleBlue.Render("[mine]")
	} else {
		title += " " + theme.StyleDim.Render("[all]")
	}
	if s.autoRefresh {
		title += " " + theme.StyleDim.Render("[auto]")
	}
	if n := s.table.SelectedCount(); n > 0 {
		title += " " + theme.StyleWarning.Render(fmt.Sprintf("[%d selected]", n))
	}
	sb.WriteString(title + "\n")
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
		if s.baseURL != "" && !c.Gone {
			sb.WriteString(theme.StyleSubtle.Render(strings.TrimRight(s.baseURL, "/") + "/computer/" + c.Name + "/"))
			sb.WriteString("\n")
		}
	}

	sb.WriteString(theme.StyleDim.Render("Enter menu  ·  Space select  ·  ^N new  ·  ^A mine/all  ·  i/u track  ·  ^D delete  ·  F auto  ·  r refresh  ·  / search"))
	return sb.String()
}
