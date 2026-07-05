package ask

import (
	"strings"
	"testing"
)

// TestAskJSONShape locks the --json payload to the TS CLI's shape: key order
// query, source, provider, answer, structured, hits; empty hits as [] (not
// null); and provider null when unset.
func TestAskJSONShape(t *testing.T) {
	// raw path, no provider, no hits → provider null, hits []
	b, err := askJSONBytes("nope", &AnswerResult{Source: "raw"})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)

	wantOrder := []string{`"query"`, `"source"`, `"provider"`, `"answer"`, `"structured"`, `"hits"`}
	last := -1
	for _, k := range wantOrder {
		i := strings.Index(got, k)
		if i < 0 {
			t.Fatalf("missing key %s in:\n%s", k, got)
		}
		if i < last {
			t.Fatalf("key %s out of order in:\n%s", k, got)
		}
		last = i
	}
	if !strings.Contains(got, `"provider": null`) {
		t.Errorf("provider should be null when unset:\n%s", got)
	}
	if !strings.Contains(got, `"hits": []`) {
		t.Errorf("empty hits should be [] not null:\n%s", got)
	}

	// lm path with provider set → provider is a string
	b2, _ := askJSONBytes("q", &AnswerResult{Source: "lm", Provider: "local-lm", Text: "hi"})
	if !strings.Contains(string(b2), `"provider": "local-lm"`) {
		t.Errorf("provider should serialize as string when set:\n%s", b2)
	}
}
