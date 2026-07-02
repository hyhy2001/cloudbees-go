package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/tui/theme"
)

// EmailSpec is the friendly model the EmailBuilder overlay edits.
type EmailSpec struct {
	Enabled       bool
	Email         string
	EmailCond     string
	EmailKeywords string
	EmailRegex    string
}

// EmailResultMsg carries the edited spec on save, or Cancelled=true on Esc.
type EmailResultMsg struct {
	Spec      EmailSpec
	Cancelled bool
}

type emailRow string

const (
	erEnabled  emailRow = "enabled"
	erEmail    emailRow = "email"
	erCond     emailRow = "cond"
	erKeywords emailRow = "keywords"
	erRegex    emailRow = "regex"
)

var emailCondOptions = []string{"failed", "success", "always", "custom"}

var emailCondLabel = map[string]string{
	"failed":  "On failure",
	"success": "On success",
	"always":  "Always",
	"custom":  "Custom (keyword/regex)",
}

var emailRowLabel = map[emailRow]string{
	erEnabled:  "Enable email",
	erEmail:    "Recipient(s)",
	erCond:     "Send condition",
	erKeywords: "Keywords",
	erRegex:    "Regex filter",
}

var emailRowHint = map[emailRow]string{
	erEnabled:  "toggle with ←/→",
	erEmail:    "Enter to edit",
	erCond:     "←/→ cycle · choose Custom to filter by keyword/regex",
	erKeywords: "Enter to edit · comma-separated",
	erRegex:    "Enter to edit · Java regex",
}

func emailActiveRows(spec EmailSpec) []emailRow {
	if !spec.Enabled {
		return []emailRow{erEnabled}
	}
	if spec.EmailCond == "custom" {
		return []emailRow{erEnabled, erEmail, erCond, erKeywords, erRegex}
	}
	return []emailRow{erEnabled, erEmail, erCond}
}

// EmailBuilder is a full-screen overlay for editing a job's email-ext
// config. Mirrors EmailBuilder.tsx.
type EmailBuilder struct {
	spec     EmailSpec
	cursor   int
	editing  *emailRow
	editBuf  string
	groovyOK bool
	visible  bool
}

// Show opens the builder. groovyAvailable disables keywords/regex rows
// (email-ext plugin not detected) same as TS's groovyAvailable prop.
func (b *EmailBuilder) Show(initial EmailSpec, groovyAvailable bool) {
	b.spec = initial
	b.cursor = 0
	b.editing = nil
	b.editBuf = ""
	b.groovyOK = groovyAvailable
	b.visible = true
}

// Hide dismisses the builder without emitting a result.
func (b *EmailBuilder) Hide() { b.visible = false }

// Visible reports whether the overlay is currently shown.
func (b EmailBuilder) Visible() bool { return b.visible }

func (b EmailBuilder) currentRow() emailRow {
	rows := emailActiveRows(b.spec)
	i := b.cursor
	if i >= len(rows) {
		i = len(rows) - 1
	}
	return rows[i]
}

func (b *EmailBuilder) startEdit(kind emailRow) {
	var val string
	switch kind {
	case erEmail:
		val = b.spec.Email
	case erKeywords:
		val = b.spec.EmailKeywords
	case erRegex:
		val = b.spec.EmailRegex
	}
	b.editing = &kind
	b.editBuf = val
}

func (b *EmailBuilder) commitEdit() {
	if b.editing == nil {
		return
	}
	switch *b.editing {
	case erEmail:
		b.spec.Email = b.editBuf
	case erKeywords:
		b.spec.EmailKeywords = b.editBuf
	case erRegex:
		b.spec.EmailRegex = b.editBuf
	}
	b.editing = nil
}

