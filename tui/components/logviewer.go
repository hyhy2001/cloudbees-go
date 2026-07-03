package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"

	"bee/tui/theme"
)

const logViewerPollMs = 2000 * time.Millisecond
const logViewerPage = 10

// logPollMsg is the internal tick for progressive log streaming.
type logPollMsg struct{ id int }

// LogLine is a single log line with optional ANSI-style colour hint.
type LogLine struct {
	Text  string
	Color string // lipgloss-compatible colour string, "" = default
}

// LogViewerResult is emitted when the viewer is closed (Esc).
type LogViewerResult struct{}

// LogViewer is a full-screen scrollable overlay for streaming build logs.
// The caller supplies lines via AppendLines() as they arrive from polling.
// Scroll is pinned to the bottom by default; user can scroll up.
//
// Usage pattern:
//  1. Call Show(jobName) to open.
//  2. Fire the tea.Cmd returned by Show() to start the first poll tick.
//  3. On each logPollMsg, the parent screen polls StreamBuildLog/StreamLastBuildLog,
//     calls AppendLines(), and fires the next poll cmd via NextPollCmd().
//  4. Update() routes LogViewerResult → parent closes.
type LogViewer struct {
	JobName    string
	BuildNum   int    // 0 = "last build"
	BuildLabel string // display label, e.g. "#42 [1/5]" or "latest"
	Status     string // "connecting…", "streaming…", "finished", "error: …"

	lines     []LogLine
	scrollTop int  // -1 = pinned to bottom
	visible   bool
	id        int  // monotonic, used to discard stale poll ticks
	width     int
	height    int

	// Build history navigation.
	BuildNums []int // sorted descending (latest first), nil = not loaded
	BuildIdx  int   // index into BuildNums; 0 = newest
}

// Show opens the viewer for the given job. Returns the first poll tick cmd.
func (v *LogViewer) Show(jobName string) {
	v.JobName = jobName
	v.BuildNum = 0
	v.BuildLabel = "latest"
	v.lines = nil
	v.scrollTop = -1
	v.Status = "connecting…"
	v.visible = true
	v.id++
	v.BuildNums = nil
	v.BuildIdx = 0
}

// NextPollCmd returns a tea.Cmd that fires a logPollMsg after the poll interval.
// Call this from the parent screen's poll handler to keep streaming.
func (v *LogViewer) NextPollCmd() tea.Cmd {
	id := v.id
	return tea.Tick(logViewerPollMs, func(_ time.Time) tea.Msg {
		return logPollMsg{id: id}
	})
}

// ImmediatePollCmd returns a logPollMsg immediately (no delay), used to kick
// off the first poll or after a build-history navigation.
func (v *LogViewer) ImmediatePollCmd() tea.Cmd {
	id := v.id
	return func() tea.Msg { return logPollMsg{id: id} }
}

// ResetForBuild resets lines/scroll/status after the user navigates to a
// different build number. The caller should fire ImmediatePollCmd() after.
func (v *LogViewer) ResetForBuild(buildNum int) {
	v.BuildNum = buildNum
	if buildNum == 0 {
		v.BuildLabel = "latest"
	} else {
		v.BuildLabel = fmt.Sprintf("#%d", buildNum)
		if v.BuildNums != nil {
			v.BuildLabel = fmt.Sprintf("#%d [%d/%d]", buildNum, v.BuildIdx+1, len(v.BuildNums))
		}
	}
	v.lines = nil
	v.scrollTop = -1
	v.Status = "connecting…"
	v.id++
}

// AppendLines adds new log lines. Returns true if any were added.
func (v *LogViewer) AppendLines(lines []LogLine) bool {
	if len(lines) == 0 {
		return false
	}
	v.lines = append(v.lines, lines...)
	return true
}

// IsPollTick reports whether msg is a poll tick for this viewer (not stale).
func (v *LogViewer) IsPollTick(msg tea.Msg) bool {
	if tick, ok := msg.(logPollMsg); ok {
		return tick.id == v.id
	}
	return false
}

// Hide closes the viewer.
func (v *LogViewer) Hide() { v.visible = false }

// Visible reports whether the viewer is shown.
func (v LogViewer) Visible() bool { return v.visible }

// SetSize updates terminal dimensions.
func (v *LogViewer) SetSize(w, h int) { v.width = w; v.height = h }

func (v LogViewer) logRows() int {
	r := v.height - 8
	if r < 5 {
		r = 5
	}
	return r
}

