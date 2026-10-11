// Package transport provides a flavor-agnostic HTTP layer: a thin client that
// applies request decorators (auth, user-agent) and retries transient failures.
// The client lives in github.com/angelmsger/agent-cli-kit/transport, shared by
// every CLI in the family; this package re-exports it with
// wecom-calendar-cli's defaults: its User-Agent, the same-origin redirect
// guard, and retries for the read-only CalDAV verbs PROPFIND and REPORT.
//
// This package backs the wecom-calendar-cli command layer and is also
// importable as a library; see the repository README. The CLI relies on its
// retry, timeout and decorator behavior; change it additively and keep the
// defaults stable rather than tuning them for a single local call site.
package transport

import (
	kit "github.com/angelmsger/agent-cli-kit/transport"
	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
)

type (
	// Doer executes an HTTP request. *http.Client satisfies it; tests inject fakes.
	Doer = kit.Doer
	// Decorator mutates an outgoing request before it is sent (e.g. adds headers).
	Decorator = kit.Decorator
	// Client is a retrying HTTP client.
	Client = kit.Client
	// Options configures a Client.
	Options = kit.Options
)

// caldavReadMethods are the read-only CalDAV verbs retried alongside GET and
// HEAD. They never mutate server state; PUT, DELETE, POST and PROPPATCH are
// never retried, so a write is sent at most once.
var caldavReadMethods = []string{"PROPFIND", "REPORT"}

// New returns a Client with wecom-calendar-cli's defaults. Requests that carry
// no User-Agent get wecom-calendar-cli's. The default Doer refuses any redirect
// that leaves the first request's scheme, host and port, so Basic credentials
// never leave the configured server. PROPFIND and REPORT are retried on
// transient failures in addition to GET, HEAD and any opt.RetryMethods.
func New(opt Options) *Client {
	if opt.UserAgent == "" {
		opt.UserAgent = constants.UserAgent()
	}
	opt.SameOriginRedirects = true
	opt.RetryMethods = append(append([]string(nil), caldavReadMethods...), opt.RetryMethods...)
	return kit.New(opt)
}
