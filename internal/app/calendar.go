package app

import (
	"github.com/spf13/cobra"
)

func newCalendarCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "List calendars",
	}
	cmd.AddCommand(newCalendarListCmd(s))
	return cmd
}

func newCalendarListCmd(s *appState) *cobra.Command {
	var refresh bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the calendars in the local store",
		Long: "List calendars from the local store, with id, display name and change\n" +
			"tag. Use --refresh to query the server directly instead.",
		Example: "  wecom-calendar-cli calendar list\n" +
			"  wecom-calendar-cli calendar list --refresh --format table",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if refresh {
				ctx, cancel := cmdContext(s)
				defer cancel()
				client, err := s.newClient(ctx)
				if err != nil {
					return err
				}
				cals, err := client.ListCalendars(ctx)
				if err != nil {
					return err
				}
				return s.emitList(cals, pageInfo{})
			}
			st, err := s.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			s.staleNotice(st)
			cals, err := st.Calendars()
			if err != nil {
				return err
			}
			return s.emitList(cals, pageInfo{})
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "query the server instead of the local store")
	return cmd
}
