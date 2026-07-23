package app

import (
	"github.com/angelmsger/wecom-calendar-cli/internal/config"
	"github.com/spf13/cobra"
)

// enumComplete registers a fixed set of completion values for a flag. Errors
// from registration are ignored: completion is a convenience, never required.
func enumComplete(cmd *cobra.Command, flag string, values ...string) {
	_ = cmd.RegisterFlagCompletionFunc(flag,
		cobra.FixedCompletions(values, cobra.ShellCompDirectiveNoFileComp))
}

// completeContextNames suggests context names from the config file. It is
// best-effort: any failure yields no suggestions.
func completeContextNames(s *appState) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		if s.resolved == nil {
			if err := s.load(); err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		}
		file, _, err := config.ReadFile(s.cfgDir)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return file.ContextNames(), cobra.ShellCompDirectiveNoFileComp
	}
}
