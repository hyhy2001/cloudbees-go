package ask

import "testing"

func TestExpandGraph(t *testing.T) {
	corpus := []DocItem{
		{ID: "job.create.pipeline", Type: "command"},
		{ID: "job.create.freestyle", Type: "command"},
		{ID: "job.update.pipeline", Type: "command"},
		{ID: "job.delete", Type: "command"},
		{ID: "node.list", Type: "command"},
		{ID: "concept.pipeline", Type: "doc"},
	}
	g := buildGraphFromCorpus(corpus)
	// starting from one job command, expansion should pull in job siblings
	// (same group) but not node.list or the doc.
	hits := []DocItem{{ID: "job.create.pipeline", Type: "command"}}
	extra := expandGraph(hits, corpus, g, 10)
	ids := map[string]bool{}
	for _, e := range extra {
		ids[e.ID] = true
	}
	for _, want := range []string{"job.create.freestyle", "job.update.pipeline", "job.delete"} {
		if !ids[want] {
			t.Errorf("expected graph neighbor %q, got %v", want, ids)
		}
	}
	if ids["node.list"] {
		t.Errorf("node.list should not be a neighbor of a job command")
	}
	if ids["concept.pipeline"] {
		t.Errorf("docs must not appear in graph expansion")
	}
	// maxExtra cap respected
	if len(expandGraph(hits, corpus, g, 1)) != 1 {
		t.Errorf("maxExtra=1 not respected")
	}
}
