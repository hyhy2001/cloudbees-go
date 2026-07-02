package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

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

// runInstall creates a bee.csh wrapper next to the running binary and
// symlinks it to ~/.local/bin/bee — works even if the binary was copied
// somewhere else, since it resolves its own path via os.Executable().
func runInstall() error {
	binaryPath, err := os.Executable()
	if err != nil {
		return err
	}
	wrapperPath := filepath.Join(filepath.Dir(binaryPath), "bee.csh")
	wrapperContent := fmt.Sprintf("#!/usr/bin/env csh\nexec \"%s\" $*\n", binaryPath)
	if err := os.WriteFile(wrapperPath, []byte(wrapperContent), 0o755); err != nil {
		return err
	}
	fmt.Printf("  [OK] wrapper: %s\n", wrapperPath)

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	installDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return err
	}
	linkTarget := filepath.Join(installDir, "bee")
	if _, err := os.Lstat(linkTarget); err == nil {
		if err := os.Remove(linkTarget); err != nil {
			return err
		}
	}
	if err := os.Symlink(wrapperPath, linkTarget); err != nil {
		return err
	}
	fmt.Printf("  [OK] symlink: %s -> %s\n", linkTarget, wrapperPath)
	fmt.Println("\nAdd ~/.local/bin to your PATH if not already present.")
	return nil
}

func main() {
	// --install: self-install without needing source or make.
	if slices.Contains(os.Args[1:], "--install") {
		if err := runInstall(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	dbPath := os.Getenv("CB_DB_PATH")
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagUI {
				if err := tui.Run(database, dbPath, version); err != nil {
					return fmt.Errorf("tui error: %w", err)
				}
				return nil
			}
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if flagUI && cmd.Name() != "bee" {
				if err := tui.Run(database, dbPath, version); err != nil {
					fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
				}
				os.Exit(0)
			}
			return nil
		},
	}

	root.PersistentFlags().BoolVarP(&flagUI, "ui", "u", false, "Launch interactive TUI")
	root.PersistentFlags().Bool("install", false, "Install bee: create wrapper + symlink to ~/.local/bin/bee")

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
