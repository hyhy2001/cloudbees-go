package ask

import (
	"testing"
)

func TestBuildMatchExpr(t *testing.T) {
	expr := buildMatchExpr("how do I run a job")
	if expr == "" {
		t.Fatal("expected non-empty match expr")
	}
	t.Logf("matchExpr: %s", expr)
}

func TestBuildCorpusNoRoot(t *testing.T) {
	corpus := BuildCorpus(nil)
	if len(corpus) == 0 {
		t.Fatal("expected corpus items from help facts")
	}
	t.Logf("corpus size: %d", len(corpus))
}

func TestSearchDocs(t *testing.T) {
	corpus := BuildCorpus(nil)
	hits := searchDocs("run job", corpus, 5, true, true)
	if len(hits) == 0 {
		t.Fatal("expected hits for 'run job'")
	}
	t.Logf("hits: %v", func() []string {
		ids := make([]string, len(hits))
		for i, h := range hits {
			ids[i] = h.ID
		}
		return ids
	}())
}

func TestStripThinkBlock(t *testing.T) {
	in := "<think>some reasoning</think> actual answer"
	out := StripThinkBlock(in)
	if out != "actual answer" {
		t.Fatalf("got %q", out)
	}
}
