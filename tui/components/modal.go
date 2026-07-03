package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/tui/theme"
)

// ConfirmResultMsg carries the user's yes/no answer.
type ConfirmResultMsg struct{ Yes bool }

// ConfirmModal shows a "Are you sure? [y/N]" prompt with a bordered chrome.
type ConfirmModal struct {
	Title   string
	Body    string
	Width   int // optional terminal width for border sizing
	visible bool
}

// Show makes the modal visible.
func (m *ConfirmModal) Show(title, body string) {
	m.Title = title
	m.Body = body
	m.visible = true
}

// SetWidth stores the terminal width for border sizing.
func (m *ConfirmModal) SetWidth(w int) { m.Width = w }

// Hide dismisses the modal.
func (m *ConfirmModal) Hide() { m.visible = false }

// Visible returns whether the modal is currently shown.
func (m ConfirmModal) Visible() bool { return m.visible }

// Update handles key events when visible. Returns ConfirmResultMsg on y/n/esc/enter.
func (m ConfirmModal) Update(msg tea.Msg) (ConfirmModal, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch strings.ToLower(km.String()) {
		case "y":
			m.visible = false
			return m, func() tea.Msg { return ConfirmResultMsg{Yes: true} }
		case "n", "esc", "q":
			m.visible = false
			return m, func() tea.Msg { return ConfirmResultMsg{Yes: false} }
		case "enter":
			m.visible = false
			return m, func() tea.Msg { return ConfirmResultMsg{Yes: false} }
		}
	}
	return m, nil
}

// View renders the confirm modal with a danger-coloured rounded border.
func (m ConfirmModal) View() string {
	if !m.visible {
		return ""
	}
	var inner strings.Builder
	inner.WriteString(theme.StyleWarning.Render(theme.SymWarn + " " + m.Title))
	if m.Body != "" {
		inner.WriteString("\n")
		inner.WriteString(theme.StyleNormal.Render(m.Body))
	}
	inner.WriteString("\n")
	inner.WriteString(theme.StyleDim.Render("Are you sure? [") +
		theme.StyleDanger.Render("y") +
		theme.StyleDim.Render("/") +
		theme.StyleSuccess.Render("N") +
		theme.StyleDim.Render("]"))
	return theme.BorderBox(inner.String(), "danger", m.Width)
}

// MessageModal shows informational text, dismissed with any key.
type MessageModal struct {
	Title   string
	Body    string
	Width   int // optional terminal width for border sizing
	visible bool
}

// Show makes the modal visible.
func (m *MessageModal) Show(title, body string) {
	m.Title = title
	m.Body = body
	m.visible = true
}

// SetWidth stores the terminal width for border sizing.
func (m *MessageModal) SetWidth(w int) { m.Width = w }

// Hide dismisses the modal.
func (m *MessageModal) Hide() { m.visible = false }

// Visible returns whether the modal is visible.
func (m MessageModal) Visible() bool { return m.visible }

// Update hides on any key press.
func (m MessageModal) Update(msg tea.Msg) (MessageModal, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	if _, ok := msg.(tea.KeyMsg); ok {
		m.visible = false
	}
	return m, nil
}

// View renders the message modal with a rounded info border.
func (m MessageModal) View() string {
	if !m.visible {
		return ""
	}
	var inner strings.Builder
	inner.WriteString(theme.StyleKeyHint.Render(theme.SymArrow + " " + m.Title))
	if m.Body != "" {
		inner.WriteString("\n")
		inner.WriteString(theme.StyleNormal.Render(m.Body))
	}
	inner.WriteString("\n")
	inner.WriteString(theme.StyleDim.Render("(press any key to close)"))
	return theme.BorderBox(inner.String(), "info", m.Width)
}
