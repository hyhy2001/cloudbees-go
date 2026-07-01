package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"bee/internal/db"
	"bee/plugins/ask"
	"bee/plugins/auth"
	"bee/plugins/controller"
	"bee/plugins/cred"
	"bee/plugins/job"
	"bee/plugins/node"
	"bee/tui"
)

var version = "1.0.0"

func main() {
	dbPath := os.Getenv("BEE_DB_PATH")
	if dbPath == "" {
		dbPath = db.DefaultPath()
	}

	database, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot open database: %v\n", err)
		os.Exit(1)
	}

	var flagUI bool

	root := &cobra.Command{
		Use:           "bee",
		Short:         "CloudBees CI / Jenkins CLI",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if flagUI {
				if err := tui.Run(database, dbPath); err != nil {
					fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
				}
				os.Exit(0)
			}
			return nil
		},
	}

	root.PersistentFlags().BoolVarP(&flagUI, "ui", "u", false, "Launch interactive TUI")

	// Register plugins
	auth.Register(root, database, dbPath)
	controller.Register(root, database, dbPath)
	cred.Register(root, database, dbPath)
	node.Register(root, database, dbPath)
	job.Register(root, database, dbPath)
	ask.Register(root, database, dbPath)

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
