package screens

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/internal/session"
	"bee/plugins/controller"
	credpkg "bee/plugins/cred"
	"bee/tui/components"
	"bee/tui/theme"
)

// credEntry is a single credential row.
type credEntry struct {
	ID          string
	TypeName    string
	Scope       string
	Description string
}

type credsLoaded struct {
	creds    []credEntry
	username string
	err      error
}

type credActionDone struct{ err error }

// credConfigLoaded carries the fetched username for the highlighted
// credential's detail panel (fetch-on-cursor-move, cached by caller).
type credConfigLoaded struct {
	id       string
	username string
}

// CredScreen is the TUI screen for listing/managing credentials.
type CredScreen struct {
	db       *sql.DB
	dbPath   string
	username string // active session username, for the "user" store segment
	table    components.DataTable
	search   components.SearchBox
	modal    components.ConfirmModal
	detail   components.MessageModal
	loading  bool
	err      error
	creds    []credEntry
	store    string // "system" | "user"
	width    int
	height   int
	pending  string // ID being acted on

	// detail panel: username fetched from the highlighted credential's config.xml
	credUsername      string
	credUsernameCache map[string]string
}

// NewCredScreen creates a new CredScreen.
func NewCredScreen(db *sql.DB, dbPath string) CredScreen {
	cols := []components.Column{
		{Header: "ID", Width: 28, Flex: true},
		{Header: "Type", Width: 24},
		{Header: "Scope", Width: 10},
		{Header: "Description", Width: 34, Flex: true},
	}
	return CredScreen{
		db:                db,
		dbPath:            dbPath,
		table:             components.NewDataTable(cols),
		loading:           true,
		store:             "system",
		credUsernameCache: make(map[string]string),
	}
}

// Init fires the initial data fetch.
func (s CredScreen) Init() tea.Cmd {
	return s.fetchCreds()
}

// InputCaptured reports whether the confirm modal, detail message, or search
// box is currently capturing input, meaning this screen wants raw keys routed
// to it instead of being intercepted by the app shell for tab-switching/quit.
func (s CredScreen) InputCaptured() bool {
	return s.modal.Visible() || s.detail.Visible() || s.search.Editing()
}

func (s CredScreen) fetchCreds() tea.Cmd {
	db, dbPath, store := s.db, s.dbPath, s.store
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return credsLoaded{err: err}
		}
		var username string
		if sess, serr := session.LoadSession(db, dbPath); serr == nil {
			username = sess.Profile.Username
		}
		rawCreds, err := credpkg.ListCredentials(context.Background(), client, store, username)
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
		return credsLoaded{creds: creds, username: username}
	}
}

func (s CredScreen) doDeleteCred(id string) tea.Cmd {
	db, dbPath, store := s.db, s.dbPath, s.store
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return credActionDone{err: err}
		}
		if err := credpkg.DeleteCredential(context.Background(), client, id, "", store); err != nil {
			return credActionDone{err: err}
		}
		return credActionDone{}
	}
}

// fetchCredConfig loads the username field from a credential's config.xml for
// the detail panel. Mirrors the TS getCredentialConfig() fetch-on-cursor-move
// behavior; results are cached by the caller (credUsernameCache).
func (s CredScreen) fetchCredConfig(id string) tea.Cmd {
	db, dbPath, store, username := s.db, s.dbPath, s.store, s.username
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return credConfigLoaded{id: id}
		}
		xmlStr, err := credpkg.GetCredentialXML(context.Background(), client, id, username, store)
		if err != nil {
			return credConfigLoaded{id: id}
		}
		return credConfigLoaded{id: id, username: credpkg.ExtractUsername(xmlStr)}
	}
}

// filteredCreds applies the search box filter to the full credential list.
func (s CredScreen) filteredCreds() []credEntry {
	if s.search.Query() == "" {
		return s.creds
	}
	out := make([]credEntry, 0, len(s.creds))
	for _, c := range s.creds {
		if s.search.Matches(c.ID + " " + c.Description + " " + c.TypeName) {
			out = append(out, c)
		}
	}
	return out
}

func buildCredRows(creds []credEntry) ([][]components.Cell, []string) {
	rows := make([][]components.Cell, len(creds))
	keys := make([]string, len(creds))
	for i, c := range creds {
		rows[i] = []components.Cell{
			{Text: c.ID},
			{Text: c.TypeName},
			{Text: c.Scope, Dim: true},
			{Text: c.Description},
		}
		keys[i] = c.ID
	}
	return rows, keys
}

