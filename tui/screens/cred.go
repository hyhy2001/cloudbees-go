package screens

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	internaldb "bee/internal/db"
	"bee/internal/session"
	"bee/plugins/controller"
	credpkg "bee/plugins/cred"
	"bee/tui/components"
	"bee/tui/theme"
)

// credAutoRefreshMsg is the tick for the cred screen's auto-refresh.
type credAutoRefreshMsg struct{}

type credEntry struct {
	ID          string
	TypeName    string
	Scope       string
	Description string
	Gone        bool // synthesized placeholder for a tracked cred missing on the server
}

type credsLoaded struct {
	creds    []credEntry
	username string
	tracked  map[string]bool
	baseURL  string
	err      error
}

type credActionDone struct {
	label string
	err   error
}

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
	form     formOverlay
	menu     menuOverlay
	loading  bool
	err      error
	creds    []credEntry
	store    string // "system" | "user"
	width    int
	height   int
	pending  string // ID being acted on
	activeID string // ID currently in context (for edit)

	autoRefresh bool

	// form intent: "create-up", "create-st", "edit"
	credFormIntent string

	// Mine/All filter
	showAll bool
	tracked map[string]bool
	baseURL string // active controller URL for track/untrack

	// detail panel: username fetched from the highlighted credential's config.xml
	credUsername      string
	credUsernameCache map[string]string

	pendingBulkDelete bool
	refresh           components.AutoRefresh
	editType          string // TypeName of the credential currently being edited
}

// NewCredScreen creates a new CredScreen.
func NewCredScreen(db *sql.DB, dbPath string) CredScreen {
	cols := []components.Column{
		{Header: "ID", Width: 28, Flex: true},
		{Header: "Type", Width: 24},
		{Header: "Scope", Width: 10},
		{Header: "Description", Width: 34, Flex: true},
	}
	tbl := components.NewDataTable(cols)
	tbl.SetSelectable(true)
	return CredScreen{
		db:                db,
		dbPath:            dbPath,
		table:             tbl,
		loading:           true,
		store:             "system",
		showAll:           internaldb.GetScopeShowAll(db, "credential"),
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
	return s.modal.Visible() || s.detail.Visible() || s.search.Editing() ||
		s.form.Visible() || s.menu.Visible()
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
		ctrlKey := client.BaseURL + "." + store
		profileName := controller.GetActiveProfileName(db)
		trackedNames, _ := internaldb.ListTracked(db, "credential", profileName, ctrlKey)
		tracked := make(map[string]bool, len(trackedNames))
		for _, n := range trackedNames {
			tracked[n] = true
		}
		return credsLoaded{creds: creds, username: username, tracked: tracked, baseURL: ctrlKey}
	}
}

func (s CredScreen) doDeleteCred(id string) tea.Cmd {
	db, dbPath, store, username := s.db, s.dbPath, s.store, s.username
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return credActionDone{label: "delete", err: err}
		}
		if err := credpkg.DeleteCredential(context.Background(), client, id, username, store); err != nil {
			return credActionDone{label: "delete", err: err}
		}
		return credActionDone{label: "delete " + id}
	}
}

func (s CredScreen) doCreateUP(id, user, pass, desc string) tea.Cmd {
	db, dbPath, store, sessUser := s.db, s.dbPath, s.store, s.username
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return credActionDone{label: "create", err: err}
		}
		err = credpkg.CreateUsernamePasswordCredential(context.Background(), client, id, user, pass, desc, "GLOBAL", store, sessUser)
		return credActionDone{label: "create " + id, err: err}
	}
}

func (s CredScreen) doCreateST(id, secret, desc string) tea.Cmd {
	db, dbPath, store, sessUser := s.db, s.dbPath, s.store, s.username
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return credActionDone{label: "create", err: err}
		}
		err = credpkg.CreateSecretTextCredential(context.Background(), client, id, secret, desc, "GLOBAL", store, sessUser)
		return credActionDone{label: "create " + id, err: err}
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

// filteredCreds applies Mine/All filter and search box filter.
func (s CredScreen) filteredCreds() []credEntry {
	creds := s.creds
	if !s.showAll && len(s.tracked) > 0 {
		present := make(map[string]bool, len(creds))
		out := make([]credEntry, 0, len(creds))
		for _, c := range creds {
			if s.tracked[c.ID] {
				out = append(out, c)
				present[c.ID] = true
			}
		}
		// Synthesize placeholders for tracked creds missing from the server.
		for id := range s.tracked {
			if !present[id] {
				out = append(out, credEntry{ID: id, Gone: true})
			}
		}
		creds = out
	}
	if s.search.Query() == "" {
		return creds
	}
	out := make([]credEntry, 0, len(creds))
	for _, c := range creds {
		if s.search.Matches(c.ID + " " + c.Description + " " + c.TypeName) {
			out = append(out, c)
		}
	}
	return out
}

