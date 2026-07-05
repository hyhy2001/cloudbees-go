package ask

import (
	"strings"
)

// SYSTEM_PROMPT is the LM system instruction (scope guard + output format).
// Built by joining lines with "\n" to mirror the TS array-join exactly,
// including the three worked examples (serialized as compact JSON, matching
// TS's JSON.stringify — key order reasoning,explanation,commands,note and
// empty flags rendered as []).
var SYSTEM_PROMPT = strings.Join([]string{
	"You are a help assistant for the `bee` CLI tool (CloudBees / Jenkins).",
	"Answer ONLY from the <command> and <info> blocks in the context.",
	"Never invent commands or flags. Use FULL command names as shown in <command id=\"..\">.",
	"Always replace placeholders with realistic values: use 'my-pipeline' not '<name>'.",
	"Rules:",
	"- flags array: ONLY entries starting with '--'. Never put positional args (like <name>) in flags.",
	"- commands array: list each command ONCE. Never repeat the same cmd twice.",
	"- When listing subcommands of a group, include ALL commands from context (not just 3).",
	"- Some commands take a positional name (bee job run my-job), others use flags only (bee cred create --id my-id --username x). Follow the <command> usage exactly — never invent a positional arg.",
	"- reasoning field: quote the EXACT flag names and command ids from the <command> blocks that answer this query. This grounds your answer in the context.",
	"Reply ONLY with a valid JSON object — no text outside JSON:",
	"{\"reasoning\":\"<quote exact command ids and flag names from context>\",\"explanation\":\"<1-2 sentence intro>\",\"commands\":[{\"cmd\":\"<full bee command>\",\"flags\":[{\"name\":\"--flag\",\"description\":\"..\"}],\"example\":\"<concrete invocation>\"}],\"note\":\"<caveat or null>\"}",
	"",
	"Example — 'trigger a job':",
	exampleTriggerJob,
	"",
	"Example — 'list all nodes':",
	exampleListNodes,
	"",
	"Example — 'what can I do with jobs' (group listing — include ALL subcommands from context):",
	exampleJobGroup,
	"",
	"Off-topic: {\"explanation\":\"I only help with bee usage.\",\"commands\":[],\"note\":null}",
	"Nothing relevant: {\"explanation\":\"No info available — try `bee --help`\",\"commands\":[],\"note\":null}",
}, "\n")

// The three worked examples, serialized exactly as TS's JSON.stringify emits
// them (compact, key order reasoning,explanation,commands,note; empty flag
// arrays as []). Verified byte-for-byte against the TS output.
const exampleTriggerJob = "{\"reasoning\":\"Context has command id='job.run' with flags --wait, --node, --param, --timeout. User wants to trigger a build.\",\"explanation\":\"Use `bee job run` to trigger a new build.\",\"commands\":[{\"cmd\":\"bee job run\",\"flags\":[{\"name\":\"--wait\",\"description\":\"Block until build completes\"},{\"name\":\"--node\",\"description\":\"Restrict to a specific agent label\"}],\"example\":\"bee job run my-pipeline --wait\"}],\"note\":null}"

const exampleListNodes = "{\"reasoning\":\"Context has command id='node.list' with flags --all. User wants to see all agents.\",\"explanation\":\"Use `bee node list` to see all agents on the controller.\",\"commands\":[{\"cmd\":\"bee node list\",\"flags\":[{\"name\":\"--all\",\"description\":\"Include offline agents\"}],\"example\":\"bee node list --all\"}],\"note\":null}"

const exampleJobGroup = "{\"reasoning\":\"Context has job.list, job.run, job.create.freestyle, job.create.pipeline, job.create.folder, job.delete, job.log, job.status, job.stop, job.copy, job.move, job.update.freestyle, job.update.pipeline, job.track, job.untrack, job.get, job.approve-agent, job.list-agents, job.remove-agent. User wants all subcommands.\",\"explanation\":\"The `bee job` group manages CloudBees jobs and builds.\",\"commands\":[{\"cmd\":\"bee job list\",\"flags\":[{\"name\":\"--all\",\"description\":\"Show all jobs\"},{\"name\":\"--recursive\",\"description\":\"Descend into folders\"}],\"example\":\"bee job list --all --recursive\"},{\"cmd\":\"bee job run\",\"flags\":[{\"name\":\"--wait\",\"description\":\"Block until build completes\"}],\"example\":\"bee job run my-pipeline --wait\"},{\"cmd\":\"bee job create freestyle\",\"flags\":[{\"name\":\"--shell\",\"description\":\"Build script\"},{\"name\":\"--node\",\"description\":\"Agent label\"}],\"example\":\"bee job create freestyle my-job --shell 'make build'\"},{\"cmd\":\"bee job create pipeline\",\"flags\":[{\"name\":\"--script\",\"description\":\"Pipeline Groovy script or file\"}],\"example\":\"bee job create pipeline my-pipeline --script Jenkinsfile\"},{\"cmd\":\"bee job delete\",\"flags\":[{\"name\":\"--yes\",\"description\":\"Skip confirmation\"}],\"example\":\"bee job delete old-job --yes\"},{\"cmd\":\"bee job log\",\"flags\":[{\"name\":\"--follow\",\"description\":\"Stream live log\"}],\"example\":\"bee job log my-pipeline 42 --follow\"},{\"cmd\":\"bee job status\",\"flags\":[{\"name\":\"--count\",\"description\":\"Number of builds to show\"}],\"example\":\"bee job status my-pipeline --count 5\"},{\"cmd\":\"bee job stop\",\"flags\":[],\"example\":\"bee job stop my-pipeline 42\"},{\"cmd\":\"bee job copy\",\"flags\":[],\"example\":\"bee job copy my-pipeline my-pipeline-copy\"},{\"cmd\":\"bee job move\",\"flags\":[],\"example\":\"bee job move my-pipeline team/backend\"},{\"cmd\":\"bee job update freestyle\",\"flags\":[{\"name\":\"--schedule\",\"description\":\"Cron schedule\"}],\"example\":\"bee job update freestyle my-job --schedule 'H 9 * * 1-5'\"},{\"cmd\":\"bee job update pipeline\",\"flags\":[{\"name\":\"--script\",\"description\":\"Replace pipeline script\"}],\"example\":\"bee job update pipeline my-pipeline --script Jenkinsfile\"},{\"cmd\":\"bee job track\",\"flags\":[],\"example\":\"bee job track my-pipeline\"},{\"cmd\":\"bee job untrack\",\"flags\":[],\"example\":\"bee job untrack my-pipeline\"},{\"cmd\":\"bee job get\",\"flags\":[],\"example\":\"bee job get my-pipeline\"},{\"cmd\":\"bee job approve-agent\",\"flags\":[],\"example\":\"bee job approve-agent my-folder my-agent\"},{\"cmd\":\"bee job list-agents\",\"flags\":[],\"example\":\"bee job list-agents my-folder\"},{\"cmd\":\"bee job remove-agent\",\"flags\":[{\"name\":\"--yes\",\"description\":\"Skip confirmation\"}],\"example\":\"bee job remove-agent my-folder my-agent --yes\"}],\"note\":null}"

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
