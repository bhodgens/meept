package githubsvc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/google/go-github/v93/github"
)

// ClientPoolOptions configures a ClientPool.
type ClientPoolOptions struct {
	// Tokens resolves the GitHub API token for each request context.
	Tokens TokenSource
	// BaseURL overrides the GitHub API origin (GitHub Enterprise, or a
	// test fake server). Empty uses api.github.com.
	BaseURL string
	// HTTPClient is the egress-policy seam: every pooled client routes
	// through this *http.Client (its Transport). Nil uses the
	// go-github default.
	HTTPClient *http.Client
}

// ClientPool caches one *github.Client per resolved token so repeated
// calls for the same owner/repo share a single authenticated client.
type ClientPool struct {
	mu      sync.Mutex
	clients map[string]*github.Client
	opts    ClientPoolOptions
}

// NewClientPool validates opts and returns an empty pool. It errors when
// no TokenSource is configured: the pool is fail-closed and never
// constructs unauthenticated clients.
func NewClientPool(opts ClientPoolOptions) (*ClientPool, error) {
	if opts.Tokens == nil {
		return nil, fmt.Errorf("%w: no TokenSource configured", ErrNoToken)
	}
	return &ClientPool{
		clients: map[string]*github.Client{},
		opts:    opts,
	}, nil
}

// For returns a pooled *github.Client for owner/repo under ctx. The
// token is resolved via the pool's TokenSource; on any resolution error
// no client is created or cached and the error is returned unchanged
// (sentinels like ErrNoToken stay reachable via errors.Is). Two calls
// whose resolved token is unchanged return the same *github.Client
// pointer; a changed token rebuilds the pool.
func (p *ClientPool) For(ctx context.Context, owner, repo string) (*github.Client, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: nil ClientPool", ErrNoToken)
	}
	token, err := p.opts.Tokens.Token(ctx)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	cached, ok := p.clients[token]
	if ok {
		return cached, nil
	}

	client, err := p.buildClient(token)
	if err != nil {
		// No client created for this token; leave the cache untouched
		// so the next call retries from a clean slate.
		return nil, err
	}
	// Token changed: the old client carries the stale token and must
	// not be handed out, so start a fresh cache.
	p.clients = map[string]*github.Client{}
	p.clients[token] = client
	return client, nil
}

// buildClient constructs a *github.Client authenticated with token,
// honoring the pool's BaseURL and HTTPClient options.
func (p *ClientPool) buildClient(token string) (*github.Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("%w: resolved token is empty", ErrNoToken)
	}
	if err := validateBaseURL(p.opts.BaseURL); err != nil {
		return nil, err
	}
	var opts []github.ClientOptionsFunc
	if p.opts.HTTPClient != nil {
		opts = append(opts, github.WithHTTPClient(p.opts.HTTPClient))
	}
	opts = append(opts, github.WithAuthToken(token))
	if p.opts.BaseURL != "" {
		baseURL := p.opts.BaseURL
		// go-github's WithURLs validates the URL format and ensures a
		// trailing slash; a nil upload URL keeps the default.
		opts = append(opts, github.WithURLs(&baseURL, nil))
	}
	return github.NewClient(opts...)
}

// validateBaseURL rejects a misconfigured BaseURL early, before any
// client is constructed. A non-empty BaseURL must be an absolute
// http(s) URL; go-github appends a trailing slash when missing.
func validateBaseURL(baseURL string) error {
	if baseURL == "" {
		return nil
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("githubsvc: invalid BaseURL %q: %w", baseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("githubsvc: BaseURL %q must be an http(s) URL", baseURL)
	}
	if u.Host == "" {
		return fmt.Errorf("githubsvc: BaseURL %q must include a host", baseURL)
	}
	return nil
}

// Close releases the pooled clients. The pool is inert afterwards.
func (p *ClientPool) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients = map[string]*github.Client{}
}
