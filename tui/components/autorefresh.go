package components

import "time"

// AutoRefresh computes the exponential-backoff interval for a screen's periodic
// refresh: base 5s, doubling each tick, capped at 60s. Mirrors the TS TUI's
// useAutoRefresh cadence. The screen owns its own tick message type and enabled
// flag; this just tracks the growing interval so a quiet screen polls less
// often. Reset() returns to the base interval (call after any user action or a
// data change so the next poll is prompt).
type AutoRefresh struct {
	interval time.Duration
}

const (
	autoRefreshBase = 5 * time.Second
	autoRefreshMax  = 60 * time.Second
)

// Next returns the interval to wait before the next refresh, then advances the
// backoff (doubling, capped). Call once per scheduled tick.
func (a *AutoRefresh) Next() time.Duration {
	if a.interval < autoRefreshBase {
		a.interval = autoRefreshBase
	}
	d := a.interval
	a.interval *= 2
	if a.interval > autoRefreshMax {
		a.interval = autoRefreshMax
	}
	return d
}

// Reset returns the backoff to the base interval — call when the user acts or
// data changes so polling resumes promptly rather than at the stretched interval.
func (a *AutoRefresh) Reset() { a.interval = autoRefreshBase }
