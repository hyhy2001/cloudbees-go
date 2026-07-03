// Package screens provides the TUI screen implementations.
package screens

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	internaldb "bee/internal/db"
	"bee/plugins/controller"
	jobo "bee/plugins/job"
	"bee/plugins/node"
	"bee/tui/components"
	"bee/tui/theme"
)

// ── async result messages ─────────────────────────────────────────────────────

type jobEntry struct {
	Name  string
	Class string
	Color string
	Desc  string
	Build int
	URL   string
	Gone  bool // synthesized placeholder for a tracked job missing on the server
}

type jobsLoaded struct {
	jobs        []jobEntry
	folder      string // which folder these jobs belong to (empty = root)
	tracked     map[string]bool
	baseURL     string
	queueReason map[string]string // job URL (no trailing slash) → wait reason
	err         error
}

type jobOpDone struct {
	label string
	err   error
}

type nodeNamesLoaded struct {
	names []string
	err   error
}

type agentsLoaded struct {
	items []components.GrantItem
	err   error
}

type configLoaded struct {
	summary jobo.ConfigSummary
	err     error
}

// buildHistoryLoaded carries build numbers for the log viewer's [/] nav.
type buildHistoryLoaded struct {
	jobName string
	nums    []int // sorted descending
}

// logChunkLoaded carries one progressive log poll result.
type logChunkLoaded struct {
	text      string
	newOffset int64
	hasMore   bool
	err       error
}

// scriptLoaded carries a fetched pipeline script.
type scriptLoaded struct {
	script string
	err    error
}

// autoRefreshMsg is the tick for the job screen's auto-refresh.
type jobAutoRefreshMsg struct{}

// ── form (multi-field inline text entry) ─────────────────────────────────────

type formField struct {
	Label       string
	Value       string
	Placeholder string
	Required    bool
	Password    bool // mask input with •
	// Options non-nil → cycle-select, not free-text.
	Options []string
}

type formOverlay struct {
	Title   string
	fields  []formField
	cursor  int
	buf     []string
	visible bool
	width   int    // terminal width for border sizing
	errMsg  string // validation message shown below the fields
}

func (f *formOverlay) Show(title string, fields []formField) {
	f.Title = title
	f.fields = fields
	f.buf = make([]string, len(fields))
	for i, fl := range fields {
		f.buf[i] = fl.Value
	}
	f.cursor = 0
	f.visible = true
	f.errMsg = ""
}

func (f *formOverlay) Hide()        { f.visible = false }
func (f formOverlay) Visible() bool { return f.visible }

// FormSubmitMsg signals the user pressed Enter on the last field (or a
// submit-trigger field). Values() carries the field values.
type FormSubmitMsg struct{ Values []string }

// FormCancelMsg signals Esc.
type FormCancelMsg struct{}

func (f formOverlay) Update(msg tea.Msg) (formOverlay, tea.Cmd) {
	if !f.visible {
		return f, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	if len(f.fields) == 0 {
		return f, nil
	}
	cur := f.fields[f.cursor]

	switch km.String() {
	case "esc":
		f.visible = false
		return f, func() tea.Msg { return FormCancelMsg{} }
	case "tab", "down":
		f.cursor = (f.cursor + 1) % len(f.fields)
		return f, nil
	case "shift+tab", "up":
		f.cursor = (f.cursor + len(f.fields) - 1) % len(f.fields)
		return f, nil
	case "enter":
		// Enter advances (and submits on the last field) regardless of field
		// type; option fields are cycled with ←→, so Enter must not be trapped
		// into cycling — otherwise a form ending in a dropdown can never submit.
		if f.cursor == len(f.fields)-1 {
			// Enforce required fields before submitting — jump to the first
			// empty one and show why, rather than submitting a bad payload.
			for i, fl := range f.fields {
				if fl.Required && strings.TrimSpace(f.buf[i]) == "" {
					f.cursor = i
					f.errMsg = fl.Label + " is required"
					return f, nil
				}
			}
			vals := append([]string{}, f.buf...)
			f.visible = false
			return f, func() tea.Msg { return FormSubmitMsg{Values: vals} }
		}
		f.cursor++
		return f, nil
	case "left":
		if len(cur.Options) > 0 {
			opts := cur.Options
			idx := 0
			for i, o := range opts {
				if o == f.buf[f.cursor] {
					idx = i
					break
				}
			}
			f.buf[f.cursor] = opts[(idx+len(opts)-1)%len(opts)]
		}
		return f, nil
	case "right":
		if len(cur.Options) > 0 {
			opts := cur.Options
			idx := 0
			for i, o := range opts {
				if o == f.buf[f.cursor] {
					idx = i
					break
				}
			}
			f.buf[f.cursor] = opts[(idx+1)%len(opts)]
		}
		return f, nil
	}

	if len(cur.Options) == 0 {
		switch km.Type {
		case tea.KeyBackspace, tea.KeyDelete:
			v := f.buf[f.cursor]
			if len(v) > 0 {
				f.buf[f.cursor] = v[:len(v)-1]
				f.errMsg = ""
			}
		case tea.KeyRunes:
			f.buf[f.cursor] += string(km.Runes)
			f.errMsg = ""
		case tea.KeySpace:
			f.buf[f.cursor] += " "
			f.errMsg = ""
		}
	}
	return f, nil
}

func (f formOverlay) View() string {
	if !f.visible {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(theme.SymGear + " " + f.Title))
	sb.WriteString("\n\n")
	for i, fl := range f.fields {
		on := i == f.cursor
		marker := " "
		labelStyle := theme.StyleDim
		if on {
			marker = theme.SymArrow
			labelStyle = theme.StyleKeyHint
		}
		val := f.buf[i]
		display := val
		if fl.Password {
			display = strings.Repeat("•", len([]rune(val)))
		}
		if len(fl.Options) > 0 {
			if on {
				display = theme.StyleDim.Render(theme.SymArrow+" ") + val + theme.StyleDim.Render(" "+theme.SymArrow)
			}
		} else {
			if on {
				display += "_"
			}
			if val == "" && fl.Placeholder != "" {
				if on {
					display = fl.Placeholder + "_"
				} else {
					display = theme.StyleDim.Render(fl.Placeholder)
				}
			}
		}
		sb.WriteString(labelStyle.Render(marker+" "+fixWidth(fl.Label, 16)+" ") + display)
		sb.WriteString("\n")
	}
	if f.errMsg != "" {
		sb.WriteString("\n")
		sb.WriteString(theme.StyleError.Render(theme.SymFail + " " + f.errMsg))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("Tab/↑↓ move · ←→ cycle (select) · Enter next/submit · Esc cancel"))
	return theme.BorderBox(sb.String(), "info", f.width)
}

// ── context menu ──────────────────────────────────────────────────────────────

type menuOverlay struct {
	title   string
	items   []string
	cursor  int
	visible bool
	width   int // terminal width for border sizing
}

func (m *menuOverlay) Show(title string, items []string) {
	m.title = title
	m.items = items
	m.cursor = 0
	m.visible = true
}
func (m *menuOverlay) Hide()        { m.visible = false }
func (m menuOverlay) Visible() bool { return m.visible }

// MenuSelectMsg carries the index chosen.
type MenuSelectMsg struct{ Index int }

func (m menuOverlay) Update(msg tea.Msg) (menuOverlay, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "esc", "q":
		m.visible = false
		return m, nil
	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "enter":
		idx := m.cursor
		m.visible = false
		return m, func() tea.Msg { return MenuSelectMsg{Index: idx} }
	default:
		// 1–9 quick pick
		if len(km.String()) == 1 && km.String() >= "1" && km.String() <= "9" {
			idx := int(km.String()[0] - '1')
			if idx < len(m.items) {
				m.visible = false
				return m, func() tea.Msg { return MenuSelectMsg{Index: idx} }
			}
		}
	}
	return m, nil
}

