package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/tui/theme"
)

const scriptViewerPage = 10

// ScriptViewerResult is emitted when the viewer is closed (Esc).
type ScriptViewerResult struct{}

// scriptLoadedMsg is sent by the parent screen when the script fetch completes.
type ScriptLoadedMsg struct {
	JobName string
	Script  string
	Err     error
}

// ScriptViewer is a full-screen, scrollable overlay for viewing a pipeline
// script (Jenkinsfile). The parent fetches the script and calls SetScript().
type ScriptViewer struct {
	JobName   string
	script    string
	lines     []string
	loading   bool
	errMsg    string
	scrollTop int
	visible   bool
	width     int
	height    int
}

// Show opens the viewer in "loading" state. The parent should kick off a fetch
// and deliver the result via ScriptLoadedMsg.
func (v *ScriptViewer) Show(jobName string) {
	v.JobName = jobName
	v.script = ""
	v.lines = nil
	v.loading = true
	v.errMsg = ""
	v.scrollTop = 0
	v.visible = true
}

// SetScript sets the loaded script content (or error).
func (v *ScriptViewer) SetScript(script string, err error) {
	v.loading = false
	if err != nil {
		v.errMsg = err.Error()
		return
	}
	v.script = script
	v.lines = strings.Split(script, "\n")
}

// Hide closes the viewer.
func (v *ScriptViewer) Hide() { v.visible = false }

// Visible reports whether the viewer is shown.
func (v ScriptViewer) Visible() bool { return v.visible }

// SetSize updates terminal dimensions.
func (v *ScriptViewer) SetSize(w, h int) { v.width = w; v.height = h }

func (v ScriptViewer) contentHeight() int {
	h := v.height - 8
	if h < 5 {
		h = 5
	}
	return h
}

// Update handles scroll keys and Esc.
func (v ScriptViewer) Update(msg tea.Msg) (ScriptViewer, tea.Cmd) {
	if !v.visible {
		return v, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return v, nil
	}
	ch := v.contentHeight()
	maxTop := len(v.lines) - ch
	if maxTop < 0 {
		maxTop = 0
	}

	clamp := func(n int) int {
		if n < 0 {
			return 0
		}
		if n > maxTop {
			return maxTop
		}
		return n
	}

	switch km.String() {
	case "esc":
		v.visible = false
		return v, func() tea.Msg { return ScriptViewerResult{} }
	case "up":
		v.scrollTop = clamp(v.scrollTop - 1)
	case "down":
		v.scrollTop = clamp(v.scrollTop + 1)
	case "ctrl+f":
		v.scrollTop = clamp(v.scrollTop + scriptViewerPage)
	case "ctrl+b":
		v.scrollTop = clamp(v.scrollTop - scriptViewerPage)
	case "home":
		v.scrollTop = 0
	case "end":
		v.scrollTop = maxTop
	}
	return v, nil
}

// View renders the script viewer.
func (v ScriptViewer) View() string {
	if !v.visible {
		return ""
	}
	ch := v.contentHeight()

	var sb strings.Builder
	sb.WriteString(theme.StyleKeyHint.Render(theme.SymArrow + " Pipeline Script: "))
	sb.WriteString(theme.StyleTitle.Render(v.JobName))
	sb.WriteString("\n\n")

	switch {
	case v.loading:
		sb.WriteString(theme.StyleDim.Render("Loading…"))
		sb.WriteString("\n")
	case v.errMsg != "":
		sb.WriteString(theme.StyleError.Render(theme.SymFail + " " + v.errMsg))
		sb.WriteString("\n")
	case len(v.lines) == 0:
		sb.WriteString(theme.StyleDim.Render("(no script found)"))
		sb.WriteString("\n")
	default:
		end := v.scrollTop + ch
		if end > len(v.lines) {
			end = len(v.lines)
		}
		visible := v.lines[v.scrollTop:end]
		for i, line := range visible {
			num := fmt.Sprintf("%4d  ", v.scrollTop+i+1)
			sb.WriteString(theme.StyleDim.Render(num))
			if line == "" {
				sb.WriteString(" ")
			} else {
				sb.WriteString(line)
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	posHint := " "
	if len(v.lines) > 0 {
		end := v.scrollTop + ch
		if end > len(v.lines) {
			end = len(v.lines)
		}
		posHint = fmt.Sprintf(" lines %d–%d/%d", v.scrollTop+1, end, len(v.lines))
		if v.scrollTop+ch >= len(v.lines) {
			posHint += " [end]"
		}
		posHint += " · "
	}
	sb.WriteString(theme.StyleDim.Render(posHint + "↑/↓ scroll · Home/End · Esc back"))

	return theme.BorderBox(sb.String(), "info", v.width)
}
