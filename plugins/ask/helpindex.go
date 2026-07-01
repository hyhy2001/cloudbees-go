package ask

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed help-index.json
var helpIndexJSON []byte

// HelpFact is one entry from the generated help-index.json.
type HelpFact struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	Terms    []string `json:"terms"`
	Answer   string   `json:"answer"`
	Commands []string `json:"commands"`
	Related  []string `json:"related"`
}

var (
	_helpFacts     []HelpFact
	_helpFactsOnce sync.Once
)

func getHelpFacts() []HelpFact {
	_helpFactsOnce.Do(func() {
		_ = json.Unmarshal(helpIndexJSON, &_helpFacts)
	})
	return _helpFacts
}
