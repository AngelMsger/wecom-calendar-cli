// Package app wires the cobra command tree and runs the CLI.
package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/angelmsger/wecom-calendar-cli/internal/cliflags"
	"github.com/angelmsger/wecom-calendar-cli/internal/output"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/spf13/cobra"
)

// NewRootCmd builds the full cobra command tree. It exists so tooling — most
// notably the docs generator (cmd/gen-docs) — can walk the same command tree
// the CLI runs, keeping generated reference docs in lock-step with --help.
func NewRootCmd() *cobra.Command { return newRootCmd() }

func newRootCmd() *cobra.Command {
	root, _ := newRootCmdWithState()
	return root
}

// Execute builds and runs the root command, returning a process exit code.
func Execute() int {
	root, state := newRootCmdWithState()
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		defaultHelp(cmd, args)
		if block := contextReminderBlock(state); block != "" {
			fmt.Fprint(cmd.OutOrStdout(), "\n"+block)
		}
	})
	// Absorb common argv slips (--calendarId -> --calendar-id, --limit5 ->
	// --limit 5) before cobra parses, echoing each fix to stderr so stdout data
	// is untouched and the agent learns the canonical form.
	if corrected, corrections := cliflags.Normalize(os.Args[1:], cliflags.Collect(root)); len(corrections) > 0 {
		root.SetArgs(corrected)
		output.EmitNotice(os.Stderr, map[string]any{"_notice": map[string]any{"corrections": corrections}})
	}
	cmd, err := root.ExecuteC()
	// Surface an available-update notice on stderr regardless of whether the
	// command succeeded; best-effort and bounded, never affects the exit code.
	maybeNotifyUpdate(state, cmd)
	if err != nil {
		ce := cerrors.AsCLIError(err)
		if ce.Category == cerrors.CategoryInternal && !isCLIError(err) {
			ce = cerrors.Wrap(err, cerrors.CategoryUsage, "USAGE", err.Error())
		}
		output.EmitError(ce, os.Stderr)
		return cerrors.ExitCode(ce)
	}
	return 0
}

func isCLIError(err error) bool {
	_, ok := err.(*cerrors.CLIError)
	return ok
}

// newRootCmdWithState assembles the full command tree and returns the appState
// it is wired to, so Execute can emit the update notice after the command runs.
func newRootCmdWithState() (*cobra.Command, *appState) {
	state := &appState{}

	root := &cobra.Command{
		Use:   constants.AppName,
		Short: "Sync WeCom (Enterprise WeChat) calendars to a local store and query them, built for coding agents",
		Long: "wecom-calendar-cli syncs your WeCom calendars over CalDAV into a local\n" +
			"SQLite store, then serves fast, agent-friendly queries over that data.\n" +
			"Events carry a free-form, agent-maintained metadata layer for your own\n" +
			"classification and cross-tool task links. Output is JSON with structured\n" +
			"errors.\n\n" +
			"AGENT NOTE: a companion Skill (\"wecom-calendar\") carries the canonical\n" +
			"usage, safety modes, and setup and is the source of truth for driving this\n" +
			"CLI. If you are an agent, load that Skill before composing commands. Check\n" +
			"status with `wecom-calendar-cli skill status`; install it with\n" +
			"`wecom-calendar-cli skill install`.",
		Version:       versionString(),
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			output.SetErrorPretty(state.gflags.pretty)
			maybeSkillHint(cmd)
			if err := state.load(); err != nil {
				return err
			}
			maybeContextHint(cmd, state)
			return nil
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&state.gflags.baseURL, "base-url", "", "CalDAV server URL (overrides config)")
	pf.StringVarP(&state.gflags.format, "format", "f", "", "output format: json, table or ndjson")
	pf.StringVar(&state.gflags.fields, "fields", "", "comma-separated dot-path fields to keep")
	pf.StringVar(&state.gflags.timeout, "timeout", "", "request timeout, e.g. 30s")
	pf.StringVar(&state.gflags.configPath, "config", "", "config directory (default ~/.angelmsger/wecom-calendar)")
	pf.StringVar(&state.gflags.useContext, "use-context", "", "use a named context for this invocation")
	pf.BoolVarP(&state.gflags.verbose, "verbose", "v", false, "verbose diagnostics on stderr")
	pf.BoolVar(&state.gflags.pretty, "pretty", false,
		"human-friendly mode for interactive terminal use only (agents/scripts should omit): TUI in `config init`, colorized JSON elsewhere; errors without a TTY")
	pf.BoolVar(&state.gflags.allowWrites, "allow-writes", false,
		"override read-only mode (defaults.read_only / WECOM_CALENDAR_CLI_READ_ONLY) for this invocation")

	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return cerrors.Wrap(err, cerrors.CategoryUsage, "BAD_FLAG", err.Error())
	})
	root.SetVersionTemplate("{{.Name}} {{.Version}}\n")

	enumComplete(root, "format", "json", "table", "ndjson")

	root.AddCommand(
		newSyncCmd(state),
		newExpandCmd(state),
		newCalendarCmd(state),
		newEventCmd(state),
		newMetaCmd(state),
		newConfigCmd(state),
		newAuthCmd(state),
		newWhoamiCmd(state),
		newDoctorCmd(state),
		newSkillCmd(state),
		newVersionCmd(),
	)
	enforceSubcommands(root)
	return root, state
}

// enforceSubcommands makes every pure command group (one with subcommands but no
// action of its own) reject an unknown subcommand instead of cobra's default
// print-help-and-exit-0, which reads as a successful no-op to agents.
func enforceSubcommands(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		enforceSubcommands(child)
	}
	if cmd.HasParent() && cmd.HasSubCommands() && !cmd.Runnable() {
		requireSubcommand(cmd)
	}
}

func requireSubcommand(cmd *cobra.Command) {
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) == 0 {
			return c.Help()
		}
		msg := fmt.Sprintf("unknown command %q for %q", args[0], c.CommandPath())
		if c.SuggestionsMinimumDistance <= 0 {
			c.SuggestionsMinimumDistance = 2
		}
		if s := c.SuggestionsFor(args[0]); len(s) > 0 {
			msg += "\n\nDid you mean this?\n\t" + strings.Join(s, "\n\t")
		}
		return cerrors.New(cerrors.CategoryUsage, "UNKNOWN_COMMAND", msg).
			WithNextSteps(c.CommandPath() + " --help")
	}
}

// versionString renders the version, commit and build time as one line.
func versionString() string {
	return fmt.Sprintf("%s (commit %s, built %s)",
		constants.Version, constants.Commit, constants.BuildTime)
}

// newVersionCmd prints build metadata. It mirrors the `--version` flag.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(os.Stdout, "%s %s\n", constants.AppName, versionString())
			return nil
		},
	}
}
