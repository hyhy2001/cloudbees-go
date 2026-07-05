package ask

import (
	"strings"
	"testing"
)

func TestStripInventedCommands(t *testing.T) {
	corpus := []DocItem{
		{ID: "job.list", Type: "command", Body: "flags --all show all"},
		{ID: "job.run", Type: "command", Body: "flags --wait"},
	}
	// valid command + valid flag survive; invented command + flag stripped.
	in := "Use `bee job list --all` to list. Then run `bee job start --turbo` and `bee frobnicate`."
	out := StripInventedCommands(in, corpus)
	if !strings.Contains(out, "bee job list --all") {
		t.Errorf("valid command was stripped: %q", out)
	}
	if strings.Contains(out, "bee job start") {
		t.Errorf("invented command 'bee job start' survived: %q", out)
	}
	if strings.Contains(out, "frobnicate") {
		t.Errorf("invented command 'bee frobnicate' survived: %q", out)
	}
	if strings.Contains(out, "--turbo") {
		t.Errorf("invented flag --turbo survived: %q", out)
	}
	// no sentinel char leaks into output
	if strings.ContainsRune(out, '') {
		t.Errorf("sentinel leaked: %q", out)
	}
	// text with only valid commands returns unchanged
	clean := "Run `bee job run --wait` to execute."
	if StripInventedCommands(clean, corpus) != clean {
		t.Errorf("clean text altered: %q", StripInventedCommands(clean, corpus))
	}
}
