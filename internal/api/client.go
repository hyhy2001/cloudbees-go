// Package api provides the Jenkins HTTP client.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client is a Jenkins REST API client.
type Client struct {
	BaseURL    string
	BasicToken string // base64(user:token)
	HTTPClient *http.Client
	mu         sync.Mutex
	crumb      string
	crumbField string
	crumbAt    time.Time
}

// New creates a Client for the given base URL and basic auth token.
func New(baseURL, basicToken string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		BasicToken: basicToken,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// crumbValid returns true if the cached crumb is less than 5 minutes old.
func (c *Client) crumbValid() bool {
	return c.crumb != "" && time.Since(c.crumbAt) < 5*time.Minute
}

// fetchCrumb fetches a CSRF crumb from Jenkins.
func (c *Client) fetchCrumb(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/crumbIssuer/api/json", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Basic "+c.BasicToken)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// CSRF protection disabled
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("crumb fetch: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Crumb            string `json:"crumb"`
		CrumbRequestField string `json:"crumbRequestField"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	c.crumb = body.Crumb
	c.crumbField = body.CrumbRequestField
	c.crumbAt = time.Now()
	return nil
}

// Do executes an HTTP request with auth + crumb injection.
// Retries once on 403 (crumb expiry).
func (c *Client) Do(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	return c.do(ctx, method, path, body, contentType, true)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string, retry bool) (*http.Response, error) {
	if method != http.MethodGet {
		c.mu.Lock()
		if !c.crumbValid() {
			if err := c.fetchCrumb(ctx); err != nil {
				c.mu.Unlock()
				return nil, fmt.Errorf("crumb: %w", err)
			}
		}
		c.mu.Unlock()
	}

	url := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Basic "+c.BasicToken)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.mu.Lock()
	if c.crumb != "" {
		req.Header.Set(c.crumbField, c.crumb)
	}
	c.mu.Unlock()

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	// Retry once on 403 — crumb may have expired
	if resp.StatusCode == http.StatusForbidden && retry {
		resp.Body.Close()
		c.mu.Lock()
		c.crumb = ""
		c.mu.Unlock()
		return c.do(ctx, method, path, body, contentType, false)
	}
	return resp, nil
}

// GetJSON sends a GET request and decodes the JSON response into v.
func (c *Client) GetJSON(ctx context.Context, path string, v any) error {
	resp, err := c.Do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// PostJSON sends a POST with a JSON body and decodes the response.
func (c *Client) PostJSON(ctx context.Context, path string, payload any, v any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := c.Do(ctx, http.MethodPost, path, strings.NewReader(string(data)), "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if v != nil {
		return json.NewDecoder(resp.Body).Decode(v)
	}
	return nil
}

// PostXML sends a POST with an XML body.
func (c *Client) PostXML(ctx context.Context, path, xmlBody string) error {
	resp, err := c.Do(ctx, http.MethodPost, path, strings.NewReader(xmlBody), "application/xml")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// PostFormGetLocation sends a POST with form body and returns the Location header
// from the response (without following the redirect). Used for Jenkins endpoints
// that 302-redirect to the newly created resource URL.
func (c *Client) PostFormGetLocation(ctx context.Context, path string, params map[string]string) (string, error) {
	var sb strings.Builder
	first := true
	for k, v := range params {
		if !first {
			sb.WriteByte('&')
		}
		sb.WriteString(k + "=" + v)
		first = false
	}

	// Use a client that does NOT follow redirects so we can read Location header.
	noRedirect := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, strings.NewReader(sb.String()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Basic "+c.BasicToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.mu.Lock()
	if !c.crumbValid() {
		if e := c.fetchCrumb(ctx); e != nil {
			c.mu.Unlock()
			return "", fmt.Errorf("crumb: %w", e)
		}
	}
	if c.crumb != "" {
		req.Header.Set(c.crumbField, c.crumb)
	}
	c.mu.Unlock()

	resp, err := noRedirect.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" && resp.StatusCode >= 400 {
		return "", fmt.Errorf("POST %s: HTTP %d", path, resp.StatusCode)
	}
	return loc, nil
}

// GetHTML fetches a page as raw HTML string.
func (c *Client) GetHTML(ctx context.Context, path string) (string, error) {
	resp, err := c.Do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: HTTP %d", path, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

// PostForm sends a POST with form-encoded body.
func (c *Client) PostForm(ctx context.Context, path string, params map[string]string) error {
	var sb strings.Builder
	for k, v := range params {
		if sb.Len() > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k + "=" + v)
	}
	resp, err := c.Do(ctx, http.MethodPost, path, strings.NewReader(sb.String()), "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
