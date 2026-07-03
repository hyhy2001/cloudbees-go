package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"bee/tui/theme"
)

// dtPage is the row count for Ctrl+F/Ctrl+B page navigation.
const dtPage = 10

// Column is a single DataTable column definition.
//
//   - Width is the fixed character width, OR the *minimum* width when Flex.
//   - Flex columns split the terminal's leftover width evenly (after fixed
//     columns + separators), so the table fills the screen instead of using
//     hardcoded character counts. Falls back to Width when TableWidth is 0
//     (piped output, no WindowSizeMsg yet).
type Column struct {
	Header string
	Width  int
	Flex   bool
}

// Cell is a single table cell with optional color and dim styling.
type Cell struct {
	Text string
	// Color is a lipgloss hex color (e.g. theme.ColorSuccess). Empty means
	// "use the default foreground" (theme.ColorNormal, or the selected-row
	// color when this is the cursor row).
	Color string
	Dim   bool
}

// resolveColumnWidths resolves each column's effective render width. Fixed
// columns keep their Width; flex columns split the leftover terminal width
// evenly (never below their declared Width, which acts as a minimum). Pure
// function — exported for tests, mirrors DataTable.tsx's resolveColumnWidths.
func resolveColumnWidths(columns []Column, tableWidth int) []int {
	widths := make([]int, len(columns))
	var flexIdx []int
	for i, c := range columns {
		if c.Flex {
			flexIdx = append(flexIdx, i)
		}
	}
	// No terminal width, or no flex columns → use declared widths verbatim.
	if tableWidth <= 0 || len(flexIdx) == 0 {
		for i, c := range columns {
			widths[i] = c.Width
		}
		return widths
	}

	// Budget: 2 leading indicator chars + 1 trailing space per column.
	chrome := 2 + len(columns)
	fixedTotal := 0
	for _, c := range columns {
		if !c.Flex {
			fixedTotal += c.Width
		}
	}
	leftover := tableWidth - chrome - fixedTotal
	perFlexMin := 0
	for _, c := range columns {
		if c.Flex {
			perFlexMin += c.Width
		}
	}

	// Not enough room to grow → fall back to declared minimums.
	if leftover <= perFlexMin {
		for i, c := range columns {
			widths[i] = c.Width
		}
		return widths
	}

	each := leftover / len(flexIdx)
	for i, c := range columns {
		if c.Flex {
			w := c.Width
			if each > w {
				w = each
			}
			widths[i] = w
		} else {
			widths[i] = c.Width
		}
	}
	return widths
}

// padCell truncates or pads s to exactly width visible characters, appending
// "…" when truncated. Mirrors DataTable.tsx's pad().
func padCell(s string, width int) string {
	r := []rune(s)
	if len(r) > width {
		if width <= 0 {
			return ""
		}
		return string(r[:width-1]) + "…"
	}
	return s + strings.Repeat(" ", width-len(r))
}

// DataTable is a scrollable table with a cursor row, vim-style navigation,
// per-cell color, flex columns, viewport centering, and optional multi-select.
// It is a straight port of DataTable.tsx (Ink/React) to Bubbletea.
//
// Navigation (always active — callers gate Update() on their own InputCaptured
// logic, same contract as the old TableModel):
//
//	↓/j          cursor down
//	↑/k          cursor up
//	Home/g       jump to first row
//	End/G        jump to last row
//	Ctrl+f       page down (10 rows)
//	Ctrl+b       page up (10 rows)
//	Space        toggle select on cursor row (only when SetSelectable(true))
//
// Enter is intentionally NOT handled here — it stays owned by the screen's
// keymap, matching the TS component's contract (avoids double-fire).
type DataTable struct {
	columns   []Column
	rows      [][]Cell
	rowKeys   []string // stable identity per row (job name / node name / cred id); optional
	cursor    int
	height    int // viewport height; default 12
	width     int // table width in characters (0 = no flex — use declared widths)
	emptyText string

	selectable bool
	selected   map[string]bool
}

