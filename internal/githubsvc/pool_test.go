package githubsvc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-github/v93/github"
)

// NOTE: gate tests for the github-pr-ci tree (leaf 00T). This file is
// intentionally RED: NewClientPool, For, ErrNoToken and the go-github v93
// dependency do not exist yet — they land in leaf 01. A build failure on
// any of those symbols is the expected state until the implementation
// arrives.

// stubTokenSource is a test double for the TokenSource contract:
// Token(ctx) (string, error).
type stubTokenSource struct {
	tok string
	err error
}

func (s stubTokenSource) Token(ctx context.Context) (string, error) {
	return s.tok, s.err
}

// countingTransport records every request that passes through it and
// answers with a minimal successful JSON body so the go-github client can
// decode it.
type countingTransport struct {
	hits int64
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	atomic.AddInt64(&c.hits, 1)
	return &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"login":"octo"}`)),
		Request:    req,
	}, nil
}

// TestClientPool_BuildsAndReuses verifies the contract-1 cache: two For
// calls for the same owner/repo return the same *github.Client pointer.
func TestClientPool_BuildsAndReuses(t *testing.T) {
	ctx := context.Background()

	pool, err := NewClientPool(ClientPoolOptions{
		Tokens: stubTokenSource{tok: "ghs_testtoken"},
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	defer pool.Close()

	c1, err := pool.For(ctx, "octo", "cat")
	if err != nil {
		t.Fatalf("For(1): %v", err)
	}
	c2, err := pool.For(ctx, "octo", "cat")
	if err != nil {
		t.Fatalf("For(2): %v", err)
	}
	if c1 != c2 {
		t.Fatalf("expected the same *github.Client pointer for the same identity, got %p and %p", c1, c2)
	}
}

// TestClientPool_BaseURLOverride verifies the BaseURL seam: a pool built
// with BaseURL pointed at a fake GitHub server sends requests to that
// server, not to api.github.com.
func TestClientPool_BaseURLOverride(t *testing.T) {
	var fakeHit int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&fakeHit, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"octo"}`))
	}))
	defer srv.Close()

	pool, err := NewClientPool(ClientPoolOptions{
		Tokens:  stubTokenSource{tok: "ghs_testtoken"},
		BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	defer pool.Close()

	client, err := pool.For(context.Background(), "octo", "cat")
	if err != nil {
		t.Fatalf("For: %v", err)
	}

	// Drive a real request through the pool's client against the fake.
	_, _, err = client.Users.Get(context.Background(), "octo")
	if err != nil {
		t.Fatalf("Users.Get against fake server: %v", err)
	}
	if n := atomic.LoadInt64(&fakeHit); n == 0 {
		t.Fatalf("fake GitHub server was never hit: BaseURL override not in effect")
	}
}

// TestClientPool_HTTPClientPlumbed verifies that a custom *http.Client
// (the daemon's egress-policy transport seam) is used by the pool's
// clients: requests flow through the supplied RoundTripper.
func TestClientPool_HTTPClientPlumbed(t *testing.T) {
	transport := &countingTransport{}
	pool, err := NewClientPool(ClientPoolOptions{
		Tokens:     stubTokenSource{tok: "ghs_testtoken"},
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	defer pool.Close()

	client, err := pool.For(context.Background(), "octo", "cat")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	// The custom transport answers, so the request completes and the
	// assertion is that it reached our transport at all.
	if _, _, err := client.Users.Get(context.Background(), "octo"); err != nil {
		t.Fatalf("Users.Get via custom transport: %v", err)
	}

	if n := atomic.LoadInt64(&transport.hits); n == 0 {
		t.Fatalf("custom HTTPClient transport was never used: pool did not plumb the supplied client")
	}
}

// TestClientPool_ErrNoToken verifies fail-closed token resolution: a
// TokenSource whose error wraps ErrNoToken must surface that sentinel via
// errors.Is and must NOT create a client.
func TestClientPool_ErrNoToken(t *testing.T) {
	cause := errors.New("secret github_token missing")
	tokenErr := errors.Join(ErrNoToken, cause)

	pool, err := NewClientPool(ClientPoolOptions{
		Tokens: stubTokenSource{err: tokenErr},
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	defer pool.Close()

	_, err = pool.For(context.Background(), "octo", "cat")
	if err == nil {
		t.Fatalf("expected For to fail when the token source has no token")
	}
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("expected errors.Is(err, ErrNoToken), got: %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("expected the wrapped cause to be preserved, got: %v", err)
	}
	// The pool must not have created any client for this identity.
	if c, err := pool.For(context.Background(), "octo", "cat"); err == nil || c != nil {
		t.Fatalf("expected no client after ErrNoToken, got client=%v err=%v", c, err)
	}
}

// keep the go-github import referenced even where a test's failure happens
// earlier; it documents the pinned major version the pool is built on.
var _ = github.String