func (m menuOverlay) View() string {
	if !m.visible {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(theme.SymGear + " " + m.title))
	sb.WriteString("\n\n")
	for i, item := range m.items {
		num := ""
		if i < 9 {
			num = fmt.Sprintf("%d ", i+1)
		}
		if on := i == m.cursor; on {
			sb.WriteString(theme.StyleKeyHint.Render(theme.SymArrow + " " + num + item))
		} else {
			sb.WriteString(theme.StyleDim.Render("  "+num) + item)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("↑↓ move  ·  1–9 pick  ·  Enter run  ·  Esc back"))
	return theme.BorderBox(sb.String(), "info", m.width)
}

// ── JobScreen ─────────────────────────────────────────────────────────────────

// jobAction identifies what the context menu or a key triggered.
type jobAction int

const (
	actViewLog    jobAction = iota // 0
	actViewScript                  // 1 — PL only
	actRun                         // 2
	actStop                        // 3
	actEdit                        // 4
	actParams                      // 5 — FS only
	actSchedule                    // 6 — FS only
	actEmail                       // 7 — FS only
	actDelete                      // 8
	actMove                        // 9 — FS+FD only
	actClone                       // 10 — FS only
	actAgents                      // 11 — FD only
)

// JobScreen is the TUI screen for listing/managing Jenkins jobs.
type JobScreen struct {
	db     *sql.DB
	dbPath string

	table   components.DataTable
	search  components.SearchBox
	loading bool
	err     error
	jobs    []jobEntry
	width   int
	height  int

	// folder drill-down — folderStack[len-1] is the current folder, empty = root
	folderStack []string

	// auto-refresh ticker
	autoRefresh    bool
	autoRefreshCmd tea.Cmd

	// overlay state — exactly one active at a time; priority in View()
	menu       menuOverlay
	form       formOverlay
	schedule   components.ScheduleBuilder
	email      components.EmailBuilder
	params     components.ParamListEditor
	agents     components.GrantListOverlay
	confirm    components.ConfirmModal
	message    components.MessageModal
	logViewer  components.LogViewer
	scriptView components.ScriptViewer

	// context carrying which job an overlay is operating on
	activeJob     string
	activeJobType string // FS / PL / FD
	// for confirm, remember what it is confirming
	pendingDelete     string
	pendingAction     jobAction
	pendingBulkDelete bool // confirm targets the multi-selection, not pendingDelete
	// for "add agent" form, remember the folder
	agentFolder string

	// nodes list (fetched lazily for create/edit forms)
	nodeNames []string

	// "what did the form submission mean?"
	formIntent string // "create-freestyle","create-pipeline","create-folder","edit-freestyle","edit-pipeline","add-agent","move","clone"

	// detail panel: config summary for the highlighted job (fetch-on-cursor-move)
	detailSummary      *jobo.ConfigSummary
	detailSummaryCache map[string]jobo.ConfigSummary

	// log viewer: progressive polling offset
	logOffset int64

	// Mine/All filter
	showAll bool
	tracked map[string]bool
	baseURL string // active controller URL for track/untrack

	// run-with-params: parameter names in form-field order (parallel to the
	// run-params form's values), so a submit maps back to name→value.
	runParamNames []string

	// auto-refresh backoff (5s→60s), reset on user action / data load
	refresh components.AutoRefresh

	// build-queue wait reasons keyed by job URL (trailing slash stripped)
	queueReason map[string]string
}

// NewJobScreen constructs a JobScreen.
func NewJobScreen(db *sql.DB, dbPath string) JobScreen {
	cols := []components.Column{
		{Header: "Status", Width: 12},
		{Header: "T", Width: 3},
		{Header: "Name", Width: 40, Flex: true},
		{Header: "Build #", Width: 9},
		{Header: "Reason", Width: 18},
		{Header: "Description", Width: 22, Flex: true},
	}
	tbl := components.NewDataTable(cols)
	tbl.SetSelectable(true)
	return JobScreen{
		db:                 db,
		dbPath:             dbPath,
		table:              tbl,
		loading:            true,
		showAll:            internaldb.GetScopeShowAll(db, "job"),
		detailSummaryCache: make(map[string]jobo.ConfigSummary),
	}
}

// Init fires the initial data fetch.
func (s JobScreen) Init() tea.Cmd {
	return s.fetchJobs()
}

// InputCaptured reports whether any overlay/form/menu/search is currently
// visible, meaning this screen wants raw keys (digits, tab, q) routed to it
// instead of being intercepted by the app shell for tab-switching/quit.
func (s JobScreen) InputCaptured() bool {
	return s.schedule.Visible() || s.email.Visible() || s.params.Visible() ||
		s.agents.Visible() || s.form.Visible() || s.menu.Visible() ||
		s.confirm.Visible() || s.message.Visible() || s.search.Editing() ||
		s.logViewer.Visible() || s.scriptView.Visible()
}

// ── data fetches ──────────────────────────────────────────────────────────────

func (s JobScreen) fetchJobs() tea.Cmd {
	db, dbPath := s.db, s.dbPath
	folder := s.currentFolder()
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobsLoaded{err: err, folder: folder}
		}
		var rawJobs []jobo.JobDTO
		if folder == "" {
			rawJobs, err = jobo.ListJobs(context.Background(), db, client)
		} else {
			rawJobs, err = jobo.ListJobsInFolder(context.Background(), client, folder)
		}
		if err != nil {
			return jobsLoaded{err: err, folder: folder}
		}
		entries := make([]jobEntry, 0, len(rawJobs))
		for _, j := range rawJobs {
			e := jobEntry{Name: j.Name, Class: j.Class, Color: j.Color, Desc: j.Description, URL: j.URL}
			if j.LastBuild != nil {
				e.Build = j.LastBuild.Number
			}
			entries = append(entries, e)
		}
		profileName := controller.GetActiveProfileName(db)
		trackedNames, _ := internaldb.ListTracked(db, "job", profileName, client.BaseURL)
		tracked := make(map[string]bool, len(trackedNames))
		for _, n := range trackedNames {
			tracked[n] = true
		}
		reason := make(map[string]string)
		for _, q := range jobo.ListQueue(context.Background(), client) {
			if q.Why != "" && q.TaskURL != "" {
				reason[strings.TrimRight(q.TaskURL, "/")] = q.Why
			}
		}
		return jobsLoaded{jobs: entries, folder: folder, tracked: tracked, baseURL: client.BaseURL, queueReason: reason}
	}
}

// currentFolder returns the folder currently being viewed ("" = root).
func (s JobScreen) currentFolder() string {
	if len(s.folderStack) == 0 {
		return ""
	}
	return s.folderStack[len(s.folderStack)-1]
}

func (s JobScreen) autoRefreshID() int { return 0 }

func (s *JobScreen) scheduleAutoRefresh() tea.Cmd {
	return tea.Tick(s.refresh.Next(), func(_ time.Time) tea.Msg { return jobAutoRefreshMsg{} })
}

func (s JobScreen) fetchNodes() tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return nodeNamesLoaded{err: err}
		}
		nodes, err := node.ListNodes(context.Background(), client)
		if err != nil {
			return nodeNamesLoaded{err: err}
		}
		names := make([]string, 0, len(nodes)+1)
		names = append(names, "(none)")
		for _, n := range nodes {
			names = append(names, n.DisplayName)
		}
		return nodeNamesLoaded{names: names}
	}
}

func (s JobScreen) fetchAgents(folderName string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return agentsLoaded{err: err}
		}
		grants, err := jobo.ListControlledAgents(client, folderName)
		if err != nil {
			return agentsLoaded{err: err}
		}
		items := make([]components.GrantItem, len(grants))
		for i, g := range grants {
			items[i] = components.GrantItem{Label: g.AgentName, ID: g.GrantID, Pending: g.AgentName == ""}
		}
		return agentsLoaded{items: items}
	}
}

