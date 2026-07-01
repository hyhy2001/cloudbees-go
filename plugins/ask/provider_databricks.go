package ask

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

)

// DatabricksProvider authenticates via OAuth M2M and calls the Databricks AI Gateway.
type DatabricksProvider struct {
	host         string
	clientID     string
	clientSecret string
	model        string
	chatEndpoint string

	mu          sync.Mutex
	cachedToken string
	expiresAt   time.Time

	tokenEndpoint string
}

// NewDatabricks creates a Databricks OAuth M2M provider.
func NewDatabricks(host, clientID, clientSecret, model, chatEndpoint string) *DatabricksProvider {
	return &DatabricksProvider{
		host:         strings.TrimRight(host, "/"),
		clientID:     clientID,
		clientSecret: clientSecret,
		model:        model,
		chatEndpoint: chatEndpoint,
	}
}

func (p *DatabricksProvider) Name() string { return "databricks-oauth" }

func (p *DatabricksProvider) getToken() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cachedToken != "" && time.Now().Before(p.expiresAt) {
		return p.cachedToken, nil
	}
	return p.fetchToken()
}

// Validate attempts to get a token and returns whether it succeeds.
func (p *DatabricksProvider) Validate() bool {
	_, err := p.getToken()
	return err == nil
}

const azureAppID = "2ff814a6-3304-4ab8-85cb-cd0e6f879c1d"

func (p *DatabricksProvider) fetchToken() (string, error) {
	if p.tokenEndpoint == "" {
		if err := p.discoverTokenEndpoint(); err != nil {
			return "", err
		}
	}
	// Strategy 1: basic auth OIDC
	if tok, err := p.basicAuthExchange(p.tokenEndpoint); err == nil {
		return tok, nil
	}

	// Strategy 2: Azure AD
	tenants, _ := p.discoverAzureTenant()
	for _, tenant := range append(tenants, "common") {
		if tenant == "" {
			continue
		}
		// v2 scope
		form := url.Values{
			"grant_type":    {"client_credentials"},
			"client_id":     {p.clientID},
			"client_secret": {p.clientSecret},
			"scope":         {azureAppID + "/.default"},
		}
		if tok, err := p.postToken(fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenant), form); err == nil {
			return tok, nil
		}
		// v1 resource
		form2 := url.Values{
			"grant_type":    {"client_credentials"},
			"client_id":     {p.clientID},
			"client_secret": {p.clientSecret},
			"resource":      {azureAppID},
		}
		if tok, err := p.postToken(fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/token", tenant), form2); err == nil {
			return tok, nil
		}
	}
	return "", fmt.Errorf("token exchange failed — check credentials")
}

func (p *DatabricksProvider) basicAuthExchange(tokenURL string) (string, error) {
	basic := base64.StdEncoding.EncodeToString([]byte(p.clientID + ":" + p.clientSecret))
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {"all-apis"}}
	hreq, _ := http.NewRequest("POST", tokenURL, strings.NewReader(form.Encode()))
	hreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hreq.Header.Set("Authorization", "Basic "+basic)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(hreq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return p.parseTokenResp(resp)
}

func (p *DatabricksProvider) postToken(tokenURL string, form url.Values) (string, error) {
	hreq, _ := http.NewRequest("POST", tokenURL, strings.NewReader(form.Encode()))
	hreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(hreq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return p.parseTokenResp(resp)
}

func (p *DatabricksProvider) parseTokenResp(resp *http.Response) (string, error) {
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("token HTTP %d: %s", resp.StatusCode, string(raw[:min(200, len(raw))]))
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.AccessToken == "" {
		return "", fmt.Errorf("no access_token in response")
	}
	expiresIn := payload.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 3600
	}
	p.cachedToken = payload.AccessToken
	p.expiresAt = time.Now().Add(time.Duration(expiresIn-30) * time.Second)
	return payload.AccessToken, nil
}

