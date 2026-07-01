package node

import (
	"database/sql"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, db *sql.DB, dbPath string) {
	cmd := &cobra.Command{Use: "node", Short: "Manage CloudBees build agents"}
	// ponytail: node commands — implement in Phase 2
	root.AddCommand(cmd)
}