func (s JobScreen) fetchConfigSummary(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return configLoaded{err: err}
		}
		sum, err := jobo.GetJobConfigSummary(context.Background(), client, name)
		return configLoaded{summary: sum, err: err}
	}
}

// detailSummaryLoaded carries a job's config summary for the detail panel
// (fetch-on-cursor-move, cached by caller — distinct from configLoaded, which
// drives the Edit form and always re-fetches).
type detailSummaryLoaded struct {
	name    string
	summary jobo.ConfigSummary
	ok      bool
}

func (s JobScreen) fetchDetailSummary(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return detailSummaryLoaded{name: name}
		}
		sum, err := jobo.GetJobConfigSummary(context.Background(), client, name)
		if err != nil {
			return detailSummaryLoaded{name: name}
		}
		return detailSummaryLoaded{name: name, summary: sum, ok: true}
	}
}

func (s JobScreen) doDelete(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "delete", err: err}
		}
		err = jobo.DeleteJob(context.Background(), client, name)
		return jobOpDone{label: "delete", err: err}
	}
}

// doBulkDelete deletes several jobs, reporting the first error encountered.
func (s JobScreen) doBulkDelete(names []string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "delete", err: err}
		}
		var failed int
		var firstErr error
		for _, n := range names {
			if e := jobo.DeleteJob(context.Background(), client, n); e != nil {
				failed++
				if firstErr == nil {
					firstErr = e
				}
			}
		}
		if firstErr != nil {
			return jobOpDone{label: "delete", err: fmt.Errorf("%d of %d failed: %w", failed, len(names), firstErr)}
		}
		return jobOpDone{label: fmt.Sprintf("delete %d jobs", len(names))}
	}
}

// selectionOrCursor returns the multi-selection when non-empty, else the single
// cursor row's name (as a one-element slice, or empty when nothing is under it).
func (s JobScreen) selectionOrCursor() []string {
	if s.table.SelectedCount() > 0 {
		out := make([]string, 0, s.table.SelectedCount())
		for k := range s.table.Selected() {
			out = append(out, k)
		}
		return out
	}
	if name := s.selectedJobName(); name != "" {
		return []string{name}
	}
	return nil
}

// trackNames tracks or untracks the given job names in the local Mine list.
func (s *JobScreen) trackNames(names []string, track bool) {
	if s.baseURL == "" || len(names) == 0 {
		return
	}
	profileName := controller.GetActiveProfileName(s.db)
	if s.tracked == nil {
		s.tracked = make(map[string]bool)
	}
	for _, n := range names {
		if track {
			_ = internaldb.TrackResource(s.db, "job", n, profileName, s.baseURL)
			s.tracked[n] = true
		} else {
			_ = internaldb.UntrackResource(s.db, "job", n, profileName, s.baseURL)
			delete(s.tracked, n)
		}
	}
}

// rebuildRows regenerates the table rows from the current filtered set.
func (s *JobScreen) rebuildRows() {
	rows, keys := buildJobRows(s.filteredJobs(), s.currentFolder(), s.tracked, s.queueReason)
	s.table.SetRows(rows, keys)
}

func (s JobScreen) doRun(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "run", err: err}
		}
		err = jobo.TriggerBuild(context.Background(), client, name, nil)
		return jobOpDone{label: "run", err: err}
	}
}

// paramDefsLoaded carries a job's parameter definitions so the run flow can
// prompt for values (empty → trigger immediately).
type paramDefsLoaded struct {
	name   string
	params []jobo.StringParamDef
	err    error
}

func (s JobScreen) fetchParamDefs(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return paramDefsLoaded{name: name, err: err}
		}
		defs, err := jobo.GetJobParamDefs(context.Background(), client, name)
		return paramDefsLoaded{name: name, params: defs, err: err}
	}
}

func (s JobScreen) doRunParams(name string, params map[string]string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "run", err: err}
		}
		err = jobo.TriggerBuild(context.Background(), client, name, params)
		return jobOpDone{label: "run " + name, err: err}
	}
}

func (s JobScreen) doStop(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "stop", err: err}
		}
		num, err := jobo.GetLastBuildNumber(context.Background(), client, name)
		if err != nil {
			return jobOpDone{label: "stop", err: err}
		}
		if num == 0 {
			return jobOpDone{label: "stop", err: fmt.Errorf("no builds for %q", name)}
		}
		err = jobo.StopBuild(context.Background(), client, name, num)
		return jobOpDone{label: "stop", err: err}
	}
}

func (s JobScreen) doCreateFreestyle(vals []string, nodes []string) tea.Cmd {
	// vals: 0=name, 1=desc, 2=shell, 3=chdir, 4=node
	db, dbPath := s.db, s.dbPath
	name, desc, shell, chdir, nodeName := vals[0], vals[1], vals[2], vals[3], vals[4]
	if nodeName == "(none)" {
		nodeName = ""
	}
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "create", err: err}
		}
		err = jobo.CreateFreestyleJob(context.Background(), client, jobo.CreateFreestyleParams{
			Name: name, Description: desc, Shell: shell, Chdir: chdir, Node: nodeName,
		})
		return jobOpDone{label: "create " + name, err: err}
	}
}

func (s JobScreen) doCreateFolder(vals []string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	name, desc := vals[0], vals[1]
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "create", err: err}
		}
		err = jobo.CreateFolderJob(context.Background(), client, name, "", desc)
		return jobOpDone{label: "create " + name, err: err}
	}
}

// doCreatePipeline creates a pipeline job. scriptFile is a path to a Groovy
// script (resolved server-side by CreatePipelineJob); node injects an agent label.
func (s JobScreen) doCreatePipeline(name, desc, scriptFile, nodeName string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	name = strings.TrimSpace(name)
	scriptFile = strings.TrimSpace(scriptFile)
	if nodeName == "(none)" {
		nodeName = ""
	}
	return func() tea.Msg {
		if scriptFile == "" {
			return jobOpDone{label: "create", err: fmt.Errorf("pipeline requires a script file")}
		}
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "create", err: err}
		}
		err = jobo.CreatePipelineJob(context.Background(), client, jobo.CreatePipelineParams{
			Name: name, Description: desc, Script: scriptFile, Node: nodeName,
		})
		return jobOpDone{label: "create " + name, err: err}
	}
}

func (s JobScreen) doEditFreestyle(name string, vals []string) tea.Cmd {
	// vals: 0=desc, 1=shell, 2=chdir, 3=node
	db, dbPath := s.db, s.dbPath
	desc, shell, chdir, nodeName := vals[0], vals[1], vals[2], vals[3]
	if nodeName == "(none)" {
		nodeName = ""
	}
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "edit", err: err}
		}
		err = jobo.UpdateFreestyleJob(context.Background(), client, name, jobo.FreestyleUpdateFields{
			Description: &desc, Shell: &shell, Chdir: &chdir, Node: &nodeName,
		})
		return jobOpDone{label: "edit " + name, err: err}
	}
}

func (s JobScreen) doApplySchedule(name, cron string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "schedule", err: err}
		}
		err = jobo.UpdateFreestyleJob(context.Background(), client, name, jobo.FreestyleUpdateFields{
			Schedule: &cron,
		})
		return jobOpDone{label: "schedule " + name, err: err}
	}
}