func (p *DatabricksProvider) discoverTokenEndpoint() error {
	for _, path := range []string{"/.well-known/databricks-config", "/oidc/.well-known/oauth-authorization-server"} {
		u := p.host + path
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(u)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(path, "databricks-config") {
			var meta struct{ OIDCEndpoint string `json:"oidc_endpoint"` }
			if json.Unmarshal(raw, &meta) == nil && meta.OIDCEndpoint != "" {
				resp2, err2 := (&http.Client{Timeout: 5 * time.Second}).Get(meta.OIDCEndpoint)
				if err2 == nil && resp2.StatusCode == 200 {
					raw2, _ := io.ReadAll(resp2.Body)
					resp2.Body.Close()
					var oidc struct{ TokenEndpoint string `json:"token_endpoint"` }
					if json.Unmarshal(raw2, &oidc) == nil && oidc.TokenEndpoint != "" {
						p.tokenEndpoint = oidc.TokenEndpoint
						return nil
					}
				}
			}
		} else {
			var oidc struct{ TokenEndpoint string `json:"token_endpoint"` }
			if json.Unmarshal(raw, &oidc) == nil && oidc.TokenEndpoint != "" {
				p.tokenEndpoint = oidc.TokenEndpoint
				return nil
			}
		}
	}
	return fmt.Errorf("OIDC discovery failed — verify workspace URL")
}

func (p *DatabricksProvider) discoverAzureTenant() ([]string, error) {
	for _, path := range []string{"/aad/auth", "/oidc/v1/authorize"} {
		client := &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.Get(p.host + path)
		if err != nil {
			continue
		}
		location := resp.Header.Get("Location")
		resp.Body.Close()
		re := regexp.MustCompile(`login\.microsoftonline\.com/([^/?]+)`)
		m := re.FindStringSubmatch(location)
		if len(m) > 1 {
			return []string{m[1]}, nil
		}
	}
	return nil, nil
}

// extractContent handles both string and Databricks reasoning-model array content.
func extractContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		for i := len(v) - 1; i >= 0; i-- {
			block, ok := v[i].(map[string]interface{})
			if !ok {
				continue
			}
			if block["type"] == "text" {
				if t, ok := block["text"].(string); ok {
					return t
				}
			}
		}
		for _, item := range v {
			block, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if block["type"] == "reasoning" {
				if summ, ok := block["summary"].([]interface{}); ok {
					var parts []string
					for _, s := range summ {
						sm, _ := s.(map[string]interface{})
						if sm["type"] == "summary_text" {
							if t, ok := sm["text"].(string); ok {
								parts = append(parts, t)
							}
						}
					}
					if len(parts) > 0 {
						return strings.Join(parts, " ")
					}
				}
			}
		}
	}
	return ""
}

