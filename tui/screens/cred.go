package screens

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	credpkg "github.com/hyhy2001/bee/plugins/cred"
	"github.com/hyhy2001/bee/plugins/controller"
	"github.com/hyhy2001/bee/tui/components"
	"github.com/hyhy2001/bee/tui/theme"
)

// credEntry is a single credential row.
type credEntry struct {
	ID          string
	TypeName    string
	Scope       string
	Description string
}

type credsLoaded struct {
	creds []credEntry
	err   error
}

type credActionDone struct{ err error }

// CredScreen is the TUI screen for listing/managing credentials.
type CredScreen struct {
	db      *sql.DB
	dbPath  string
	table   components.TableModel
	modal   components.ConfirmModal
	detail  components.MessageModal
	loading bool
	err     error
	creds   []credEntry
	width   int
	height  int
	pending string // ID being acted on
}

// NewCredScreen creates a new CredScreen.
func NewCredScreen(db *sql.DB, dbPath string) CredScreen {
	cols := []table.Column{
		{Title: "ID", Width: 28},
		{Title: "Type", Width: 22},
		{Title: "Scope", Width: 10},
		{Title: "Description", Width: 28},
	}
	return CredScreen{
		db:      db,
		dbPath:  dbPath,
		table:   components.New(cols, nil),
		loading: true,
	}
}

// Init fires the initial data fetch.
func (s CredScreen) Init() tea.Cmd {
	return s.fetchCreds()
}

func (s CredScreen) fetchCreds() tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return credsLoaded{err: err}
		}
		rawCreds, err := credpkg.ListCredentials(context.Background(), client, "system", "")
		if err != nil {
			return credsLoaded{err: err}
		}
		creds := make([]credEntry, 0, len(rawCreds))
		for _, c := range rawCreds {
			creds = append(creds, credEntry{
				ID:          c.ID,
				TypeName:    c.TypeName,
				Scope:       c.Scope,
				Description: c.Description,
			})
		}
		return credsLoaded{creds: creds}
	}
}

func (s CredScreen) doDeleteCred(id string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return credActionDone{err: err}
		}
		if err := credpkg.DeleteCredential(context.Background(), client, id, "", "system"); err != nil {
			return credActionDone{err: err}
		}
		return credActionDone{}
	}
}

func buildCredRows(creds []credEntry) []table.Row {
	rows := make([]table.Row, len(creds))
	for i, c := range creds {
		typeName := c.TypeName
		if len(typeName) > 22 {
			typeName = typeName[:19] + "..."
		}
		desc := c.Description
		if len(desc) > 28 {
			desc = desc[:25] + "..."
		}
		rows[i] = table.Row{c.ID, typeName, c.Scope, desc}
	}
	return rows
}

// Update handles messages and key input.
func (s CredScreen) Update(msg tea.Msg) (CredScreen, tea.Cmd) {
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
	case credsLoaded:
		s.loading = false
		s.err = msg.err
		if msg.err == nil {
			s.creds = msg.creds
			s.table.SetRows(buildCredRows(s.creds))
		}
		return s, nil

	case credActionDone:
		if msg.err != nil {
			s.detail.Show("Error", msg.err.Error())
		} else {
			s.detail.Show("Deleted", fmt.Sprintf("Credential '%s' deleted.", s.pending))
			s.pending = ""
			return s, s.fetchCreds()
		}
		return s, nil

	case components.ConfirmResultMsg:
		if msg.Yes && s.pending != "" {
			return s, s.doDeleteCred(s.pending)
		}
		s.pending = ""
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
				s.modal.Show("Delete credential", fmt.Sprintf("Delete credential '%s'? This cannot be undone.", row[0]))
			}
			return s, nil
		case "r":
			s.loading = true
			return s, s.fetchCreds()
		}
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

// View renders the credential screen.
func (s CredScreen) View() string {
	if s.modal.Visible() {
		return s.modal.View()
	}
	if s.detail.Visible() {
		return s.detail.View()
	}
	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(theme.SymGear+" Credentials") + "\n")
	if s.loading {
		sb.WriteString(theme.StyleDim.Render(theme.SymLoading + " Loading credentials..."))
		return sb.String()
	}
	if s.err != nil {
		sb.WriteString(theme.StyleError.Render(theme.SymFail + " " + s.err.Error()))
		return sb.String()
	}
	if len(s.creds) == 0 {
		sb.WriteString(theme.StyleDim.Render("No credentials found."))
		return sb.String()
	}
	sb.WriteString(s.table.View())
	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("^X=delete  r=refresh  ^F=search"))
	return sb.String()
}
