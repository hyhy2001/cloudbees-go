package ask

import (
	"fmt"
	"os"
	"text/tabwriter"

	"golang.org/x/term"
)

// renderStructuredAnswer prints the LM answer in a human-friendly table format.
func renderStructuredAnswer(ans *LMAnswer) {
	if ans == nil {
		return
	}
	if ans.Explanation != "" {
		fmt.Println(ans.Explanation)
	}
	if len(ans.Commands) == 0 {
		return
	}
	fmt.Println()
	for _, cmd := range ans.Commands {
		fmt.Printf("  \033[1;36m%s\033[0m\n", cmd.Cmd)
		if len(cmd.Flags) > 0 {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, f := range cmd.Flags {
				fmt.Fprintf(w, "    \033[33m%s\033[0m\t%s\n", f.Name, f.Description)
			}
			w.Flush()
		}
		if cmd.Example != "" {
			fmt.Printf("    \033[2mExample: %s\033[0m\n", cmd.Example)
		}
		fmt.Println()
	}
	if ans.Note != nil && *ans.Note != "" {
		fmt.Printf("\033[2mNote: %s\033[0m\n", *ans.Note)
	}
}

// dim wraps s in the dim ANSI code on a TTY, plain when piped — matching the
// TS CLI's chalk.dim auto-detection so captured output compares byte-for-byte.
func dim(s string) string {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return "\033[2m" + s + "\033[22m"
	}
	return s
}

// renderFooter prints the "AI-generated" disclaimer plus token usage, matching
// the TS renderFooter: always to stdout, disclaimer always, token counts when
// usage is present, rewrite counts only with --debug.
func renderFooter(usage, rewriteUsage TokenUsage, debug bool) {
	tokenInfo := ""
	if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		tokenInfo = dim(fmt.Sprintf(" (↑%d ↓%d tokens)", usage.PromptTokens, usage.CompletionTokens))
	}
	if debug && (rewriteUsage.PromptTokens > 0 || rewriteUsage.CompletionTokens > 0) {
		tokenInfo += dim(fmt.Sprintf(" rewrite:(↑%d ↓%d)", rewriteUsage.PromptTokens, rewriteUsage.CompletionTokens))
	}
	fmt.Println(dim("\nAI-generated — verify before use.") + tokenInfo)
}

// renderHits prints the offline BM25 hits when no LM is configured.
func renderHits(query string, hits []DocItem) {
	if len(hits) == 0 {
		fmt.Printf("No results for '%s'.\nTry: bee --help  or  bee ask <shorter keyword>\n", query)
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, h := range hits {
		fmt.Fprintf(w, "  \033[1;36m%-30s\033[0m\t%s\n", h.Title, h.Description)
	}
	w.Flush()
}
