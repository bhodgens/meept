package browser

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/security/ssrf"
	"github.com/chromedp/chromedp"
)

// chromeAvailable reports whether a Chrome/Chromium binary exists, mirroring
// the hasDocker skip idiom used by internal/runtime/docker_test.go.
func chromeAvailable() bool {
	if _, err := exec.LookPath("google-chrome"); err == nil {
		return true
	}
	for _, p := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	} {
		if _, err := exec.LookPath(p); err == nil {
			return true
		}
	}
	return discoverable()
}

func discoverable() bool {
	_, err := discoverChrome("")
	return err == nil
}

// fixtureHTML is deterministic content served by the local test server.
const fixtureHTML = `<!doctype html>
<html><head><title>Fixture Page</title></head>
<body>
<h1 class="headline">Hello Fixture</h1>
<form><input type="text" id="box" name="q"></form>
<a href="/redirect-target" id="redir">escape link</a>
<p id="para">fixture body text</p>
</body></html>`

// newFixtureServer starts a local httptest server serving fixture HTML plus a
// redirect endpoint that bounces to a private IP (redirect-escape target).
func newFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, fixtureHTML)
	})
	// Redirect to a private-range IP: the guard must block the final hop.
	privateIP := privateLoopbackTarget(t)
	mux.HandleFunc("/redirect-to-private", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, privateIP, http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// privateLoopbackTarget returns an http URL pointing at a loopback address
// that is NOT the test server itself (so the blocklist path is exercised).
func privateLoopbackTarget(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr + "/secret"
}

// testGuard builds a resolver-pinned guard that resolves every hostname to
// the httptest server's own loopback IP so CheckURL passes for it while all
// other private IPs stay blocked.
func testGuard(t *testing.T, srvURL string) *ssrf.Guard {
	t.Helper()
	u, err := url.Parse(srvURL)
	if err != nil {
		t.Fatalf("parse srv url: %v", err)
	}
	host := u.Hostname()
	g, err := ssrf.NewGuard(ssrf.GuardConfig{})
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	// Resolve only the fixture host to its real (loopback) IP; everything
	// else resolves normally. The fixture server is 127.0.0.1, which is in
	// the default blocklist, so allow-list it explicitly.
	g2, err := ssrf.NewGuard(ssrf.GuardConfig{
		AllowedHosts: []string{host},
	})
	if err != nil {
		t.Fatalf("guard2: %v", err)
	}
	_ = g
	return g2
}

func newTestManager(t *testing.T, srv *httptest.Server) *Manager {
	t.Helper()
	m, err := NewManager(Config{
		Enabled:  true,
		Headless: true,
		MaxPages: 3,
	}, testGuard(t, srv.URL), slog.Default())
	if err != nil {
		t.Skipf("chrome unavailable or manager init failed: %v", err)
	}
	t.Cleanup(m.Close)
	return m
}

func TestManager_Navigate_ReadText_Screenshot(t *testing.T) {
	srv := newFixtureServer(t)
	m := newTestManager(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	finalURL, title, err := m.Navigate(ctx, "sess-a", srv.URL+"/")
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if !strings.HasPrefix(finalURL, srv.URL) {
		t.Errorf("final url %q not under %s", finalURL, srv.URL)
	}
	if title != "Fixture Page" {
		t.Errorf("title = %q, want Fixture Page", title)
	}

	text, err := m.ReadText(ctx, "sess-a", "")
	if err != nil {
		t.Fatalf("read text: %v", err)
	}
	if !strings.Contains(text, "fixture body text") {
		t.Errorf("read text missing fixture body text; got %.200s", text)
	}

	png, err := m.Screenshot(ctx, "sess-a")
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	if len(png) < 8 ||
		png[0] != 0x89 || png[1] != 'P' || png[2] != 'N' || png[3] != 'G' {
		t.Fatalf("screenshot not a PNG (%d bytes)", len(png))
	}
}

func TestManager_Click_Type(t *testing.T) {
	srv := newFixtureServer(t)
	m := newTestManager(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, _, err := m.Navigate(ctx, "sess-b", srv.URL+"/"); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := m.Type(ctx, "sess-b", "#box", "hello"); err != nil {
		t.Fatalf("type: %v", err)
	}
	var val string
	s, ok := m.getSession("sess-b")
	if !ok {
		t.Fatal("session missing")
	}
	if err := chromedp.Run(s.ctx,
		chromedp.Value("#box", &val, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("eval value: %v", err)
	}
	if val != "hello" {
		t.Errorf("input value = %q, want hello", val)
	}
	if err := m.Click(ctx, "sess-b", ".headline"); err != nil {
		t.Errorf("click headline: %v", err)
	}
}

func TestManager_RedirectToPrivateIP_Blocked(t *testing.T) {
	srv := newFixtureServer(t)
	m := newTestManager(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, _, err := m.Navigate(ctx, "sess-c", srv.URL+"/redirect-to-private")
	if err == nil {
		t.Fatal("expected redirect-to-private to be blocked")
	}
	// Session must have been torn down after the escape attempt.
	if _, ok := m.getSession("sess-c"); ok {
		t.Error("session should be closed after blocked redirect")
	}
}

// TestManager_checkURLFinal_BlocksPrivateIP proves the open-redirect defense
// logic directly: a final location on a private IP is refused.
func TestManager_checkURLFinal_BlocksPrivateIP(t *testing.T) {
	srv := newFixtureServer(t)
	m := newTestManager(t, srv)

	err := m.checkURLFinal("http://10.1.2.3/secret")
	if err == nil {
		t.Fatal("expected private-IP final location to be blocked")
	}
	err = m.checkURLFinal("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("expected link-local metadata final location to be blocked")
	}
	if err := m.checkURLFinal(srv.URL + "/"); err != nil {
		t.Errorf("allowed fixture origin should pass, got %v", err)
	}
}

func TestManager_GuardDenialBeforeLaunch(t *testing.T) {
	srv := newFixtureServer(t)
	// Strict guard with no allowlist: even the loopback fixture host is
	// denied before any launch is attempted.
	g, err := ssrf.NewGuard(ssrf.GuardConfig{})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(Config{Enabled: true, Headless: true}, g, slog.Default())
	if err != nil {
		t.Skipf("chrome unavailable: %v", err)
	}
	t.Cleanup(mgr.Close)

	_, _, navErr := mgr.Navigate(context.Background(), "no-launch", srv.URL+"/")
	if navErr == nil {
		t.Fatal("expected guard denial for loopback fixture host")
	}
	// No session/chrome may have been launched.
	if _, ok := mgr.getSession("no-launch"); ok {
		t.Error("chrome launched despite guard denial")
	}
}

func TestNewManager_Disabled_Inert(t *testing.T) {
	m, err := NewManager(Config{}, ssrf.DefaultGuard(), slog.Default())
	if err != nil {
		t.Fatalf("disabled manager construction: %v", err)
	}
	if !m.Disabled() {
		t.Error("expected Disabled() true")
	}
	if _, _, err := m.Navigate(context.Background(), "x", "https://example.com"); err == nil {
		t.Error("expected ErrDisabled from Navigate")
	}
	if err := m.Click(context.Background(), "x", "a"); err == nil {
		t.Error("expected ErrDisabled from Click")
	}
	if _, err := m.ReadText(context.Background(), "x", ""); err == nil {
		t.Error("expected ErrDisabled from ReadText")
	}
	if _, err := m.Screenshot(context.Background(), "x"); err == nil {
		t.Error("expected ErrDisabled from Screenshot")
	}
}

func TestValidateSelector_RejectsNonCSS(t *testing.T) {
	for _, bad := range []string{"javascript:alert(1)", " javascript:x ", ""} {
		if err := validateSelector(bad); err == nil {
			t.Errorf("validateSelector(%q) = nil, want error", bad)
		}
	}
	for _, good := range []string{"#id", ".cls > p", "a.link:hover", "form input[name=q]"} {
		if err := validateSelector(good); err != nil {
			t.Errorf("validateSelector(%q) = %v, want nil", good, err)
		}
	}
}

func TestNewManager_Enabled_MissingBinary_Fails(t *testing.T) {
	_, err := NewManager(Config{Enabled: true, ChromePath: "/nonexistent/chrome-binary-xyz"}, nil, nil)
	if err == nil {
		t.Error("expected error when chrome binary missing and enabled=true")
	}
}

func TestManager_UnknownSession_OperationsFail(t *testing.T) {
	m := newTestManager(t, newFixtureServer(t))
	ctx := context.Background()
	if err := m.Click(ctx, "ghost", "#a"); err == nil {
		t.Error("click on unknown session should fail")
	}
	if err := m.CloseSession(ctx, "ghost"); err == nil {
		t.Error("close unknown session should fail")
	}
}

// TestChromeSkipHelpersAgree verifies the two environment-detection helpers
// stay consistent. This also keeps them referenced so U1000 does not fire
// on hosts where every chrome-gated test skips.
func TestChromeSkipHelpersAgree(t *testing.T) {
	hasChrome := chromeAvailable()
	if hasChrome != discoverable() {
		t.Errorf("chromeAvailable=%v disagrees with discoverable=%v", hasChrome, discoverable())
	}
}
