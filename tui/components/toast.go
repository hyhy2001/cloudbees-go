package components

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"bee/tui/theme"
)

// ToastKind controls the Toast's border colour.
type ToastKind string

const (
	ToastInfo    ToastKind = "info"
	ToastSuccess ToastKind = "success"
	ToastWarning ToastKind = "warning"
	ToastError   ToastKind = "error"
)

// toastExpiredMsg is sent by the auto-dismiss timer.
type toastExpiredMsg struct{ id int }

// Toast is a transient, self-dismissing notification banner (4-second default
// TTL). Severity controls the border colour (info/success/warning/error).
// Call Show() to display; call Update() every tick so the timer fires.
type Toast struct {
	Message string
	Kind    ToastKind
	Width   int
	visible bool
	id      int // monotonic counter, avoids stale timer msgs
}

// Show displays the Toast for ttl (pass 0 → 4-second default).
func (t *Toast) Show(msg string, kind ToastKind, ttl time.Duration) tea.Cmd {
	if ttl <= 0 {
		ttl = 4 * time.Second
	}
	t.Message = msg
	t.Kind = kind
	t.visible = true
	t.id++
	id := t.id
	return tea.Tick(ttl, func(_ time.Time) tea.Msg {
		return toastExpiredMsg{id: id}
	})
}

// ShowInfo is a convenience wrapper for ToastInfo.
func (t *Toast) ShowInfo(msg string) tea.Cmd { return t.Show(msg, ToastInfo, 0) }

// ShowSuccess is a convenience wrapper for ToastSuccess.
func (t *Toast) ShowSuccess(msg string) tea.Cmd { return t.Show(msg, ToastSuccess, 0) }

// ShowWarning is a convenience wrapper for ToastWarning.
func (t *Toast) ShowWarning(msg string) tea.Cmd { return t.Show(msg, ToastWarning, 0) }

// ShowError is a convenience wrapper for ToastError.
func (t *Toast) ShowError(msg string) tea.Cmd { return t.Show(msg, ToastError, 5*time.Second) }

// Hide dismisses the Toast immediately.
func (t *Toast) Hide() { t.visible = false }

// Visible reports whether the Toast is currently shown.
func (t Toast) Visible() bool { return t.visible }

// Update handles the auto-dismiss tick.
func (t Toast) Update(msg tea.Msg) (Toast, tea.Cmd) {
	if exp, ok := msg.(toastExpiredMsg); ok && exp.id == t.id {
		t.visible = false
	}
	return t, nil
}

// View renders the Toast. Returns "" when not visible.
func (t Toast) View() string {
	if !t.visible {
		return ""
	}
	sym := theme.SymArrow
	switch t.Kind {
	case ToastSuccess:
		sym = theme.SymOK
	case ToastWarning:
		sym = theme.SymWarn
	case ToastError:
		sym = theme.SymFail
	}
	severity := string(t.Kind)
	if severity == "error" {
		severity = "danger"
	}
	content := sym + " " + t.Message
	return theme.BorderBox(content, severity, t.Width)
}