func (p *DatabricksProvider) chatCall(prompt string, maxTokens int) (string, TokenUsage, error) {
	token, err := p.getToken()
	if err != nil {
		return "", TokenUsage{}, err
	}
	reqBody := map[string]interface{}{
		"model": p.model,
		"messages": []map[string]interface{}{
			{"role": "system", "content": SYSTEM_PROMPT},
			{"role": "user", "content": prompt},
		},
		"max_tokens":  maxTokens,
		"temperature": 0,
	}
	body, _ := json.Marshal(reqBody)
	hreq, _ := http.NewRequest("POST", p.chatEndpoint, bytes.NewReader(body))
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(hreq)
	if err != nil {
		return "", TokenUsage{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", TokenUsage{}, fmt.Errorf("Databricks LM HTTP %d: %s", resp.StatusCode, string(raw[:min(200, len(raw))]))
	}
	var cr struct {
		Choices []struct {
			Message *struct {
				Content          interface{} `json:"content"`
				ReasoningContent interface{} `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", TokenUsage{}, fmt.Errorf("non-JSON response: %s", string(raw[:min(200, len(raw))]))
	}
	var text string
	if len(cr.Choices) > 0 && cr.Choices[0].Message != nil {
		m := cr.Choices[0].Message
		content := m.Content
		if content == nil {
			content = m.ReasoningContent
		}
		text = extractContent(content)
	}
	text = thinkRe.ReplaceAllString(strings.TrimSpace(text), "")
	var usage TokenUsage
	if cr.Usage != nil {
		usage.PromptTokens = cr.Usage.PromptTokens
		usage.CompletionTokens = cr.Usage.CompletionTokens
	}
	return strings.TrimSpace(text), usage, nil
}


func (p *DatabricksProvider) Generate(prompt string, maxTokens int) (string, error) {
	text, _, err := p.chatCall(prompt, maxTokens)
	return text, err
}

func (p *DatabricksProvider) GenerateWithUsage(prompt string, maxTokens int) (string, TokenUsage, error) {
	return p.chatCall(prompt, maxTokens)
}

func (p *DatabricksProvider) GenerateJSON(prompt string) (*LMAnswer, TokenUsage, error) {
	token, err := p.getToken()
	if err != nil {
		return nil, TokenUsage{}, err
	}
	reqBody := map[string]interface{}{
		"model": p.model,
		"messages": []map[string]interface{}{
			{"role": "system", "content": SYSTEM_PROMPT},
			{"role": "user", "content": prompt + "\n\nRespond with JSON only."},
		},
		"max_tokens":  2048,
		"temperature": 0,
		"stream":      true,
	}
	body, _ := json.Marshal(reqBody)
	hreq, _ := http.NewRequest("POST", p.chatEndpoint, bytes.NewReader(body))
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, TokenUsage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, TokenUsage{}, fmt.Errorf("Databricks LM HTTP %d", resp.StatusCode)
	}

	var chunks []string
	var usage TokenUsage
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
					Content          interface{} `json:"content"`
					ReasoningContent interface{} `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(line[6:]), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
			d := chunk.Choices[0].Delta
			content := d.Content
			if content == nil {
				content = d.ReasoningContent
			}
			if t := extractContent(content); t != "" {
				chunks = append(chunks, t)
			}
		}
		if chunk.Usage != nil {
			usage.PromptTokens = chunk.Usage.PromptTokens
			usage.CompletionTokens = chunk.Usage.CompletionTokens
		}
	}

	content := thinkRe.ReplaceAllString(strings.TrimSpace(strings.Join(chunks, "")), "")
	if content == "" {
		return nil, usage, fmt.Errorf("empty response")
	}
	start := strings.Index(content, "{")
	if start == -1 {
		return nil, usage, fmt.Errorf("no JSON in response")
	}
	var ans LMAnswer
	if err := json.Unmarshal([]byte(content[start:]), &ans); err != nil {
		return nil, usage, fmt.Errorf("JSON parse: %w", err)
	}
	if ans.Explanation == "" {
		return nil, usage, fmt.Errorf("missing explanation")
	}
	return &ans, usage, nil
}

func (p *DatabricksProvider) Stream(prompt string, write func(string)) error {
	token, err := p.getToken()
	if err != nil {
		return err
	}
	reqBody := map[string]interface{}{
		"model": p.model,
		"messages": []map[string]interface{}{
			{"role": "system", "content": SYSTEM_PROMPT},
			{"role": "user", "content": prompt},
		},
		"max_tokens":  8192,
		"temperature": 0,
		"stream":      true,
	}
	body, _ := json.Marshal(reqBody)
	hreq, _ := http.NewRequest("POST", p.chatEndpoint, bytes.NewReader(body))
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Databricks LM HTTP %d", resp.StatusCode)
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
					Content          interface{} `json:"content"`
					ReasoningContent interface{} `json:"reasoning_content"`
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
		content := d.Content
		if content == nil {
			content = d.ReasoningContent
		}
		if t := extractContent(content); t != "" {
			write(t)
		}
	}
	return scanner.Err()
}