func (s JobScreen) doApplyEmail(name string, spec components.EmailSpec) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "email", err: err}
		}
		f := jobo.FreestyleUpdateFields{}
		if !spec.Enabled {
			empty := ""
			f.Email = &empty
			f.ClearEmailKeywords = true
			f.ClearEmailRegex = true
		} else {
			f.Email = &spec.Email
			f.EmailCond = &spec.EmailCond
			if spec.EmailKeywords != "" {
				kws := strings.Split(spec.EmailKeywords, ",")
				f.EmailKeywords = &kws
			} else {
				f.ClearEmailKeywords = true
			}
			if spec.EmailRegex != "" {
				f.EmailRegex = &spec.EmailRegex
			} else {
				f.ClearEmailRegex = true
			}
		}
		err = jobo.UpdateFreestyleJob(context.Background(), client, name, f)
		return jobOpDone{label: "email " + name, err: err}
	}
}

func (s JobScreen) doApplyParams(name string, params []jobo.StringParamDef) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "params", err: err}
		}
		defs := jobo.ParamDefsToStrings(params)
		f := jobo.FreestyleUpdateFields{ParamDefs: &defs, ClearParams: len(defs) == 0}
		err = jobo.UpdateFreestyleJob(context.Background(), client, name, f)
		return jobOpDone{label: "params " + name, err: err}
	}
}

func (s JobScreen) doAddAgent(folderName, agentName string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "add-agent", err: err}
		}
		err = node.ApproveFolder(context.Background(), client, agentName, folderName)
		return jobOpDone{label: "add-agent", err: err}
	}
}

func (s JobScreen) doRevokeAgent(folderName, grantID string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "revoke-agent", err: err}
		}
		err = node.RemoveControlledAgentGrant(context.Background(), client, folderName, grantID)
		return jobOpDone{label: "revoke-agent", err: err}
	}
}

func (s JobScreen) fetchBuildHistory(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return buildHistoryLoaded{jobName: name}
		}
		hist, err := jobo.GetBuildHistory(context.Background(), client, name, 20)
		if err != nil || len(hist) == 0 {
			return buildHistoryLoaded{jobName: name}
		}
		nums := make([]int, len(hist))
		for i, b := range hist {
			nums[i] = b.Number
		}
		// sort descending (GetBuildHistory returns newest-first already, but ensure)
		for i := 0; i < len(nums)-1; i++ {
			for j := i + 1; j < len(nums); j++ {
				if nums[j] > nums[i] {
					nums[i], nums[j] = nums[j], nums[i]
				}
			}
		}
		return buildHistoryLoaded{jobName: name, nums: nums}
	}
}

func (s JobScreen) fetchLogChunk(name string, buildNum int, offset int64) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return logChunkLoaded{err: err}
		}
		var text string
		var newOffset int64
		var hasMore bool
		if buildNum == 0 {
			text, newOffset, hasMore, err = jobo.StreamLastBuildLog(context.Background(), client, name, offset)
		} else {
			text, newOffset, hasMore, err = jobo.StreamBuildLog(context.Background(), client, name, buildNum, offset)
		}
		return logChunkLoaded{text: text, newOffset: newOffset, hasMore: hasMore, err: err}
	}
}

func (s JobScreen) fetchScript(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return scriptLoaded{err: err}
		}
		script, err := jobo.GetPipelineScript(context.Background(), client, name)
		return scriptLoaded{script: script, err: err}
	}
}

// pipelineScriptForEdit carries a pipeline's current script into the edit form
// (distinct from scriptLoaded, which feeds the read-only script viewer).
type pipelineScriptForEdit struct {
	name   string
	script string
	err    error
}

func (s JobScreen) fetchPipelineScript(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return pipelineScriptForEdit{name: name, err: err}
		}
		script, err := jobo.GetPipelineScript(context.Background(), client, name)
		return pipelineScriptForEdit{name: name, script: script, err: err}
	}
}

func (s JobScreen) doMove(src, dst string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "move", err: err}
		}
		err = jobo.MoveJob(context.Background(), client, src, dst)
		return jobOpDone{label: "move " + src + " → " + dst, err: err}
	}
}

func (s JobScreen) doClone(src, dst string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "clone", err: err}
		}
		err = jobo.CopyJob(context.Background(), client, src, dst)
		return jobOpDone{label: "clone " + src + " → " + dst, err: err}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func typeLabel(class string) string { return jobo.JobType(class) }

// filteredJobs applies Mine/All filter and search box filter to the full job list.
func (s JobScreen) filteredJobs() []jobEntry {
	jobs := s.jobs
	// Mine filter only at root (folder drill shows everything in subfolder)
	if !s.showAll && s.currentFolder() == "" && len(s.tracked) > 0 {
		present := make(map[string]bool, len(jobs))
		out := make([]jobEntry, 0, len(jobs))
		for _, j := range jobs {
			if s.tracked[j.Name] {
				out = append(out, j)
				present[j.Name] = true
			}
		}
		// Synthesize placeholders for tracked jobs missing from the server so
		// the user sees (and can untrack) stale tracks.
		for name := range s.tracked {
			if !present[name] {
				out = append(out, jobEntry{Name: name, Gone: true})
			}
		}
		jobs = out
	}
	if s.search.Query() == "" {
		return jobs
	}
	out := make([]jobEntry, 0, len(jobs))
	for _, j := range jobs {
		if s.search.Matches(j.Name + " " + j.Desc) {
			out = append(out, j)
		}
	}
	return out
}

