package ask

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Register adds the `bee ask` command to root.
func Register(root *cobra.Command, db *sql.DB, dbPath string) {
	var flagLimit int
	var flagJSON bool
	var flagDebug bool
	var flagNoStream bool

	cmd := &cobra.Command{
		Use:   "ask <query...>",
		Short: "AI-powered contextual help for bee commands",
		Long:  "Ask how to use bee. Requires LM endpoint via CB_DATABRICK_URL (+ CB_API_KEY or OAuth creds) or bee.yaml at build time.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(strings.Join(args, " "))
			onlyAlnum := strings.TrimSpace(strings.Map(func(r rune) rune {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' {
					return r
				}
				return ' '
			}, strings.ToLower(query)))
			if query == "" || onlyAlnum == "" {
				return fmt.Errorf("empty query — try: bee ask create job")
			}

			provider := buildProvider()
			if provider == nil {
				return fmt.Errorf("bee ask requires an LM provider. Set CB_DATABRICK_URL (and CB_API_KEY or OAuth credentials) or create bee.yaml and rebuild")
			}

			corpus := BuildCorpus(root)

			stop := startSpinner("Thinking…")

			limit := flagLimit
			if limit <= 0 {
				limit = 8
			}

			result, err := Answer(query, corpus, provider, limit)
			stop()
			if err != nil {
				return err
			}

			if result.Source == "raw" && len(result.Hits) == 0 {
				fmt.Println("I only help with bee usage.")
				return nil
			}

			if flagJSON {
				return printJSON(query, result)
			}

			if result.Source == "lm" && result.Structured != nil {
				renderStructuredAnswer(result.Structured)
				renderFooter(result.Usage, result.RewriteUsage, flagDebug)
				return nil
			}

			if result.Source == "lm" && result.Text != "" {
				fmt.Println(result.Text)
				return nil
			}

			renderHits(result.Hits)
			return nil
		},
	}

	cmd.Flags().IntVar(&flagLimit, "limit", 8, "Max context items to retrieve")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output machine-readable JSON")
	cmd.Flags().BoolVar(&flagDebug, "debug", false, "Show token usage and debug info")
	cmd.Flags().BoolVar(&flagNoStream, "no-stream", false, "Disable streaming (for compatibility)")

	root.AddCommand(cmd)
}

// buildProvider selects and initialises the LM provider from config.
// If ClientID/ClientSecret are set against a Databricks host, it OAuths
// directly (legacy path). Otherwise it treats lmURL() as an OpenAI-compatible
// endpoint — typically an llm-gateway instance holding the real credential.
func buildProvider() LMProvider {
	u := lmURL()
	if u == "" {
		return nil
	}
	ep := chatEndpoint()

	if isDatabricksHost(u) && clientID() != "" && clientSecret() != "" {
		return NewDatabricks(u, clientID(), clientSecret(), lmModel(), ep)
	}

	return NewOpenAI(ep, apiKey(), lmModel())
}

// startSpinner shows a TTY spinner on stderr; returns a stop func.
func startSpinner(text string) func() {
	fi, err := os.Stderr.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return func() {}
	}
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	stop := make(chan struct{})
	go func() {
		i := 0
		for {
			select {
			case <-stop:
				fmt.Fprintf(os.Stderr, "\r\033[2K")
				return
			default:
				fmt.Fprintf(os.Stderr, "\r\033[36m%s\033[0m \033[2m%s\033[0m", frames[i%len(frames)], text)
				i++
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
	return func() { close(stop); time.Sleep(10 * time.Millisecond) }
}

func printJSON(query string, result *AnswerResult) error {
	type hitOut struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Source      string `json:"source"`
	}
	var hits []hitOut
	for _, h := range result.Hits {
		hits = append(hits, hitOut{h.ID, h.Type, h.Title, h.Description, h.Source})
	}
	out := map[string]interface{}{
		"query":      query,
		"source":     result.Source,
		"provider":   result.Provider,
		"answer":     result.Text,
		"structured": result.Structured,
		"hits":       hits,
	}
	enc, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(enc))
	return nil
}
