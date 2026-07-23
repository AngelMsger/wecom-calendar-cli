package app

import (
	"os"

	"golang.org/x/term"
)

// isTerminal reports whether f is an interactive terminal. It uses a real
// isatty check (not a ModeCharDevice heuristic, which also matches /dev/null and
// other character devices) so the destructive-write confirmation gate and secret
// prompts treat a redirected or piped stdin as non-interactive.
func isTerminal(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}

func stdinIsTTY() bool  { return isTerminal(os.Stdin) }
func stdoutIsTTY() bool { return isTerminal(os.Stdout) }
func stderrIsTTY() bool { return isTerminal(os.Stderr) }
