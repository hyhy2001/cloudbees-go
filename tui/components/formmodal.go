package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/tui/theme"
)

// FormField describes one input row in a FormModal.
type FormField struct {
	Name        string
	Label       string
	Value       string
	Placeholder string
	Required    bool
	Password    bool     // mask input with •
	Options     []string // non-nil → cycle-select instead of free text
}

// FormResultMsg is emitted when a FormModal closes. Canceled is true on Esc;
// otherwise Values holds each field's value keyed by field order (Names lets
// callers map back). ID identifies which modal produced the result.
type FormResultMsg struct {
	ID       string
	Names    []string
	Values   []string
	Canceled bool
}

// FormModal is an app-level bordered form with text/password/select fields.
// Distinct from screens.formOverlay so the app shell (login, switch-profile)
// can own its own modal without reaching into the screens package.
type FormModal struct {
	ID      string
	Title   string
	Width   int
	fields  []FormField
	buf     []string
	cursor  int
	visible bool
}

// Show opens the modal with the given fields.
func (m *FormModal) Show(id, title string, fields []FormField) {
	m.ID = id
	m.Title = title
	m.fields = fields
	m.buf = make([]string, len(fields))
	for i, f := range fields {
		m.buf[i] = f.Value
	}
	m.cursor = 0
	m.visible = true
}

// SetWidth stores the terminal width for border sizing.
func (m *FormModal) SetWidth(w int) { m.Width = w }

// Hide dismisses the modal.
func (m *FormModal) Hide() { m.visible = false }

// Visible reports whether the modal is shown.
func (m FormModal) Visible() bool { return m.visible }

// Update handles key input. Emits FormResultMsg on submit/cancel.
func (m FormModal) Update(msg tea.Msg) (FormModal, tea.Cmd) {
	if !m.visible || len(m.fields) == 0 {
		return m, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	cur := m.fields[m.cursor]

	switch km.String() {
	case "esc":
		id := m.ID
		m.visible = false
		return m, func() tea.Msg { return FormResultMsg{ID: id, Canceled: true} }
	case "tab", "down":
		m.cursor = (m.cursor + 1) % len(m.fields)
		return m, nil
	case "shift+tab", "up":
		m.cursor = (m.cursor + len(m.fields) - 1) % len(m.fields)
		return m, nil
	case "enter":
		if len(cur.Options) > 0 {
			m.buf[m.cursor] = cycleOption(cur.Options, m.buf[m.cursor], 1)
			return m, nil
		}
		if m.cursor == len(m.fields)-1 {
			names := make([]string, len(m.fields))
			for i, f := range m.fields {
				names[i] = f.Name
			}
			vals := append([]string{}, m.buf...)
			id := m.ID
			m.visible = false
			return m, func() tea.Msg { return FormResultMsg{ID: id, Names: names, Values: vals} }
		}
		m.cursor++
		return m, nil
	case "left":
		if len(cur.Options) > 0 {
			m.buf[m.cursor] = cycleOption(cur.Options, m.buf[m.cursor], -1)
		}
		return m, nil
	case "right":
		if len(cur.Options) > 0 {
			m.buf[m.cursor] = cycleOption(cur.Options, m.buf[m.cursor], 1)
		}
		return m, nil
	}

	if len(cur.Options) == 0 {
		switch km.Type {
		case tea.KeyBackspace, tea.KeyDelete:
			if v := m.buf[m.cursor]; len(v) > 0 {
				m.buf[m.cursor] = v[:len(v)-1]
			}
		case tea.KeyRunes:
			m.buf[m.cursor] += string(km.Runes)
		case tea.KeySpace:
			m.buf[m.cursor] += " "
		}
	}
	return m, nil
}

// cycleOption returns the option dir steps away from cur (wrapping).
func cycleOption(opts []string, cur string, dir int) string {
	idx := 0
	for i, o := range opts {
		if o == cur {
			idx = i
			break
		}
	}
	return opts[(idx+dir+len(opts))%len(opts)]
}

// View renders the modal with a bordered chrome.
func (m FormModal) View() string {
	if !m.visible {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(theme.SymBee + " " + m.Title))
	sb.WriteString("\n\n")
	for i, f := range m.fields {
		on := i == m.cursor
		marker := " "
		labelStyle := theme.StyleDim
		if on {
			marker = theme.SymArrow
			labelStyle = theme.StyleKeyHint
		}
		val := m.buf[i]
		var display string
		switch {
		case len(f.Options) > 0:
			display = val
			if on {
				display = theme.StyleDim.Render(theme.SymArrowLeft+" ") + val + theme.StyleDim.Render(" "+theme.SymArrow)
			}
		case f.Password:
			display = strings.Repeat("•", len([]rune(val)))
			if on {
				display += "_"
			}
			if val == "" && !on && f.Placeholder != "" {
				display = theme.StyleDim.Render(f.Placeholder)
			}
		default:
			display = val
			if on {
				display += "_"
			}
			if val == "" && f.Placeholder != "" {
				if on {
					display = f.Placeholder + "_"
				} else {
					display = theme.StyleDim.Render(f.Placeholder)
				}
			}
		}
		sb.WriteString(labelStyle.Render(marker + " " + padRight(f.Label, 14) + " "))
		sb.WriteString(display)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("Tab/↑↓ move · ←→ cycle · Enter next/submit · Esc cancel"))
	return theme.BorderBox(sb.String(), "info", m.Width)
}
