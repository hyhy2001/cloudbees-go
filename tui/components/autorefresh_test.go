package components

import (
	"testing"
	"time"
)

func TestAutoRefreshBackoff(t *testing.T) {
	var a AutoRefresh
	// 5s → 10s → 20s → 40s → 60s (cap) → 60s
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 60 * time.Second, 60 * time.Second}
	for i, w := range want {
		if got := a.Next(); got != w {
			t.Fatalf("Next() call %d = %v, want %v", i+1, got, w)
		}
	}
	// Reset returns to the base interval.
	a.Reset()
	if got := a.Next(); got != 5*time.Second {
		t.Fatalf("after Reset, Next() = %v, want 5s", got)
	}
}