// Update handles key events while visible.
func (b EmailBuilder) Update(msg tea.Msg) (EmailBuilder, tea.Cmd) {
	if !b.visible {
		return b, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return b, nil
	}

	if b.editing != nil {
		switch km.Type {
		case tea.KeyEnter:
			b.commitEdit()
		case tea.KeyEsc:
			b.editing = nil
		case tea.KeyBackspace, tea.KeyDelete:
			if len(b.editBuf) > 0 {
				b.editBuf = b.editBuf[:len(b.editBuf)-1]
			}
		case tea.KeyRunes:
			b.editBuf += string(km.Runes)
		case tea.KeySpace:
			b.editBuf += " "
		}
		return b, nil
	}

	rows := emailActiveRows(b.spec)
	row := b.currentRow()

	switch km.Type {
	case tea.KeyEsc:
		b.visible = false
		return b, func() tea.Msg { return EmailResultMsg{Cancelled: true} }
	case tea.KeyEnter:
		switch row {
		case erEmail:
			b.startEdit(erEmail)
			return b, nil
		case erKeywords, erRegex:
			if b.groovyOK {
				b.startEdit(row)
			}
			return b, nil
		case erEnabled:
			b.spec.Enabled = !b.spec.Enabled
			return b, nil
		default:
			b.visible = false
			spec := b.spec
			return b, func() tea.Msg { return EmailResultMsg{Spec: spec} }
		}
	case tea.KeyUp:
		if b.cursor > 0 {
			b.cursor--
		}
		return b, nil
	case tea.KeyDown:
		if b.cursor < len(rows)-1 {
			b.cursor++
		}
		return b, nil
	case tea.KeyLeft, tea.KeyRight:
		dir := -1
		if km.Type == tea.KeyRight {
			dir = 1
		}
		switch row {
		case erEnabled:
			wasEnabled := b.spec.Enabled
			b.spec.Enabled = !b.spec.Enabled
			if wasEnabled {
				b.cursor = 0
			}
		case erCond:
			idx := 0
			for i, c := range emailCondOptions {
				if c == b.spec.EmailCond {
					idx = i
					break
				}
			}
			b.spec.EmailCond = emailCondOptions[wrap(idx+dir, len(emailCondOptions))]
		}
		return b, nil
	}
	return b, nil
}

func (b EmailBuilder) renderRow(kind emailRow, idx int) string {
	rows := emailActiveRows(b.spec)
	cursor := b.cursor
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	on := idx == cursor
	label := emailRowLabel[kind]

	isEditing := b.editing != nil && *b.editing == kind
	unavailable := !b.groovyOK && (kind == erKeywords || kind == erRegex)

	var value string
	switch kind {
	case erEnabled:
		if b.spec.Enabled {
			value = "[X] enabled"
		} else {
			value = "[ ] disabled"
		}
	case erEmail:
		if isEditing {
			value = b.editBuf + "_"
		} else if b.spec.Email != "" {
			value = b.spec.Email
		} else {
			value = "(none)"
		}
	case erCond:
		if l, ok := emailCondLabel[b.spec.EmailCond]; ok {
			value = l
		} else {
			value = b.spec.EmailCond
		}
	case erKeywords:
		if unavailable {
			value = "(unavailable — email-ext plugin not installed)"
		} else if isEditing {
			value = b.editBuf + "_"
		} else if b.spec.EmailKeywords != "" {
			value = b.spec.EmailKeywords
		} else {
			value = "(none)"
		}
	case erRegex:
		if unavailable {
			value = "(unavailable — email-ext plugin not installed)"
		} else if isEditing {
			value = b.editBuf + "_"
		} else if b.spec.EmailRegex != "" {
			value = b.spec.EmailRegex
		} else {
			value = "(none)"
		}
	}

	cycler := kind == erCond || kind == erEnabled

	labelStyle := theme.StyleDim
	marker := " "
	if on {
		labelStyle = theme.StyleKeyHint
		marker = theme.SymArrow
	}
	var sb strings.Builder
	sb.WriteString(labelStyle.Render(marker + " " + padWidth(label, 16) + " "))
	if cycler && on {
		sb.WriteString(theme.StyleDim.Render(theme.SymArrow + " "))
	}
	sb.WriteString(value)
	if cycler && on {
		sb.WriteString(theme.StyleDim.Render(" " + theme.SymArrow))
	}

	if on {
		rowHint := emailRowHint[kind]
		if unavailable {
			rowHint = "requires email-ext plugin on the CloudBees server"
		}
		sb.WriteString("\n")
		hintStyle := theme.StyleDim
		if unavailable {
			hintStyle = theme.StyleWarning
		}
		sb.WriteString(hintStyle.Render(strings.Repeat(" ", 19) + rowHint))
	}
	return sb.String()
}

// View renders the overlay.
func (b EmailBuilder) View() string {
	if !b.visible {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(theme.SymGear + " Email Settings"))
	sb.WriteString("\n\n")

	rows := emailActiveRows(b.spec)
	for i, kind := range rows {
		sb.WriteString(b.renderRow(kind, i))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	hint := "↑↓ move · ←→ toggle · Enter edit/save · Esc cancel"
	if b.editing != nil {
		hint = "Enter save · Esc cancel edit"
	}
	sb.WriteString(theme.StyleDim.Render(hint))
	return sb.String()
}