func buildJobRows(jobs []jobEntry, currentFolder string, tracked map[string]bool, queueReason map[string]string) ([][]components.Cell, []string) {
	rows := make([][]components.Cell, len(jobs))
	keys := make([]string, len(jobs))
	for i, j := range jobs {
		// strip folder prefix so only the leaf name shows in the table
		leafName := j.Name
		if currentFolder != "" && strings.HasPrefix(j.Name, currentFolder+"/") {
			leafName = j.Name[len(currentFolder)+1:]
		}

		// Synthetic placeholder for a tracked job that's gone from the server.
		if j.Gone {
			rows[i] = []components.Cell{
				{Text: "GONE", Color: theme.ColorError},
				{Text: "—", Dim: true},
				{Text: "★ " + leafName, Color: theme.ColorError},
				{Text: "—", Dim: true},
				{Text: "", Dim: true},
				{Text: "deleted on server", Dim: true},
			}
			keys[i] = j.Name
			continue
		}

		label, col := theme.JobStatusLabel(j.Color)
		reason := ""
		if r, ok := queueReason[strings.TrimRight(j.URL, "/")]; ok && j.URL != "" {
			label, col = "PEND", theme.ColorYellow
			reason = r
		}
		build := "—"
		if j.Build > 0 {
			build = fmt.Sprintf("#%d", j.Build)
		}
		typ := typeLabel(j.Class)
		typColor := ""
		if typ == "FD" || typ == "MB" {
			typColor = theme.ColorYellow
		} else if typ == "PL" {
			typColor = theme.ColorBlue
		}
		// mark folders/multibranch as drillable
		if typ == "FD" || typ == "MB" {
			leafName = leafName + "/ " + theme.SymArrow
		}
		if tracked[j.Name] {
			leafName = "★ " + leafName
		}
		rows[i] = []components.Cell{
			{Text: label, Color: col},
			{Text: typ, Color: typColor},
			{Text: leafName},
			{Text: build},
			{Text: reason, Dim: true},
			{Text: j.Desc, Dim: true},
		}
		keys[i] = j.Name
	}
	return rows, keys
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// strGet returns vals[i] trimmed, or "" if out of range.
func strGet(vals []string, i int) string {
	if i < len(vals) {
		return strings.TrimSpace(vals[i])
	}
	return ""
}

// strGetDef returns vals[i] trimmed, or def if out of range or empty.
func strGetDef(vals []string, i int, def string) string {
	if v := strGet(vals, i); v != "" {
		return v
	}
	return def
}

// current returns the job entry at the table cursor, or nil.
func (s JobScreen) current() *jobEntry {
	filtered := s.filteredJobs()
	i := s.table.Cursor()
	if i < 0 || i >= len(filtered) {
		return nil
	}
	return &filtered[i]
}

// selectedJobType returns the two-letter type ("FS"/"PL"/"FD"/"??") of the
// currently selected row, or "" when nothing is selected.
func (s JobScreen) selectedJobType() string {
	c := s.current()
	if c == nil {
		return ""
	}
	return typeLabel(c.Class)
}

// selectedJobName returns the Name of the currently selected row, or "".
func (s JobScreen) selectedJobName() string {
	c := s.current()
	if c == nil {
		return ""
	}
	return c.Name
}

// menuItemsFor builds the context-menu item list for the given job type,
// returning the items and a map from item-index → jobAction.
func menuItemsFor(typ string) ([]string, []jobAction) {
	all := []struct {
		label string
		act   jobAction
		types string // "*"=all, else comma list
	}{
		{"View Log", actViewLog, "*"},
		{"View Script", actViewScript, "PL"},
		{"Run", actRun, "*"},
		{"Stop", actStop, "*"},
		{"Edit", actEdit, "*"},
		{"Params", actParams, "FS"},
		{"Schedule", actSchedule, "FS"},
		{"Email", actEmail, "FS"},
		{"Delete", actDelete, "*"},
		{"Move", actMove, "FS,FD,MB"},
		{"Clone", actClone, "FS"},
		{"Controlled Agents", actAgents, "FD"},
	}
	var labels []string
	var actions []jobAction
	for _, item := range all {
		if item.types == "*" || strings.Contains(item.types, typ) {
			labels = append(labels, item.label)
			actions = append(actions, item.act)
		}
	}
	return labels, actions
}

// ── Update ────────────────────────────────────────────────────────────────────

// menuActions holds per-menu-open the mapping from item-index to jobAction.
// Stored in JobScreen so Update can use it after MenuSelectMsg fires.
// (Re-computed each time the menu opens via openMenu.)

func (s JobScreen) openMenu(name, typ string) JobScreen {
	s.activeJob = name
	s.activeJobType = typ
	labels, _ := menuItemsFor(typ)
	s.menu.Show("Job: "+name, labels)
	return s
}

// Update handles messages and keyboard input.
func (s JobScreen) Update(msg tea.Msg) (JobScreen, tea.Cmd) {
	// ── overlay priority — highest first ───────────────────────────────────

	// Log viewer and script viewer take absolute priority.
	if s.logViewer.Visible() {
		var cmd tea.Cmd
		s.logViewer, cmd = s.logViewer.Update(msg)
		if _, ok := msg.(components.LogViewerResult); ok {
			s.logOffset = 0
		}
		return s, cmd
	}
	if s.scriptView.Visible() {
		var cmd tea.Cmd
		s.scriptView, cmd = s.scriptView.Update(msg)
		return s, cmd
	}

	// Auto-refresh tick
	if _, ok := msg.(jobAutoRefreshMsg); ok && s.autoRefresh {
		return s, tea.Batch(s.fetchJobs(), s.scheduleAutoRefresh())
	}

	// Form submit/cancel arrive one cycle after the form hides itself, so handle
	// them before the overlay-visibility gates (which would otherwise swallow
	// the deferred message). formIntent disambiguates the flows.
	switch m := msg.(type) {
	case FormSubmitMsg:
		if s.formIntent == "add-agent" {
			folder := s.agentFolder
			cmd2 := s.handleFormSubmit(m.Values)
			s.agents.Show("Controlled Agents — "+folder, "", "Node", "No controlled agents.", "add agent")
			return s, tea.Batch(cmd2, s.fetchAgents(folder))
		}
		return s, s.handleFormSubmit(m.Values)
	case FormCancelMsg:
		if s.formIntent == "add-agent" {
			folder := s.agentFolder
			s.formIntent = ""
			s.agents.Show("Controlled Agents — "+folder, "", "Node", "No controlled agents.", "add agent")
			return s, s.fetchAgents(folder)
		}
		s.formIntent = ""
		return s, nil
	}

	if s.schedule.Visible() {
		var cmd tea.Cmd
		s.schedule, cmd = s.schedule.Update(msg)
		if _, ok := msg.(components.ScheduleResultMsg); !ok {
			return s, cmd
		}
		// handle result
		res := msg.(components.ScheduleResultMsg)
		if !res.Cancelled {
			return s, tea.Batch(cmd, s.doApplySchedule(s.activeJob, res.Cron))
		}
		return s, cmd
	}
	if s.email.Visible() {
		var cmd tea.Cmd
		s.email, cmd = s.email.Update(msg)
		if _, ok := msg.(components.EmailResultMsg); !ok {
			return s, cmd
		}
		res := msg.(components.EmailResultMsg)
		if !res.Cancelled {
			return s, tea.Batch(cmd, s.doApplyEmail(s.activeJob, res.Spec))
		}
		return s, cmd
	}
	if s.params.Visible() {
		var cmd tea.Cmd
		s.params, cmd = s.params.Update(msg)
		if _, ok := msg.(components.ParamListResultMsg); !ok {
			return s, cmd
		}
		res := msg.(components.ParamListResultMsg)
		if !res.Cancelled {
			return s, tea.Batch(cmd, s.doApplyParams(s.activeJob, res.Params))
		}
		return s, cmd
	}
	if s.agents.Visible() {
		var cmd tea.Cmd
		s.agents, cmd = s.agents.Update(msg)
		switch msg := msg.(type) {
		case components.GrantAddMsg:
			s.formIntent = "add-agent"
			// Hide the agents overlay first — it sits earlier in the priority
			// chain than the form, so leaving it visible would swallow all
			// subsequent key input meant for the "Add Agent" form and it would
			// never render either. Re-shown via GrantCloseMsg/openAgents flow
			// isn't needed here since the form result reopens the list itself.
			s.agents.Hide()
			s.form.Show("Add Agent", []formField{
				{Label: "Node name", Required: true},
			})
		case components.GrantRevokeMsg:
			return s, tea.Batch(cmd, s.doRevokeAgent(s.agentFolder, msg.Item.ID))
		case components.GrantRefreshMsg:
			s.agents.Loaded = false
			folder := s.agentFolder
			return s, tea.Batch(cmd, s.fetchAgents(folder))
		case components.GrantCloseMsg:
			s.agents.Hide()
		}
		_ = msg
		return s, cmd
	}
	if s.form.Visible() {
		var cmd tea.Cmd
		s.form, cmd = s.form.Update(msg)
		return s, cmd
	}
	if sel, ok := msg.(MenuSelectMsg); ok {
		_, actions := menuItemsFor(s.activeJobType)
		if sel.Index < len(actions) {
			return s.handleMenuAction(actions[sel.Index])
		}
		return s, nil
	}
	if s.menu.Visible() {
		var cmd tea.Cmd
		s.menu, cmd = s.menu.Update(msg)
		return s, cmd
	}
	if s.confirm.Visible() {
		var cmd tea.Cmd
		s.confirm, cmd = s.confirm.Update(msg)
		if res, ok := msg.(components.ConfirmResultMsg); ok {
			if !res.Yes {
				s.pendingBulkDelete = false
				s.pendingDelete = ""
				return s, cmd
			}
			switch s.pendingAction {
			case actDelete:
				if s.pendingBulkDelete {
					s.pendingBulkDelete = false
					names := make([]string, 0, s.table.SelectedCount())
					for k := range s.table.Selected() {
						names = append(names, k)
					}
					s.table.ClearSelection()
					return s, tea.Batch(cmd, s.doBulkDelete(names))
				}
				name := s.pendingDelete
				s.pendingDelete = ""
				return s, tea.Batch(cmd, s.doDelete(name))
			case actStop:
				name := s.activeJob
				return s, tea.Batch(cmd, s.doStop(name))
			}
		}
		return s, cmd
	}
	if s.message.Visible() {
		var cmd tea.Cmd
		s.message, cmd = s.message.Update(msg)
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

	// ── async results ──────────────────────────────────────────────────────
	switch msg := msg.(type) {
	case jobsLoaded:
		// Discard stale results from a previous folder.
		if msg.folder != s.currentFolder() {
			return s, nil
		}
		s.loading = false
		s.err = msg.err
		if msg.err == nil {
			s.jobs = msg.jobs
			s.tracked = msg.tracked
			s.baseURL = msg.baseURL
			s.queueReason = msg.queueReason
			s.refresh.Reset()
			s.rebuildRows()
		}
		return s, s.maybeFetchDetail()

	case nodeNamesLoaded:
		if msg.err == nil {
			s.nodeNames = msg.names
		}
		// nodes fetched before a create-form open — now open the form. Edit
		// and run flows drive their own form open from their own result msg,
		// so only the create intent should auto-open here.
		if s.formIntent == "create" || s.formIntent == "create-freestyle" {
			return s.openFormAfterNodes()
		}
		return s, nil

	case agentsLoaded:
		if msg.err != nil {
			s.message.Show("Error", msg.err.Error())
		} else {
			s.agents.SetItems(msg.items)
		}
		return s, nil

	case configLoaded:
		if msg.err == nil && s.formIntent == "edit-freestyle" {
			return s.openEditFreestyle(msg.summary)
		}
		return s, nil

	case detailSummaryLoaded:
		if msg.ok {
			sum := msg.summary
			s.detailSummaryCache[msg.name] = sum
			if c := s.current(); c != nil && c.Name == msg.name {
				s.detailSummary = &sum
			}
		}
		return s, nil

	case buildHistoryLoaded:
		if s.logViewer.Visible() && s.logViewer.JobName == msg.jobName {
			s.logViewer.BuildNums = msg.nums
		}
		return s, nil

	case logChunkLoaded:
		if !s.logViewer.Visible() {
			return s, nil
		}
		if msg.err != nil {
			s.logViewer.Status = "error: " + msg.err.Error()
			return s, nil
		}
		if msg.text != "" {
			rawLines := strings.Split(strings.TrimRight(msg.text, "\n"), "\n")
			lines := make([]components.LogLine, len(rawLines))
			for i, l := range rawLines {
				lines[i] = components.LogLine{Text: l, Color: components.ColorForLogLine(l)}
			}
			s.logViewer.AppendLines(lines)
		}
		s.logOffset = msg.newOffset
		if msg.hasMore {
			s.logViewer.Status = "streaming…"
			return s, s.fetchLogChunk(s.logViewer.JobName, s.logViewer.BuildNum, s.logOffset)
		}
		s.logViewer.Status = "finished"
		return s, nil

	case scriptLoaded:
		if s.scriptView.Visible() {
			s.scriptView.SetScript(msg.script, msg.err)
		}
		return s, nil

	case pipelineScriptForEdit:
		if msg.err != nil {
			s.message.Show("Error", msg.err.Error())
			return s, nil
		}
		var cmd tea.Cmd
		s, cmd = s.openEditPipeline(msg.name, msg.script)
		return s, cmd

	case paramDefsLoaded:
		if msg.err != nil {
			s.message.Show("Error", msg.err.Error())
			return s, nil
		}
		if len(msg.params) == 0 {
			return s, s.doRun(msg.name)
		}
		s.formIntent = "run-params"
		s.runParamNames = make([]string, len(msg.params))
		fields := make([]formField, len(msg.params))
		for i, p := range msg.params {
			s.runParamNames[i] = p.Name
			fields[i] = formField{Label: p.Name, Value: p.DefaultValue, Placeholder: p.Description}
		}
		s.form.Show("Run "+msg.name+" — parameters", fields)
		return s, nil

	case scheduleReadyMsg:
		s.activeJob = msg.name
		s.schedule.Show(msg.spec, msg.name)
		return s, nil

	case emailReadyMsg:
		s.activeJob = msg.name
		s.email.Show(msg.spec, true)
		return s, nil

	case paramsReadyMsg:
		s.activeJob = msg.name
		defs := jobo.ParamDefsFromStrings(msg.defs)
		s.params.Show(defs)
		return s, nil

	case jobOpDone:
		if msg.err != nil {
			s.message.Show("Error", msg.err.Error())
		} else {
			s.message.Show("Done", msg.label+" succeeded.")
			return s, s.fetchJobs()
		}
		return s, nil

	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.table.SetSize(msg.Width, maxInt(5, msg.Height-12))
		s.menu.width = msg.Width
		s.form.width = msg.Width
		s.confirm.SetWidth(msg.Width)
		s.message.SetWidth(msg.Width)
		s.schedule.Width = msg.Width
		s.email.Width = msg.Width
		s.params.Width = msg.Width
		s.agents.Width = msg.Width
		s.logViewer.SetSize(msg.Width, msg.Height)
		s.scriptView.SetSize(msg.Width, msg.Height)
		return s, nil
	}

	// ── list-view keys ─────────────────────────────────────────────────────
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "/":
			s.search.Open()
			return s, nil
		case "ctrl+a":
			s.showAll = !s.showAll
			_ = internaldb.SetScopeShowAll(s.db, "job", s.showAll)
			s.rebuildRows()
			return s, nil
		case "esc":
			if s.table.SelectedCount() > 0 {
				s.table.ClearSelection()
				return s, nil
			}
			return s, nil
		case "i":
			names := s.selectionOrCursor()
			s.trackNames(names, true)
			s.table.ClearSelection()
			s.rebuildRows()
			return s, nil
		case "u":
			names := s.selectionOrCursor()
			s.trackNames(names, false)
			s.table.ClearSelection()
			s.rebuildRows()
			return s, nil
		case "backspace":
			// drill back up one folder level
			if len(s.folderStack) > 0 {
				s.folderStack = s.folderStack[:len(s.folderStack)-1]
				s.jobs = nil
				s.loading = true
				s.search = components.SearchBox{}
				return s, s.fetchJobs()
			}
			return s, nil
		case "f", "F":
			s.autoRefresh = !s.autoRefresh
			if s.autoRefresh {
				s.refresh.Reset()
				return s, s.scheduleAutoRefresh()
			}
			return s, nil
		case "enter":
			name := s.selectedJobName()
			typ := s.selectedJobType()
			if name == "" {
				return s, nil
			}
			// A "gone" placeholder has no server object — only untrack (u) applies.
			if c := s.current(); c != nil && c.Gone {
				return s, nil
			}
			// Drill into folders/multibranch instead of opening the menu.
			if typ == "FD" || typ == "MB" {
				s.folderStack = append(s.folderStack, name)
				s.jobs = nil
				s.loading = true
				s.search = components.SearchBox{}
				return s, s.fetchJobs()
			}
			s = s.openMenu(name, typ)
			return s, nil
		case "ctrl+n":
			// new job — fetch nodes first, then open create form
			s.formIntent = "create-freestyle"
			if s.nodeNames == nil {
				return s, s.fetchNodes()
			}
			return s.openCreateForm()
		case "ctrl+d":
			if n := s.table.SelectedCount(); n > 0 {
				s.pendingAction = actDelete
				s.pendingBulkDelete = true
				s.confirm.Show("Delete Jobs", fmt.Sprintf("Delete %d selected job(s)? This cannot be undone.", n))
				return s, nil
			}
			name := s.selectedJobName()
			if name == "" {
				return s, nil
			}
			s.pendingDelete = name
			s.pendingAction = actDelete
			s.confirm.Show("Delete Job", fmt.Sprintf("Delete '%s'? This cannot be undone.", name))
			return s, nil
		case "c":
			name := s.selectedJobName()
			if name == "" || s.selectedJobType() != "FS" {
				return s, nil
			}
			s.activeJob = name
			s.formIntent = "clone"
			s.form.Show("Clone: "+name, []formField{
				{Label: "New name", Placeholder: name + "-copy", Required: true},
			})
			return s, nil
		case "m":
			name := s.selectedJobName()
			typ := s.selectedJobType()
			if name == "" || (typ != "FS" && typ != "FD" && typ != "MB") {
				return s, nil
			}
			s.activeJob = name
			s.formIntent = "move"
			s.form.Show("Move: "+name, []formField{
				{Label: "Destination", Placeholder: "folder/newname", Required: true},
			})
			return s, nil
		case "A":
			name := s.selectedJobName()
			typ := s.selectedJobType()
			if name == "" || typ != "FD" {
				return s, nil
			}
			return s.openAgents(name)
		case "r":
			s.loading = true
			return s, s.fetchJobs()
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

// maybeFetchDetail serves the detail-panel config summary from cache, or
// kicks off a background fetch when the highlighted job hasn't been seen yet.
func (s *JobScreen) maybeFetchDetail() tea.Cmd {
	c := s.current()
	if c == nil || c.Gone {
		s.detailSummary = nil
		return nil
	}
	if cached, ok := s.detailSummaryCache[c.Name]; ok {
		sum := cached
		s.detailSummary = &sum
		return nil
	}
	s.detailSummary = nil
	return s.fetchDetailSummary(c.Name)
}

// ── menu action dispatch ──────────────────────────────────────────────────────

func (s JobScreen) handleMenuAction(act jobAction) (JobScreen, tea.Cmd) {
	name := s.activeJob
	typ := s.activeJobType
	switch act {
	case actViewLog:
		s.menu.Hide()
		s.logViewer.Show(name)
		s.logOffset = 0
		return s, tea.Batch(
			s.fetchLogChunk(name, 0, 0),
			s.fetchBuildHistory(name),
		)
	case actViewScript:
		s.menu.Hide()
		s.scriptView.Show(name)
		return s, s.fetchScript(name)
	case actRun:
		s.menu.Hide()
		s.activeJob = name
		return s, s.fetchParamDefs(name)
	case actStop:
		s.menu.Hide()
		s.pendingAction = actStop
		s.confirm.Show("Stop Job", fmt.Sprintf("Stop latest build of '%s'?", name))
		return s, nil
	case actEdit:
		s.menu.Hide()
		if typ == "FS" || typ == "FD" {
			s.formIntent = "edit-freestyle"
			if s.nodeNames == nil {
				return s, tea.Batch(s.fetchNodes(), s.fetchConfigSummary(name))
			}
			return s, s.fetchConfigSummary(name)
		}
		if typ == "PL" {
			s.formIntent = "edit-pipeline"
			s.activeJob = name
			if s.nodeNames == nil {
				return s, tea.Batch(s.fetchNodes(), s.fetchPipelineScript(name))
			}
			return s, s.fetchPipelineScript(name)
		}
		s.message.Show("Edit", "This job type can't be edited from the TUI.")
		return s, nil
	case actParams:
		s.menu.Hide()
		var cmd tea.Cmd
		s, cmd = s.openParamsOverlay(name)
		return s, cmd
	case actSchedule:
		s.menu.Hide()
		var cmd tea.Cmd
		s, cmd = s.openScheduleOverlay(name)
		return s, cmd
	case actEmail:
		s.menu.Hide()
		var cmd tea.Cmd
		s, cmd = s.openEmailOverlay(name)
		return s, cmd
	case actDelete:
		s.pendingAction = actDelete
		s.pendingDelete = name
		s.menu.Hide()
		s.confirm.Show("Delete Job", fmt.Sprintf("Delete '%s'? This cannot be undone.", name))
		return s, nil
	case actMove:
		s.menu.Hide()
		s.formIntent = "move"
		s.form.Show("Move: "+name, []formField{
			{Label: "Destination", Placeholder: "folder/newname", Required: true},
		})
		return s, nil
	case actClone:
		s.menu.Hide()
		s.formIntent = "clone"
		s.form.Show("Clone: "+name, []formField{
			{Label: "New name", Placeholder: name+"-copy", Required: true},
		})
		return s, nil
	case actAgents:
		s.menu.Hide()
		return s.openAgents(name)
	}
	return s, nil
}

// ── overlay openers ───────────────────────────────────────────────────────────

func (s JobScreen) openScheduleOverlay(name string) (JobScreen, tea.Cmd) {
	db, dbPath := s.db, s.dbPath
	s.activeJob = name
	return s, func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "schedule", err: err}
		}
		sum, _ := jobo.GetJobConfigSummary(context.Background(), client, name)
		spec := jobo.ParseCron(sum.Schedule)
		return scheduleReadyMsg{spec: spec, name: name}
	}
}

