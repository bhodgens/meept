package builtin

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/browser"
	"github.com/caimlas/meept/internal/security/ssrf"
	"github.com/caimlas/meept/internal/tools"
)

func browserFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Tool Fixture</title></head>
<body><h1 class="hd">hi</h1><form><input id="box"></form></body></html>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newBrowserTestManager(t *testing.T, srvURL string) *browser.Manager {
	t.Helper()
	g, err := ssrf.NewGuard(ssrf.GuardConfig{AllowedHosts: []string{hostOf(srvURL)}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := browser.NewManager(browser.Config{
		Enabled: true, Headless: true, MaxPages: 2,
	}, g, slog.Default())
	if err != nil {
		t.Skipf("chrome unavailable: %v", err)
	}
	t.Cleanup(m.Close)
	return m
}

func hostOf(raw string) string {
	i := strings.Index(raw, "://")
	rest := raw[i+3:]
	if j := strings.Index(rest, "/"); j >= 0 {
		rest = rest[:j]
	}
	if h, _, err := net.SplitHostPort(rest); err == nil {
		return h
	}
	return rest
}

func TestBrowserTools_DisabledManager_Nil(t *testing.T) {
	m, err := browser.NewManager(browser.Config{}, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if got := NewBrowserTools(m); got != nil {
		t.Errorf("NewBrowserTools(disabled) = %d tools, want nil", len(got))
	}
	if got := NewBrowserTools(nil); got != nil {
		t.Error("NewBrowserTools(nil) should be nil")
	}
}

func TestBrowserTools_Enabled_FullFamilyRegistered(t *testing.T) {
	srv := browserFixtureServer(t)
	m := newBrowserTestManager(t, srv.URL)
	got := NewBrowserTools(m)
	want := []string{
		"browser_navigate", "browser_click", "browser_type",
		"browser_read_text", "browser_screenshot", "browser_close",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d tools, want %d", len(got), len(want))
	}
	for i, n := range want {
		if got[i].Name() != n {
			t.Errorf("tool[%d] = %s, want %s", i, got[i].Name(), n)
		}
	}
}

func TestBrowserNavigateTool_ArgValidation(t *testing.T) {
	srv := browserFixtureServer(t)
	m := newBrowserTestManager(t, srv.URL)
	tool := NewBrowserNavigateTool(m)
	ctx := context.Background()

	if _, err := tool.Execute(ctx, map[string]any{}); err == nil {
		t.Error("missing url should error")
	}
	// javascript: scheme rejected by the manager's guard before launch.
	if _, err := tool.Execute(ctx, map[string]any{"url": "javascript:alert(1)"}); err == nil {
		t.Error("javascript: URL should be rejected")
	}
}

func TestBrowserClickTypeTools_ArgValidation(t *testing.T) {
	srv := browserFixtureServer(t)
	m := newBrowserTestManager(t, srv.URL)
	click := NewBrowserClickTool(m)
	typ := NewBrowserTypeTool(m)
	ctx := context.Background()

	if _, err := click.Execute(ctx, map[string]any{}); err == nil {
		t.Error("missing selector should error")
	}
	if _, err := typ.Execute(ctx, map[string]any{"selector": "#box"}); err == nil {
		t.Error("missing text should error")
	}
	if _, err := typ.Execute(ctx, map[string]any{"text": "x"}); err == nil {
		t.Error("missing selector should error")
	}
}

func TestBrowserTools_SessionScoping(t *testing.T) {
	srv := browserFixtureServer(t)
	m := newBrowserTestManager(t, srv.URL)
	ctx := context.Background()

	// Without a session ID in ctx everything lands on the shared
	// "default" session and works end-to-end.
	nav := NewBrowserNavigateTool(m)
	if _, err := nav.Execute(ctx, map[string]any{"url": srv.URL + "/"}); err != nil {
		t.Fatalf("navigate default session: %v", err)
	}
	read := NewBrowserReadTextTool(m)
	res, err := read.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	rm, ok := res.(*tools.ToolResult)
	if !ok || !rm.Success {
		t.Fatalf("read result type/success: %#v", res)
	}
	if txt, _ := rm.Result.(map[string]any)["text"].(string); !strings.Contains(txt, "hi") {
		t.Errorf("read text = %.100q, want fixture text", txt)
	}

	// Operations against an unopened scoped session fail cleanly.
	scoped := ContextWithSessionID(context.Background(), "sess-9")
	if _, err := read.Execute(scoped, map[string]any{}); err == nil {
		t.Error("read on unopened scoped session should error")
	}
	closeT := NewBrowserCloseTool(m)
	if _, err := closeT.Execute(scoped, map[string]any{}); err == nil {
		t.Error("close on unopened scoped session should error")
	}
}

func TestBrowserScreenshotTool_PNGEvidence(t *testing.T) {
	srv := browserFixtureServer(t)
	m := newBrowserTestManager(t, srv.URL)
	ctx := context.Background()

	if _, err := NewBrowserNavigateTool(m).Execute(ctx, map[string]any{"url": srv.URL + "/"}); err != nil {
		t.Fatal(err)
	}
	shot := NewBrowserScreenshotTool(m)
	res, err := shot.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	rm, ok := res.(*tools.ToolResult)
	if !ok || !rm.Success {
		t.Fatalf("screenshot result type/success: %#v", res)
	}
	data, _ := rm.Result.(map[string]any)["image"].(string)
	if !strings.HasPrefix(data, "data:image/png;base64,") {
		t.Fatalf("image not a png data url: %.60s", data)
	}
	if len(rm.Evidence) == 0 {
		t.Error("expected evidence attached to screenshot result")
	}
}
