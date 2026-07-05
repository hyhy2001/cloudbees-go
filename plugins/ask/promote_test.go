package ask

import "testing"

// TestInjectOrPromoteAfterFirst locks the TS splice(promoted?1:0) semantics:
// once a top hit is promoted, a later promotion must land at position 1 and
// leave index 0 untouched (this is what keeps a command on top when a broad
// doc pattern like "pipeline job" also matches).
func TestInjectOrPromoteAfterFirst(t *testing.T) {
	base := []DocItem{
		{ID: "concept.pipeline", Type: "doc"},
		{ID: "job.create.pipeline", Type: "command"},
		{ID: "concept.what-is-job", Type: "doc"},
	}

	// Simulate the intent loop: first promote the command to top (not afterFirst),
	// then a second pattern promotes the doc — afterFirst=true must keep the
	// command at index 0.
	got := injectOrPromote(base, "job.create.pipeline", nil, false)
	if got[0].ID != "job.create.pipeline" {
		t.Fatalf("first promote: want command at 0, got %q", got[0].ID)
	}
	got = injectOrPromote(got, "concept.pipeline", nil, true)
	if got[0].ID != "job.create.pipeline" {
		t.Errorf("afterFirst promote clobbered top: got %q, want job.create.pipeline", got[0].ID)
	}
	if got[1].ID != "concept.pipeline" {
		t.Errorf("afterFirst promote should land at index 1, got %q", got[1].ID)
	}

	// afterFirst=false still moves to index 0.
	got2 := injectOrPromote(base, "job.create.pipeline", nil, false)
	if got2[0].ID != "job.create.pipeline" {
		t.Errorf("want command at 0, got %q", got2[0].ID)
	}

	// Fetch-from-corpus path with afterFirst inserts at index 1.
	corpus := []DocItem{{ID: "node.list", Type: "command"}}
	got3 := injectOrPromote(base, "node.list", corpus, true)
	if got3[0].ID != "concept.pipeline" || got3[1].ID != "node.list" {
		t.Errorf("corpus-fetch afterFirst: got [%q, %q], want [concept.pipeline, node.list]", got3[0].ID, got3[1].ID)
	}
}
