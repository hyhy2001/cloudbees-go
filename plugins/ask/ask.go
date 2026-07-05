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
		Long:  "Ask how to use bee — requires LM endpoint configured in bee.lm.json or env",
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
				return fmt.Errorf("Empty query. Try: bee ask create job")
			}

			provider := buildProvider()
			if provider == nil {
				return fmt.Errorf("bee ask requires an LM provider to be configured. Set LM_URL (and LM_API_KEY or client credentials) in bee.lm.yml or environment variables.")
			}

			corpus := BuildCorpus(root)

			stop := startSpinner("Thinking…")

			limit := flagLimit
			if limit <= 0 {
				limit = 5 // matches the TS parse fallback
			}

			// JSON output drains any stream first, so disable streaming for it.
			wantStream := !flagNoStream && !flagJSON
			result, err := Answer(query, corpus, provider, limit, wantStream)
			stop()
			if err != nil {
				return err
			}

			if result.Source == "raw" && len(result.Hits) == 0 {
				fmt.Println("I only help with bee usage.")
				return nil
			}

			// Streaming answer: drive it, printing chunks as they arrive. It may
			// turn out to be JSON, in which case Structured gets populated.
			if result.Stream && result.StreamOutput != nil && !flagJSON {
				text := result.StreamOutput(func(chunk string) { fmt.Print(chunk) })
				if result.Structured != nil {
					renderStructuredAnswer(result.Structured)
					renderFooter(result.Usage, result.RewriteUsage, flagDebug)
					return nil
				}
				if text != "" {
					fmt.Println()
				}
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

			renderHits(query, result.Hits)
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
	enc, err := askJSONBytes(query, result)
	if err != nil {
		return err
	}
	fmt.Println(string(enc))
	return nil
}

// askJSONBytes builds the --json payload. Struct field order fixes the JSON key
// order to match the TS CLI (query, source, provider, answer, structured, hits);
// hits is a non-nil slice so an empty result serializes as [] not null, and
// provider is a pointer so it serializes as null when unset (TS `?? null`).
func askJSONBytes(query string, result *AnswerResult) ([]byte, error) {
	type hitOut struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Source      string `json:"source"`
	}
	hits := []hitOut{}
	for _, h := range result.Hits {
		hits = append(hits, hitOut{h.ID, h.Type, h.Title, h.Description, h.Source})
	}
	var provider *string
	if result.Provider != "" {
		provider = &result.Provider
	}
	out := struct {
		Query      string    `json:"query"`
		Source     string    `json:"source"`
		Provider   *string   `json:"provider"`
		Answer     string    `json:"answer"`
		Structured *LMAnswer `json:"structured"`
		Hits       []hitOut  `json:"hits"`
	}{query, result.Source, provider, result.Text, result.Structured, hits}
	return json.MarshalIndent(out, "", "  ")
}
