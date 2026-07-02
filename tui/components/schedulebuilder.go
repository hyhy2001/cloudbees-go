package components

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bee/plugins/job"
	"bee/tui/theme"
)

// ScheduleResultMsg carries the composed cron string on save (""=no
// schedule), or Cancelled=true on Esc.
type ScheduleResultMsg struct {
	Cron      string
	Cancelled bool
}

type scheduleRow string

const (
	rowFrequency scheduleRow = "frequency"
	rowHour      scheduleRow = "hour"
	rowMinute    scheduleRow = "minute"
	rowDay       scheduleRow = "day"
	rowDom       scheduleRow = "dom"
	rowCustom    scheduleRow = "custom"
)

var scheduleFrequencies = []job.Frequency{job.FreqOff, job.FreqHourly, job.FreqDaily, job.FreqWeekly, job.FreqMonthly, job.FreqCustom}

var scheduleFreqLabel = map[job.Frequency]string{
	job.FreqOff:     "Off (manual only)",
	job.FreqHourly:  "Hourly",
	job.FreqDaily:   "Daily",
	job.FreqWeekly:  "Weekly",
	job.FreqMonthly: "Monthly",
	job.FreqCustom:  "Custom cron",
}

func scheduleRowsFor(freq job.Frequency) []scheduleRow {
	switch freq {
	case job.FreqOff:
		return []scheduleRow{rowFrequency}
	case job.FreqHourly:
		return []scheduleRow{rowFrequency, rowMinute}
	case job.FreqDaily:
		return []scheduleRow{rowFrequency, rowHour, rowMinute}
	case job.FreqWeekly:
		return []scheduleRow{rowFrequency, rowHour, rowMinute, rowDay}
	case job.FreqMonthly:
		return []scheduleRow{rowFrequency, rowHour, rowMinute, rowDom}
	case job.FreqCustom:
		return []scheduleRow{rowFrequency, rowCustom}
	}
	return []scheduleRow{rowFrequency}
}

func wrap(n, max int) int {
	n %= max
	if n < 0 {
		n += max
	}
	return n
}

// ScheduleBuilder is a full-screen overlay for visually building a cron
// schedule. Mirrors ScheduleBuilder.tsx.
type ScheduleBuilder struct {
	spec    job.ScheduleSpec
	cursor  int
	jobName string
	visible bool
}

// Show opens the builder with an initial spec (e.g. job.ParseCron(existingCron)).
func (b *ScheduleBuilder) Show(initial job.ScheduleSpec, jobName string) {
	b.spec = initial
	b.cursor = 0
	b.jobName = jobName
	b.visible = true
}

// Hide dismisses the builder without emitting a result.
func (b *ScheduleBuilder) Hide() { b.visible = false }

// Visible reports whether the overlay is currently shown.
func (b ScheduleBuilder) Visible() bool { return b.visible }

func (b ScheduleBuilder) currentRow() scheduleRow {
	rows := scheduleRowsFor(b.spec.Frequency)
	i := b.cursor
	if i >= len(rows) {
		i = len(rows) - 1
	}
	return rows[i]
}

func (b *ScheduleBuilder) change(dir int) {
	switch b.currentRow() {
	case rowFrequency:
		i := 0
		for idx, f := range scheduleFrequencies {
			if f == b.spec.Frequency {
				i = idx
				break
			}
		}
		b.spec.Frequency = scheduleFrequencies[wrap(i+dir, len(scheduleFrequencies))]
		b.cursor = 0
	case rowHour:
		b.spec.Hour = wrap(b.spec.Hour+dir, 24)
	case rowMinute:
		b.spec.Minute = wrap(b.spec.Minute+dir, 60)
	case rowDay:
		i := 0
		for idx, d := range job.DayPresets {
			if d == b.spec.DayPreset {
				i = idx
				break
			}
		}
		b.spec.DayPreset = job.DayPresets[wrap(i+dir, len(job.DayPresets))]
	case rowDom:
		b.spec.Dom = wrap(b.spec.Dom-1+dir, 31) + 1
	}
}

