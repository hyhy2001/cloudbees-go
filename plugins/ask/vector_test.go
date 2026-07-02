package ask

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	if got := cosineSimilarity([]float64{1, 0}, []float64{1, 0}); math.Abs(got-1) > 1e-9 {
		t.Errorf("identical vectors: got %v, want 1", got)
	}
	if got := cosineSimilarity([]float64{1, 0}, []float64{0, 1}); math.Abs(got) > 1e-9 {
		t.Errorf("orthogonal vectors: got %v, want 0", got)
	}
	if got := cosineSimilarity([]float64{0, 0}, []float64{1, 1}); got != 0 {
		t.Errorf("zero-norm vector: got %v, want 0", got)
	}
}

func TestRRFFusion(t *testing.T) {
	a := DocItem{ID: "a", Body: "x"}
	b := DocItem{ID: "b", Body: "x"}
	c := DocItem{ID: "c", Body: "x"}

	bm25 := []DocItem{a, b}
	vector := []DocItem{b, c}
	fused := rrfFusion(bm25, vector, 60)

	if len(fused) != 3 {
		t.Fatalf("expected 3 unique items, got %d", len(fused))
	}
	// b appears at rank 0 in both lists — should rank first.
	if fused[0].ID != "b" {
		t.Errorf("expected b first (present in both lists), got %s", fused[0].ID)
	}
}

func TestChunkMarkdown(t *testing.T) {
	content := "# Title\n\nintro text\n\n## Section One\n\nbody one\n\n```\n# not a heading\n```\n\nmore body\n\n## Section One\n\nduplicate heading body"
	chunks := chunkMarkdown("test.md", content)

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].ID != "test.md#title" {
		t.Errorf("chunk 0 id = %q, want test.md#title", chunks[0].ID)
	}
	if chunks[1].ID != "test.md#section-one" {
		t.Errorf("chunk 1 id = %q, want test.md#section-one", chunks[1].ID)
	}
	if chunks[2].ID != "test.md#section-one-2" {
		t.Errorf("chunk 2 id (deduped) = %q, want test.md#section-one-2", chunks[2].ID)
	}
	// fenced "# not a heading" must stay inside chunk 1's body, not split it.
	if !contains(chunks[1].Body, "not a heading") {
		t.Errorf("fenced code heading leaked out of its chunk: %q", chunks[1].Body)
	}
}

func TestChunkMarkdownSkipsEmptyHeading(t *testing.T) {
	// A top-level "#" immediately followed by "##" has no body of its own —
	// must be skipped (its keywords are already covered by the child section).
	content := "# Empty Title\n## Real Section\nbody"
	chunks := chunkMarkdown("test.md", content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (empty title skipped), got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Heading != "Real Section" {
		t.Errorf("heading = %q, want Real Section", chunks[0].Heading)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}
