package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/tui/theme"
)

const commandLogCap = 200 // ring-buffer size
const commandLogLines = 4 // lines shown when panel is open

// CommandLogMsg is published by any screen action that mutates state —
// create/update/delete/run/stop/schedule/email/params. The app passes it to
// the CommandLog component via Update.
type CommandLogMsg struct{ Line string }

// CommandLog is a ring-buffer of recent action lines shown as a panel at
// the bottom of the screen when toggled with "L".
type CommandLog struct {
	lines   []string
	visible bool
	Width   int
}

// Add appends a line to the ring buffer (auto-trims to cap).
func (cl *CommandLog) Add(line string) {
	cl.lines = append(cl.lines, line)
	if len(cl.lines) > commandLogCap {
		cl.lines = cl.lines[len(cl.lines)-commandLogCap:]
	}
}

// Toggle shows/hides the panel.
func (cl *CommandLog) Toggle() { cl.visible = !cl.visible }

// Visible reports whether the panel is shown.
func (cl CommandLog) Visible() bool { return cl.visible }

// Update captures CommandLogMsg to add lines; ignores all other messages.
func (cl CommandLog) Update(msg tea.Msg) (CommandLog, tea.Cmd) {
	if m, ok := msg.(CommandLogMsg); ok {
		cl.Add(m.Line)
	}
	return cl, nil
}

// View renders the last N lines in a bordered panel.
func (cl CommandLog) View() string {
	if !cl.visible {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(theme.StyleKeyHint.Render(theme.SymArrow + " Command Log"))
	sb.WriteString(theme.StyleDim.Render("  (L to close)"))
	sb.WriteString("\n")

	start := len(cl.lines) - commandLogLines
	if start < 0 {
		start = 0
	}
	shown := cl.lines[start:]
	if len(shown) == 0 {
		sb.WriteString(theme.StyleDim.Render("  (no recent commands)"))
	} else {
		for _, line := range shown {
			sb.WriteString(theme.StyleDim.Render("  " + line))
			sb.WriteString("\n")
		}
	}
	return theme.BorderBox(sb.String(), "info", cl.Width)
}
