package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/tui/theme"
)

// SearchBox is a small "/"-to-filter inline search box, mirroring useSearch()
// + SearchBar.tsx from the TS TUI: press "/" to enter search mode, printable
// chars + backspace edit the query, Enter confirms (keeps the filter, exits
// edit mode), Esc clears the query entirely and exits edit mode.
type SearchBox struct {
	query   string
	editing bool
}

// Query returns the current filter text (may be non-empty even when not
// editing — the filter stays applied until cleared with Esc).
func (b SearchBox) Query() string { return b.query }

// Editing reports whether the user is actively typing into the search box.
// Screens should treat this as "input captured" so global digit/tab/quit
// bindings don't fire while typing a query.
func (b SearchBox) Editing() bool { return b.editing }

// Active reports whether a filter is in effect or being edited — used to
// decide whether to render the search line at all.
func (b SearchBox) Active() bool { return b.query != "" || b.editing }

// Open enters search-edit mode (bind to "/").
func (b *SearchBox) Open() { b.editing = true }

// Clear empties the query and exits edit mode.
func (b *SearchBox) Clear() {
	b.query = ""
	b.editing = false
}

// Update handles key input while editing. Callers should only forward
// tea.KeyMsg here while Editing() is true (or always — Update is a no-op
// when not editing).
func (b SearchBox) Update(msg tea.Msg) (SearchBox, tea.Cmd) {
	if !b.editing {
		return b, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return b, nil
	}
	switch km.Type {
	case tea.KeyEsc:
		b.query = ""
		b.editing = false
	case tea.KeyEnter:
		b.editing = false
	case tea.KeyBackspace, tea.KeyDelete:
		if len(b.query) > 0 {
			r := []rune(b.query)
			b.query = string(r[:len(r)-1])
		}
	case tea.KeyRunes:
		if !km.Alt {
			b.query += km.String()
		}
	}
	return b, nil
}

// Matches reports whether haystack (case-insensitive) contains the current
// query. Empty query matches everything.
func (b SearchBox) Matches(haystack string) bool {
	if b.query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(b.query))
}

// View renders the search line: the live edit box while editing, or a
// "filter: X (esc clear)" hint once a filter is applied but confirmed.
// Returns "" when inactive (nothing to render).
func (b SearchBox) View() string {
	if b.editing {
		return theme.StyleKeyHint.Render("Search: ") + theme.StyleNormal.Render(b.query) + theme.StyleDim.Render("_")
	}
	if b.query != "" {
		return theme.StyleDim.Render("filter: " + b.query + " (esc clear)")
	}
	return ""
}
