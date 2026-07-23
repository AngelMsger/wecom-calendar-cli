// Package constants holds project-wide constants and build-time metadata.
package constants

import "time"

// Build-time metadata, injected via -ldflags. See Makefile.
var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

const (
	// AppName is the binary / command name.
	AppName = "wecom-calendar-cli"

	// EnvPrefix is the environment variable prefix for all settings.
	EnvPrefix = "WECOM_CALENDAR_"

	// ConfigParentDirName groups every angelmsger CLI's per-user config under
	// one shared $HOME-relative directory (~/.angelmsger).
	ConfigParentDirName = ".angelmsger"

	// ConfigDirName is the per-CLI config directory under ConfigParentDirName,
	// i.e. ~/.angelmsger/wecom-calendar.
	ConfigDirName = "wecom-calendar"

	// ConfigFileName is the YAML config file within ConfigDirName.
	ConfigFileName = "config.yaml"

	// DatabaseFileName is the local SQLite store, kept alongside the config file.
	DatabaseFileName = "calendar.db"

	// CredentialsFileName is the fallback secret store when no keychain is available.
	CredentialsFileName = "credentials"

	// KeychainService is the service name used for OS keychain entries.
	KeychainService = "wecom-calendar-cli"
)

// Defaults for runtime behaviour.
const (
	DefaultFormat     = "json"
	DefaultPageSize   = 100
	DefaultTimeout    = 30 * time.Second
	DefaultMaxRetries = 3
	// DefaultServerURL is the WeCom / Tencent Exmail CalDAV endpoint. The
	// calendar-home is the bare /calendar/ collection; the Basic-auth user
	// determines whose calendars are served.
	DefaultServerURL = "https://caldav.wecom.work/"
	// StaleAfter is how long since the last successful sync before read
	// commands emit a staleness notice on stderr.
	StaleAfter = 24 * time.Hour
)

// UserAgent identifies the CLI to the CalDAV server.
func UserAgent() string {
	return AppName + "/" + Version
}
