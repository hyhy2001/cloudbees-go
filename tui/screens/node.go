package screens

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	nodepkg "bee/plugins/node"
	"bee/plugins/controller"
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

// NodeScreen is the TUI screen for listing/managing Jenkins nodes.
type NodeScreen struct {
	db      *sql.DB
	dbPath  string
	table   components.TableModel
	modal   components.ConfirmModal
	detail  components.MessageModal
	loading bool
	err     error
	nodes   []nodeEntry
	width   int
	height  int
	pending string // name of node targeted for action
	action  string // "delete" | "toggle"
}

// NewNodeScreen creates a new NodeScreen.
func NewNodeScreen(db *sql.DB, dbPath string) NodeScreen {
	cols := []table.Column{
		{Title: "Name", Width: 32},
		{Title: "Status", Width: 10},
		{Title: "Exec", Width: 5},
		{Title: "Labels", Width: 24},
		{Title: "Description", Width: 22},
	}
	return NodeScreen{
		db:      db,
		dbPath:  dbPath,
		table:   components.New(cols, nil),
		loading: true,
	}
}

// Init fires the initial data fetch.
func (s NodeScreen) Init() tea.Cmd {
	return s.fetchNodes()
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

func buildNodeRows(nodes []nodeEntry) []table.Row {
	rows := make([]table.Row, len(nodes))
	for i, n := range nodes {
		status := theme.SymOnline + " online"
		if n.Offline {
			status = theme.SymOffline + " offline"
		}
		desc := n.Description
		if len(desc) > 22 {
			desc = desc[:19] + "..."
		}
		rows[i] = table.Row{
			n.DisplayName,
			status,
			fmt.Sprintf("%d", n.Executors),
			n.Labels,
			desc,
		}
	}
	return rows
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

	switch msg := msg.(type) {
	case nodesLoaded:
		s.loading = false
		s.err = msg.err
		if msg.err == nil {
			s.nodes = msg.nodes
			s.table.SetRows(buildNodeRows(s.nodes))
		}
		return s, nil

	case nodeActionDone:
		if msg.err != nil {
			s.detail.Show("Error", msg.err.Error())
		} else {
			s.detail.Show("Done", fmt.Sprintf("Action on '%s' completed.", s.pending))
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
		s.table.SetSize(msg.Width, maxInt(5, msg.Height-8))
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+x":
			row := s.table.SelectedRow()
			if row != nil {
				s.pending = row[0]
				s.action = "delete"
				s.modal.Show("Delete node", fmt.Sprintf("Delete node '%s'? This cannot be undone.", row[0]))
			}
			return s, nil
		case "ctrl+o":
			row := s.table.SelectedRow()
			if row != nil {
				s.pending = row[0]
				s.action = "toggle"
				s.modal.Show("Toggle offline", fmt.Sprintf("Toggle offline state of '%s'?", row[0]))
			}
			return s, nil
		case "r":
			s.loading = true
			return s, s.fetchNodes()
		}
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
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
	sb.WriteString(s.table.View())
	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("Enter menu  ·  ↑↓ move  ·  Ctrl+n new  ·  Ctrl+d delete  ·  r refresh  ·  / search"))
	return sb.String()
}
