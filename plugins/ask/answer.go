package ask

import (
	"encoding/json"
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
	Source       string // "lm" | "raw"
	Text         string
	Structured   *LMAnswer
	Usage        TokenUsage
	RewriteUsage TokenUsage
	Hits         []DocItem
	Provider     string

	// Stream is set when the answer should be produced by streaming. The CLI
	// calls StreamOutput(write) to drive it; write receives each chunk (the
	// final cleaned text) and StreamOutput returns the full text. After it
	// returns, Structured may be populated if the stream turned out to be JSON.
	Stream       bool
	StreamOutput func(write func(string)) string
}

var thinkRe = regexp.MustCompile(`(?is)<think>.*?</think>\s*`)

// StripThinkBlock removes an inline <think>...</think> block.
func StripThinkBlock(text string) string {
	return strings.TrimLeft(thinkRe.ReplaceAllString(text, ""), " \t\r\n")
}

// sent is the invisible sentinel (U+E000, private-use) that marks a stripped
// span before cleanup collapses surrounding punctuation. Matches TS SENT.
const sent = ""

var (
	flagInBodyRe   = regexp.MustCompile(`--[\w-]+`)
	backtickRe     = regexp.MustCompile("`([^`]*)`")
	beeCmdInner    = regexp.MustCompile(`(?i)^\s*bee\s+([a-z][-a-z]*)(?:\s+([a-z][-a-z]*))?`)
	beeCmdBoundary = regexp.MustCompile(`(?i)(^|[.:;\n])\s*(bee\s+([a-z][-a-z]*)(?:\s+([a-z][-a-z]*))?)`)
	anyFlagRe      = regexp.MustCompile(`--[\w-]+`)
)

// StripInventedCommands removes `bee ...` command spans and --flags from LM
// prose that don't exist in the corpus, so a generated answer can't confidently
// tell the user to run a non-existent command. Mirrors TS stripInventedCommands.
func StripInventedCommands(text string, corpus []DocItem) string {
	valid := map[string]bool{}
	validFlags := map[string]bool{}
	for _, item := range corpus {
		if item.Type != "command" {
			continue
		}
		valid[item.ID] = true
		if dot := strings.Index(item.ID, "."); dot > 0 {
			valid[item.ID[:dot]] = true
		}
		for _, f := range flagInBodyRe.FindAllString(item.Body, -1) {
			validFlags[f] = true
		}
	}
	valid["ask"] = true
	valid["help"] = true
	validFlags["--help"] = true
	validFlags["--version"] = true
	if len(valid) == 0 {
		return text
	}

	isValidBeeCmd := func(group, sub string) bool {
		g := strings.ToLower(group)
		s := strings.ToLower(sub)
		if g == "ask" {
			return true
		}
		if g == "help" {
			return s == ""
		}
		id := g
		if s != "" {
			id = g + "." + s
		}
		return valid[id]
	}

	// 1. backtick `bee ...` spans
	result := backtickRe.ReplaceAllStringFunc(text, func(full string) string {
		inner := full[1 : len(full)-1]
		m := beeCmdInner.FindStringSubmatch(inner)
		if m == nil {
			return full
		}
		if isValidBeeCmd(m[1], m[2]) {
			return full
		}
		return sent
	})

	// 2. inline "bee <group> <sub>" at a boundary
	result = beeCmdBoundary.ReplaceAllStringFunc(result, func(full string) string {
		m := beeCmdBoundary.FindStringSubmatch(full)
		if isValidBeeCmd(m[3], m[4]) {
			return full
		}
		return m[1] + " " + sent
	})

	// 3. hallucinated --flags
	result = anyFlagRe.ReplaceAllStringFunc(result, func(flag string) string {
		if validFlags[flag] {
			return flag
		}
		return sent
	})

	if !strings.Contains(result, sent) {
		return text
	}

	// Cleanup: collapse the sentinel and the connectors/punctuation around it.
	for _, re := range sentCleanupRes {
		result = re.re.ReplaceAllString(result, re.repl)
	}
	return strings.TrimSpace(result)
}

var sentCleanupRes = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`\s*,\s*` + sent), ""},              // ", <removed>"
	{regexp.MustCompile(sent + `\s*,\s*`), ""},              // "<removed>, "
	{regexp.MustCompile(`(?i)\s+and\s+` + sent), ""},        // " and <removed>"
	{regexp.MustCompile(`(?i)` + sent + `\s+and\s+`), ""},   // "<removed> and "
	{regexp.MustCompile(`(?i)\s+or\s+` + sent), ""},         // " or <removed>"
	{regexp.MustCompile(`(?i)` + sent + `\s+or\s+`), ""},    // "<removed> or "
	{regexp.MustCompile(`,?\s*` + sent), ""},                // any sentinel w/ optional comma
	{regexp.MustCompile(`,\s*,`), ","},                      // collapsed double commas
	{regexp.MustCompile(`\s+([.,])`), "$1"},                 // space before punctuation
	{regexp.MustCompile(`(?im)\bUse:\s*$`), ""},             // dangling "Use:"
	{regexp.MustCompile(`[ \t]{2,}`), " "},                  // runs of spaces
	{regexp.MustCompile(`(?m)[ \t]+$`), ""},                 // trailing spaces per line
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

