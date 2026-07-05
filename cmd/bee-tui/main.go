// Command bee-tui is the interactive TUI, split into its own binary so the
// main `bee` CLI never links bubbletea — whose init() queries the terminal
// (OSC 11 + cursor position) and would otherwise pollute CLI output. `bee --ui`
// execs this binary.
package main

import (
	"fmt"
	"os"

	"bee/internal/db"
	"bee/tui"
)

var version = "1.0.0"

func main() {
	dbPath := os.Getenv("CB_DB_PATH")
	if dbPath == "" {
		dbPath = db.DefaultPath()
	}

	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot open database: %v\n", err)
		os.Exit(1)
	}

	if err := tui.Run(database, dbPath, version); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
}
