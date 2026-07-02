package ask

import "testing"

// fakeProvider satisfies LMProvider without any network calls.
type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) Generate(prompt string, maxTokens int) (string, error) {
	return "fake answer", nil
}
func (fakeProvider) GenerateWithUsage(prompt string, maxTokens int) (string, TokenUsage, error) {
	return "fake answer", TokenUsage{}, nil
}
func (fakeProvider) GenerateJSON(prompt string) (*LMAnswer, TokenUsage, error) {
	return &LMAnswer{Explanation: "fake explanation", Commands: nil}, TokenUsage{}, nil
}
func (fakeProvider) Stream(prompt string, write func(string)) error { return nil }

// Mandatory P3 check: bee ask must work in BM25-only mode when no embedding
// endpoint is configured (embeddings_gen.go's baked DIM==0 placeholder, or
// CB_EMBEDDING_URL unset) — vector fusion must no-op, not error.
func TestAnswerFallsBackToBM25WhenNoEmbeddingConfigured(t *testing.T) {
	t.Setenv("CB_EMBEDDING_URL", "")
	t.Setenv("CB_DATABRICK_URL", "")

	corpus := BuildCorpus(nil)
	result, err := Answer("how do I run a job", corpus, fakeProvider{}, 5)
	if err != nil {
		t.Fatalf("Answer returned error in BM25-only mode: %v", err)
	}
	if result == nil {
		t.Fatal("Answer returned nil result")
	}
	if result.Source != "lm" {
		t.Fatalf("expected source=lm (fake provider still answers), got %q", result.Source)
	}
}

func TestGetVectorDbEmptyWhenNoBakedVectors(t *testing.T) {
	// embeddings_gen.go ships as a DIM=0 placeholder until cmd/genembeddings
	// runs — getVectorDb must return an empty (not nil, not panicking) db.
	vdb := getVectorDb()
	if vdb == nil {
		t.Fatal("getVectorDb returned nil")
	}
	if DIM == 0 && len(vdb.Matrix) != 0 {
		t.Fatalf("expected empty matrix when DIM==0, got %d rows", len(vdb.Matrix))
	}
}
