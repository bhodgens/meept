package builtin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/security/ssrf"
	"github.com/caimlas/meept/internal/tools"
)

func TestWebFetchTool_SSRFGuardEnabledBlocksLoopbackStub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "secret internal data")
	}))
	defer srv.Close()

	tool := NewWebFetchTool(5*time.Second, 1000)
	g, err := ssrf.NewGuard(ssrf.GuardConfig{})
	if err != nil {
		t.Fatalf("NewGuard: %v", err)
	}
	tool.SetSSRFGuard(g)

	_, err = tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err == nil {
		t.Fatal("web_fetch against 127.0.0.1 stub succeeded with SSRF guard enabled")
	}
	if !errors.Is(err, ssrf.ErrBlockedAddress) {
		t.Fatalf("error = %v, want ssrf.ErrBlockedAddress", err)
	}
}

func TestWebFetchTool_SSRFGuardDisabledLegacyFetchWorks(t *testing.T) {
	// Disabled guard = legacy behavior: SetAllowPrivateRanges(true) permits
	// the 127.0.0.1 test stub, and no ssrf.Guard is installed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello from stub")
	}))
	defer srv.Close()

	tool := NewWebFetchTool(5*time.Second, 1000)
	tool.SetAllowPrivateRanges(true)

	res, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("legacy fetch to stub failed: %v", err)
	}
	tr, ok := res.(tools.ToolResult)
	if !ok {
		t.Fatalf("result type %T, want tools.ToolResult", res)
	}
	if !tr.Success {
		t.Fatal("legacy fetch reported failure")
	}
}

func TestWebFetchTool_SSRFGuardAllowedCIDRPermitsStub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "corp-internal ok")
	}))
	defer srv.Close()

	tool := NewWebFetchTool(5*time.Second, 1000)
	g, err := ssrf.NewGuard(ssrf.GuardConfig{AllowedCIDRs: []string{"127.0.0.0/8"}})
	if err != nil {
		t.Fatalf("NewGuard: %v", err)
	}
	tool.SetSSRFGuard(g)

	res, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("fetch with allowed CIDR failed: %v", err)
	}
	tr, ok := res.(tools.ToolResult)
	if !ok || !tr.Success {
		t.Fatalf("unexpected result %#v", res)
	}
}

func TestWebFetchTool_SetSSRFGuardNilIsIgnored(t *testing.T) {
	tool := NewWebFetchTool(5*time.Second, 1000)
	tool.SetSSRFGuard(nil) // typed-nil guard: must not install anything
	if tool.guardEnabled() {
		t.Fatal("SetSSRFGuard(nil) installed a guard")
	}
}

func TestWebFetchTool_SSRFGuardBlocksNonHTTPSchemes(t *testing.T) {
	tool := NewWebFetchTool(5*time.Second, 1000)
	g, err := ssrf.NewGuard(ssrf.GuardConfig{})
	if err != nil {
		t.Fatalf("NewGuard: %v", err)
	}
	tool.SetSSRFGuard(g)

	_, err = tool.Execute(context.Background(), map[string]any{"url": "ftp://example.com/x"})
	if err == nil {
		t.Fatal("ftp:// accepted with guard enabled")
	}
	if !errors.Is(err, ssrf.ErrSchemeNotAllowed) {
		t.Fatalf("error = %v, want ssrf.ErrSchemeNotAllowed", err)
	}
}

func TestWebSearchTool_SSRFGuardRevalidatesRedirects(t *testing.T) {
	tool := NewWebSearchTool(5 * time.Second)
	g, err := ssrf.NewGuard(ssrf.GuardConfig{})
	if err != nil {
		t.Fatalf("NewGuard: %v", err)
	}
	tool.SetSSRFGuard(g)

	tool.guardMu.Lock()
	client := tool.client
	tool.guardMu.Unlock()

	if client.CheckRedirect == nil {
		t.Fatal("search client has no CheckRedirect after SetSSRFGuard")
	}
	req := httptest.NewRequest(http.MethodGet, "http://10.0.0.1/internal", nil)
	via := []*http.Request{httptest.NewRequest(http.MethodGet, "https://html.duckduckgo.com/html/?q=x", nil)}
	err = client.CheckRedirect(req, via)
	if err == nil {
		t.Fatal("search client followed redirect to private IP")
	}
	if !errors.Is(err, ssrf.ErrBlockedAddress) {
		t.Fatalf("error = %v, want ssrf.ErrBlockedAddress", err)
	}
}

func TestWebSearchTool_SetSSRFGuardNilIsIgnored(t *testing.T) {
	tool := NewWebSearchTool(5 * time.Second)
	tool.SetSSRFGuard(nil)
	if tool.guardEnabled() {
		t.Fatal("SetSSRFGuard(nil) installed a guard")
	}
}
