package components

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"bee/tui/theme"
)

// spinnerFrames is the 10-frame braille spinner matching TS's spinnerFrames.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerTickMsg is the internal tick message for advancing the spinner frame.
type spinnerTickMsg struct{ id int }

const spinnerInterval = 100 * time.Millisecond

// Spinner is a braille-frame spinning indicator (10 frames, 100ms each).
// Show()/Hide() toggle it; call Update() on every message to advance frames.
type Spinner struct {
	Label   string
	frame   int
	running bool
	id      int
}

// Start starts the spinner with the given label and returns the first tick cmd.
func (s *Spinner) Start(label string) tea.Cmd {
	s.Label = label
	s.running = true
	s.id++
	return s.tick()
}

// Stop halts the spinner.
func (s *Spinner) Stop() { s.running = false }

// Running reports whether the spinner is active.
func (s Spinner) Running() bool { return s.running }

func (s Spinner) tick() tea.Cmd {
	id := s.id
	return tea.Tick(spinnerInterval, func(_ time.Time) tea.Msg {
		return spinnerTickMsg{id: id}
	})
}

// Update advances the spinner frame on each tick.
func (s Spinner) Update(msg tea.Msg) (Spinner, tea.Cmd) {
	if tick, ok := msg.(spinnerTickMsg); ok && tick.id == s.id && s.running {
		s.frame = (s.frame + 1) % len(spinnerFrames)
		return s, s.tick()
	}
	return s, nil
}

// View renders the current spinner frame + label. Returns "" when not running.
func (s Spinner) View() string {
	if !s.running {
		return ""
	}
	return theme.StyleKeyHint.Render(spinnerFrames[s.frame]) +
		theme.StyleDim.Render(" "+s.Label)
}
