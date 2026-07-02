// genembeddings pre-computes neural embeddings for the bee-ask corpus using
// the configured API embedding endpoint (LM_EMBED_URL / LM_URL+LM_EMBED_PATH),
// quantizes them to int16, and writes plugins/ask/embeddings_gen.go.
//
// The generated file is committed and baked into the binary — no embedding
// model call is needed at runtime unless the operator wants live re-query
// vector search (which reuses the same endpoint via plugins/ask/vector.go).
//
// Run: go run ./cmd/genembeddings   (same LM_* env vars as `make build`)
package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"bee/plugins/ask"
	"bee/plugins/auth"
	"bee/plugins/controller"
	"bee/plugins/cred"
	"bee/plugins/job"
	"bee/plugins/node"

	_ "modernc.org/sqlite"
)

const scale = 10000

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func postEmbed(endpoint, bearer, model, text string) ([]float64, int, error) {
	body, _ := json.Marshal(map[string]string{"input": text, "model": model})
	req, _ := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, resp.StatusCode, fmt.Errorf("embedding API returned %d at %s", resp.StatusCode, endpoint)
	}
	var payload struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, resp.StatusCode, err
	}
	if len(payload.Data) == 0 {
		return nil, resp.StatusCode, fmt.Errorf("no embedding data in response")
	}
	return payload.Data[0].Embedding, resp.StatusCode, nil
}

func oidcToken(baseURL, clientID, clientSecret string) (string, error) {
	tokenURL := strings.TrimRight(baseURL, "/") + "/oidc/v1/token"
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"scope":         {"all-apis"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	req, _ := http.NewRequest("POST", tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("oidc token HTTP %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.AccessToken == "" {
		return "", fmt.Errorf("no access_token in response")
	}
	return payload.AccessToken, nil
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "genembeddings: "+format+"\n", a...)
	os.Exit(1)
}

func main() {
	baseURL := env("LM_URL", "")
	apiKey := env("LM_API_KEY", "")
	model := env("LM_EMBED_MODEL", "default")
	clientID := env("LM_CLIENT_ID", "")
	clientSecret := env("LM_CLIENT_SECRET", "")
	embedPath := env("LM_EMBED_PATH", "/v1/embeddings")
	embedURLOverride := env("LM_EMBED_URL", "")

	apiURL := embedURLOverride
	if apiURL == "" && baseURL != "" {
		apiURL = strings.TrimRight(baseURL, "/") + embedPath
	}
	if apiURL == "" {
		fail("no embedding API configured — set LM_EMBED_URL or LM_URL in bee.yaml/env")
	}

	bearer := apiKey
	if bearer == "" && clientID != "" && clientSecret != "" && baseURL != "" {
		if tok, err := oidcToken(baseURL, clientID, clientSecret); err == nil {
			bearer = tok
		}
	}

	urlCandidates := []string{apiURL}
	if strings.HasPrefix(embedPath, "/ai-gateway/") && baseURL != "" {
		urlCandidates = append(urlCandidates,
			strings.TrimRight(baseURL, "/")+"/v1/embeddings",
			strings.TrimRight(baseURL, "/")+"/serving-endpoints/"+url.PathEscape(model)+"/invocations",
		)
	}

	embed := func(text string) []float64 {
		for _, u := range urlCandidates {
			vec, status, err := postEmbed(u, bearer, model, text)
			if err == nil {
				return vec
			}
			if status != 404 {
				fail("%v", err)
			}
			fmt.Fprintf(os.Stderr, "  embedding API 404 at %s — trying next candidate\n", u)
		}
		fail("embedding API returned 404 for all candidates")
		return nil
	}

	fmt.Println("Building corpus...")
	memDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		fail("open in-memory db: %v", err)
	}
	root := &cobra.Command{Use: "bee"}
	auth.Register(root, memDB, ":memory:")
	controller.Register(root, memDB, ":memory:")
	cred.Register(root, memDB, ":memory:")
	node.Register(root, memDB, ":memory:")
	job.Register(root, memDB, ":memory:")
	ask.Register(root, memDB, ":memory:")
	corpus := ask.BuildCorpus(root)
	if len(corpus) == 0 {
		fail("empty corpus")
	}

	fmt.Println("Using API embedding...")
	first := embed(corpus[0].Title)
	dim := len(first)

	ids := make([]string, 0, len(corpus))
	values := make([]float64, 0, len(corpus)*dim)
	for i, item := range corpus {
		text := strings.Join(nonEmpty(item.Title, item.Description, item.Body), " ")
		if len(text) > 512 {
			text = text[:512] // ponytail: byte-truncated like TS's UTF-16 .slice(0,512); add rune-safety if non-ASCII docs grow
		}
		vec := embed(text)
		ids = append(ids, item.ID)
		values = append(values, vec...)
		if (i+1)%20 == 0 {
			fmt.Printf("  %d/%d\n", i+1, len(corpus))
		}
	}

	quantized := make([]int16, len(values))
	for i, v := range values {
		q := int64(v*scale + sign(v)*0.5) // round half away from zero
		if q > 32767 {
			q = 32767
		}
		if q < -32768 {
			q = -32768
		}
		quantized[i] = int16(q)
	}

	buf := make([]byte, len(quantized)*2)
	for i, v := range quantized {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
	}
	b64 := base64.StdEncoding.EncodeToString(buf)

	writeGenFile(dim, model, ids, b64)
	fmt.Printf("\n%d x %d (%d bytes) -> plugins/ask/embeddings_gen.go\n", len(ids), dim, len(buf))
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

func nonEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func writeGenFile(dim int, model string, ids []string, b64 string) {
	var sb strings.Builder
	sb.WriteString("// Code generated by cmd/genembeddings — DO NOT EDIT.\n")
	sb.WriteString(fmt.Sprintf("// Model: %s (%d-dim).\n", model, dim))
	sb.WriteString("package ask\n\n")
	sb.WriteString(fmt.Sprintf("const DIM = %d\n", dim))
	sb.WriteString(fmt.Sprintf("const SCALE = %d\n", scale))
	sb.WriteString(fmt.Sprintf("const CORPUS_MODEL = %s\n\n", strconv.Quote(model)))
	sb.WriteString("var VEC_IDS = []string{\n")
	for _, id := range ids {
		sb.WriteString("\t" + strconv.Quote(id) + ",\n")
	}
	sb.WriteString("}\n\n")
	sb.WriteString("const VEC_B64 = " + strconv.Quote(b64) + "\n")

	if err := os.WriteFile("plugins/ask/embeddings_gen.go", []byte(sb.String()), 0o644); err != nil {
		fail("write embeddings_gen.go: %v", err)
	}
}
