package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseFlatYAML(t *testing.T) {
	in := strings.NewReader(`
# a comment
url: http://127.0.0.1:20128
apiKey: "sk-quoted-value"
model: 'single-quoted'
empty:
blank-line-above: yes
: no-key-ignored
`)
	got := parseFlatYAML(in)
	want := map[string]string{
		"url":               "http://127.0.0.1:20128",
		"apiKey":            "sk-quoted-value",
		"model":             "single-quoted",
		"empty":             "",
		"blank-line-above":  "yes",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got[""]; ok {
		t.Errorf("empty key should be ignored, got %v", got)
	}
}

// TestFileLayerPriority verifies env > file > baked, and CB_SKIP_LM_FILE.
func TestFileLayerPriority(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "bee.lm.yml"),
		[]byte("url: http://from-file:9000\nmodel: file-model\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Reset the sync.Once so loadFile re-reads inside this test's cwd.
	fileOnce = sync.Once{}
	fileVals = nil
	t.Setenv("CB_SKIP_LM_FILE", "")

	if got := LMURL(); got != "http://from-file:9000" {
		t.Errorf("file layer: LMURL = %q, want from-file", got)
	}

	// env overrides file
	t.Setenv("CB_DATABRICK_URL", "http://from-env:1")
	if got := LMURL(); got != "http://from-env:1" {
		t.Errorf("env override: LMURL = %q, want from-env", got)
	}

	// CB_SKIP_LM_FILE=1 ignores the file (Model falls back to default, not file-model)
	t.Setenv("CB_SKIP_LM_FILE", "1")
	fileOnce = sync.Once{}
	fileVals = nil
	if got := Model(); got != "default" {
		t.Errorf("skip file: Model = %q, want default", got)
	}
}
