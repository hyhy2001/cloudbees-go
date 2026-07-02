// Package screens provides the TUI screen implementations.
package screens

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

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
}

type jobsLoaded struct {
	jobs []jobEntry
	err  error
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

// ── form (multi-field inline text entry) ─────────────────────────────────────

type formField struct {
	Label       string
	Value       string
	Placeholder string
	Required    bool
	// Options non-nil → cycle-select, not free-text.
	Options []string
}

type formOverlay struct {
	Title   string
	fields  []formField
	cursor  int
	buf     []string
	visible bool
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
		if len(cur.Options) > 0 {
			// cycle and advance if not last
			opts := cur.Options
			idx := 0
			for i, o := range opts {
				if o == f.buf[f.cursor] {
					idx = i
					break
				}
			}
			f.buf[f.cursor] = opts[(idx+1)%len(opts)]
			return f, nil
		}
		if f.cursor == len(f.fields)-1 {
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
			}
		case tea.KeyRunes:
			f.buf[f.cursor] += string(km.Runes)
		case tea.KeySpace:
			f.buf[f.cursor] += " "
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
		if len(fl.Options) > 0 {
			if on {
				display = theme.StyleDim.Render("◀ ") + val + theme.StyleDim.Render(" ▶")
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
	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("Tab/↑↓ move · ←→ cycle (select) · Enter next/submit · Esc cancel"))
	return sb.String()
}

// ── context menu ──────────────────────────────────────────────────────────────

type menuOverlay struct {
	title   string
	items   []string
	cursor  int
	visible bool
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
		if on := i == m.cursor; on {
			sb.WriteString(theme.StyleKeyHint.Render(theme.SymArrow + " " + item))
		} else {
			sb.WriteString(theme.StyleDim.Render("  " + item))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("↑↓ move · Enter select · Esc back"))
	return sb.String()
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

	table   components.TableModel
	loading bool
	err     error
	jobs    []jobEntry
	width   int
	height  int

	// overlay state — exactly one active at a time; priority in View()
	menu     menuOverlay
	form     formOverlay
	schedule components.ScheduleBuilder
	email    components.EmailBuilder
	params   components.ParamListEditor
	agents   components.GrantListOverlay
	confirm  components.ConfirmModal
	message  components.MessageModal

	// context carrying which job an overlay is operating on
	activeJob     string
	activeJobType string // FS / PL / FD
	// for confirm, remember what it is confirming
	pendingDelete string
	pendingAction jobAction
	// for "add agent" form, remember the folder
	agentFolder string

	// nodes list (fetched lazily for create/edit forms)
	nodeNames []string

	// "what did the form submission mean?"
	formIntent string // "create-freestyle","create-pipeline","create-folder","edit-freestyle","edit-pipeline","add-agent"
}

// NewJobScreen constructs a JobScreen.
func NewJobScreen(db *sql.DB, dbPath string) JobScreen {
	cols := []table.Column{
		{Title: "Name", Width: 36},
		{Title: "Type", Width: 6},
		{Title: "Status", Width: 12},
		{Title: "Build #", Width: 9},
		{Title: "Description", Width: 28},
	}
	return JobScreen{
		db:      db,
		dbPath:  dbPath,
		table:   components.New(cols, nil),
		loading: true,
	}
}

// Init fires the initial data fetch.
func (s JobScreen) Init() tea.Cmd {
	return s.fetchJobs()
}

// ── data fetches ──────────────────────────────────────────────────────────────

func (s JobScreen) fetchJobs() tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobsLoaded{err: err}
		}
		jobs, err := jobo.ListJobs(context.Background(), db, client)
		if err != nil {
			return jobsLoaded{err: err}
		}
		entries := make([]jobEntry, 0, len(jobs))
		for _, j := range jobs {
			e := jobEntry{Name: j.Name, Class: j.Class, Color: j.Color, Desc: j.Description}
			if j.LastBuild != nil {
				e.Build = j.LastBuild.Number
			}
			entries = append(entries, e)
		}
		return jobsLoaded{jobs: entries}
	}
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

// ── helpers ───────────────────────────────────────────────────────────────────

func typeLabel(class string) string { return jobo.JobType(class) }

