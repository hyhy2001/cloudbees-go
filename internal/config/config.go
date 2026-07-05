// Package config holds LM endpoint configuration for `bee ask`.
//
// Priority order (highest wins):
//  1. Runtime env   — CB_DATABRICK_URL / CB_API_KEY / CB_LM_MODEL / ...
//  2. Runtime file  — bee.lm.yml (~/.config/bee/lm.yml, then ./bee.lm.yml),
//     a flat `key: value` file with friendly keys (url, apiKey, model, ...).
//     Lets the endpoint change without a rebuild. Skip with CB_SKIP_LM_FILE=1.
//  3. Build-time    — baked via -ldflags "-X config.BakedLMURL=<xor-encoded>",
//     values sourced from bee.lm.yml at build time (see Makefile).
//     Values are XOR-obfuscated so they don't appear in `strings ./bee`.
//
// LMURL/APIKey normally point at an llm-gateway instance (OpenAI-compatible),
// which holds the real provider credential server-side. ClientID/ClientSecret
// are only for the legacy direct-Databricks-OAuth path (provider_databricks.go),
// kept for setups that call Databricks directly instead of through a gateway.
//
// When LM_URL is empty, no provider is registered and bee ask runs offline.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"bee/internal/obfuscate"
)

// fileConfig holds LM settings read from a runtime bee.lm.yml, so the endpoint
// can be changed without rebuilding. Loaded once, lazily. Mirrors the TS CLI's
// bee.lm.json layer; keys use the friendly names (url, apiKey, model, ...).
var (
	fileOnce sync.Once
	fileVals map[string]string
)

// loadFile reads the first existing config file — ~/.config/bee/lm.yml then
// ./bee.lm.yml — as a flat `key: value` map. Skipped when CB_SKIP_LM_FILE=1.
// A missing or unreadable file just yields an empty map (env/baked still apply).
func loadFile() map[string]string {
	fileOnce.Do(func() {
		fileVals = map[string]string{}
		if os.Getenv("CB_SKIP_LM_FILE") == "1" {
			return
		}
		var candidates []string
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".config", "bee", "lm.yml"))
		}
		candidates = append(candidates, "bee.lm.yml")
		for _, p := range candidates {
			f, err := os.Open(p)
			if err != nil {
				continue
			}
			fileVals = parseFlatYAML(f)
			f.Close()
			return
		}
	})
	return fileVals
}

// parseFlatYAML parses a flat `key: value` file — the only shape bee.lm.yml
// needs. Ignores blank lines and `#` comments; strips matching surrounding
// quotes from values. Not a general YAML parser (no nesting/lists) on purpose.
func parseFlatYAML(r interface{ Read([]byte) (int, error) }) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, ':')
		if i < 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		if key != "" {
			out[key] = val
		}
	}
	return out
}

// Baked build-time values — set via:
//
//	go build -ldflags "-X bee/internal/config.BakedLMURL=<xor>"
//
// All values are XOR-obfuscated (obfuscate.Encode). Empty string = not set.
var (
	BakedLMURL        string
	BakedAPIKey       string
	BakedModel        string
	BakedRewriteModel string
	BakedClientID     string
	BakedClientSecret string
	BakedChatPath     string
	BakedEmbedModel   string
	BakedEmbedURL     string
	BakedEmbedPath    string
)

// pick resolves a value by priority: env override > runtime file (bee.lm.yml)
// > decoded baked value > fallback. fileKey is the friendly key in the file.
func pick(envKey, fileKey, baked, fallback string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if v := loadFile()[fileKey]; v != "" {
		return v
	}
	if baked != "" {
		return obfuscate.Decode(baked)
	}
	return fallback
}

// LMURL is the base URL for the LM provider (an llm-gateway instance, or a
// direct provider host when ClientID/ClientSecret are set).
func LMURL() string { return pick("CB_DATABRICK_URL", "url", BakedLMURL, "") }

// APIKey is the static Bearer token (llm-gateway client key, or provider key).
func APIKey() string { return pick("CB_API_KEY", "apiKey", BakedAPIKey, "") }

// Model is the main chat model identifier.
func Model() string { return pick("CB_LM_MODEL", "model", BakedModel, "default") }

// RewriteModel is the model used for query rewriting (falls back to Model).
func RewriteModel() string {
	v := pick("CB_REWRITE_MODEL", "rewriteModel", BakedRewriteModel, "")
	if v == "" {
		return Model()
	}
	return v
}

// ClientID is the OAuth M2M client ID (direct Databricks provider only).
func ClientID() string { return pick("CB_CLIENT_ID", "clientId", BakedClientID, "") }

// ClientSecret is the OAuth M2M client secret (direct Databricks provider only).
func ClientSecret() string {
	return pick("CB_CLIENT_SECRET", "clientSecret", BakedClientSecret, "")
}

// ChatPath is the chat completions endpoint path.
func ChatPath() string {
	return pick("CB_CHAT_PATH", "chatPath", BakedChatPath, "/v1/chat/completions")
}

// EmbedModel is the embedding model name.
func EmbedModel() string {
	return pick("CB_EMBEDDING_MODEL", "embeddingModel", BakedEmbedModel, "default")
}

// EmbedURL is the full embedding endpoint URL (overrides base URL + path).
func EmbedURL() string { return pick("CB_EMBEDDING_URL", "embeddingUrl", BakedEmbedURL, "") }

// EmbedPath is the embedding endpoint path.
func EmbedPath() string {
	return pick("CB_EMBEDDING_PATH", "embeddingPath", BakedEmbedPath, "/v1/embeddings")
}