type scheduleReadyMsg struct {
	spec jobo.ScheduleSpec
	name string
}

func (s JobScreen) openEmailOverlay(name string) (JobScreen, tea.Cmd) {
	db, dbPath := s.db, s.dbPath
	s.activeJob = name
	return s, func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "email", err: err}
		}
		sum, _ := jobo.GetJobConfigSummary(context.Background(), client, name)
		spec := components.EmailSpec{
			Enabled:       sum.Email != "" && sum.Email != "-",
			Email:         sum.Email,
			EmailCond:     sum.EmailCond,
			EmailKeywords: sum.EmailKeyword,
			EmailRegex:    sum.EmailRegex,
		}
		if spec.EmailCond == "" || spec.EmailCond == "-" {
			spec.EmailCond = "failed"
		}
		return emailReadyMsg{spec: spec, name: name}
	}
}

type emailReadyMsg struct {
	spec components.EmailSpec
	name string
}

func (s JobScreen) openParamsOverlay(name string) (JobScreen, tea.Cmd) {
	db, dbPath := s.db, s.dbPath
	s.activeJob = name
	return s, func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "params", err: err}
		}
		sum, _ := jobo.GetJobConfigSummary(context.Background(), client, name)
		var defs []string
		if sum.Params != "" && sum.Params != "-" {
			defs = strings.Split(sum.Params, ",")
		}
		return paramsReadyMsg{defs: defs, name: name}
	}
}