func buildCredRows(creds []credEntry, tracked map[string]bool) ([][]components.Cell, []string) {
	rows := make([][]components.Cell, len(creds))
	keys := make([]string, len(creds))
	for i, c := range creds {
		if c.Gone {
			rows[i] = []components.Cell{
				{Text: "★ " + c.ID, Color: theme.ColorError},
				{Text: "GONE", Color: theme.ColorError},
				{Text: "", Dim: true},
				{Text: "deleted on server", Dim: true},
			}
			keys[i] = c.ID
			continue
		}
		id := c.ID
		if tracked[c.ID] {
			id = "★ " + id
		}
		rows[i] = []components.Cell{
			{Text: id},
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
	// Form submit/cancel arrive one cycle after the form hides itself, so handle
	// them before the visibility gate below (which would otherwise swallow them).
	switch m := msg.(type) {
	case FormSubmitMsg:
		return s, s.handleCredFormSubmit(m.Values)
	case FormCancelMsg:
		s.credFormIntent = ""
		return s, nil
	}
	if s.form.Visible() {
		var cmd tea.Cmd
		s.form, cmd = s.form.Update(msg)
		return s, cmd
	}
	// MenuSelectMsg arrives one cycle after the menu hides itself, so handle it
	// before the visibility gate below (which would otherwise swallow it).
	if sel, ok := msg.(MenuSelectMsg); ok {
		return s.handleCredMenuSelect(sel.Index)
	}
	if s.menu.Visible() {
		var cmd tea.Cmd
		s.menu, cmd = s.menu.Update(msg)
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
	case credsLoaded:
		s.loading = false
		s.err = msg.err
		if msg.err == nil {
			s.creds = msg.creds
			s.username = msg.username
			s.tracked = msg.tracked
			s.baseURL = msg.baseURL
			s.refresh.Reset()
			s.rebuildRows()
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
			return s, nil
		}
		s.detail.Show("Done", msg.label+" succeeded.")
		if strings.HasPrefix(msg.label, "delete") {
			delete(s.credUsernameCache, s.pending)
			s.pending = ""
		}
		return s, s.fetchCreds()

	case components.ConfirmResultMsg:
		if msg.Yes && s.pendingBulkDelete {
			s.pendingBulkDelete = false
			ids := make([]string, 0, s.table.SelectedCount())
			for k := range s.table.Selected() {
				ids = append(ids, k)
			}
			s.table.ClearSelection()
			return s, s.doBulkDeleteCreds(ids)
		}
		if msg.Yes && s.pending != "" {
			return s, s.doDeleteCred(s.pending)
		}
		s.pending = ""
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
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "/":
			s.search.Open()
			return s, nil
		case "enter":
			if c := s.current(); c != nil && !c.Gone {
				s.activeID = c.ID
				items := []string{"Edit", "Delete"}
				if s.tracked[c.ID] {
					items = append(items, "Untrack")
				} else {
					items = append(items, "Track")
				}
				s.menu.Show("Credential: "+c.ID, items)
			}
			return s, nil
		case "esc":
			if s.table.SelectedCount() > 0 {
				s.table.ClearSelection()
			}
			return s, nil
		case "ctrl+n":
			s.menu.Show("New Credential", []string{"Username + Password", "Secret Text"})
			s.credFormIntent = "type-pick"
			return s, nil
		case "ctrl+d":
			if n := s.table.SelectedCount(); n > 0 {
				s.pendingBulkDelete = true
				s.modal.Show("Delete credentials", fmt.Sprintf("Delete %d selected credential(s)? This cannot be undone.", n))
				return s, nil
			}
			if c := s.current(); c != nil {
				s.pending = c.ID
				s.modal.Show("Delete credential", fmt.Sprintf("Delete credential '%s'? This cannot be undone.", c.ID))
			}
			return s, nil
		case "f", "F":
			s.autoRefresh = !s.autoRefresh
			if s.autoRefresh {
				s.refresh.Reset()
				return s, s.credScheduleAutoRefresh()
			}
			return s, nil
		case "S":
			if s.store == "system" {
				s.store = "user"
			} else {
				s.store = "system"
			}
			s.loading = true
			s.credUsernameCache = make(map[string]string)
			return s, s.fetchCreds()
		case "ctrl+a":
			s.showAll = !s.showAll
			_ = internaldb.SetScopeShowAll(s.db, "credential", s.showAll)
			s.rebuildRows()
			return s, nil
		case "i":
			s.trackIDs(s.selectionOrCursor(), true)
			s.table.ClearSelection()
			s.rebuildRows()
			return s, nil
		case "u":
			s.trackIDs(s.selectionOrCursor(), false)
			s.table.ClearSelection()
			s.rebuildRows()
			return s, nil
		case "r":
			s.loading = true
			return s, s.fetchCreds()
		}
	}

	if _, ok := msg.(credAutoRefreshMsg); ok && s.autoRefresh {
		return s, tea.Batch(s.fetchCreds(), s.credScheduleAutoRefresh())
	}

	before := s.table.Cursor()
	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	if s.table.Cursor() != before {
		return s, tea.Batch(cmd, s.maybeFetchDetail())
	}
	return s, cmd
}

func (s *CredScreen) credScheduleAutoRefresh() tea.Cmd {
	return tea.Tick(s.refresh.Next(), func(_ time.Time) tea.Msg { return credAutoRefreshMsg{} })
}

// maybeFetchDetail serves the detail-panel username from cache, or kicks off
// a background fetch when the highlighted credential hasn't been seen yet.
func (s *CredScreen) maybeFetchDetail() tea.Cmd {
	c := s.current()
	if c == nil || c.Gone {
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

func (s CredScreen) handleCredMenuSelect(idx int) (CredScreen, tea.Cmd) {
	if s.credFormIntent == "type-pick" {
		// idx 0=UP, 1=ST
		s.credFormIntent = ""
		if idx == 0 {
			s.credFormIntent = "create-up"
			s.form.Show("New Credential: Username+Password", []formField{
				{Label: "ID", Placeholder: "my-cred-id"},
				{Label: "Username", Required: true},
				{Label: "Password", Required: true, Password: true},
				{Label: "Description"},
			})
		} else {
			s.credFormIntent = "create-st"
			s.form.Show("New Credential: Secret Text", []formField{
				{Label: "ID", Placeholder: "my-secret-id"},
				{Label: "Secret", Required: true, Password: true},
				{Label: "Description"},
			})
		}
		return s, nil
	}
	// context menu for existing cred: 0=Edit, 1=Delete, 2=Track|Untrack
	switch idx {
	case 0: // Edit — type-aware fields (secret/password can't be read back, so
		// they're blank = "keep current"; description prefills)
		cur := s.currentByID(s.activeID)
		desc := ""
		typeName := ""
		if cur != nil {
			desc = cur.Description
			typeName = cur.TypeName
		}
		s.editType = typeName
		s.credFormIntent = "edit"
		lower := strings.ToLower(typeName)
		switch {
		case strings.Contains(lower, "username"):
			s.form.Show("Edit: "+s.activeID, []formField{
				{Label: "Username", Value: s.credUsername, Placeholder: "leave blank to keep"},
				{Label: "Password", Password: true, Placeholder: "leave blank to keep"},
				{Label: "Description", Value: desc},
			})
		case strings.Contains(lower, "secret") || strings.Contains(lower, "string"):
			s.form.Show("Edit: "+s.activeID, []formField{
				{Label: "Secret", Password: true, Placeholder: "leave blank to keep"},
				{Label: "Description", Value: desc},
			})
		default:
			s.form.Show("Edit: "+s.activeID, []formField{
				{Label: "Description", Value: desc},
			})
		}
	case 1: // Delete
		s.pending = s.activeID
		s.modal.Show("Delete credential", fmt.Sprintf("Delete credential '%s'? This cannot be undone.", s.activeID))
	case 2: // Track / Untrack (label depends on current state)
		track := !s.tracked[s.activeID]
		s.trackIDs([]string{s.activeID}, track)
		s.rebuildRows()
	}
	return s, nil
}

func (s CredScreen) currentByID(id string) *credEntry {
	for i := range s.creds {
		if s.creds[i].ID == id {
			return &s.creds[i]
		}
	}
	return nil
}

func (s CredScreen) doBulkDeleteCreds(ids []string) tea.Cmd {
	db, dbPath, store, username := s.db, s.dbPath, s.store, s.username
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return credActionDone{label: "delete", err: err}
		}
		var failed int
		var firstErr error
		for _, id := range ids {
			if e := credpkg.DeleteCredential(context.Background(), client, id, username, store); e != nil {
				failed++
				if firstErr == nil {
					firstErr = e
				}
			}
		}
		if firstErr != nil {
			return credActionDone{label: "delete", err: fmt.Errorf("%d of %d failed: %w", failed, len(ids), firstErr)}
		}
		return credActionDone{label: fmt.Sprintf("delete %d credentials", len(ids))}
	}
}

// selectionOrCursor returns the multi-selection when non-empty, else the cursor
// row's ID as a one-element slice (empty when nothing is under the cursor).
func (s CredScreen) selectionOrCursor() []string {
	if s.table.SelectedCount() > 0 {
		out := make([]string, 0, s.table.SelectedCount())
		for k := range s.table.Selected() {
			out = append(out, k)
		}
		return out
	}
	if c := s.current(); c != nil {
		return []string{c.ID}
	}
	return nil
}

func (s *CredScreen) trackIDs(ids []string, track bool) {
	if s.baseURL == "" || len(ids) == 0 {
		return
	}
	profileName := controller.GetActiveProfileName(s.db)
	if s.tracked == nil {
		s.tracked = make(map[string]bool)
	}
	for _, id := range ids {
		if track {
			_ = internaldb.TrackResource(s.db, "credential", id, profileName, s.baseURL)
			s.tracked[id] = true
		} else {
			_ = internaldb.UntrackResource(s.db, "credential", id, profileName, s.baseURL)
			delete(s.tracked, id)
		}
	}
}

func (s *CredScreen) rebuildRows() {
	rows, keys := buildCredRows(s.filteredCreds(), s.tracked)
	s.table.SetRows(rows, keys)
}

func (s CredScreen) handleCredFormSubmit(vals []string) tea.Cmd {
	intent := s.credFormIntent
	s.credFormIntent = ""
	switch intent {
	case "create-up":
		if len(vals) < 4 {
			return nil
		}
		id, user, pass, desc := strings.TrimSpace(vals[0]), vals[1], vals[2], vals[3]
		if id == "" {
			id = user + "-cred"
		}
		return s.doCreateUP(id, user, pass, desc)
	case "create-st":
		if len(vals) < 3 {
			return nil
		}
		id, secret, desc := strings.TrimSpace(vals[0]), vals[1], vals[2]
		if id == "" {
			id = "secret-text"
		}
		return s.doCreateST(id, secret, desc)
	case "edit":
		// Map form fields to config.xml tags by credential type. Blank values
		// are skipped (= keep current); secrets can't be read back, so a blank
		// leaves them untouched.
		lower := strings.ToLower(s.editType)
		var updates []struct{ tag, value string }
		switch {
		case strings.Contains(lower, "username"):
			// fields: 0=Username, 1=Password, 2=Description
			if v := strGet(vals, 0); v != "" {
				updates = append(updates, struct{ tag, value string }{"username", v})
			}
			if v := strGet(vals, 1); v != "" {
				updates = append(updates, struct{ tag, value string }{"password", v})
			}
			updates = append(updates, struct{ tag, value string }{"description", strGet(vals, 2)})
		case strings.Contains(lower, "secret") || strings.Contains(lower, "string"):
			// fields: 0=Secret, 1=Description
			if v := strGet(vals, 0); v != "" {
				updates = append(updates, struct{ tag, value string }{"secret", v})
			}
			updates = append(updates, struct{ tag, value string }{"description", strGet(vals, 1)})
		default:
			// fields: 0=Description
			updates = append(updates, struct{ tag, value string }{"description", strGet(vals, 0)})
		}
		db, dbPath, store, username := s.db, s.dbPath, s.store, s.username
		id := s.activeID
		return func() tea.Msg {
			client, err := controller.GetActiveControllerClient(db, dbPath)
			if err != nil {
				return credActionDone{label: "edit", err: err}
			}
			for _, u := range updates {
				if err := credpkg.UpdateCredentialField(context.Background(), client, id, u.tag, u.value, username, store); err != nil {
					return credActionDone{label: "edit", err: err}
				}
			}
			return credActionDone{label: "edit " + id}
		}
	}
	return nil
}

// View renders the credential screen.
func (s CredScreen) View() string {
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
	sb.WriteString(theme.StyleTitle.Render(theme.SymGear + " Credentials"))
	sb.WriteString("  ")
	if s.store == "user" {
		sb.WriteString(theme.StyleWarning.Render("[USER]"))
	} else {
		sb.WriteString(theme.StyleBlue.Render("[SYSTEM]"))
	}
	if len(s.tracked) > 0 || s.showAll {
		if s.showAll {
			sb.WriteString("  " + theme.StyleDim.Render("[all]"))
		} else {
			sb.WriteString("  " + theme.StyleWarning.Render("[mine]"))
		}
	}
	if s.autoRefresh {
		sb.WriteString("  " + theme.StyleDim.Render("[auto]"))
	}
	if n := s.table.SelectedCount(); n > 0 {
		sb.WriteString("  " + theme.StyleWarning.Render(fmt.Sprintf("[%d selected]", n)))
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

	sb.WriteString(theme.StyleDim.Render("Enter menu  ·  Space select  ·  ^N new  ·  ^D delete  ·  i/u track  ·  ^A mine/all  ·  S store  ·  F auto  ·  r refresh  ·  / search"))
	return sb.String()
}
