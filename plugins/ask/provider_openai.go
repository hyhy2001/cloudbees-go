// Package providers implements LMProvider backends for bee ask.
package ask

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"strings"
	"time"
)

// OpenAIProvider calls any OpenAI-compatible chat completions endpoint.
type OpenAIProvider struct {
	endpoint string
	apiKey   string
	model    string
}

// NewOpenAI creates a new OpenAI-compatible provider.
func NewOpenAI(endpoint, apiKey, model string) *OpenAIProvider {
	return &OpenAIProvider{endpoint: endpoint, apiKey: apiKey, model: model}
}

// Name derives a display label from the endpoint host.
func (p *OpenAIProvider) Name() string {
	parts := strings.Split(p.endpoint, "/")
	for _, part := range parts {
		if strings.Contains(part, ".") {
			if strings.Contains(part, "localhost") || part == "127.0.0.1" {
				return "local-lm"
			}
			return part
		}
	}
	return "lm"
}

func (p *OpenAIProvider) headers() http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		h.Set("Authorization", "Bearer "+p.apiKey)
	}
	if u := localUsername(); u != "" {
		h.Set("X-Bee-User", u)
	}
	return h
}

// localUsername identifies the caller for llm-gateway's per-user usage
// tracking. Falls back through $USER before os/user, since os/user can fail
// in minimal/container environments without nsswitch entries.
func localUsername() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if cu, err := user.Current(); err == nil {
		return cu.Username
	}
	return ""
}

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReq struct {
	Model          string    `json:"model"`
	Messages       []chatMsg `json:"messages"`
	Temperature    float64   `json:"temperature"`
	MaxTokens      int       `json:"max_tokens"`
	EnableThinking bool      `json:"enable_thinking"`
	Stream         bool      `json:"stream,omitempty"`
	ResponseFormat *rfmt     `json:"response_format,omitempty"`
}

type rfmt struct {
	Type string `json:"type"`
}

type chatResp struct {
	Choices []struct {
		Message *struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
		Delta *struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (p *OpenAIProvider) do(req chatReq) (*chatResp, error) {
	body, _ := json.Marshal(req)
	hreq, _ := http.NewRequest("POST", p.endpoint, bytes.NewReader(body))
	for k, vs := range p.headers() {
		for _, v := range vs {
			hreq.Header.Set(k, v)
		}
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LM HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw[:min(300, len(raw))])))
	}
	var cr chatResp
	if err := json.Unmarshal(bytes.TrimSpace(raw), &cr); err != nil {
		return nil, fmt.Errorf("LM non-JSON: %s", string(raw[:min(300, len(raw))]))
	}
	return &cr, nil
}

func msgContent(cr *chatResp) string {
	if len(cr.Choices) == 0 || cr.Choices[0].Message == nil {
		return ""
	}
	m := cr.Choices[0].Message
	if m.Content != "" {
		return m.Content
	}
	return m.ReasoningContent
}

