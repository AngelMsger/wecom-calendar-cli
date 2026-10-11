package config

import (
	"path/filepath"
	"strconv"

	kit "github.com/angelmsger/agent-cli-kit/config"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	_ "github.com/angelmsger/wecom-calendar-cli/pkg/errors" // installs WeCom Calendar's error guidance
)

// EnvNoContextHint opts out of the runtime multi-context notice.
const EnvNoContextHint = "WECOM_CALENDAR_CLI_NO_CONTEXT_HINT"

func init() {
	kit.Register(&kit.Schema{
		// Each row names a field's environment variable and built-in default.
		// Service fields are the team presets a setup load keeps; the WeCom
		// email and the CalDAV password are personal.
		Fields: []kit.Field{
			// The server has a default: the public WeCom CalDAV endpoint.
			{Key: fieldServer, Env: []string{"WECOM_CALENDAR_SERVER"}, Default: constants.DefaultServerURL, Service: true},
			{Key: fieldCredentialURL, Env: []string{"WECOM_CALENDAR_CREDENTIAL_URL"}, Service: true},
			{Key: fieldAuthScheme, Env: []string{"WECOM_CALENDAR_AUTH_SCHEME"}, Default: SchemeBasic, Service: true},
			{Key: fieldAuthUsername, Env: []string{"WECOM_CALENDAR_USERNAME"}},
			{Key: fieldFormat, Env: []string{"WECOM_CALENDAR_FORMAT"}, Default: constants.DefaultFormat, Service: true},
			{Key: fieldPageSize, Default: strconv.Itoa(constants.DefaultPageSize)},
			{Key: fieldTimeout, Default: constants.DefaultTimeout.String()},
			{Key: fieldMaxRetries, Default: strconv.Itoa(constants.DefaultMaxRetries)},
			{Key: fieldReadOnly, Env: []string{"WECOM_CALENDAR_CLI_READ_ONLY"}, Service: true},
			// A password in the environment implies the only scheme.
			{Key: fieldPassword, Env: []string{"WECOM_CALENDAR_PASSWORD"}, Implies: SchemeBasic},
		},
		Paths: kit.Paths{
			Dir:  filepath.Join(constants.ConfigParentDirName, constants.ConfigDirName),
			File: constants.ConfigFileName,
		},
		Words: kit.Words{
			App:          constants.AppName,
			ContextFlag:  "--use-context",
			ContextEnv:   "WECOM_CALENDAR_CONTEXT",
			NoHintEnv:    EnvNoContextHint,
			ListContexts: "config get-contexts",
			UseContext:   "config use-context",
			SetContext:   "config set-context",
			AuthGuide:    "auth guide",
			AuthLogin:    "auth login",
			SchemeHint:   "Use: basic. WeCom CalDAV accepts only HTTP Basic with the WeCom email and an app-specific CalDAV password.",
		},
		Schemes: []string{SchemeBasic},
		// No URL rules: a bare host is BAD_BASE_URL, and the path is kept.
		// No hooks: the default layer already supplies the only scheme.
	})
}
