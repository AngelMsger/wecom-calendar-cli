package app

import (
	"os"

	"github.com/angelmsger/agent-cli-kit/terminal"
)

// The process's streams are interactive only when their descriptor is a TTY:
// stdin on /dev/null (cron, CI, agent runners) or a pipe is not. The kit's
// terminal.Is is the family's single definition. stdinIsTTY gates the
// destructive-write confirmation and the login prompts, so non-TTY callers fail
// structurally rather than hang; stderrIsTTY keeps agent-only nudges and the
// progress display off a pipe.
func stdinIsTTY() bool  { return terminal.Is(os.Stdin) }
func stderrIsTTY() bool { return terminal.Is(os.Stderr) }

// prompter asks on stderr and reads stdin, so stdout stays a clean data channel.
func prompter() terminal.Prompter { return terminal.Prompter{In: os.Stdin, Out: os.Stderr} }
