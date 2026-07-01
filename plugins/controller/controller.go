package controller

import (
	"database/sql"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, db *sql.DB, dbPath string) {
	cmd := &cobra.Command{Use: "controller", Short: "Manage CloudBees / Jenkins controller instances"}
	// ponytail: controller commands — implement in Phase 2
	root.AddCommand(cmd)
}
