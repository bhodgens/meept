//go:build e2e

// Package toolswebssrf covers the SSRF guard on the web tools through REAL
// agent turns. The sandbox daemon boots with [security.ssrf] enabled (the
// config default the harness meept.json5/models.json5 never override), so the
// centralized guard is installed on web_fetch/web_search and every loopback
// target must be refused BEFORE any dial — proven here with a live local
// httptest server and a zero-hit counter.
//
// Coverage map (manifest scenarios):
//
//	tools-web-ssrf-01  web_fetch to 127.0.0.1 blocked, zero hits  — TestWebFetchLoopbackBlockedWithZeroHits
//	tools-web-ssrf-02  private-range fetch returns page text      — deferred variant (t.Skip; the shipped default guard blocks private ranges — the observable default behavior is covered by the private-IP literal refusal below)
//	tools-web-ssrf-03  web_search stubbed provider results        — deferred (t.Skip; backend URL is hardwired to DuckDuckGo, not stubbable without network)
package toolswebssrf

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// toolResultTexts collects the content of every role=tool message the daemon
// has ever sent back to the fake LLM.
func toolResultTexts(f *harness.FakeLLM) []string {
	var out []string
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role == "tool" {
				if c, _ := msg["content"].(string); c != "" {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

func toolResultsJoined(f *harness.FakeLLM) string {
	return strings.Join(toolResultTexts(f), "\n")
}

// mkChatSession boots a stack and binds a session; the caller pins the fake
// classifier to intent=chat so the turn runs the chat lane (the chat agent
// holds the web_fetch/web_search grants).
func mkChatSession(t *testing.T, name string) *harness.Stack {
	t.Helper()
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	_ = s.CreateSession(t, name, s.ProjectDir)
	return s
}

// tools-web-ssrf-01: web_fetch to a live loopback httptest server is blocked
// by the SSRF guard with zero server hits, and the refusal text is what the
// model sees.
func TestWebFetchLoopbackBlockedWithZeroHits(t *testing.T) {
	s := mkChatSession(t, "ssrf-loopback")
	sessionID := s.CreateSession(t, "ssrf-loopback", s.ProjectDir)

	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("secret loopback page"))
	}))
	defer target.Close()

	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "web_fetch",
		Arguments: `{"url":"` + target.URL + `/admin"}`,
	})

	s.ChatTurn(t, sessionID,
		"Fetch the contents of "+target.URL+"/admin and tell me what the page says",
		120*time.Second)

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, "web_fetch blocked") {
		t.Fatalf("web_fetch refusal text missing; results:\n%s", res)
	}
	if !strings.Contains(res, "127.0.0.1") {
		t.Fatalf("refusal text does not name the loopback target; results:\n%s", res)
	}
	if !strings.Contains(res, "loopback") && !strings.Contains(res, "blocked") {
		t.Fatalf("refusal text missing the blocked reason; results:\n%s", res)
	}
	if !strings.Contains(res, `"success":false`) {
		t.Fatalf("refusal must be a failure envelope; results:\n%s", res)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("loopback server recorded %d hits; the SSRF guard must refuse before any dial", got)
	}
}

// Defense-in-depth companion to scenario 01: a literal PRIVATE-range IP (no
// DNS involved) is refused by the same guard.
func TestWebFetchPrivateIPLiteralBlocked(t *testing.T) {
	s := mkChatSession(t, "ssrf-private")
	sessionID := s.CreateSession(t, "ssrf-private", s.ProjectDir)

	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "web_fetch",
		Arguments: `{"url":"http://192.168.1.1/router-admin"}`,
	})

	s.ChatTurn(t, sessionID,
		"Fetch the contents of http://192.168.1.1/router-admin and summarize the page for me",
		120*time.Second)

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, "web_fetch blocked") || !strings.Contains(res, "192.168.1.1") {
		t.Fatalf("private-range literal not refused; results:\n%s", res)
	}
}

// tools-web-ssrf-02 (manifest wording: "web_fetch to a private-range local
// test server returns page text"). DEFERRED: the shipped default guard blocks
// private ranges, and the harness-written meept.json5/models.json5 leave
// [security.ssrf] at its enabled default with no allowed_cidrs seam, so the
// happy-path fetch to a local test server cannot run without a config change
// the harness does not expose.
func TestWebFetchPrivateTestServerReturnsPageText(t *testing.T) {
	t.Skip("deferred: the default [security.ssrf] guard (enabled, no allowed_cidrs " +
		"in the harness-written configs) refuses private-range fetches by design; " +
		"the positive path needs an allowed_cidrs override the harness does not " +
		"currently expose. The default-refusal behavior is covered by " +
		"TestWebFetchLoopbackBlockedWithZeroHits and TestWebFetchPrivateIPLiteralBlocked.")
}

// tools-web-ssrf-03: web_search returns provider results into the tool
// envelope. DEFERRED: the DuckDuckGo backend URL is hardwired in the tool;
// a hermetic stub would need a search-provider seam (the MCP searxng
// provider) or real network access, neither available in the sandbox.
func TestWebSearchReturnsProviderResults(t *testing.T) {
	t.Skip("deferred: web_search's backend (html.duckduckgo.com / lite endpoint) " +
		"is hardwired; the hermetic sandbox cannot stub it without a search-provider " +
		"seam (MCP searxng) or network access. Requires a provider seam to cover e2e.")
}
