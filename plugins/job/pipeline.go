// Package job — pipeline script helpers: file/inline resolution, parameter
// auto-detection, node injection, and Jenkins-side validation.
package job

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"bee/internal/api"
)

// ResolveScript reads --script as a file path when it exists on disk,
// otherwise treats the value as an inline Groovy script.
func ResolveScript(script string) (string, error) {
	if info, err := os.Stat(script); err == nil && !info.IsDir() {
		b, err := os.ReadFile(script)
		if err != nil {
			return "", err
		}
		script = string(b)
	}
	if strings.TrimSpace(script) == "" {
		return "", fmt.Errorf("pipeline script is empty")
	}
	return script, nil
}

var (
	parametersBlockRe = regexp.MustCompile(`(?s)parameters\s*\{(.*?)\n\s*\}`)
	paramCallRe       = regexp.MustCompile(`(?s)(string|booleanParam|choice|text|password)\s*\(([^)]*)\)`)
	choicesListRe     = regexp.MustCompile(`choices\s*:\s*\[([^\]]*)\]`)
)

func extractKV(args, key string) string {
	if m := regexp.MustCompile(key + `\s*:\s*'([^']*)'`).FindStringSubmatch(args); m != nil {
		return m[1]
	}
	if m := regexp.MustCompile(key + `\s*:\s*"([^"]*)"`).FindStringSubmatch(args); m != nil {
		return m[1]
	}
	return ""
}

func firstChoice(args string) string {
	m := choicesListRe.FindStringSubmatch(args)
	if m == nil {
		return ""
	}
	parts := strings.Split(m[1], ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(parts[0]), `'"`)
}

// parseParametersFromScript scans a Jenkinsfile's `parameters { ... }` block
// and returns "NAME=default" entries for string/booleanParam/choice/text/password.
func parseParametersFromScript(script string) []string {
	m := parametersBlockRe.FindStringSubmatch(script)
	if m == nil {
		return nil
	}
	var defs []string
	for _, pm := range paramCallRe.FindAllStringSubmatch(m[1], -1) {
		kind, args := pm[1], pm[2]
		name := extractKV(args, "name")
		if name == "" {
			continue
		}
		var def string
		if kind == "choice" {
			def = firstChoice(args)
		} else {
			def = extractKV(args, "defaultValue")
		}
		defs = append(defs, name+"="+def)
	}
	return defs
}

// mergeParamDefs merges auto-detected parameter defs with explicit CLI
// --param-def values. CLI values override auto-detected defaults by name;
// CLI-only names are appended in the order given.
func mergeParamDefs(autoDetected, cli []string) []string {
	var order []string
	values := map[string]string{}
	seen := map[string]bool{}
	for _, pd := range autoDetected {
		name, def, _ := strings.Cut(pd, "=")
		name = strings.TrimSpace(name)
		order = append(order, name)
		values[name] = def
		seen[name] = true
	}
	for _, pd := range cli {
		name, def, _ := strings.Cut(pd, "=")
		name = strings.TrimSpace(name)
		if !seen[name] {
			order = append(order, name)
			seen[name] = true
		}
		values[name] = def
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		if values[name] != "" {
			out = append(out, name+"="+values[name])
		} else {
			out = append(out, name)
		}
	}
	return out
}

var (
	agentBlockStartRe = regexp.MustCompile(`agent\s*\{`)
	agentAnyNoneRe    = regexp.MustCompile(`agent\s+(any|none)\b`)
	pipelineOpenRe    = regexp.MustCompile(`pipeline\s*\{`)
)

// injectAgent replaces an existing `agent { ... }`/`agent any`/`agent none`
// block with `agent { label 'node' }`, or inserts it right after `pipeline {`
// when the script has no agent directive at all. Brace-counting handles a
// nested block inside `agent { ... }` (e.g. docker { ... }).
func injectAgent(script, node string) string {
	if node == "" {
		return script
	}
	replacement := `agent { label '` + node + `' }`

	if loc := agentBlockStartRe.FindStringIndex(script); loc != nil {
		depth := 0
		for i := loc[1] - 1; i < len(script); i++ {
			switch script[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return script[:loc[0]] + replacement + script[i+1:]
				}
			}
		}
		return script // unbalanced braces — leave untouched
	}
	if loc := agentAnyNoneRe.FindStringIndex(script); loc != nil {
		return script[:loc[0]] + replacement + script[loc[1]:]
	}
	if loc := pipelineOpenRe.FindStringIndex(script); loc != nil {
		return script[:loc[1]] + "\n    " + replacement + script[loc[1]:]
	}
	return script
}

// ValidatePipelineScript posts the raw (pre-agent-injection) script to
// Jenkins' pipeline-model-converter validation endpoint. Fails open — a
// network error, a 404 (plugin not installed), or an unparseable response
// never blocks the command, only an explicit validation failure does.
func ValidatePipelineScript(ctx context.Context, client *api.Client, script string) error {
	body := "jenkinsfile=" + url.QueryEscape(script)
	resp, err := client.Do(ctx, "POST", "/pipeline-model-converter/validate", strings.NewReader(body), "application/x-www-form-urlencoded")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var result struct {
		Result string   `json:"result"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		return nil
	}
	if result.Result == "success" || result.Result == "" {
		return nil
	}
	if len(result.Errors) > 0 {
		return fmt.Errorf("pipeline validation failed: %s", strings.Join(result.Errors, "; "))
	}
	return fmt.Errorf("pipeline validation failed")
}
