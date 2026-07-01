package ask

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
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

// renderFooter prints token usage when --debug is set or when tokens were used.
func renderFooter(usage, rewriteUsage TokenUsage, debug bool) {
	if !debug && usage.PromptTokens == 0 && usage.CompletionTokens == 0 {
		return
	}
	parts := []string{}
	if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		parts = append(parts, fmt.Sprintf("tokens: %d prompt / %d completion", usage.PromptTokens, usage.CompletionTokens))
	}
	if rewriteUsage.PromptTokens > 0 || rewriteUsage.CompletionTokens > 0 {
		parts = append(parts, fmt.Sprintf("rewrite: %d prompt / %d completion", rewriteUsage.PromptTokens, rewriteUsage.CompletionTokens))
	}
	if len(parts) > 0 {
		fmt.Fprintf(os.Stderr, "\033[2m[%s]\033[0m\n", strings.Join(parts, " | "))
	}
}

// renderHits prints the offline BM25 hits when no LM is configured.
func renderHits(hits []DocItem) {
	if len(hits) == 0 {
		fmt.Println("No matching commands found. Try: bee --help")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, h := range hits {
		fmt.Fprintf(w, "  \033[1;36m%-30s\033[0m\t%s\n", h.Title, h.Description)
	}
	w.Flush()
}