// current returns the credential entry at the table cursor, or nil.
func (s CredScreen) current() *credEntry {
	filtered := s.filteredCreds()
	i := s.table.Cursor()
	if i < 0 || i >= len(filtered) {
		return nil
	}
	return &filtered[i]
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
	if s.search.Editing() {
		var cmd tea.Cmd
		prevQuery := s.search.Query()
		s.search, cmd = s.search.Update(msg)
		if s.search.Query() != prevQuery || !s.search.Editing() {
			rows, keys := buildCredRows(s.filteredCreds())
			s.table.SetRows(rows, keys)
		}
		return s, cmd
	}

	switch msg := msg.(type) {
	case credsLoaded:
		s.loading = false
		s.err = msg.err
		if msg.err == nil {
			s.creds = msg.creds
			s.username = msg.username
			rows, keys := buildCredRows(s.filteredCreds())
			s.table.SetRows(rows, keys)
		}
		return s, s.maybeFetchDetail()

	case credConfigLoaded:
		if c := s.current(); c != nil && c.ID == msg.id {
			s.credUsername = msg.username
		}
		s.credUsernameCache[msg.id] = msg.username
		return s, nil

	case credActionDone:
		if msg.err != nil {
			s.detail.Show("Error", msg.err.Error())
		} else {
			s.detail.Show("Deleted", fmt.Sprintf("Credential '%s' deleted.", s.pending))
			delete(s.credUsernameCache, s.pending)
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
		s.table.SetSize(msg.Width, maxInt(5, msg.Height-12))
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "/":
			s.search.Open()
			return s, nil
		case "ctrl+d":
			if c := s.current(); c != nil {
				s.pending = c.ID
				s.modal.Show("Delete credential", fmt.Sprintf("Delete credential '%s'? This cannot be undone.", c.ID))
			}
			return s, nil
		case "S":
			// Toggle store — server-side scope, so refetch.
			if s.store == "system" {
				s.store = "user"
			} else {
				s.store = "system"
			}
			s.loading = true
			s.credUsernameCache = make(map[string]string)
			return s, s.fetchCreds()
		case "r":
			s.loading = true
			return s, s.fetchCreds()
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

// maybeFetchDetail serves the detail-panel username from cache, or kicks off
// a background fetch when the highlighted credential hasn't been seen yet.
func (s *CredScreen) maybeFetchDetail() tea.Cmd {
	c := s.current()
	if c == nil {
		s.credUsername = ""
		return nil
	}
	if cached, ok := s.credUsernameCache[c.ID]; ok {
		s.credUsername = cached
		return nil
	}
	s.credUsername = ""
	return s.fetchCredConfig(c.ID)
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
	sb.WriteString(theme.StyleTitle.Render(theme.SymGear + " Credentials"))
	sb.WriteString("  ")
	if s.store == "user" {
		sb.WriteString(theme.StyleWarning.Render("[USER]"))
	} else {
		sb.WriteString(theme.StyleBlue.Render("[SYSTEM]"))
	}
	sb.WriteString("\n")
	if s.loading {
		sb.WriteString(theme.StyleDim.Render(theme.SymLoading + " Loading credentials..."))
		return sb.String()
	}
	if s.err != nil {
		sb.WriteString(theme.StyleError.Render(theme.SymFail + " " + s.err.Error()))
		return sb.String()
	}
	if len(s.creds) == 0 {
		sb.WriteString(theme.StyleDim.Render("No credentials. Press Ctrl+n to create one."))
		return sb.String()
	}
	if sv := s.search.View(); sv != "" {
		sb.WriteString(sv + "\n")
	}
	sb.WriteString(s.table.View())
	sb.WriteString("\n")

	if c := s.current(); c != nil {
		sb.WriteString("\n")
		sb.WriteString(theme.StyleTitle.Render(c.ID))
		sb.WriteString("  ")
		sb.WriteString(theme.StyleBlue.Render(c.TypeName))
		sb.WriteString("\n")
		if s.credUsername != "" {
			sb.WriteString(theme.StyleDim.Render("user ") + s.credUsername + "   ")
		}
		sb.WriteString(theme.StyleDim.Render("scope ") + c.Scope + "   ")
		sb.WriteString(theme.StyleDim.Render("store "))
		if s.store == "system" {
			sb.WriteString(theme.StyleBlue.Render("system"))
		} else {
			sb.WriteString(theme.StyleWarning.Render("user"))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(theme.StyleDim.Render("↑↓ move  ·  ^D delete  ·  S store  ·  r refresh  ·  / search"))
	return sb.String()
}