type paramsReadyMsg struct {
	defs []string
	name string
}

func (s JobScreen) openAgents(folderName string) (JobScreen, tea.Cmd) {
	s.agentFolder = folderName
	s.agents.Show(
		"Controlled Agents — "+folderName,
		"",
		"Node",
		"No controlled agents.",
		"add agent",
	)
	return s, s.fetchAgents(folderName)
}

// ── form helpers ──────────────────────────────────────────────────────────────

func (s JobScreen) openCreateForm() (JobScreen, tea.Cmd) {
	nodeOpts := s.nodeNames
	if len(nodeOpts) == 0 {
		nodeOpts = []string{"(none)"}
	}
	s.form.Show("New Job", []formField{
		{Label: "Name", Required: true},
		{Label: "Type", Options: []string{"freestyle", "folder", "pipeline"}, Value: "freestyle"},
		{Label: "Description"},
		{Label: "Shell command", Placeholder: "echo hello (freestyle)"},
		{Label: "Working dir", Placeholder: "/home/jenkins (freestyle)"},
		{Label: "Script file", Placeholder: "path/to/Jenkinsfile (pipeline)"},
		{Label: "Node", Options: nodeOpts, Value: nodeOpts[0]},
	})
	s.formIntent = "create"
	return s, nil
}

func (s JobScreen) openFormAfterNodes() (JobScreen, tea.Cmd) {
	return s.openCreateForm()
}

