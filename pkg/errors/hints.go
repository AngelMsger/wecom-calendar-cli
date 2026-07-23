package errors

// defaultGuidance returns the default hint and next-step commands for a
// category. Callers may override these via WithHint / WithNextSteps when more
// specific guidance is available.
func defaultGuidance(cat Category) (hint string, steps []string) {
	switch cat {
	case CategoryUsage:
		return "The command was invoked incorrectly. Check flags and arguments.",
			[]string{"wecom-calendar-cli <command> --help"}
	case CategoryConfig:
		return "No usable configuration was found or it is invalid.",
			[]string{"wecom-calendar-cli config init", "wecom-calendar-cli config show --explain"}
	case CategoryAuth:
		return "The server rejected the credentials. The app-specific password may have been refreshed.",
			[]string{"wecom-calendar-cli auth status", "wecom-calendar-cli auth login"}
	case CategoryPermission:
		return "The credentials are valid but lack access to this calendar or resource.",
			[]string{"Verify the account can see the calendar in the WeCom client."}
	case CategoryNotFound:
		return "The requested calendar or event does not exist in the local store.",
			[]string{"wecom-calendar-cli sync", "wecom-calendar-cli calendar list"}
	case CategoryConflict:
		return "The resource changed since it was last read.",
			[]string{"Re-run wecom-calendar-cli sync, then retry."}
	case CategoryRateLimit:
		return "The server is rate limiting requests. Retry after a short wait.",
			[]string{"Wait and retry; narrow --since/--until or sync one --calendar at a time."}
	case CategoryNetwork:
		return "The CalDAV server could not be reached (DNS, TLS or timeout).",
			[]string{"wecom-calendar-cli doctor", "Check --base-url and network connectivity."}
	case CategoryServer:
		return "The CalDAV server returned an error.",
			[]string{"Retry later.", "wecom-calendar-cli doctor"}
	case CategoryParse:
		return "A response or an iCalendar resource could not be parsed.",
			[]string{"Retry with --verbose to inspect the raw exchange."}
	default:
		return "An unexpected internal error occurred.",
			[]string{"Retry with --verbose for details."}
	}
}
