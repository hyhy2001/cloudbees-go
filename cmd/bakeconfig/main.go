// bakeconfig reads LM_* env vars, XOR-obfuscates each non-empty value, and
// prints the -ldflags -X assignments for internal/config Baked* vars.
// Used by `make build` so plaintext credentials never reach the linker
// command line or a Makefile variable — only the encoded form does.
package main

import (
	"crypto/rand"
	"fmt"
	"os"

	"bee/internal/obfuscate"
)

func encode(v string) string {
	if v == "" {
		return ""
	}
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		fmt.Fprintf(os.Stderr, "bakeconfig: %v\n", err)
		os.Exit(1)
	}
	return obfuscate.Encode(v, key)
}

func main() {
	vars := []struct{ env, baked string }{
		{"LM_URL", "BakedLMURL"},
		{"LM_API_KEY", "BakedAPIKey"},
		{"LM_MODEL", "BakedModel"},
		{"LM_REWRITE", "BakedRewriteModel"},
		{"LM_CLIENT_ID", "BakedClientID"},
		{"LM_CLIENT_SECRET", "BakedClientSecret"},
		{"LM_CHAT_PATH", "BakedChatPath"},
		{"LM_EMBED_MODEL", "BakedEmbedModel"},
		{"LM_EMBED_URL", "BakedEmbedURL"},
		{"LM_EMBED_PATH", "BakedEmbedPath"},
	}
	for _, v := range vars {
		fmt.Printf("-X 'bee/internal/config.%s=%s' ", v.baked, encode(os.Getenv(v.env)))
	}
}