func (s JobScreen) openEditFreestyle(sum jobo.ConfigSummary) (JobScreen, tea.Cmd) {
	nodeOpts := s.nodeNames
	if len(nodeOpts) == 0 {
		nodeOpts = []string{"(none)"}
	}
	initNode := sum.Node
	if initNode == "" || initNode == "-" {
		initNode = nodeOpts[0]
	}
	s.form.Show("Edit: "+s.activeJob, []formField{
		{Label: "Description", Value: func() string {
			if sum.Description == "-" {
				return ""
			}
			return sum.Description
		}()},
		{Label: "Shell command", Value: func() string {
			if sum.ShellCmd == "-" {
				return ""
			}
			return sum.ShellCmd
		}()},
		{Label: "Working dir", Value: func() string {
			if sum.Chdir == "-" {
				return ""
			}
			return sum.Chdir
		}()},
		{Label: "Node", Options: nodeOpts, Value: initNode},
	})
	return s, nil
}

// openEditPipeline opens the pipeline edit form. curScript is shown as a
// read-only preview in the title hint; the Script field takes a new file path
// (leave blank to keep the current script and only update description/node).
func (s JobScreen) openEditPipeline(name, curScript string) (JobScreen, tea.Cmd) {
	nodeOpts := s.nodeNames
	if len(nodeOpts) == 0 {
		nodeOpts = []string{"(none)"}
	}
	preview := strings.TrimSpace(curScript)
	if len(preview) > 48 {
		preview = preview[:48] + "…"
	}
	title := "Edit pipeline: " + name
	if preview != "" {
		title += "  (current: " + strings.ReplaceAll(preview, "\n", " ") + ")"
	}
	s.form.Show(title, []formField{
		{Label: "Description"},
		{Label: "Script file", Placeholder: "path/to/Jenkinsfile (blank = keep)"},
		{Label: "Node", Options: nodeOpts, Value: nodeOpts[0]},
	})
	return s, nil
}

func (s JobScreen) doEditPipeline(name string, vals []string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	desc := strGet(vals, 0)
	scriptFile := strGet(vals, 1)
	nodeName := strGet(vals, 2)
	if nodeName == "(none)" {
		nodeName = ""
	}
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobOpDone{label: "edit", err: err}
		}
		err = jobo.UpdatePipelineFromTUI(context.Background(), client, name, desc, scriptFile, nodeName)
		return jobOpDone{label: "edit " + name, err: err}
	}
}

func (s JobScreen) handleFormSubmit(vals []string) tea.Cmd {
	intent := s.formIntent
	s.formIntent = ""
	switch intent {
	case "create":
		// vals: 0=name, 1=type, 2=desc, 3=shell, 4=chdir, 5=scriptFile, 6=node
		if len(vals) < 7 {
			return nil
		}
		switch vals[1] {
		case "folder":
			return s.doCreateFolder([]string{vals[0], vals[2]})
		case "pipeline":
			return s.doCreatePipeline(vals[0], vals[2], vals[5], vals[6])
		default: // freestyle
			return s.doCreateFreestyle([]string{vals[0], vals[2], vals[3], vals[4], vals[6]}, s.nodeNames)
		}
	case "edit-freestyle":
		if len(vals) < 4 {
			return nil
		}
		return s.doEditFreestyle(s.activeJob, vals)
	case "edit-pipeline":
		if len(vals) < 3 {
			return nil
		}
		return s.doEditPipeline(s.activeJob, vals)
	case "run-params":
		params := map[string]string{}
		for i, name := range s.runParamNames {
			if i < len(vals) {
				params[name] = vals[i]
			}
		}
		return s.doRunParams(s.activeJob, params)
	case "add-agent":
		if len(vals) < 1 || strings.TrimSpace(vals[0]) == "" {
			return nil
		}
		return s.doAddAgent(s.agentFolder, strings.TrimSpace(vals[0]))
	case "move":
		if len(vals) < 1 || strings.TrimSpace(vals[0]) == "" {
			return nil
		}
		return s.doMove(s.activeJob, strings.TrimSpace(vals[0]))
	case "clone":
		if len(vals) < 1 || strings.TrimSpace(vals[0]) == "" {
			return nil
		}
		return s.doClone(s.activeJob, strings.TrimSpace(vals[0]))
	}
	return nil
}

// ── View ──────────────────────────────────────────────────────────────────────

// View renders the job screen.
func (s JobScreen) View() string {
	// overlay priority (first visible wins)
	if s.logViewer.Visible() {
		return s.logViewer.View()
	}
	if s.scriptView.Visible() {
		return s.scriptView.View()
	}
	if s.schedule.Visible() {
		return s.schedule.View()
	}
	if s.email.Visible() {
		return s.email.View()
	}
	if s.params.Visible() {
		return s.params.View()
	}
	if s.agents.Visible() {
		return s.agents.View()
	}
	if s.form.Visible() {
		return s.form.View()
	}
	if s.menu.Visible() {
		return s.menu.View()
	}
	if s.confirm.Visible() {
		return s.confirm.View()
	}
	if s.message.Visible() {
		return s.message.View()
	}

	// breadcrumb header
	title := theme.SymGear + " Jobs"
	if len(s.folderStack) > 0 {
		crumb := " › " + strings.Join(s.folderStack, " › ")
		title += theme.StyleDim.Render(crumb) + theme.StyleDim.Render("  ↤ Backspace")
	}
	if s.currentFolder() == "" {
		if !s.showAll {
			title += " " + theme.StyleBlue.Render("[mine]")
		} else {
			title += " " + theme.StyleDim.Render("[all]")
		}
	}
	if s.autoRefresh {
		title += theme.StyleDim.Render("  [auto]")
	}
	if n := s.table.SelectedCount(); n > 0 {
		title += " " + theme.StyleWarning.Render(fmt.Sprintf("[%d selected]", n))
	}

	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(title) + "\n")
	if s.loading {
		sb.WriteString(theme.StyleDim.Render(theme.SymLoading + " Loading jobs..."))
		return sb.String()
	}
	if s.err != nil {
		sb.WriteString(theme.StyleError.Render(theme.SymFail + " " + s.err.Error()))
		return sb.String()
	}
	if len(s.jobs) == 0 {
		sb.WriteString(theme.StyleDim.Render("No jobs. Press Ctrl+n to create one."))
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
		if c.Build > 0 {
			sb.WriteString(theme.StyleDim.Render(fmt.Sprintf("  #%d", c.Build)))
		}
		if reason, ok := s.queueReason[strings.TrimRight(c.URL, "/")]; ok && c.URL != "" {
			sb.WriteString("  " + theme.StyleWarning.Render(theme.SymLoading+" "+reason))
		}
		sb.WriteString("\n")
		sb.WriteString(theme.StyleDim.Render("type ") + theme.StyleBlue.Render(typeLabel(c.Class)))
		if s.detailSummary != nil {
			sum := s.detailSummary
			if sum.Schedule != "" && sum.Schedule != "-" {
				sb.WriteString(theme.StyleDim.Render("   schedule ") + sum.Schedule)
			}
			if sum.Node != "" && sum.Node != "-" {
				sb.WriteString(theme.StyleDim.Render("   node ") + sum.Node)
			}
			if sum.Email != "" && sum.Email != "-" {
				sb.WriteString(theme.StyleDim.Render("   email ") + sum.Email)
			}
		}
		sb.WriteString("\n")
		if c.URL != "" {
			sb.WriteString(theme.StyleSubtle.Render(c.URL))
			sb.WriteString("\n")
		}
		if c.Desc != "" {
			sb.WriteString(theme.StyleDim.Render(c.Desc))
			sb.WriteString("\n")
		}
	}

	sb.WriteString(theme.StyleDim.Render("Enter drill/menu  ·  Space select  ·  ⌫ up  ·  ^N new  ·  c clone  ·  m move  ·  ^D delete  ·  A agents  ·  ^A mine/all  ·  i/u track  ·  F auto  ·  r refresh  ·  / search"))
	return sb.String()
}
