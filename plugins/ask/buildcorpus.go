package ask

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// walkCommands traverses the cobra tree and appends DocItems for each command.
func walkCommands(cmd *cobra.Command, path []string, out *[]DocItem) {
	for _, sub := range cmd.Commands() {
		if sub.Hidden {
			continue
		}
		subPath := append(append([]string{}, path...), sub.Name())
		desc := sub.Short

		var flagLines []string
		sub.Flags().VisitAll(func(f *pflag.Flag) {
			name := "--" + f.Name
			line := name
			if f.Shorthand != "" {
				line = fmt.Sprintf("-%-2s %s", f.Shorthand+",", name)
			}
			// Pad to 28 chars
			for len(line) < 28 {
				line += " "
			}
			flagLines = append(flagLines, line+f.Usage)
		})

		if desc != "" || len(flagLines) > 0 {
			// Build usage sig from positional args in Use field
			use := sub.Use
			parts := strings.Fields(use)
			var sig string
			if len(parts) > 1 {
				sig = strings.Join(parts[1:], " ")
			}
			titleParts := append([]string{"bee"}, subPath...)
			if sig != "" {
				titleParts = append(titleParts, sig)
			}
			title := strings.Join(titleParts, " ")

			body := ""
			if len(flagLines) > 0 {
				body = "flags options parameters cloudbees jenkins\n" + strings.Join(flagLines, "\n")
			}

			*out = append(*out, DocItem{
				ID:          strings.Join(subPath, "."),
				Type:        "command",
				Title:       title,
				Description: desc,
				Body:        body,
				Source:      "command",
			})
		}

		walkCommands(sub, subPath, out)
	}
}

// BuildCorpus creates the full DocItem corpus from the cobra root and embedded help facts.
func BuildCorpus(root *cobra.Command) []DocItem {
	var items []DocItem
	if root != nil {
		walkCommands(root, nil, &items)
	}

	for _, fact := range getHelpFacts() {
		parts := []string{fact.Answer}
		parts = append(parts, fact.Terms...)
		parts = append(parts, fact.Commands...)
		parts = append(parts, fact.Related...)
		body := strings.Join(parts, "\n")
		items = append(items, DocItem{
			ID:          fact.ID,
			Type:        "doc",
			Title:       fact.Title,
			Description: fact.Kind,
			Body:        body,
			Source:      "help:" + fact.Kind,
		})
	}

	if includeDocChunks() {
		for _, chunk := range buildDocChunks() {
			items = append(items, DocItem{
				ID:          chunk.ID,
				Type:        "doc",
				Title:       chunk.Heading,
				Description: chunk.Source,
				Body:        chunk.Body,
				Source:      chunk.Source,
			})
		}
	}

	return items
}