// Answer is the main entry point for bee ask. A nil provider degrades to the
// offline BM25 hits (source "raw") rather than erroring, matching the TS CLI.
// When stream is true and the provider supports streaming, the returned result
// carries a StreamOutput closure instead of a finished answer.
func Answer(query string, corpus []DocItem, provider LMProvider, limit int, stream bool) (*AnswerResult, error) {
	hits := searchDocs(query, corpus, limit, true, true)

	if provider == nil {
		return &AnswerResult{Source: "raw", Hits: hits}, nil
	}

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

	// Graph expansion: append related commands from the same group/CRUD family.
	graph := buildGraphFromCorpus(corpus)
	fused := append(append([]DocItem{}, fusedBase...), expandGraph(fusedBase, corpus, graph, 10)...)

	contextHits := selectContextHits(fused, corpus, limit)
	prompt := BuildUserPrompt(query, contextHits)

	// Structured JSON path (preferred).
	structured, usage, err := provider.GenerateJSON(prompt)
	if err == nil && structured != nil {
		structured.Commands = ValidateCommands(structured.Commands, corpus)
		return &AnswerResult{
			Source: "lm", Text: structured.Explanation, Structured: structured,
			Usage: usage, RewriteUsage: rewriteUsage, Hits: hits, Provider: provider.Name(),
		}, nil
	}

	// Streaming path: caller drives StreamOutput, which writes the cleaned text.
	if stream {
		res := &AnswerResult{Source: "lm", Hits: hits, Provider: provider.Name(), Stream: true, RewriteUsage: rewriteUsage}
		res.StreamOutput = func(write func(string)) string {
			jsonPrompt := prompt + "\n\nRespond with JSON only."
			var sb strings.Builder
			serr := provider.Stream(jsonPrompt, func(chunk string) { sb.WriteString(chunk) })
			full := sb.String()
			if serr != nil || full == "" {
				if g, gerr := provider.Generate(jsonPrompt, 8192); gerr == nil {
					full = g
				}
			}
			trimmed := strings.TrimSpace(StripThinkBlock(full))
			if i := strings.IndexByte(trimmed, '{'); i >= 0 {
				var parsed LMAnswer
				if json.Unmarshal([]byte(trimmed[i:]), &parsed) == nil && parsed.Explanation != "" {
					parsed.Commands = ValidateCommands(parsed.Commands, corpus)
					res.Structured = &parsed
					return parsed.Explanation
				}
			}
			cleaned := StripInventedCommands(trimmed, corpus)
			write(cleaned)
			return cleaned
		}
		return res, nil
	}

	// Non-stream fallback: plain generate, hardened against invented commands.
	text, err2 := provider.Generate(prompt, 8192)
	if err2 != nil {
		return &AnswerResult{Source: "raw", Text: "", Hits: hits}, nil
	}
	text = StripInventedCommands(StripThinkBlock(text), corpus)
	return &AnswerResult{Source: "lm", Text: text, Hits: hits, Provider: provider.Name()}, nil
}

// selectContextHits applies TS group-expansion: if 3+ of the top 10 fused hits
// share a top-level group, pull ALL that group's members from the full corpus
// (front-loaded), sliced to max(limit, groupSize); otherwise just the top limit.
func selectContextHits(fused []DocItem, corpus []DocItem, limit int) []DocItem {
	groupCounts := map[string]int{}
	top := fused
	if len(top) > 10 {
		top = top[:10]
	}
	for _, h := range top {
		groupCounts[strings.SplitN(h.ID, ".", 2)[0]]++
	}
	dominant := ""
	for _, h := range top { // iterate in fused order for determinism
		g := strings.SplitN(h.ID, ".", 2)[0]
		if groupCounts[g] >= 3 {
			dominant = g
			break
		}
	}
	if dominant == "" {
		if len(fused) > limit {
			return fused[:limit]
		}
		return fused
	}
	// TS uses corpus group members only for the slice-width bound; the ordering
	// reshuffles the *fused* list (group-in-fused first, then the rest).
	groupSize := 0
	seen := map[string]bool{}
	for _, c := range corpus {
		if c.ID == dominant || strings.HasPrefix(c.ID, dominant+".") {
			groupSize++
			seen[c.ID] = true
		}
	}
	var front, rest []DocItem
	for _, h := range fused {
		if seen[h.ID] {
			front = append(front, h)
		} else {
			rest = append(rest, h)
		}
	}
	out := append(front, rest...)
	n := limit
	if groupSize > n {
		n = groupSize
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}
