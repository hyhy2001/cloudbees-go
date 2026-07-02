package ask

import (
	"regexp"
	"strings"
)

// LMAnswer is the structured JSON response from the LM.
type LMAnswer struct {
	Reasoning   string    `json:"reasoning,omitempty"`
	Explanation string    `json:"explanation"`
	Commands    []Command `json:"commands"`
	Note        *string   `json:"note"`
}

// Command is one command entry in LMAnswer.
type Command struct {
	Cmd     string  `json:"cmd"`
	Flags   []Flag  `json:"flags,omitempty"`
	Example string  `json:"example,omitempty"`
}

// Flag is one flag entry in a Command.
type Flag struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// TokenUsage holds prompt/completion token counts.
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
}

// LMProvider is the interface each provider implements.
type LMProvider interface {
	Name() string
	Generate(prompt string, maxTokens int) (string, error)
	GenerateWithUsage(prompt string, maxTokens int) (string, TokenUsage, error)
	GenerateJSON(prompt string) (*LMAnswer, TokenUsage, error)
	Stream(prompt string, write func(string)) error
}

// AnswerResult is what Answer returns.
type AnswerResult struct {
	Source       string    // "lm" | "raw"
	Text         string
	Structured   *LMAnswer
	Usage        TokenUsage
	RewriteUsage TokenUsage
	Hits         []DocItem
	Provider     string
}

var thinkRe = regexp.MustCompile(`(?is)<think>.*?</think>\s*`)

// StripThinkBlock removes an inline <think>...</think> block.
func StripThinkBlock(text string) string {
	return strings.TrimLeft(thinkRe.ReplaceAllString(text, ""), " \t\r\n")
}

const rewritePrompt = `Normalize this bee CLI question to BM25 search keywords (3-6 lowercase tokens). Output ONLY the tokens, space-separated.

Examples:
  "Hello I am a newbie, how to use this?" → getting started login
  "kick off a pipeline" → job run pipeline
  "rotate my api key" → credential update rotate
  "put agent in maintenance" → node offline
  "403 forbidden login error" → auth error 403
  "all options for create node" → node create flags`

// rewriteQuery rewrites a free-form query to BM25 keywords using the LM.
func rewriteQuery(query string, provider LMProvider) (string, TokenUsage) {
	prompt := rewritePrompt + "\n\n  \"" + query + "\" →"
	text, usage, err := provider.GenerateWithUsage(prompt, 32)
	if err != nil {
		return query, TokenUsage{}
	}
	keywords := strings.Join(strings.Fields(strings.TrimSpace(text))[:min8(strings.Fields(strings.TrimSpace(text)), 8)], " ")
	if keywords == "" {
		return query, usage
	}
	return keywords, usage
}

func min8(s []string, n int) int {
	if len(s) < n {
		return len(s)
	}
	return n
}

// getCorpusFlags parses flag names and descriptions from a command's corpus body.
func getCorpusFlags(cmdID string, corpus []DocItem) map[string]string {
	for _, item := range corpus {
		if item.ID != cmdID {
			continue
		}
		if item.Body == "" {
			return nil
		}
		flags := make(map[string]string)
		for _, line := range strings.Split(item.Body, "\n") {
			// Match lines like: --flag-name <arg>    Description
			re := regexp.MustCompile(`^\s*(?:-\w,\s*)?(--[a-z][-a-z]*)\s*(?:<[^>]*)?\s+(.*)`)
			m := re.FindStringSubmatch(line)
			if m != nil {
				flags[m[1]] = strings.TrimSpace(m[2])
			}
		}
		if len(flags) > 0 {
			return flags
		}
		return nil
	}
	return nil
}

