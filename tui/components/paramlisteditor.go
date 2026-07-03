package components

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/plugins/job"
	"bee/tui/theme"
)

// ParamListResultMsg carries the finalized param rows on save, or
// Cancelled=true on Esc.
type ParamListResultMsg struct {
	Params    []job.StringParamDef
	Cancelled bool
}

type paramField int

const (
	pfName paramField = iota
	pfDefault
	pfDescription
)

// ParamListEditor is a full-screen overlay for editing a job's String build
// parameters. Mirrors ParamListEditor.tsx: list mode (↑↓ move, Enter edit
// row, Ctrl+n add, Ctrl+d delete, Ctrl+s save, Esc cancel) plus a nested
// 3-field row editor (Tab/↑↓ between fields, Enter save row, Esc cancel row).
type ParamListEditor struct {
	params  []job.StringParamDef
	cursor  int
	editing *int // row index being edited, nil when in list mode
	field   paramField
	buf     [3]string // name, defaultValue, description while editing
	visible bool
	Width   int // terminal width for border sizing
}

// Show opens the editor with initial rows (e.g. parsed from job.ParamDefsFromStrings).
func (e *ParamListEditor) Show(initial []job.StringParamDef) {
	e.params = append([]job.StringParamDef{}, initial...)
	e.cursor = 0
	e.editing = nil
	e.visible = true
}

// Hide dismisses the editor without emitting a result.
func (e *ParamListEditor) Hide() { e.visible = false }

// Visible reports whether the overlay is currently shown.
func (e ParamListEditor) Visible() bool { return e.visible }

func clampCursor(n, length int) int {
	if length == 0 {
		return 0
	}
	if n < 0 {
		return 0
	}
	if n >= length {
		return length - 1
	}
	return n
}

func (e *ParamListEditor) openEdit(index int) {
	e.editing = &index
	row := job.StringParamDef{}
	if index >= 0 && index < len(e.params) {
		row = e.params[index]
	}
	e.buf = [3]string{row.Name, row.DefaultValue, row.Description}
	e.field = pfName
}

// Update handles key events while visible.
func (e ParamListEditor) Update(msg tea.Msg) (ParamListEditor, tea.Cmd) {
	if !e.visible {
		return e, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return e, nil
	}

	if e.editing != nil {
		return e.updateRowEditor(km)
	}

	switch km.String() {
	case "esc":
		e.visible = false
		return e, func() tea.Msg { return ParamListResultMsg{Cancelled: true} }
	case "enter":
		if len(e.params) > 0 {
			e.openEdit(e.cursor)
		}
		return e, nil
	case "down":
		e.cursor = clampCursor(e.cursor+1, len(e.params))
		return e, nil
	case "up":
		e.cursor = clampCursor(e.cursor-1, len(e.params))
		return e, nil
	case "home":
		e.cursor = 0
		return e, nil
	case "end":
		e.cursor = clampCursor(len(e.params)-1, len(e.params))
		return e, nil
	case "ctrl+s":
		e.visible = false
		final := job.FinalizeParams(e.params)
		return e, func() tea.Msg { return ParamListResultMsg{Params: final} }
	case "ctrl+n":
		e.params = job.AddParam(e.params)
		idx := len(e.params) - 1
		e.cursor = idx
		e.openEdit(idx)
		return e, nil
	case "ctrl+d":
		if len(e.params) == 0 {
			return e, nil
		}
		e.params = job.RemoveParam(e.params, e.cursor)
		e.cursor = clampCursor(e.cursor, len(e.params))
		return e, nil
	}
	return e, nil
}

func (e ParamListEditor) updateRowEditor(km tea.KeyMsg) (ParamListEditor, tea.Cmd) {
	index := *e.editing
	switch km.Type {
	case tea.KeyEsc:
		if index < len(e.params) && strings.TrimSpace(e.params[index].Name) == "" {
			// Cancelled a freshly-added blank row — drop it.
			e.params = job.RemoveParam(e.params, index)
			e.cursor = clampCursor(e.cursor, len(e.params))
		}
		e.editing = nil
		return e, nil
	case tea.KeyEnter:
		if index < len(e.params) {
			e.params[index] = job.StringParamDef{Name: e.buf[0], DefaultValue: e.buf[1], Description: e.buf[2]}
		}
		e.editing = nil
		return e, nil
	case tea.KeyTab, tea.KeyDown:
		e.field = (e.field + 1) % 3
		return e, nil
	case tea.KeyShiftTab, tea.KeyUp:
		e.field = (e.field + 2) % 3
		return e, nil
	case tea.KeyBackspace, tea.KeyDelete:
		v := e.buf[e.field]
		if len(v) > 0 {
			e.buf[e.field] = v[:len(v)-1]
		}
		return e, nil
	case tea.KeyRunes:
		e.buf[e.field] += string(km.Runes)
		return e, nil
	case tea.KeySpace:
		e.buf[e.field] += " "
		return e, nil
	}
	return e, nil
}

// View renders the overlay.
func (e ParamListEditor) View() string {
	if !e.visible {
		return ""
	}
	if e.editing != nil {
		return e.viewRowEditor()
	}

	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(theme.SymGear + " Edit Build Parameters"))
	sb.WriteString("\n\n")

	if len(e.params) == 0 {
		sb.WriteString(theme.StyleDim.Render("No parameters. Press Ctrl+n to add one."))
	} else {
		for i, p := range e.params {
			on := i == e.cursor
			marker := " "
			numStyle := theme.StyleDim
			if on {
				marker = theme.SymArrow
				numStyle = theme.StyleKeyHint
			}
			name := p.Name
			if name == "" {
				name = "(unnamed)"
			}
			def := p.DefaultValue
			if def == "" {
				def = "—"
			}
			line := numStyle.Render(marker+" "+padWidth(strconv.Itoa(i+1), 2)) + " " +
				padWidth(name, 20) + theme.StyleDim.Render("default="+def)
			if p.Description != "" {
				line += theme.StyleDim.Render(" · " + p.Description)
			}
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("↑/↓ move · Enter edit · Ctrl+n add · Ctrl+d delete · Ctrl+s save · Esc cancel"))
	return theme.BorderBox(sb.String(), "info", e.Width)
}

func (e ParamListEditor) viewRowEditor() string {
	labels := []string{"Name", "Default", "Description"}
	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(theme.SymGear + " Parameter " + strconv.Itoa(*e.editing+1)))
	sb.WriteString("\n\n")
	for i, label := range labels {
		on := paramField(i) == e.field
		marker := " "
		labelStyle := theme.StyleDim
		if on {
			marker = theme.SymArrow
			labelStyle = theme.StyleKeyHint
		}
		val := e.buf[i]
		if on {
			val += "_"
		}
		sb.WriteString(labelStyle.Render(marker+" "+padWidth(label, 14)) + " " + val)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("Tab/↑↓ move field · Enter save · Esc cancel"))
	return theme.BorderBox(sb.String(), "info", e.Width)
}
