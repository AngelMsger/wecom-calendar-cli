package config_test

// internal/auth registers the auth schemes that agent-cli-kit/reuse consults
// when the loader decides Config.ReuseStep. It imports this package, so only
// the external test package can import it; doing so registers the schemes for
// every test in this binary, as the CLI does.
import _ "github.com/angelmsger/wecom-calendar-cli/internal/auth"
