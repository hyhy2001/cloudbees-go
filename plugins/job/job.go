package job

import (
	"database/sql"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, db *sql.DB, dbPath string) {
	cmd := &cobra.Command{Use: "job", Short: "Manage CloudBees jobs"}
	// ponytail: job commands — implement in Phase 2
	root.AddCommand(cmd)
}
