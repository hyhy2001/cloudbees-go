// Package cli provides output formatting utilities for bee commands.
package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"
)

// Table prints a table with header and rows to stdout.
func Table(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	fmt.Fprintln(w, strings.Repeat("-\t", len(headers)))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	w.Flush()
}

// KV prints key-value pairs.
func KV(pairs [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, p := range pairs {
		fmt.Fprintf(w, "%s\t%s\n", p[0], p[1])
	}
	w.Flush()
}

// Success prints a green success message.
func Success(msg string) { fmt.Println("\033[32m✓\033[0m " + msg) }

// Error prints a red error message to stderr.
func Error(msg string) { fmt.Fprintln(os.Stderr, "\033[31m✗\033[0m "+msg) }

// Info prints a dim info message.
func Info(msg string) { fmt.Println("\033[2m" + msg + "\033[0m") }

// Warn prints a yellow warning message.
func Warn(msg string) { fmt.Println("\033[33m!\033[0m " + msg) }

// ReadHidden reads a password/token from stdin without echoing.
func ReadHidden(prompt string) (string, error) {
	fmt.Print(prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), err
}

// ReadLine reads a visible line from stdin.
func ReadLine(prompt string) (string, error) {
	fmt.Print(prompt)
	var line string
	_, err := fmt.Scanln(&line)
	return line, err
}

// TermWidth returns the current terminal width, defaulting to 80.
func TermWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}
