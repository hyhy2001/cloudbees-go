// Package cli provides output formatting utilities for bee commands.
package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// Table prints a box-drawing table matching the TS CLI (cli-table3 defaults):
// one space of padding each side, a ├─┼─┤ separator before every data row, and
// bold-cyan headers on a TTY (plain when piped). Empty tables render as a
// header-only box.
func Table(headers []string, rows [][]string) {
	n := len(headers)
	if n == 0 {
		return
	}
	// Column widths = max visible (rune) width across header + all cells.
	widths := make([]int, n)
	for i, h := range headers {
		widths[i] = runewidth.StringWidth(h)
	}
	for _, row := range rows {
		for i := 0; i < n && i < len(row); i++ {
			if w := runewidth.StringWidth(row[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	border := func(left, mid, right string) string {
		var b strings.Builder
		b.WriteString(left)
		for i, w := range widths {
			if i > 0 {
				b.WriteString(mid)
			}
			b.WriteString(strings.Repeat("─", w+2))
		}
		b.WriteString(right)
		return b.String()
	}
	line := func(cells []string, color bool) string {
		var b strings.Builder
		b.WriteString("│")
		for i, w := range widths {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			pad := w - runewidth.StringWidth(cell)
			if pad < 0 {
				pad = 0
			}
			text := cell + strings.Repeat(" ", pad)
			if color {
				text = "\033[1m\033[36m" + cell + "\033[39m\033[22m" + strings.Repeat(" ", pad)
			}
			b.WriteString(" " + text + " │")
		}
		return b.String()
	}

	colorHead := term.IsTerminal(int(os.Stdout.Fd()))
	fmt.Println(border("┌", "┬", "┐"))
	fmt.Println(line(headers, colorHead))
	// cli-table3 (compact:false) draws a ├─┼─┤ rule before every data row —
	// including the first — so an empty table is just top+header+bottom.
	for _, row := range rows {
		fmt.Println(border("├", "┼", "┤"))
		fmt.Println(line(row, false))
	}
	fmt.Println(border("└", "┴", "┘"))
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

// stdoutIsTTY reports whether stdout is an interactive terminal — color is
// emitted only then, matching chalk's auto-detection in the TS CLI (piped
// output is plain, so captured/redirected output compares byte-for-byte).
func stdoutIsTTY() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// Info prints a dim info message (plain when piped, matching TS chalk).
func Info(msg string) {
	if stdoutIsTTY() {
		fmt.Println("\033[2m" + msg + "\033[0m")
	} else {
		fmt.Println(msg)
	}
}

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
