// Package components provides reusable TUI components.
package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"bee/tui/theme"
)

// TableModel is a filterable table component.
type TableModel struct {
	table     table.Model
	searching bool
	searchQ   string
	allRows   []table.Row
	columns   []table.Column
	width     int
}

var tableStyles = table.Styles{
	Header:   theme.StyleTableHeader.Padding(0, 1),
	Selected: theme.StyleSelectedRow.Padding(0, 1),
	Cell:     lipgloss.NewStyle().Padding(0, 1),
}

// New creates a TableModel with the given columns and rows.
func New(columns []table.Column, rows []table.Row) TableModel {
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(10),
		table.WithStyles(tableStyles),
	)
	return TableModel{
		table:   t,
		allRows: rows,
		columns: columns,
	}
}

// SetSize updates the table height.
func (m *TableModel) SetSize(width, height int) {
	m.width = width
	m.table.SetHeight(height)
}

// SetRows replaces the data rows and resets the search filter.
func (m *TableModel) SetRows(rows []table.Row) {
	m.allRows = rows
	m.applyFilter()
}

func (m *TableModel) applyFilter() {
	if m.searchQ == "" {
		m.table.SetRows(m.allRows)
		return
	}
	q := strings.ToLower(m.searchQ)
	var filtered []table.Row
	for _, r := range m.allRows {
		for _, cell := range r {
			if strings.Contains(strings.ToLower(cell), q) {
				filtered = append(filtered, r)
				break
			}
		}
	}
	m.table.SetRows(filtered)
}

// SelectedRow returns the currently highlighted row, or nil.
func (m *TableModel) SelectedRow() table.Row {
	return m.table.SelectedRow()
}

// Cursor returns the current row index.
func (m *TableModel) Cursor() int {
	return m.table.Cursor()
}

// Searching returns whether the search bar is active.
func (m *TableModel) Searching() bool {
	return m.searching
}

// SearchQuery returns the current search query string.
func (m *TableModel) SearchQuery() string {
	return m.searchQ
}

// Update handles messages for the table component.
func (m TableModel) Update(msg tea.Msg) (TableModel, tea.Cmd) {
	if m.searching {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.Type {
			case tea.KeyEsc, tea.KeyEnter:
				m.searching = false
				if msg.Type == tea.KeyEsc {
					m.searchQ = ""
					m.applyFilter()
				}
				return m, nil
			case tea.KeyBackspace, tea.KeyDelete:
				if len(m.searchQ) > 0 {
					m.searchQ = m.searchQ[:len(m.searchQ)-1]
					m.applyFilter()
				}
				return m, nil
			case tea.KeyRunes:
				m.searchQ += msg.String()
				m.applyFilter()
				return m, nil
			}
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+f", "/"))) {
			m.searching = true
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// View renders the table (and search bar if active).
func (m TableModel) View() string {
	var sb strings.Builder
	if m.searching {
		sb.WriteString(theme.StyleKeyHint.Render("Search: "))
		sb.WriteString(theme.StyleNormal.Render(m.searchQ))
		sb.WriteString(theme.StyleDim.Render("_"))
		sb.WriteString("\n")
	} else if m.searchQ != "" {
		sb.WriteString(theme.StyleDim.Render("filter: " + m.searchQ + " (esc clear)"))
		sb.WriteString("\n")
	}
	sb.WriteString(m.table.View())
	return sb.String()
}
