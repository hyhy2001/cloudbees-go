package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/tui/theme"
)

// GrantItem is one row: a folder name (node vantage point) or an agent name
// (folder vantage point).
type GrantItem struct {
	Label   string // display label
	ID      string // opaque id used to revoke (grantId/tokenId)
	Pending bool   // grant exists but has no folder/agent assigned yet
}

// GrantAddMsg / GrantRevokeMsg / GrantRefreshMsg / GrantCloseMsg mirror the
// TS overlay's onAdd/onRevoke/onRefresh/onClose callbacks — the parent
// screen owns data fetching and the actual add/revoke/refresh calls.
type GrantAddMsg struct{}
type GrantRevokeMsg struct{ Item GrantItem }
type GrantRefreshMsg struct{}
type GrantCloseMsg struct{}

// GrantListOverlay is a read + manage overlay for Folders-Plus
// controlled-agent grants. Mirrors GrantListOverlay.tsx: pure presentation +
// key handling, the parent supplies items and reacts to the emitted msgs.
type GrantListOverlay struct {
	Title      string
	Subtitle   string
	ItemHeader string
	Items      []GrantItem // nil = loading
	Loaded     bool        // distinguishes nil-because-loading from nil-because-empty
	EmptyText  string
	AddHint    string

	cursor  int
	visible bool
}

// Show opens the overlay. Pass items=nil, loaded=false to show "Loading…"
// until the parent's fetch completes and calls SetItems.
func (g *GrantListOverlay) Show(title, subtitle, itemHeader, emptyText, addHint string) {
	g.Title = title
	g.Subtitle = subtitle
	g.ItemHeader = itemHeader
	g.EmptyText = emptyText
	g.AddHint = addHint
	g.Items = nil
	g.Loaded = false
	g.cursor = 0
	g.visible = true
}

// SetItems updates the loaded item list (e.g. after fetch/refresh completes).
func (g *GrantListOverlay) SetItems(items []GrantItem) {
	g.Items = items
	g.Loaded = true
	g.cursor = clampCursor(g.cursor, len(items))
}

// Hide dismisses the overlay.
func (g *GrantListOverlay) Hide() { g.visible = false }

// Visible reports whether the overlay is currently shown.
func (g GrantListOverlay) Visible() bool { return g.visible }

// Update handles key events while visible.
func (g GrantListOverlay) Update(msg tea.Msg) (GrantListOverlay, tea.Cmd) {
	if !g.visible {
		return g, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return g, nil
	}
	count := len(g.Items)

	switch km.String() {
	case "esc":
		g.visible = false
		return g, func() tea.Msg { return GrantCloseMsg{} }
	case "up":
		if g.cursor > 0 {
			g.cursor--
		}
		return g, nil
	case "down":
		if g.cursor < count-1 {
			g.cursor++
		}
		return g, nil
	case "a":
		return g, func() tea.Msg { return GrantAddMsg{} }
	case "r":
		return g, func() tea.Msg { return GrantRefreshMsg{} }
	case "d":
		if g.cursor >= 0 && g.cursor < count {
			item := g.Items[g.cursor]
			return g, func() tea.Msg { return GrantRevokeMsg{Item: item} }
		}
		return g, nil
	}
	return g, nil
}

// View renders the overlay.
func (g GrantListOverlay) View() string {
	if !g.visible {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(theme.StyleKeyHint.Render(g.Title))
	if g.Subtitle != "" {
		sb.WriteString("\n")
		sb.WriteString(theme.StyleDim.Render(g.Subtitle))
	}
	sb.WriteString("\n\n")

	switch {
	case !g.Loaded:
		sb.WriteString(theme.StyleDim.Render("Loading…"))
	case len(g.Items) == 0:
		sb.WriteString(theme.StyleDim.Render(g.EmptyText))
	default:
		sb.WriteString(theme.StyleSubtle.Render("   " + g.ItemHeader))
		sb.WriteString("\n")
		for i, item := range g.Items {
			on := i == g.cursor
			marker := " "
			markerStyle := theme.StyleDim
			if on {
				marker = theme.SymSelected
				markerStyle = theme.StyleKeyHint
			}
			label := item.Label
			style := theme.StyleDim
			if item.Pending {
				label = "(unassigned — pending)"
			} else if on {
				style = theme.StyleNormal
			}
			sb.WriteString(markerStyle.Render(marker+" ") + style.Render(label))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render("↑↓ move  ·  a " + g.AddHint + "  ·  d revoke  ·  r refresh  ·  Esc back"))
	return sb.String()
}
