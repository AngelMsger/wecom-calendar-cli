package app

import (
	"github.com/angelmsger/wecom-calendar-cli/internal/config"
	"github.com/spf13/cobra"
)

// whoamiOut is the identity of the configured account.
type whoamiOut struct {
	Server     string `json:"server,omitempty"`
	Username   string `json:"username,omitempty"`
	Scheme     string `json:"scheme,omitempty"`
	Configured bool   `json:"configured"`
}

// newWhoamiCmd surfaces "who am I" — the configured WeCom account — as a small
// primitive so an agent can subtract the user from an event's attendees when
// reasoning about who else was in a meeting. Unlike the sibling CLIs' `whoami`,
// there is no remote user directory to query; the identity is the configured
// account, normalized the same way attendee self-matching uses it.
func newWhoamiCmd(s *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the configured account (your own identity)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := s.cfg()
			return s.emit(whoamiOut{
				Server:     cfg.BaseURL,
				Username:   config.NormalizeUsername(cfg.Auth.Username),
				Scheme:     cfg.Auth.Scheme,
				Configured: cfg.Auth.Username != "",
			})
		},
	}
}
