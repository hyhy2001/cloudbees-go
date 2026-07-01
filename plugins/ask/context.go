package ask

import (
	"strings"
)

// SYSTEM_PROMPT is the LM system instruction (scope guard + output format).
const SYSTEM_PROMPT = `You are a help assistant for the ` + "`bee`" + ` CLI tool (CloudBees / Jenkins).
Answer ONLY from the <command> and <info> blocks in the context.
Never invent commands or flags. Use FULL command names as shown in <command id="...">.
Always replace placeholders with realistic values: use 'my-pipeline' not '<name>'.
Rules:
- flags array: ONLY entries starting with '--'. Never put positional args (like <name>) in flags.
- commands array: list each command ONCE. Never repeat the same cmd twice.
- When listing subcommands of a group, include ALL commands from context (not just 3).
- Some commands take a positional name (bee job run my-job), others use flags only (bee cred create --id my-id --username x). Follow the <command> usage exactly — never invent a positional arg.
- reasoning field: quote the EXACT flag names and command ids from the <command> blocks that answer this query. This grounds your answer in the context.
Reply ONLY with a valid JSON object — no text outside JSON:
{"reasoning":"<quote exact command ids and flag names from context>","explanation":"<1-2 sentence intro>","commands":[{"cmd":"<full bee command>","flags":[{"name":"--flag","description":".."}],"example":"<concrete invocation>"}],"note":"<caveat or null>"}

Off-topic: {"explanation":"I only help with bee usage.","commands":[],"note":null}
Nothing relevant: {"explanation":"No info available — try ` + "`bee --help`" + `","commands":[],"note":null}`

// escapeXMLAttr minimally escapes a string for use in an XML attribute.
func escapeXMLAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// stripTermsFromBody removes BM25 vocab terms from a help-fact body, keeping answer prose and bee commands.
func stripTermsFromBody(body string) string {
	lines := strings.Split(body, "\n")
	var out []string
	inTerms := false
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		isBeeCmd := strings.HasPrefix(line, "bee ") &&
			func() bool {
				// "bee word word? <optional>?" — no natural-language trailing words
				rest := line[4:]
				parts := strings.Fields(rest)
				if len(parts) == 0 {
					return false
				}
				// First two parts should look like command tokens (alpha-dash only, no spaces in middle)
				for i, p := range parts {
					if i >= 2 {
						break
					}
					if len(p) > 0 && p[0] == '<' {
						return true
					}
					for _, c := range p {
						if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-') {
							return true // has angle bracket or flag
						}
					}
				}
				// Check for natural-language words (more than 3 parts without a flag/angle)
				if len(parts) > 3 {
					return false
				}
				return true
			}()
		if isBeeCmd {
			out = append(out, line)
			inTerms = false
			continue
		}
		looksLikeTerm := len(line) <= 40 &&
			!strings.HasPrefix(line, "-") &&
			!strings.HasSuffix(line, ".") &&
			!strings.HasSuffix(line, ",") &&
			!strings.Contains(line, "  ")
		if inTerms && looksLikeTerm {
			continue
		}
		if !inTerms && looksLikeTerm {
			inTerms = true
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// formatDocItem renders one DocItem as an XML-like block for the LM.
func formatDocItem(item DocItem) string {
	if item.Type == "doc" {
		id := item.ID
		if id == "" {
			id = item.Title
		}
		body := stripTermsFromBody(item.Body)
		if body != "" {
			return `<info id="` + escapeXMLAttr(id) + `">` + "\n" + body + "\n</info>"
		}
		return `<info id="` + escapeXMLAttr(id) + `">` + "\n</info>"
	}
	// command
	var sb strings.Builder
	sb.WriteString(`<command id="` + escapeXMLAttr(item.Title) + `">` + "\n")
	if item.Description != "" {
		sb.WriteString("  <desc>" + escapeXMLAttr(item.Description) + "</desc>\n")
	}
	if item.Body != "" {
		bodyLines := strings.Split(item.Body, "\n")
		var flagLines []string
		for _, l := range bodyLines {
			trimmed := strings.TrimLeft(l, " \t")
			if strings.HasPrefix(trimmed, "-") {
				flagLines = append(flagLines, trimmed)
			}
		}
		if len(flagLines) > 0 {
			sb.WriteString("  <flags>\n")
			for _, fl := range flagLines {
				tokens := strings.Fields(fl)
				primaryIdx := -1
				for i, t := range tokens {
					if strings.HasPrefix(t, "--") {
						primaryIdx = i
						break
					}
				}
				flagName := ""
				flagDesc := ""
				if primaryIdx >= 0 {
					flagName = tokens[primaryIdx]
					flagDesc = strings.TrimSpace(strings.Join(tokens[primaryIdx+1:], " "))
				} else if len(tokens) > 0 {
					flagName = tokens[0]
				}
				if flagDesc != "" {
					sb.WriteString(`    <flag name="` + escapeXMLAttr(flagName) + `">` + escapeXMLAttr(flagDesc) + "</flag>\n")
				} else {
					sb.WriteString("    <flag>" + escapeXMLAttr(flagName) + "</flag>\n")
				}
			}
			sb.WriteString("  </flags>\n")
		}
	}
	sb.WriteString("</command>")
	return sb.String()
}

// BuildUserPrompt assembles the user message: context blocks + question.
func BuildUserPrompt(query string, corpus []DocItem) string {
	var ctxParts []string
	for _, item := range corpus {
		rendered := formatDocItem(item)
		if rendered != "" {
			ctxParts = append(ctxParts, rendered)
		}
	}
	context := strings.Join(ctxParts, "\n\n")
	return "<context>\n" + context + "\n</context>\n\nQuestion: " + query + "\n\nAnswer:"
}
