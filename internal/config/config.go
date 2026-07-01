// Package config holds LM endpoint configuration for `bee ask`.
//
// Priority order (highest wins):
//  1. Runtime env  — CB_DATABRICK_URL / CB_API_KEY / CB_LM_MODEL / ...
//  2. Build-time   — baked via -ldflags "-X config.BakedLMURL=<xor-encoded>"
//     Values are XOR-obfuscated so they don't appear in `strings ./bee`.
//
// When LM_URL is empty, no provider is registered and bee ask runs offline.
package config

import (
	"os"

	"github.com/hyhy2001/bee/internal/obfuscate"
)

// Baked build-time values — set via:
//
//	go build -ldflags "-X github.com/hyhy2001/bee/internal/config.BakedLMURL=<xor>"
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

// pick returns env override, or decoded baked value, or fallback.
func pick(envKey, baked, fallback string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if baked != "" {
		return obfuscate.Decode(baked)
	}
	return fallback
}

// LMURL is the base URL for the LM provider.
func LMURL() string { return pick("CB_DATABRICK_URL", BakedLMURL, "") }

// APIKey is the static Bearer token.
func APIKey() string { return pick("CB_API_KEY", BakedAPIKey, "") }

// Model is the main chat model identifier.
func Model() string { return pick("CB_LM_MODEL", BakedModel, "default") }

// RewriteModel is the model used for query rewriting (falls back to Model).
func RewriteModel() string {
	v := pick("CB_REWRITE_MODEL", BakedRewriteModel, "")
	if v == "" {
		return Model()
	}
	return v
}

// ClientID is the OAuth M2M client ID (Databricks).
func ClientID() string { return pick("CB_CLIENT_ID", BakedClientID, "") }

// ClientSecret is the OAuth M2M client secret (Databricks).
func ClientSecret() string { return pick("CB_CLIENT_SECRET", BakedClientSecret, "") }

// ChatPath is the chat completions endpoint path.
func ChatPath() string { return pick("CB_CHAT_PATH", BakedChatPath, "/v1/chat/completions") }

// EmbedModel is the embedding model name.
func EmbedModel() string { return pick("CB_EMBEDDING_MODEL", BakedEmbedModel, "default") }

// EmbedURL is the full embedding endpoint URL (overrides base URL + path).
func EmbedURL() string { return pick("CB_EMBEDDING_URL", BakedEmbedURL, "") }

// EmbedPath is the embedding endpoint path.
func EmbedPath() string { return pick("CB_EMBEDDING_PATH", BakedEmbedPath, "/v1/embeddings") }