// Update handles key events while visible.
func (b ScheduleBuilder) Update(msg tea.Msg) (ScheduleBuilder, tea.Cmd) {
	if !b.visible {
		return b, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return b, nil
	}
	rows := scheduleRowsFor(b.spec.Frequency)
	row := b.currentRow()

	switch km.Type {
	case tea.KeyEsc:
		b.visible = false
		return b, func() tea.Msg { return ScheduleResultMsg{Cancelled: true} }
	case tea.KeyEnter:
		b.visible = false
		cron := job.BuildCron(b.spec)
		return b, func() tea.Msg { return ScheduleResultMsg{Cron: cron} }
	case tea.KeyUp:
		if b.cursor > 0 {
			b.cursor--
		}
		return b, nil
	case tea.KeyDown:
		if b.cursor < len(rows)-1 {
			b.cursor++
		}
		return b, nil
	case tea.KeyHome:
		b.cursor = 0
		return b, nil
	case tea.KeyEnd:
		b.cursor = len(rows) - 1
		return b, nil
	}

	if row == rowCustom {
		switch km.Type {
		case tea.KeyBackspace, tea.KeyDelete:
			if len(b.spec.Custom) > 0 {
				b.spec.Custom = b.spec.Custom[:len(b.spec.Custom)-1]
			}
			return b, nil
		case tea.KeyRunes:
			b.spec.Custom += string(km.Runes)
			return b, nil
		case tea.KeySpace:
			b.spec.Custom += " "
			return b, nil
		}
	}

	switch km.Type {
	case tea.KeyLeft:
		b.change(-1)
	case tea.KeyRight:
		b.change(1)
	}
	return b, nil
}

func pad2(n int) string {
	s := strconv.Itoa(n)
	if len(s) < 2 {
		return "0" + s
	}
	return s
}

func padWidth(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func (b ScheduleBuilder) renderRow(kind scheduleRow, idx int) string {
	rows := scheduleRowsFor(b.spec.Frequency)
	cursor := b.cursor
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	on := idx == cursor

	var label, value string
	switch kind {
	case rowFrequency:
		label, value = "Frequency", scheduleFreqLabel[b.spec.Frequency]
	case rowHour:
		label, value = "Hour", pad2(b.spec.Hour)
	case rowMinute:
		label, value = "Minute", pad2(b.spec.Minute)
	case rowDay:
		label, value = "Day", job.DayPresetLabel[b.spec.DayPreset]
	case rowDom:
		label, value = "Day of month", strconv.Itoa(b.spec.Dom)
	case rowCustom:
		label, value = "Custom cron", b.spec.Custom
		if on {
			value += "_"
		}
	}

	labelStyle := theme.StyleDim
	marker := " "
	if on {
		labelStyle = theme.StyleKeyHint
		marker = theme.SymArrow
	}
	line := labelStyle.Render(marker+" "+padWidth(label, 14)) + " "
	if on && kind != rowCustom {
		line += theme.StyleDim.Render(theme.SymArrow+" ") + value + theme.StyleDim.Render(" "+theme.SymArrow)
	} else {
		line += value
	}
	return line
}

// View renders the overlay.
func (b ScheduleBuilder) View() string {
	if !b.visible {
		return ""
	}
	title := theme.SymGear + " Schedule Builder"
	if b.jobName != "" {
		title += " — " + b.jobName
	}
	var sb strings.Builder
	sb.WriteString(theme.StyleTitle.Render(title))
	sb.WriteString("\n\n")

	rows := scheduleRowsFor(b.spec.Frequency)
	for i, kind := range rows {
		sb.WriteString(b.renderRow(kind, i))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(theme.StyleDim.Render(job.DescribeSchedule(b.spec)))
	sb.WriteString("\n")
	cron := job.BuildCron(b.spec)
	if cron == "" {
		cron = "(none)"
	}
	sb.WriteString(theme.StyleDim.Render("cron: ") + theme.StyleKeyHint.Render(cron))
	sb.WriteString("\n\n")

	hint := "↑↓ move · ←→ change"
	if b.currentRow() == rowCustom {
		hint += " · type cron (min hr dom mon dow)"
	}
	hint += " · Enter save · Esc cancel"
	sb.WriteString(theme.StyleDim.Render(hint))
	return sb.String()
}
