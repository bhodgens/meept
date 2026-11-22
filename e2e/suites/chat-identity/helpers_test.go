//go:build e2e

// Local helpers for the chat-identity suite: harness package is not to be
// extended by wave branches, so this suite carries its own shims.
package chatidentity

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// insecureClient is the suite-local self-signed-TLS HTTP client.
func insecureClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // scratch cert
		},
	}
}

// looksLikeSessionID reports whether id has the primary session id shape.
func looksLikeSessionID(id string) bool {
	return strings.HasPrefix(id, "session-")
}

// newSlowServer starts a test-local HTTP server that stalls before
// answering, keeping a web_fetch tool call in flight. Closed via done.
func newSlowServer(done chan struct{}, stall time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-time.After(stall):
		case <-done:
		}
		_, _ = w.Write([]byte("slow page finally answered"))
	}))
}

// waitSteerTextInRequests polls the fake-LLM request log until some
// completion request's messages mention steerText (the drained queue
// injection rides the conversation as a USER message).
func waitSteerTextInRequests(t *testing.T, s *harness.Stack, steerText string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if requestsMention(s, steerText) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("steer text %q never reached the conversation within %s (requests: %d)",
		steerText, timeout, s.Fake.RequestCount())
}

// waitRequestMentions polls the fake-LLM request log until any request
// mentions substr.
func waitRequestMentions(t *testing.T, s *harness.Stack, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if requestsMention(s, substr) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("no LLM request mentioned %q within %s (requests: %d)",
		substr, timeout, s.Fake.RequestCount())
}

// requestsMention renders every recorded request's messages compactly and
// reports whether substr appears.
func requestsMention(s *harness.Stack, substr string) bool {
	for _, req := range s.Fake.Requests() {
		raw, err := json.Marshal(req)
		if err != nil {
			continue
		}
		if strings.Contains(string(raw), substr) {
			return true
		}
	}
	return false
}
