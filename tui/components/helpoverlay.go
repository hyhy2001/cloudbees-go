package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/tui/theme"
)

// HelpEntry is a single row in the help overlay.
type HelpEntry struct {
	Key  string
	Desc string
}

// HelpOverlay is a full-screen help reference, toggled by "?" and dismissed
// by "?" or Esc. Mirrors HelpScreen.tsx — the caller provides entries
// organised by group via SetEntries().
type HelpOverlay struct {
	Title   string
	entries [][]HelpEntry // one slice per group
	labels  []string      // group label per slice
	visible bool
	Width   int
}

// SetEntries replaces the help entries (groups of HelpEntry slices with labels).
func (h *HelpOverlay) SetEntries(labels []string, groups [][]HelpEntry) {
	h.labels = labels
	h.entries = groups
}

// Show makes the overlay visible.
func (h *HelpOverlay) Show() { h.visible = true }

// Hide dismisses the overlay.
func (h *HelpOverlay) Hide() { h.visible = false }

// Toggle flips visible state.
func (h *HelpOverlay) Toggle() { h.visible = !h.visible }

// Visible reports whether the overlay is shown.
func (h HelpOverlay) Visible() bool { return h.visible }

// Update dismisses on "?" or Esc.
func (h HelpOverlay) Update(msg tea.Msg) (HelpOverlay, tea.Cmd) {
	if !h.visible {
		return h, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "?", "esc", "q":
			h.visible = false
		}
	}
	return h, nil
}

// View renders the help overlay with a bordered chrome.
func (h HelpOverlay) View() string {
	if !h.visible {
		return ""
	}
	var sb strings.Builder
	title := h.Title
	if title == "" {
		title = "Keyboard Shortcuts"
	}
	sb.WriteString(theme.StyleTitle.Render(theme.SymArrow + " " + title))
	sb.WriteString("\n\n")

	for gi, group := range h.entries {
		if gi < len(h.labels) && h.labels[gi] != "" {
			sb.WriteString(theme.StyleKeyHint.Render(h.labels[gi]))
			sb.WriteString("\n")
		}
		for _, e := range group {
			sb.WriteString(theme.StyleKeyHint.Render("  " + padRight(e.Key, 18)))
			sb.WriteString(theme.StyleDim.Render(e.Desc))
			sb.WriteString("\n")
		}
		if gi < len(h.entries)-1 {
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("? / Esc  close"))
	return theme.BorderBox(sb.String(), "info", h.Width)
}

func padRight(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(r))
}
