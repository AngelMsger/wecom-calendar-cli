package transport

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
)

// Options configures a Client.
type Options struct {
	// Doer executes requests. Defaults to an *http.Client with Timeout.
	Doer Doer
	// Timeout is the per-request timeout when Doer is nil.
	Timeout time.Duration
	// MaxRetries is the number of additional attempts after the first failure.
	MaxRetries int
	// RetryBaseDelay is the base backoff delay; the Nth retry waits N*base.
	RetryBaseDelay time.Duration
	// Decorators are applied to every request in order.
	Decorators []Decorator
	// Verbose, when non-nil, receives a one-line log of every request's method,
	// URL and status (used by --verbose). Headers and bodies are never logged.
	Verbose io.Writer
}

// Client is a retrying HTTP client. It is flavor-agnostic: callers build fully
// formed *http.Request values and Client only adds decorators and retries.
type Client struct {
	doer       Doer
	maxRetries int
	baseDelay  time.Duration
	decorators []Decorator
}

// New builds a Client from Options, filling defaults.
func New(opt Options) *Client {
	c := &Client{
		doer:       opt.Doer,
		maxRetries: opt.MaxRetries,
		baseDelay:  opt.RetryBaseDelay,
		decorators: opt.Decorators,
	}
	if c.doer == nil {
		timeout := opt.Timeout
		if timeout == 0 {
			timeout = constants.DefaultTimeout
		}
		c.doer = &http.Client{Timeout: timeout, CheckRedirect: sameOriginRedirect}
	}
	if opt.Verbose != nil {
		c.doer = &loggingDoer{inner: c.doer, w: opt.Verbose}
	}
	if c.baseDelay == 0 {
		c.baseDelay = 500 * time.Millisecond
	}
	return c
}

// sameOriginRedirect refuses any redirect that leaves the first hop's origin.
// The default http.Client would follow redirects and, because Go's own
// same-domain check ignores scheme and port (and permits subdomains), could
// carry the Authorization header to an HTTP-downgraded, different-port, or
// subdomain target. Requiring an exact scheme+host(+port) match on every hop
// keeps Basic credentials on the configured server. req.URL.Host includes the
// port, so a port change is caught too.
func sameOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	origin := via[0].URL
	if req.URL.Scheme != origin.Scheme || !strings.EqualFold(req.URL.Host, origin.Host) {
		return fmt.Errorf("refusing cross-origin redirect from %s://%s to %s://%s",
			origin.Scheme, origin.Host, req.URL.Scheme, req.URL.Host)
	}
	return nil
}

// Do sends req, applying decorators and retrying transient failures. The
// returned response (if any) has a readable, non-nil Body that the caller must
// close. The context bounds the whole retry sequence.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	req = req.WithContext(ctx)
	for _, d := range c.decorators {
		d(req)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", constants.UserAgent())
	}
	return c.doWithRetry(ctx, req)
}
