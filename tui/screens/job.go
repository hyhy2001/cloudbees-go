// Package screens provides the TUI screen implementations.
package screens

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hyhy2001/bee/plugins/controller"
	"github.com/hyhy2001/bee/tui/components"
	"github.com/hyhy2001/bee/tui/theme"
)

// jobEntry is the TUI job row.
type jobEntry struct {
	Name        string
	Class       string
	Color       string
	Description string
	LastBuild   int
}

// jobsLoaded carries the fetched jobs.
type jobsLoaded struct {
	jobs []jobEntry
	err  error
}

// jobDeleted signals a delete completed.
type jobDeleted struct{ err error }

// JobScreen is the TUI screen for listing/managing Jenkins jobs.
type JobScreen struct {
	db       *sql.DB
	dbPath   string
	table    components.TableModel
	modal    components.ConfirmModal
	detail   components.MessageModal
	loading  bool
	err      error
	jobs     []jobEntry
	width    int
	height   int
	deleting string // name of job being deleted
}

// NewJobScreen creates a new JobScreen.
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

func (s JobScreen) fetchJobs() tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobsLoaded{err: err}
		}
		tree := "jobs[_class,name,url,color,description,buildable,lastBuild[number,result]]"
		var result struct {
			Jobs []struct {
				Class       string `json:"_class"`
				Name        string `json:"name"`
				Color       string `json:"color"`
				Description string `json:"description"`
				LastBuild   *struct {
					Number int `json:"number"`
				} `json:"lastBuild"`
			} `json:"jobs"`
		}
		if err := client.GetJSON(context.Background(), "/api/json?tree="+url.QueryEscape(tree), &result); err != nil {
			return jobsLoaded{err: err}
		}
		jobs := make([]jobEntry, 0, len(result.Jobs))
		for _, j := range result.Jobs {
			e := jobEntry{
				Name:        j.Name,
				Class:       j.Class,
				Color:       j.Color,
				Description: j.Description,
			}
			if j.LastBuild != nil {
				e.LastBuild = j.LastBuild.Number
			}
			jobs = append(jobs, e)
		}
		return jobsLoaded{jobs: jobs}
	}
}

func (s JobScreen) doDelete(name string) tea.Cmd {
	db, dbPath := s.db, s.dbPath
	return func() tea.Msg {
		client, err := controller.GetActiveControllerClient(db, dbPath)
		if err != nil {
			return jobDeleted{err: err}
		}
		seg := jobPathSegments(name)
		if err := client.PostForm(context.Background(), "/job/"+seg+"/doDelete", nil); err != nil {
			return jobDeleted{err: err}
		}
		return jobDeleted{}
	}
}

func jobPathSegments(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/job/")
}

func typeLabel(class string) string {
	switch {
	case strings.Contains(class, "FreeStyle") || strings.Contains(class, "Freestyle"):
		return "FS"
	case strings.Contains(class, "Pipeline") || strings.Contains(class, "WorkflowJob"):
		return "PL"
	case strings.Contains(class, "Folder") || strings.Contains(class, "WorkflowMultiBranchProject"):
		return "FD"
	case strings.Contains(class, "MultibranchPipeline"):
		return "MB"
	default:
		parts := strings.Split(class, ".")
		last := parts[len(parts)-1]
		if len(last) > 4 {
			return last[:4]
		}
		if last != "" {
			return last
		}
		return "--"
	}
}

func buildJobRows(jobs []jobEntry) []table.Row {
	rows := make([]table.Row, len(jobs))
	for i, j := range jobs {
		label, _ := theme.JobStatusLabel(j.Color)
		build := "—"
		if j.LastBuild > 0 {
			build = fmt.Sprintf("#%d", j.LastBuild)
		}
		desc := j.Description
		if len(desc) > 28 {
			desc = desc[:25] + "..."
		}
		rows[i] = table.Row{j.Name, typeLabel(j.Class), label, build, desc}
	}
	return rows
}

// Update handles messages and keyboard input.
func (s JobScreen) Update(msg tea.Msg) (JobScreen, tea.Cmd) {
	// Delegate to confirm modal first.
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
	case jobsLoaded:
		s.loading = false
		s.err = msg.err
		if msg.err == nil {
			s.jobs = msg.jobs
			s.table.SetRows(buildJobRows(s.jobs))
		}
		return s, nil

	case jobDeleted:
		if msg.err != nil {
			s.detail.Show("Error", msg.err.Error())
		} else {
			s.detail.Show("Deleted", fmt.Sprintf("Job '%s' deleted.", s.deleting))
			s.deleting = ""
			return s, s.fetchJobs()
		}
		return s, nil

	case components.ConfirmResultMsg:
		if msg.Yes && s.deleting != "" {
			return s, s.doDelete(s.deleting)
		}
		s.deleting = ""
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
				name := row[0]
				s.deleting = name
				s.modal.Show("Delete job", fmt.Sprintf("Delete '%s'? This cannot be undone.", name))
			}
			return s, nil
		case "r":
			s.loading = true
			return s, s.fetchJobs()
		case "enter":
			row := s.table.SelectedRow()
			if row != nil {
				s.detail.Show("Job: "+row[0], fmt.Sprintf("Type: %s\nStatus: %s\nBuild: %s\nDesc: %s",
					row[1], row[2], row[3], row[4]))
			}
			return s, nil
		}
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

// View renders the job screen.
func (s JobScreen) View() string {
	if s.modal.Visible() {
		return s.modal.View()
	}
	if s.detail.Visible() {
		return s.detail.View()
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
		sb.WriteString(theme.StyleDim.Render("No jobs found."))
		return sb.String()
	}
	sb.WriteString(s.table.View())
	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("enter=detail  ^X=delete  r=refresh  ^F=search"))
	return sb.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