// ValidateCommands filters and cleans the LM answer's command list against the corpus.
func ValidateCommands(commands []Command, corpus []DocItem) []Command {
	validIDs := make(map[string]bool)
	for _, c := range corpus {
		if c.Type == "command" {
			validIDs[c.ID] = true
		}
	}

	seen := make(map[string]bool)
	var result []Command

	for _, c := range commands {
		if strings.Contains(c.Cmd, "--help") {
			continue
		}
		// Normalize: strip flags for dedup check
		normalized := strings.TrimSpace(regexp.MustCompile(`\s+--?\S+.*$`).ReplaceAllString(c.Cmd, ""))
		if seen[normalized] {
			continue
		}
		seen[normalized] = true

		re := regexp.MustCompile(`(?i)^bee\s+([a-z][-a-z]*)(?:\s+([a-z][-a-z]*))?`)
		m := re.FindStringSubmatch(c.Cmd)
		if m == nil {
			continue
		}
		g := strings.ToLower(m[1])
		s := ""
		if len(m) > 2 {
			s = strings.ToLower(m[2])
		}
		if g != "ask" && g != "help" && !validIDs[g] && !(s != "" && validIDs[g+"."+s]) {
			continue
		}

		// Clean flags
		var cleanFlags []Flag
		for _, f := range c.Flags {
			if strings.HasPrefix(f.Name, "--") {
				cleanFlags = append(cleanFlags, f)
			}
		}

		// Cross-check flags against corpus
		re3 := regexp.MustCompile(`(?i)^bee\s+([a-z][-a-z]*)(?:\s+([a-z][-a-z]*)(?:\s+([a-z][-a-z]*))?)?`)
		m3 := re3.FindStringSubmatch(c.Cmd)
		if m3 != nil {
			g2 := strings.ToLower(m3[1])
			s2 := ""
			if len(m3) > 2 {
				s2 = strings.ToLower(m3[2])
			}
			t2 := ""
			if len(m3) > 3 {
				t2 = strings.ToLower(m3[3])
			}
			var cmdID string
			if t2 != "" {
				cmdID = g2 + "." + s2 + "." + t2
			} else if s2 != "" {
				cmdID = g2 + "." + s2
			} else {
				cmdID = g2
			}
			knownFlags := getCorpusFlags(cmdID, corpus)
			if knownFlags != nil {
				var checked []Flag
				for _, f := range cleanFlags {
					if desc, ok := knownFlags[f.Name]; ok {
						if desc != "" {
							f.Description = desc
						}
						checked = append(checked, f)
					}
				}
				cleanFlags = checked
			}
		}

		result = append(result, Command{Cmd: c.Cmd, Flags: cleanFlags, Example: c.Example})
	}
	return result
}

// Answer is the main entry point for bee ask.
func Answer(query string, corpus []DocItem, provider LMProvider, limit int) (*AnswerResult, error) {
	hits := searchDocs(query, corpus, limit, true, true)

	directHits := searchDocs(query, corpus, limit*3, true, false)
	if len(directHits) == 0 {
		return &AnswerResult{Source: "raw", Hits: nil}, nil
	}

	searchQuery := query
	var rewriteUsage TokenUsage
	if len(directHits) < 3 {
		searchQuery, rewriteUsage = rewriteQuery(query, provider)
	}

	fusedBase := directHits
	runtimeModel := embedModel()
	modelsMatch := runtimeModel == "" || runtimeModel == "default" || runtimeModel == CORPUS_MODEL
	bm25TopIsCommand := directHits[0].Type == "command"
	if modelsMatch && bm25TopIsCommand {
		if vdb := getVectorDb(); len(vdb.Matrix) > 0 {
			if queryEmb, err := embedQuery(searchQuery); err == nil && len(queryEmb) == len(vdb.Matrix[0]) {
				vectorHits := searchVector(queryEmb, vdb, corpus, limit*3)
				fusedBase = rrfFusion(directHits, vectorHits, 60)
			}
		}
	}

	contextHits := fusedBase
	if len(contextHits) > limit {
		contextHits = contextHits[:limit]
	}

	prompt := BuildUserPrompt(query, contextHits)

	structured, usage, err := provider.GenerateJSON(prompt)
	if err == nil && structured != nil {
		cleanCmds := ValidateCommands(structured.Commands, corpus)
		structured.Commands = cleanCmds
		return &AnswerResult{
			Source:       "lm",
			Text:         structured.Explanation,
			Structured:   structured,
			Usage:        usage,
			RewriteUsage: rewriteUsage,
			Hits:         hits,
			Provider:     provider.Name(),
		}, nil
	}

	// Fallback: plain generate
	text, err2 := provider.Generate(prompt, 8192)
	if err2 != nil {
		return &AnswerResult{Source: "raw", Text: "", Hits: hits}, nil
	}
	text = StripThinkBlock(text)
	return &AnswerResult{Source: "lm", Text: text, Hits: hits, Provider: provider.Name()}, nil
}
