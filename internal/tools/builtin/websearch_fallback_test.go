package builtin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The html.duckduckgo.com endpoint now answers scripted requests with an
// anti-bot challenge page (HTTP 202 + challenge HTML) instead of results.
// The tool must (a) fall back to the lite endpoint, which still serves
// parseable results (verified live 2026-09-15: 200, ~10 result anchors),
// and (b) if both endpoints challenge, produce an honest "blocked by
// anti-bot challenge" error rather than one agents misread as a
// network/transport fault.

func liteHTMLPage() string {
	return `<!DOCTYPE html><html><body>
	<table>
	<tr><td><a rel="nofollow" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fflaky&rut=abc">Flaky test - Wikipedia</a></td></tr>
	<tr><td class="result-snippet">A flaky test passes and fails without changes. Time and ordering issues are common causes.</td></tr>
	</table></body></html>`
}

func newSweepSearchTool(t *testing.T, htmlStatus int) (*WebSearchTool, *httptest.Server) {
	t.Helper()
	var stage int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stage++
		if strings.Contains(r.URL.Path, "/html/") || stage == 1 {
			// primary endpoint: anti-bot challenge
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("<html><body>Anomaly detected.</body></html>"))
			return
		}
		// lite endpoint: real results
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(liteHTMLPage()))
	}))
	t.Cleanup(srv.Close)

	tm := &WebSearchTool{
		timeout: DefaultSearchTimeout,
		client:  srv.Client(),
	}
	return tm, srv
}

func __searchCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestWebSearch_202ChallengeFallsBackToLite(t *testing.T) {
	tm, srv := newSweepSearchTool(t, http.StatusAccepted)
	// The search URLs are hardcoded to duckduckgo.com; route them to the
	// test server's html/lite paths.
	srvHost := strings.TrimPrefix(srv.URL, "http://")
	tm.client.Transport = &rewriteTransport{host: srvHost, base: http.DefaultTransport}

	res, err := tm.Execute(__searchCtx(t), map[string]any{
		"query": "flaky tests",
	})
	if err != nil {
		t.Fatalf("lite fallback should succeed: %v", err)
	}
	sr, ok := res.(SearchResults)
	if !ok {
		t.Fatalf("result type = %T", res)
	}
	if len(sr.Results) == 0 {
		t.Fatal("lite fallback should return results")
	}
	if sr.Results[0].URL != "https://example.com/flaky" {
		t.Errorf("URL = %q", sr.Results[0].URL)
	}
	if !strings.Contains(sr.Results[0].Title, "Flaky") {
		t.Errorf("Title = %q", sr.Results[0].Title)
	}
}

func TestWebSearch_BothEndpointsChallengeHonestError(t *testing.T) {
	var stage int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stage++
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("<html>Anomaly detected.</html>"))
	}))
	defer srv.Close()

	tm := &WebSearchTool{
		timeout: DefaultSearchTimeout,
		client:  srv.Client(),
	}
	tm.client.Transport = &rewriteTransport{
		host: strings.TrimPrefix(srv.URL, "http://"),
		base: http.DefaultTransport,
	}

	_, err := tm.Execute(__searchCtx(t), map[string]any{"query": "flaky tests"})
	if err == nil {
		t.Fatal("expected honest error when both endpoints challenge")
	}
	msg := err.Error()
	if !strings.Contains(msg, "challenge") {
		t.Errorf("error should name the anti-bot challenge: %s", msg)
	}
	if strings.Contains(strings.ToLower(msg), "transport") {
		t.Errorf("error must not imply a transport fault: %s", msg)
	}
}

// rewriteTransport rewrites outbound requests to point at the test server
// while preserving the original URL path (so /html/ and /lite/ differ).
type rewriteTransport struct {
	host string
	base http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme = "http"
	out.URL.Host = rt.host
	return rt.base.RoundTrip(out)
}
