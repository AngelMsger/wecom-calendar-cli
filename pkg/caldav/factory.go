package caldav

import (
	"net/url"
	"strings"
	"time"

	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/angelmsger/wecom-calendar-cli/pkg/transport"
)

// BuildParams configures a CalDAV Client.
type BuildParams struct {
	// BaseURL is the CalDAV server root, e.g. https://caldav.wecom.work/.
	BaseURL string
	// Timeout bounds each request.
	Timeout time.Duration
	// MaxRetries is the retry budget for idempotent (read) requests.
	MaxRetries int
	// AuthDecorator injects the Authorization header. May be nil for anonymous
	// probes (which will fail against a real server).
	AuthDecorator transport.Decorator
}

// Build constructs a Client from params, deriving the request origin from the
// base URL and wiring the retrying transport with the auth decorator.
func Build(p BuildParams) (Client, error) {
	raw := strings.TrimSpace(p.BaseURL)
	if raw == "" {
		return nil, cerrors.New(cerrors.CategoryConfig, "NO_BASE_URL",
			"no CalDAV server URL is configured").
			WithNextSteps("wecom-calendar-cli config init")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, cerrors.Newf(cerrors.CategoryConfig, "BAD_BASE_URL",
			"invalid CalDAV server URL: %q", raw).
			WithNextSteps("wecom-calendar-cli config init")
	}

	var decorators []transport.Decorator
	if p.AuthDecorator != nil {
		decorators = append(decorators, p.AuthDecorator)
	}
	tc := transport.New(transport.Options{
		Timeout:    p.Timeout,
		MaxRetries: p.MaxRetries,
		Decorators: decorators,
	})
	return &apiClient{origin: u.Scheme + "://" + u.Host, http: tc}, nil
}