func (p *OpenAIProvider) Generate(prompt string, maxTokens int) (string, error) {
	cr, err := p.do(chatReq{
		Model:       p.model,
		Messages:    []chatMsg{{Role: "system", Content: SYSTEM_PROMPT}, {Role: "user", Content: prompt}},
		Temperature: 0, MaxTokens: maxTokens,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(msgContent(cr)), nil
}

func (p *OpenAIProvider) GenerateWithUsage(prompt string, maxTokens int) (string, TokenUsage, error) {
	cr, err := p.do(chatReq{
		Model:       p.model,
		Messages:    []chatMsg{{Role: "system", Content: SYSTEM_PROMPT}, {Role: "user", Content: prompt}},
		Temperature: 0, MaxTokens: maxTokens,
	})
	if err != nil {
		return "", TokenUsage{}, err
	}
	usage := TokenUsage{}
	if cr.Usage != nil {
		usage.PromptTokens = cr.Usage.PromptTokens
		usage.CompletionTokens = cr.Usage.CompletionTokens
	}
	return strings.TrimSpace(msgContent(cr)), usage, nil
}

func (p *OpenAIProvider) GenerateJSON(prompt string) (*LMAnswer, TokenUsage, error) {
	req := chatReq{
		Model: p.model,
		Messages: []chatMsg{
			{Role: "system", Content: SYSTEM_PROMPT},
			{Role: "user", Content: prompt + "\n\nRespond with JSON only."},
		},
		Temperature:    0,
		MaxTokens:      2048,
		ResponseFormat: &rfmt{Type: "json_object"},
	}

	body, _ := json.Marshal(req)
	hreq, _ := http.NewRequest("POST", p.endpoint, bytes.NewReader(body))
	for k, vs := range p.headers() {
		for _, v := range vs {
			hreq.Header.Set(k, v)
		}
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, TokenUsage{}, err
	}
	defer resp.Body.Close()

	var content string
	var usage TokenUsage

	if resp.StatusCode == 400 || resp.StatusCode == 422 || resp.StatusCode == 500 {
		resp.Body.Close()
		// Fallback: stream without response_format
		var sb strings.Builder
		_ = p.Stream(prompt+"\n\nRespond with JSON only.", func(chunk string) { sb.WriteString(chunk) })
		content = sb.String()
	} else if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, TokenUsage{}, fmt.Errorf("LM HTTP %d: %s", resp.StatusCode, string(raw[:min(300, len(raw))]))
	} else {
		raw, _ := io.ReadAll(resp.Body)
		raw = bytes.TrimSpace(raw)
		// strip trailing "data: [DONE]" that some servers append
		raw = bytes.TrimRight(bytes.TrimSpace(raw), "data: [DONE]")
		raw = bytes.TrimSpace(raw)
		var cr chatResp
		if err := json.Unmarshal(raw, &cr); err != nil {
			return nil, TokenUsage{}, fmt.Errorf("LM non-JSON: %s", string(raw[:min(300, len(raw))]))
		}
		content = msgContent(&cr)
		if cr.Usage != nil {
			usage.PromptTokens = cr.Usage.PromptTokens
			usage.CompletionTokens = cr.Usage.CompletionTokens
		}
	}

	content = StripThinkBlock(content)
	if content == "" {
		return nil, usage, fmt.Errorf("empty LM response")
	}
	start := strings.Index(content, "{")
	if start == -1 {
		return nil, usage, fmt.Errorf("no JSON in response")
	}
	var ans LMAnswer
	if err := json.Unmarshal([]byte(content[start:]), &ans); err != nil {
		return nil, usage, fmt.Errorf("JSON parse error: %w", err)
	}
	if ans.Explanation == "" {
		return nil, usage, fmt.Errorf("missing explanation field")
	}
	return &ans, usage, nil
}

func (p *OpenAIProvider) Stream(prompt string, write func(string)) error {
	req := chatReq{
		Model: p.model,
		Messages: []chatMsg{
			{Role: "system", Content: SYSTEM_PROMPT},
			{Role: "user", Content: prompt},
		},
		Temperature: 0,
		MaxTokens:   8192,
		Stream:      true,
	}
	body, _ := json.Marshal(req)
	hreq, _ := http.NewRequest("POST", p.endpoint, bytes.NewReader(body))
	for k, vs := range p.headers() {
		for _, v := range vs {
			hreq.Header.Set(k, v)
		}
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LM HTTP %d: %s", resp.StatusCode, string(raw[:min(300, len(raw))]))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "data: [DONE]" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta *struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(line[6:]), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 || chunk.Choices[0].Delta == nil {
			continue
		}
		d := chunk.Choices[0].Delta
		text := d.Content
		if text == "" {
			text = d.ReasoningContent
		}
		if text != "" {
			write(text)
		}
	}
	return scanner.Err()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