func buildJobRows(jobs []jobEntry) []table.Row {
	rows := make([]table.Row, len(jobs))
	for i, j := range jobs {
		label, _ := theme.JobStatusLabel(j.Color)
		build := "—"
		if j.Build > 0 {
			build = fmt.Sprintf("#%d", j.Build)
		}
		desc := j.Desc
		if len(desc) > 28 {
			desc = desc[:25] + "..."
		}
		rows[i] = table.Row{j.Name, typeLabel(j.Class), label, build, desc}
	}
	return rows
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// selectedJobType returns the two-letter type ("FS"/"PL"/"FD"/"??") of the
// currently selected row, or "" when nothing is selected.
func (s JobScreen) selectedJobType() string {
	row := s.table.SelectedRow()
	if row == nil {
		return ""
	}
	return row[1]
}

// selectedJobName returns the Name of the currently selected row, or "".
func (s JobScreen) selectedJobName() string {
	row := s.table.SelectedRow()
	if row == nil {
		return ""
	}
	return row[0]
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
		{"Move", actMove, "FS,FD"},
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
		switch msg.(type) {
		case FormSubmitMsg:
			res := msg.(FormSubmitMsg)
			return s, tea.Batch(cmd, s.handleFormSubmit(res.Values))
		case FormCancelMsg:
			// nothing extra needed
		}
		return s, cmd
	}
	if s.menu.Visible() {
		var cmd tea.Cmd
		s.menu, cmd = s.menu.Update(msg)
		if sel, ok := msg.(MenuSelectMsg); ok {
			_, actions := menuItemsFor(s.activeJobType)
			if sel.Index < len(actions) {
				return s.handleMenuAction(actions[sel.Index])
			}
		}
		return s, cmd
	}
	if s.confirm.Visible() {
		var cmd tea.Cmd
		s.confirm, cmd = s.confirm.Update(msg)
		if res, ok := msg.(components.ConfirmResultMsg); ok && res.Yes {
			switch s.pendingAction {
			case actDelete:
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

	// ── async results ──────────────────────────────────────────────────────
	switch msg := msg.(type) {
	case jobsLoaded:
		s.loading = false
		s.err = msg.err
		if msg.err == nil {
			s.jobs = msg.jobs
			s.table.SetRows(buildJobRows(s.jobs))
		}
		return s, nil

	case nodeNamesLoaded:
		if msg.err == nil {
			s.nodeNames = msg.names
		}
		// nodes fetched before a form open — now open the form
		if s.formIntent != "" {
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
		s.table.SetSize(msg.Width, maxInt(5, msg.Height-8))
		return s, nil
	}

	// ── list-view keys ─────────────────────────────────────────────────────
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "enter":
			name := s.selectedJobName()
			typ := s.selectedJobType()
			if name == "" {
				return s, nil
			}
			if typ == "FD" {
				// folder → treat as drill (show agents shortcut) or menu
				s = s.openMenu(name, typ)
			} else {
				s = s.openMenu(name, typ)
			}
			return s, nil
		case "ctrl+n":
			// new job — fetch nodes first, then open create form
			s.formIntent = "create-freestyle"
			if s.nodeNames == nil {
				return s, s.fetchNodes()
			}
			return s.openCreateForm()
		case "ctrl+x":
			name := s.selectedJobName()
			if name == "" {
				return s, nil
			}
			s.pendingDelete = name
			s.pendingAction = actDelete
			s.confirm.Show("Delete Job", fmt.Sprintf("Delete '%s'? This cannot be undone.", name))
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

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

// ── menu action dispatch ──────────────────────────────────────────────────────

func (s JobScreen) handleMenuAction(act jobAction) (JobScreen, tea.Cmd) {
	name := s.activeJob
	typ := s.activeJobType
	switch act {
	case actViewLog:
		s.menu.Hide()
		s.message.Show("Log: "+name, "Log viewing not yet implemented in TUI. Use: bee job log "+name)
		return s, nil
	case actViewScript:
		s.menu.Hide()
		s.message.Show("Script: "+name, "Script viewing not yet implemented in TUI. Use: bee job get "+name)
		return s, nil
	case actRun:
		s.menu.Hide()
		return s, s.doRun(name)
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
		s.message.Show("Edit", "Pipeline TUI edit not yet implemented. Use: bee job update pipeline "+name)
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
		s.message.Show("Move", "Move not yet implemented in TUI. Use: bee job move "+name)
		return s, nil
	case actClone:
		s.menu.Hide()
		s.formIntent = "create-freestyle"
		s.message.Show("Clone", "Clone not yet implemented in TUI. Use: bee job clone "+name)
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
		{Label: "Shell command", Placeholder: "echo hello"},
		{Label: "Working dir", Placeholder: "/home/jenkins"},
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

func (s JobScreen) handleFormSubmit(vals []string) tea.Cmd {
	intent := s.formIntent
	s.formIntent = ""
	switch intent {
	case "create":
		// vals: 0=name, 1=type, 2=desc, 3=shell, 4=chdir, 5=node
		if len(vals) < 6 {
			return nil
		}
		switch vals[1] {
		case "folder":
			return s.doCreateFolder([]string{vals[0], vals[2]})
		default: // freestyle
			return s.doCreateFreestyle([]string{vals[0], vals[2], vals[3], vals[4], vals[5]}, s.nodeNames)
		}
	case "edit-freestyle":
		if len(vals) < 4 {
			return nil
		}
		return s.doEditFreestyle(s.activeJob, vals)
	case "add-agent":
		if len(vals) < 1 || strings.TrimSpace(vals[0]) == "" {
			return nil
		}
		return s.doAddAgent(s.agentFolder, strings.TrimSpace(vals[0]))
	}
	return nil
}

// ── View ──────────────────────────────────────────────────────────────────────

// View renders the job screen.
func (s JobScreen) View() string {
	// overlay priority (first visible wins)
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

	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(theme.SymGear+" Jobs") + "\n")
	if s.loading {
		sb.WriteString(theme.StyleDim.Render(theme.SymLoading + " Loading jobs..."))
		return sb.String()
	}
	if s.err != nil {
		sb.WriteString(theme.StyleError.Render(theme.SymFail + " " + s.err.Error()))
		return sb.String()
	}
	if len(s.jobs) == 0 {
		sb.WriteString(theme.StyleDim.Render("No jobs found. Press Ctrl+n to create one."))
		return sb.String()
	}
	sb.WriteString(s.table.View())
	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("enter=menu  ^n=new  ^X=delete  A=agents(folder)  r=refresh  ^F=search"))
	return sb.String()
}
