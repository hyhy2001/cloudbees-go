package ask

import (
	"strings"

	"bee/internal/config"
)

func lmURL() string    { return config.LMURL() }
func apiKey() string   { return config.APIKey() }
func lmModel() string  { return config.Model() }
func rewriteModel() string { return config.RewriteModel() }
func clientID() string { return config.ClientID() }
func clientSecret() string { return config.ClientSecret() }
func chatPath() string { return config.ChatPath() }

// chatEndpoint builds the full chat completions URL.
func chatEndpoint() string {
	base := lmURL()
	if base == "" {
		return ""
	}
	return joinURL(base, chatPath())
}

// joinURL joins base + path, collapsing a duplicated leading path segment.
// e.g. base="https://host/v1" path="/v1/chat/completions" → ".../v1/chat/completions"
func joinURL(base, path string) string {
	b := strings.TrimRight(base, "/")
	segs := strings.SplitN(strings.TrimLeft(path, "/"), "/", 2)
	firstSeg := segs[0]
	if firstSeg != "" && strings.HasSuffix(b, "/"+firstSeg) {
		return b[:len(b)-len(firstSeg)-1] + path
	}
	return b + path
}

// isDatabricksHost returns true when the URL looks like a Databricks workspace.
func isDatabricksHost(u string) bool {
	low := strings.ToLower(u)
	return strings.Contains(low, "databricks") || strings.Contains(low, "cloud.databricks")
}
