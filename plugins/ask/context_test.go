package ask

import (
	"strings"
	"testing"
)

// TestSystemPromptShape locks the SYSTEM_PROMPT to the TS content: the three
// worked examples must be present and the compact-JSON key order preserved.
// (Byte-for-byte parity with the TS build was verified out-of-band; this guards
// against accidental edits.)
func TestSystemPromptShape(t *testing.T) {
	for _, want := range []string{
		"Example — 'trigger a job':",
		"Example — 'list all nodes':",
		"Example — 'what can I do with jobs'",
		`{"reasoning":"Context has command id='job.run'`,
		`"flags":[],"example":"bee job stop my-pipeline 42"`, // empty flags as []
		`Off-topic: {"explanation":"I only help with bee usage."`,
	} {
		if !strings.Contains(SYSTEM_PROMPT, want) {
			t.Errorf("SYSTEM_PROMPT missing %q", want)
		}
	}
}

// TestBuildUserPromptFormat locks the context block structure.
func TestBuildUserPromptFormat(t *testing.T) {
	items := []DocItem{
		{ID: "job.run", Type: "command", Title: "bee job run <name>", Description: "Trigger a build", Body: "  --wait   Block until done"},
		{ID: "concept.x", Type: "doc", Title: "concept.x", Body: "A sentence here.\nbee job run <name>"},
	}
	got := BuildUserPrompt("how to run", items)
	for _, want := range []string{
		"<context>\n",
		`<command id="bee job run &lt;name&gt;">`,
		"  <desc>Trigger a build</desc>",
		"  <flags>\n",
		`    <flag name="--wait">Block until done</flag>`,
		"  </flags>\n</command>",
		`<info id="concept.x">`,
		"\n\nQuestion: how to run\n\nAnswer:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("user prompt missing %q\n---\n%s", want, got)
		}
	}
}
