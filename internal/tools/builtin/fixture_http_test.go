package builtin

import (
	"net/http"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/testhttp"
)

// boundedTransport gives a fixture's HTTP client the ephemeral-port retry plus
// tight idle-connection bounds, and drains pooled connections at cleanup.
//
// Why: a full `make test` sweep (-p 2) opens thousands of loopback
// connections. macOS only has 16,384 ephemeral ports and holds each in
// TIME_WAIT for 30s, so the range transiently saturates and the most
// socket-hungry package fails with `connect: can't assign requested address`
// through no fault of the code under test. Measured: 55 such failures under
// `make test`, zero under `-p 1`, zero when this package runs alone.
//
// Use it in any fixture that points a client at an httptest server. It changes
// no behaviour under test: the retry fires only on EADDRNOTAVAIL dial errors,
// and idle bounds only affect how fast keep-alive conns are recycled.
func boundedTransport(t *testing.T) *http.Transport {
	t.Helper()
	tr := testhttp.BoundedTransport()
	t.Cleanup(tr.CloseIdleConnections)
	return tr
}

// boundedHTTPClient returns an *http.Client with the bounded transport for a
// fixture that points a client pool at an httptest server. Pass it as
// githubsvc.ClientPoolOptions.HTTPClient. Cleanup drains its idle conns,
// because ClientPool.Close only drops its client map and never closes an
// injected client's pooled connections.
func boundedHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	c := &http.Client{Transport: boundedTransport(t)}
	return c
}

// allowPrivateBounded is the web_fetch fixture's loopback setup: it enables
// the private-range path (so the tool may dial an httptest server) and then
// wraps the resulting transport's dialer with the ephemeral-port retry and
// tight idle bounds. The retry re-enters the SSRF dialer on every attempt
// (testhttp.WrapDialer), so wrapping never bypasses the guard. Same-package
// access to tool.client keeps this test-only: web_fetch.go imports nothing
// from internal/testhttp.
func allowPrivateBounded(t *testing.T, tool *WebFetchTool) {
	t.Helper()
	tool.SetAllowPrivateRanges(true)
	tool.clientMu.Lock()
	defer tool.clientMu.Unlock()
	tr, ok := tool.client.Transport.(*http.Transport)
	if !ok || tr == nil {
		return
	}
	tr.DialContext = testhttp.WrapDialer(tr.DialContext)
	tr.MaxIdleConns = 4
	tr.MaxIdleConnsPerHost = 2
	tr.IdleConnTimeout = time.Second
	t.Cleanup(tr.CloseIdleConnections)
}