// NewDataTable creates a DataTable with the given columns. Rows start empty.
func NewDataTable(columns []Column) DataTable {
	return DataTable{
		columns:   columns,
		height:    12,
		emptyText: "(no rows)",
	}
}

// SetRows replaces the row data. rowKeys may be nil (falls back to positional
// identity); when non-nil its length should match rows.
func (d *DataTable) SetRows(rows [][]Cell, rowKeys []string) {
	d.rows = rows
	d.rowKeys = rowKeys
	if d.cursor >= len(rows) {
		d.cursor = len(rows) - 1
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
}

// SetSize sets the table's character width (for flex columns) and viewport
// height (row count, not counting header/footer).
func (d *DataTable) SetSize(width, height int) {
	d.width = width
	if height > 0 {
		d.height = height
	}
}

// SetEmptyText sets the text shown when there are no rows.
func (d *DataTable) SetEmptyText(s string) { d.emptyText = s }

// SetSelectable enables/disables the "Sel" checkbox column + Space toggle.
func (d *DataTable) SetSelectable(on bool) {
	d.selectable = on
	if on && d.selected == nil {
		d.selected = make(map[string]bool)
	}
}

// Selected returns the set of selected row keys (only meaningful rowKeys were
// provided to SetRows).
func (d DataTable) Selected() map[string]bool { return d.selected }

// SelectedCount returns how many rows are currently selected.
func (d DataTable) SelectedCount() int { return len(d.selected) }

// ClearSelection empties the selection set.
func (d *DataTable) ClearSelection() {
	d.selected = make(map[string]bool)
}

// Cursor returns the current cursor row index.
func (d DataTable) Cursor() int { return d.cursor }

// SetCursor sets the cursor row index, clamped to valid range.
func (d *DataTable) SetCursor(i int) {
	d.cursor = d.clamp(i)
}

// SelectedRowKey returns the rowKey at the cursor, or "" if none/out of range.
func (d DataTable) SelectedRowKey() string {
	if d.cursor < 0 || d.cursor >= len(d.rowKeys) {
		return ""
	}
	return d.rowKeys[d.cursor]
}

// SelectedRow returns the cell row at the cursor, or nil.
func (d DataTable) SelectedRow() []Cell {
	if d.cursor < 0 || d.cursor >= len(d.rows) {
		return nil
	}
	return d.rows[d.cursor]
}

func (d DataTable) clamp(i int) int {
	if len(d.rows) == 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= len(d.rows) {
		return len(d.rows) - 1
	}
	return i
}

// Update handles navigation keys. Callers are responsible for not calling
// this while a modal/overlay/search-edit is capturing input (same contract
// the old TableModel had via the screen's Update() dispatch order).
func (d DataTable) Update(msg tea.Msg) (DataTable, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	if len(d.rows) == 0 {
		return d, nil
	}
	switch km.String() {
	case "down", "j":
		d.cursor = d.clamp(d.cursor + 1)
	case "up", "k":
		d.cursor = d.clamp(d.cursor - 1)
	case "home", "g":
		d.cursor = 0
	case "end", "G":
		d.cursor = len(d.rows) - 1
	case "ctrl+f":
		d.cursor = d.clamp(d.cursor + dtPage)
	case "ctrl+b":
		d.cursor = d.clamp(d.cursor - dtPage)
	case " ":
		if d.selectable {
			key := d.SelectedRowKey()
			if key != "" {
				if d.selected == nil {
					d.selected = make(map[string]bool)
				}
				if d.selected[key] {
					delete(d.selected, key)
				} else {
					d.selected[key] = true
				}
			}
		}
	}
	return d, nil
}

const dtSelWidth = 3

// View renders the table: header, separator, visible rows (viewport-centered
// on the cursor), empty-state text, and a scroll-position footer when content
// overflows the viewport.
func (d DataTable) View() string {
	hasSel := d.selectable
	effectiveWidth := d.width
	if effectiveWidth > 0 && hasSel {
		effectiveWidth -= dtSelWidth + 1
	}
	colWidths := resolveColumnWidths(d.columns, effectiveWidth)

	var sb strings.Builder

	// Header.
	sb.WriteString("  ")
	if hasSel {
		sb.WriteString(theme.StyleKeyHint.Bold(true).Render(padCell("Sel", dtSelWidth)) + " ")
	}
	for i, c := range d.columns {
		sb.WriteString(theme.StyleKeyHint.Bold(true).Render(padCell(c.Header, colWidths[i])) + " ")
	}
	sb.WriteString("\n")

	// Header separator.
	sb.WriteString(theme.StyleSubtle.Render("  "))
	sepLine := ""
	if hasSel {
		sepLine += strings.Repeat(theme.SymSep, dtSelWidth+1)
	}
	for i := range d.columns {
		sepLine += strings.Repeat(theme.SymSep, colWidths[i]+1)
	}
	sb.WriteString(theme.StyleSubtle.Render(sepLine))
	sb.WriteString("\n")

	if len(d.rows) == 0 {
		sb.WriteString(theme.StyleDim.Render("  " + d.emptyText))
		return sb.String()
	}

	// Viewport centered on the cursor.
	start := d.cursor - d.height/2
	if start < 0 {
		start = 0
	}
	maxStart := len(d.rows) - d.height
	if maxStart < 0 {
		maxStart = 0
	}
	if start > maxStart {
		start = maxStart
	}
	end := start + d.height
	if end > len(d.rows) {
		end = len(d.rows)
	}

	for ri := start; ri < end; ri++ {
		row := d.rows[ri]
		isCursor := ri == d.cursor
		var rowKey string
		if ri < len(d.rowKeys) {
			rowKey = d.rowKeys[ri]
		}
		isSelected := rowKey != "" && d.selected[rowKey]

		indicator := " "
		if isCursor {
			indicator = theme.SymSelected
		}

		var lineSb strings.Builder
		rowStyle := lipgloss.NewStyle()
		if isCursor {
			rowStyle = rowStyle.Background(lipgloss.Color(theme.ColorSelectedBg))
		}

		indicatorColor := theme.ColorSubtle
		if isCursor {
			indicatorColor = theme.ColorActive
		}
		lineSb.WriteString(rowStyle.Foreground(lipgloss.Color(indicatorColor)).Render(indicator))
		lineSb.WriteString(rowStyle.Foreground(lipgloss.Color(theme.ColorSubtle)).Render(" "))

		if hasSel {
			mark := theme.SymUncheck
			markColor := theme.ColorSubtle
			if isSelected {
				mark = theme.SymCheck
				markColor = theme.ColorSuccess
			}
			lineSb.WriteString(rowStyle.Foreground(lipgloss.Color(markColor)).Bold(isSelected).Render(padCell(mark, dtSelWidth)) + " ")
		}

		for ci, cell := range row {
			width := 10
			if ci < len(colWidths) {
				width = colWidths[ci]
			}
			color := cell.Color
			if isCursor {
				color = theme.ColorSelectedFg
			} else if cell.Dim {
				color = theme.ColorDim
			}
			cs := rowStyle.Bold(isCursor)
			if color != "" {
				cs = cs.Foreground(lipgloss.Color(color))
			} else {
				cs = cs.Foreground(lipgloss.Color(theme.ColorNormal))
			}
			lineSb.WriteString(cs.Render(padCell(cell.Text, width)) + " ")
		}
		sb.WriteString(lineSb.String())
		sb.WriteString("\n")
	}

	// Scroll position footer — only shown when content overflows the viewport.
	if len(d.rows) > d.height {
		footer := theme.StyleDim.Render("  " + itoa(d.cursor+1) + "/" + itoa(len(d.rows)))
		if start > 0 {
			footer += theme.StyleDim.Render("  " + theme.SymArrowLeft + " scroll up")
		}
		if end < len(d.rows) {
			footer += theme.StyleDim.Render("  " + theme.SymArrow + " more below")
		}
		sb.WriteString(footer)
		sb.WriteString("\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
