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
//	tools-web-ssrf-02  private-range fetch returns page text      — TestWebFetchPrivateTestServerReturnsPageText (allowed_cidrs overlay)
//	tools-web-ssrf-03  web_search stubbed provider results        — TestWebSearchReturnsProviderResults (harness.WithFakeSearch env-gated fake provider; hermetic, zero network)
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

// tools-web-ssrf-02: web_fetch to a private-range local test server returns
// page text. The sandbox boots with security.ssrf.allowed_cidrs allowing the
// loopback (the harness config-overlay seam), so the guard permits the dial
// while everything else stays default: the fetch must succeed and return the
// served page text into the tool envelope.
func TestWebFetchPrivateTestServerReturnsPageText(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"security.ssrf.allowed_cidrs": []string{"127.0.0.0/8"},
	}))
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "ssrf-allow", s.ProjectDir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("private-range page text zebracoral"))
	}))
	defer srv.Close()

	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "web_fetch",
		Arguments: `{"url":"` + srv.URL + `/page"}`,
	})

	s.ChatTurn(t, sessionID,
		"Fetch the contents of "+srv.URL+"/page and tell me what the page says",
		120*time.Second)

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, "private-range page text zebracoral") {
		t.Fatalf("web_fetch result missing the served page text; results:\n%s", res)
	}
	if strings.Contains(res, `"success":false`) {
		t.Fatalf("allowed_cidrs fetch must not be refused; results:\n%s", res)
	}
}

// tools-web-ssrf-03: web_search returns provider results into the tool
// envelope. The harness daemon boots with MEEPT_E2E_FAKE_SEARCH=1
// (harness.WithFakeSearch): daemon startup installs an in-memory fake
// SearchProvider (internal/tools/builtin/fake_search_provider.go) instead
// of the MCP/DuckDuckGo chain, so the query is served hermetically — the
// canned fake results must land in the tool output and a bogus-argument
// call must return a clean failure envelope, with zero network.
func TestWebSearchReturnsProviderResults(t *testing.T) {
	s := harness.Start(t, harness.WithFakeSearch())
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "websearch-fake", s.ProjectDir)

	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "web_search",
		Arguments: `{"query":"hermetic fake search probe","limit":10}`,
	})

	s.ChatTurn(t, sessionID,
		"Search the web for the hermetic fake search probe and summarize what you find",
		120*time.Second)

	res := toolResultsJoined(s.Fake)
	for _, want := range harness.FakeSearchResults() {
		if !strings.Contains(res, want.Title) {
			t.Fatalf("web_search tool result missing canned fake result %q; results:\n%s", want.Title, res)
		}
		if !strings.Contains(res, want.URL) {
			t.Fatalf("web_search tool result missing canned fake URL %q; results:\n%s", want.URL, res)
		}
	}
	if !strings.Contains(res, harness.FakeSearchSnippetMarker()) {
		t.Fatalf("web_search tool result missing the fake provider snippet marker; results:\n%s", res)
	}
	if !strings.Contains(res, `"success":true`) {
		t.Fatalf("fake-provider search must produce a success envelope; results:\n%s", res)
	}
	if strings.Contains(res, "duckduckgo") || strings.Contains(res, "anti-bot") {
		t.Fatalf("fake-provider search must never consult DuckDuckGo; results:\n%s", res)
	}
}

// tools-web-ssrf-03 companion: the provider error path returns cleanly
// into the tool envelope (a failure result the model can read), still with
// zero network. A bogus query shape (missing required query argument) must
// surface as a clean argument error, not a hang or a scrape attempt.
func TestWebSearchErrorPathReturnsCleanly(t *testing.T) {
	s := harness.Start(t, harness.WithFakeSearch())
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "websearch-error", s.ProjectDir)

	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "web_search",
		Arguments: `{}`,
	})

	s.ChatTurn(t, sessionID,
		"Run a web search for me with no particular query",
		120*time.Second)

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, `"success":false`) {
		t.Fatalf("missing-query web_search must return a failure envelope; results:\n%s", res)
	}
	// The schema validator (tools.ValidateToolArgs, run by the live
	// executor path BEFORE Execute — internal/agent/executor.go) must name
	// the missing argument in the failure envelope the model reads.
	// web_search declares query Required in its Parameters(), so the
	// ArgValidationError ("query is missing") always wins the race against
	// the tool's own "query is required" defense-in-depth check — the tool
	// body never runs. Only the validator message is accepted (L16b):
	// grading the fake provider's own error string would grade the fake,
	// not the gate.
	if !strings.Contains(res, "query is missing") {
		t.Fatalf("missing-query web_search must be rejected by the arg validator naming the query argument; results:\n%s", res)
	}
}