// Update handles scroll keys and Esc.
func (v LogViewer) Update(msg tea.Msg) (LogViewer, tea.Cmd) {
	if !v.visible {
		return v, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return v, nil
	}
	logRows := v.logRows()
	total := len(v.lines)
	maxTop := total - logRows
	if maxTop < 0 {
		maxTop = 0
	}

	scrollBy := func(delta int) {
		base := v.scrollTop
		if base < 0 {
			base = maxTop
		}
		next := base + delta
		if next < 0 {
			next = 0
		}
		if next >= maxTop {
			v.scrollTop = -1 // re-pin to bottom
		} else {
			v.scrollTop = next
		}
	}

	switch km.String() {
	case "esc":
		v.visible = false
		return v, func() tea.Msg { return LogViewerResult{} }
	case "up":
		scrollBy(-1)
	case "down":
		scrollBy(1)
	case "ctrl+f":
		scrollBy(logViewerPage)
	case "ctrl+b":
		scrollBy(-logViewerPage)
	case "home":
		v.scrollTop = 0
	case "end":
		v.scrollTop = -1
	case "[":
		if v.BuildNums != nil && v.BuildIdx < len(v.BuildNums)-1 {
			v.BuildIdx++
			v.ResetForBuild(v.BuildNums[v.BuildIdx])
			return v, v.ImmediatePollCmd()
		}
	case "]":
		if v.BuildIdx > 0 {
			v.BuildIdx--
			v.ResetForBuild(v.BuildNums[v.BuildIdx])
			return v, v.ImmediatePollCmd()
		}
	}
	return v, nil
}

// View renders the log viewer.
func (v LogViewer) View() string {
	if !v.visible {
		return ""
	}
	logRows := v.logRows()
	total := len(v.lines)

	effectiveTop := v.scrollTop
	if effectiveTop < 0 {
		effectiveTop = total - logRows
	}
	if effectiveTop < 0 {
		effectiveTop = 0
	}
	maxTop := total - logRows
	if maxTop < 0 {
		maxTop = 0
	}
	if effectiveTop > maxTop {
		effectiveTop = maxTop
	}

	visible := v.lines[effectiveTop:]
	if len(visible) > logRows {
		visible = visible[:logRows]
	}

	contentWidth := v.width - 6
	if contentWidth < 10 {
		contentWidth = 40
	}

	var sb strings.Builder
	// Title line
	sb.WriteString(theme.StyleKeyHint.Render(theme.SymArrow + " Log: "))
	sb.WriteString(theme.StyleTitle.Render(v.JobName))
	sb.WriteString(" ")
	buildHint := v.BuildLabel + " [" + v.Status + "]"
	if v.BuildNums != nil && len(v.BuildNums) > 1 {
		buildHint += " · [=older ]=newer"
	}
	sb.WriteString(theme.StyleDim.Render(buildHint))
	sb.WriteString("\n\n")

	// Log lines + scrollbar
	if len(visible) == 0 {
		sb.WriteString(theme.StyleDim.Render("(no output yet)"))
		sb.WriteString("\n")
	} else {
		// Build scrollbar
		var scrollbar []string
		if total > logRows {
			trackH := logRows
			thumbH := (logRows * logRows) / total
			if thumbH < 1 {
				thumbH = 1
			}
			thumbTop := 0
			if total > logRows {
				thumbTop = (effectiveTop * (trackH - thumbH)) / (total - logRows)
			}
			scrollbar = make([]string, trackH)
			for i := range scrollbar {
				if i >= thumbTop && i < thumbTop+thumbH {
					scrollbar[i] = "█"
				} else {
					scrollbar[i] = "│"
				}
			}
		}

		for i, line := range visible {
			text := line.Text
			if len([]rune(text)) > contentWidth {
				text = string([]rune(text)[:contentWidth])
			}
			if text == "" {
				text = " "
			}
			col := line.Color
			if col != "" {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(col)).Render(text))
			} else {
				sb.WriteString(text)
			}
			if scrollbar != nil && i < len(scrollbar) {
				sb.WriteString(theme.StyleDim.Render(" " + scrollbar[i]))
			}
			sb.WriteString("\n")
		}
	}

	// Footer
	sb.WriteString("\n")
	posHint := " "
	if total > logRows {
		end := effectiveTop + logRows
		if end > total {
			end = total
		}
		posHint = fmt.Sprintf(" lines %d–%d/%d", effectiveTop+1, end, total)
		if v.scrollTop < 0 {
			posHint += " [bottom]"
		}
		posHint += " · "
	}
	sb.WriteString(theme.StyleDim.Render(posHint + "↑/↓ scroll · Home/End · Esc back"))

	return theme.BorderBox(sb.String(), "info", v.width)
}

// ColorForLogLine returns a colour hint for common Jenkins log line prefixes.
func ColorForLogLine(line string) string {
	switch {
	case strings.HasPrefix(line, "ERROR") || strings.HasPrefix(line, "FAILED") || strings.HasPrefix(line, "FAILURE"):
		return theme.ColorError
	case strings.HasPrefix(line, "WARNING") || strings.HasPrefix(line, "WARN"):
		return theme.ColorWarning
	case strings.HasPrefix(line, "Finished: SUCCESS"):
		return theme.ColorSuccess
	case strings.HasPrefix(line, "Finished: FAILURE") || strings.HasPrefix(line, "Finished: ABORTED"):
		return theme.ColorError
	default:
		return ""
	}
}
