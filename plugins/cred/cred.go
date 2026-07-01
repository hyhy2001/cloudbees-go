package cred

import (
	"database/sql"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, db *sql.DB, dbPath string) {
	cmd := &cobra.Command{Use: "cred", Short: "Manage CloudBees credentials"}
	// ponytail: credential commands — implement in Phase 2
	root.AddCommand(cmd)
}
