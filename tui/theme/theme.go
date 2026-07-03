// Package theme provides colors, symbols, and lipgloss styles for the bee TUI.
package theme

import "github.com/charmbracelet/lipgloss"

// Colors matching the TS theme (hex, xterm-256 palette).
const (
	ColorNormal     = "#d0d0d0"
	ColorHeaderFg   = "#ffffff"
	ColorHeaderBg   = "#005f87"
	ColorSelectedBg = "#0087af"
	ColorSelectedFg = "#ffffff"
	ColorSuccess    = "#5fff00"
	ColorError      = "#ff0000"
	ColorWarning    = "#ffd700"
	ColorDim        = "#808080"
	ColorSubtle     = "#4e4e4e"
	ColorActive     = "#ffaf00"
	ColorKeyHint    = "#00afff"
	ColorBlue       = "#00afff"
	ColorYellow     = "#ffd700"
	ColorDanger     = "#ff5f5f"
)

// Symbols used throughout the TUI.
const (
	SymOK        = "✓"
	SymFail      = "✗"
	SymArrow     = "▸"
	SymArrowLeft = "◂"
	SymLoading   = "…"
	SymSep       = "─"
	SymVBar      = "│"
	SymOnline    = "●"
	SymOffline   = "○"
	SymSelected  = "▶"
	SymTracked   = "★"
	SymWarn      = "⚠"
	SymGear      = "⚙"
	SymBee       = "🐝"
	SymRunning   = "⟳"
	SymCheck     = "☑"
	SymUncheck   = "☐"
)

// Lipgloss styles.
var (
	StyleTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorActive)).
			Bold(true)

	StyleTabActive = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorActive)).
			Bold(true)

	StyleTabInactive = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorDim))

	StyleSelectedRow = lipgloss.NewStyle().
				Background(lipgloss.Color(ColorSelectedBg)).
				Foreground(lipgloss.Color(ColorSelectedFg))

	StyleTableHeader = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorHeaderFg)).
				Background(lipgloss.Color(ColorHeaderBg)).
				Bold(true)

	StyleStatusBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorDim)).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(ColorSubtle))

	StyleDim = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorDim))

	StyleSuccess = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorSuccess))

	StyleError = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorError))

	StyleWarning = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorWarning))

	StyleKeyHint = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorKeyHint))

	StyleNormal = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorNormal))

	StyleSubtle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorSubtle))

	StyleBlue = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorBlue))

	StyleDanger = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorDanger))
)

// BorderBox wraps content in a rounded lipgloss border with standard padding
// (paddingX=1, no vertical padding), coloured according to severity:
//   - "info"    → subtle border (default, most overlays)
//   - "danger"  → danger red border (destructive confirm)
//   - "warning" → warning yellow border
//   - "success" → success green border
//
// Width is the outer column width (terminal width); pass 0 to let lipgloss
// determine the width from content.
func BorderBox(content, severity string, width int) string {
	borderColor := ColorSubtle
	switch severity {
	case "danger":
		borderColor = ColorDanger
	case "warning":
		borderColor = ColorWarning
	case "success":
		borderColor = ColorSuccess
	}
	s := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		PaddingLeft(1).
		PaddingRight(1)
	if width > 0 {
		s = s.Width(width - 4) // subtract border(2)+padding(2)
	}
	return s.Render(content)
}

// JobStatusLabel maps a Jenkins job color string to a short label + color hex.
func JobStatusLabel(color string) (label, col string) {
	running := len(color) > 6 && color[len(color)-6:] == "_anime"
	base := color
	if running {
		base = color[:len(color)-6]
	}
	switch base {
	case "blue":
		label, col = SymOK+"  OK  ", ColorSuccess
	case "red":
		label, col = SymFail+" FAIL", ColorError
	case "yellow":
		label, col = SymWarn+" WARN", ColorWarning
	case "aborted":
		label, col = "ABT ", ColorDim
	case "notbuilt":
		label, col = "NEW ", ColorDim
	case "disabled":
		label, col = "DIS ", ColorDim
	default:
		if len(base) > 4 {
			base = base[:4]
		}
		label, col = base, ColorDim
	}
	if running {
		label += " " + SymRunning
	}
	return
}
