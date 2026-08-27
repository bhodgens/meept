package ssrf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// staticResolver returns a ResolverFunc that maps every hostname to the given
// IPs, so tests never touch real DNS.
func staticResolver(t *testing.T, ips ...string) ResolverFunc {
	t.Helper()
	parsed := make([]net.IP, 0, len(ips))
	for _, s := range ips {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("staticResolver: bad test IP %q", s)
		}
		parsed = append(parsed, ip)
	}
	return func(ctx context.Context, host string) ([]net.IP, error) { return parsed, nil }
}

// errResolver returns a ResolverFunc that always fails.
func errResolver(msg string) ResolverFunc {
	return func(ctx context.Context, host string) ([]net.IP, error) {
		return nil, errors.New(msg)
	}
}

func mustGuard(t *testing.T, cfg GuardConfig) *Guard {
	t.Helper()
	g, err := NewGuard(cfg)
	if err != nil {
		t.Fatalf("NewGuard(%+v): %v", cfg, err)
	}
	return g
}

func TestNewGuard_InvalidConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  GuardConfig
	}{
		{"bad blocked CIDR", GuardConfig{BlockedCIDRs: []string{"not-a-cidr"}}},
		{"bad allowed CIDR", GuardConfig{AllowedCIDRs: []string{"10.0.0.0/99"}}},
		{"negative max redirects", GuardConfig{MaxRedirects: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewGuard(tc.cfg); err == nil {
				t.Fatalf("NewGuard(%+v) succeeded, want error", tc.cfg)
			}
		})
	}
}

func TestCheckURL_SchemeDenied(t *testing.T) {
	g := mustGuard(t, GuardConfig{})
	for _, raw := range []string{
		"ftp://example.com/file",
		"file:///etc/passwd",
		"gopher://example.com/",
		"javascript:alert(1)",
	} {
		err := g.CheckURL(raw)
		if err == nil {
			t.Errorf("CheckURL(%q) accepted, want scheme error", raw)
			continue
		}
		if !errors.Is(err, ErrSchemeNotAllowed) {
			t.Errorf("CheckURL(%q) error = %v, want ErrSchemeNotAllowed", raw, err)
		}
	}
}

func TestCheckURL_LiteralIPPrivateDenied(t *testing.T) {
	g := mustGuard(t, GuardConfig{})
	blocked := []string{
		"http://127.0.0.1/admin",         // loopback
		"http://10.0.0.1/internal",       // private
		"http://172.16.5.5/x",            // private
		"http://192.168.1.1/",            // private
		"http://169.254.169.254/latest/", // cloud metadata
		"http://0.0.0.0/",                // unspecified
		"http://224.0.0.1/",              // multicast
		"http://[::1]/",                  // IPv6 loopback
		"http://[fd00::1]/",              // IPv6 ULA (fc00::/7)
		"http://[fe80::1]/",              // IPv6 link-local
		"http://[ff02::1]/",              // IPv6 multicast
		"https://127.0.0.1:8443/secure",  // loopback with port
	}
	for _, raw := range blocked {
		err := g.CheckURL(raw)
		if err == nil {
			t.Errorf("CheckURL(%q) accepted, want blocked", raw)
			continue
		}
		if !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("CheckURL(%q) error = %v, want ErrBlockedAddress", raw, err)
		}
	}

	allowed := []string{
		"http://8.8.8.8/",
		"https://1.1.1.1:443/",
		"http://[2001:4860:4860::8888]/",
	}
	for _, raw := range allowed {
		if err := g.CheckURL(raw); err != nil {
			t.Errorf("CheckURL(%q) = %v, want nil", raw, err)
		}
	}
}

func TestCheckURL_HostnameResolvingPrivateDenied(t *testing.T) {
	g := mustGuard(t, GuardConfig{})
	g = g.WithResolver(staticResolver(t, "127.0.0.1"))
	if err := g.CheckURL("http://evil.example/"); err == nil {
		t.Fatal("CheckURL accepted hostname resolving to loopback")
	} else if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("error = %v, want ErrBlockedAddress", err)
	}

	// Every resolved IP must pass: one private record among public ones is enough to deny.
	g = mustGuard(t, GuardConfig{})
	g = g.WithResolver(staticResolver(t, "8.8.8.8", "10.0.0.1"))
	if err := g.CheckURL("http://mixed.example/"); err == nil {
		t.Fatal("CheckURL accepted hostname with one private A record")
	}

	// All-public resolution passes without touching real DNS.
	g = mustGuard(t, GuardConfig{})
	g = g.WithResolver(staticResolver(t, "93.184.216.34"))
	if err := g.CheckURL("https://example.com/"); err != nil {
		t.Fatalf("CheckURL rejected hostname resolving to public IP: %v", err)
	}
}

func TestCheckURL_AllowedHostsBypass(t *testing.T) {
	g := mustGuard(t, GuardConfig{AllowedHosts: []string{"trusted.example"}})
	g = g.WithResolver(staticResolver(t, "127.0.0.1"))

	if err := g.CheckURL("http://trusted.example/"); err != nil {
		t.Fatalf("allowed host denied: %v", err)
	}
	if err := g.CheckURL("http://api.trusted.example/v1"); err != nil {
		t.Fatalf("subdomain of allowed host denied: %v", err)
	}
	if err := g.CheckURL("http://TRUSTED.EXAMPLE/case"); err != nil {
		t.Fatalf("case-insensitive match failed: %v", err)
	}
	if err := g.CheckURL("http://untrusted.example/"); err == nil {
		t.Fatal("non-allowed host bypassed IP checks")
	}
	// Suffix must be dot-delimited: "eviltrusted.example" is not "trusted.example".
	if err := g.CheckURL("http://eviltrusted.example/"); err == nil {
		t.Fatal("suffix match without dot boundary bypassed IP checks")
	}
}

func TestCheckURL_AllowedCIDRs(t *testing.T) {
	g := mustGuard(t, GuardConfig{AllowedCIDRs: []string{"127.0.0.0/8"}})
	if err := g.CheckURL("http://127.0.0.1:8080/"); err != nil {
		t.Fatalf("IP inside allowed CIDR denied: %v", err)
	}
	if err := g.CheckURL("http://10.0.0.1/"); err == nil {
		t.Fatal("IP outside allowed CIDR and inside default blocklist accepted")
	}
}

func TestCheckURL_CustomBlockedCIDRsReplaceDefaults(t *testing.T) {
	g := mustGuard(t, GuardConfig{BlockedCIDRs: []string{"8.8.8.0/24"}})
	if err := g.CheckURL("http://8.8.8.8/"); err == nil {
		t.Fatal("IP in custom blocked CIDR accepted")
	}
	// An explicit BlockedCIDRs list fully replaces the defaults.
	if err := g.CheckURL("http://10.0.0.1/"); err != nil {
		t.Fatalf("IP no longer covered after replacing defaults denied: %v", err)
	}
}

func TestCheckURL_MissingHost(t *testing.T) {
	g := mustGuard(t, GuardConfig{})
	err := g.CheckURL("http:///no-host")
	if err == nil {
		t.Fatal("CheckURL accepted URL without host")
	}
	if !errors.Is(err, ErrMissingHost) {
		t.Fatalf("error = %v, want ErrMissingHost", err)
	}
}

func TestCheckURL_ResolverError(t *testing.T) {
	g := mustGuard(t, GuardConfig{})
	g = g.WithResolver(errResolver("dns boom"))
	err := g.CheckURL("http://broken.example/")
	if err == nil {
		t.Fatal("CheckURL accepted host whose resolution failed")
	}
	if !strings.Contains(err.Error(), "dns boom") {
		t.Fatalf("error %q does not wrap resolver error", err)
	}
}

func TestCheckRedirect_HopToPrivateDenied(t *testing.T) {
	g := mustGuard(t, GuardConfig{})
	req := httptest.NewRequest(http.MethodGet, "http://10.0.0.1/internal", nil)
	via := []*http.Request{httptest.NewRequest(http.MethodGet, "http://8.8.8.8/start", nil)}
	err := g.CheckRedirect(req, via)
	if err == nil {
		t.Fatal("redirect hop to private IP accepted")
	}
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("error = %v, want ErrBlockedAddress", err)
	}
}

func TestCheckRedirect_MaxRedirects(t *testing.T) {
	g := mustGuard(t, GuardConfig{MaxRedirects: 2})

	pub := httptest.NewRequest(http.MethodGet, "http://8.8.8.8/next", nil)
	one := []*http.Request{httptest.NewRequest(http.MethodGet, "http://8.8.8.8/start", nil)}
	if err := g.CheckRedirect(pub, one); err != nil {
		t.Fatalf("redirect within limit denied: %v", err)
	}

	two := append(one, httptest.NewRequest(http.MethodGet, "http://8.8.8.8/mid", nil))
	err := g.CheckRedirect(pub, two)
	if err == nil {
		t.Fatal("redirect beyond MaxRedirects accepted")
	}
	if !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("error = %v, want ErrTooManyRedirects", err)
	}
}

func TestDefaultGuard_MaxRedirectsIsFive(t *testing.T) {
	g := DefaultGuard()
	if g == nil {
		t.Fatal("DefaultGuard returned nil")
	}
	pub := httptest.NewRequest(http.MethodGet, "http://8.8.8.8/next", nil)
	var via []*http.Request
	for i := 0; i < 4; i++ {
		via = append(via, httptest.NewRequest(http.MethodGet, fmt.Sprintf("http://8.8.8.8/hop%d", i), nil))
		if err := g.CheckRedirect(pub, via); err != nil {
			t.Fatalf("hop %d within default limit denied: %v", i+1, err)
		}
	}
	via = append(via, httptest.NewRequest(http.MethodGet, "http://8.8.8.8/hop5", nil))
	if err := g.CheckRedirect(pub, via); !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("5th redirect with default MaxRedirects: error = %v, want ErrTooManyRedirects", err)
	}
}

func TestWrapClient_SetsCheckRedirectPreservesTimeout(t *testing.T) {
	g := mustGuard(t, GuardConfig{})
	c := &http.Client{Timeout: 3 * time.Second}
	got := g.WrapClient(c)
	if got != c {
		t.Fatal("WrapClient returned a different client")
	}
	if got.CheckRedirect == nil {
		t.Fatal("WrapClient did not set CheckRedirect")
	}
	if got.Timeout != 3*time.Second {
		t.Fatalf("WrapClient changed Timeout: got %v", got.Timeout)
	}
	// Nil client gets a usable fresh one.
	fresh := g.WrapClient(nil)
	if fresh == nil || fresh.CheckRedirect == nil {
		t.Fatal("WrapClient(nil) did not produce a guarded client")
	}
}

func TestWrapClient_RedirectChainEndToEnd(t *testing.T) {
	// The test server binds 127.0.0.1, so allow loopback explicitly; the
	// redirect target 10.0.0.1 must still be denied on hop 2.
	g := mustGuard(t, GuardConfig{AllowedCIDRs: []string{"127.0.0.0/8"}})
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dest", http.StatusFound)
	})
	mux.HandleFunc("/dest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "arrived")
	})
	mux.HandleFunc("/bad", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/internal", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := g.WrapClient(&http.Client{Timeout: 5 * time.Second})

	resp, err := client.Get(srv.URL + "/ok")
	if err != nil {
		t.Fatalf("benign redirect chain failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "arrived") {
		t.Fatalf("unexpected body %q", body)
	}

	_, err = client.Get(srv.URL + "/bad")
	if err == nil {
		t.Fatal("redirect chain to private IP succeeded")
	}
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("error = %v, want ErrBlockedAddress", err)
	}
}

func TestWrapClient_TooManyRedirectsEndToEnd(t *testing.T) {
	g := mustGuard(t, GuardConfig{
		AllowedCIDRs: []string{"127.0.0.0/8"},
		MaxRedirects: 1,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/b", http.StatusFound)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "should never arrive")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := g.WrapClient(&http.Client{Timeout: 5 * time.Second})
	_, err := client.Get(srv.URL + "/a")
	if err == nil {
		t.Fatal("redirect beyond MaxRedirects=1 succeeded")
	}
	if !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("error = %v, want ErrTooManyRedirects", err)
	}
}

func TestDialContext_BlocksAtDialTime(t *testing.T) {
	// Hostname path: injected resolver maps a fake host to loopback; the dial
	// must be refused before any connection attempt (no real DNS involved).
	g := mustGuard(t, GuardConfig{})
	g = g.WithResolver(staticResolver(t, "127.0.0.1"))
	client := g.WrapClient(&http.Client{Timeout: 2 * time.Second})
	_, err := client.Get("http://fake.internal.host:9/")
	if err == nil {
		t.Fatal("dial to hostname resolving to loopback succeeded")
	}
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("error = %v, want ErrBlockedAddress", err)
	}

	// Literal-IP path: default guard refuses loopback at dial time even when
	// no pre-flight CheckURL ran.
	client2 := DefaultGuard().WrapClient(&http.Client{Timeout: 2 * time.Second})
	_, err = client2.Get("http://127.0.0.1:9/")
	if err == nil {
		t.Fatal("dial to literal loopback succeeded")
	}
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("error = %v, want ErrBlockedAddress", err)
	}
}

func TestWithResolver_SharedByCheckURLAndDial(t *testing.T) {
	// Same injected resolver must drive both pre-flight CheckURL and the
	// dial-time re-check (no DNS leak through either path). The resolver maps
	// the fake host to loopback (allowed here via AllowedCIDRs) so the dial
	// attempt fails fast with connection refused instead of hitting the
	// network.
	var calls []string
	g := mustGuard(t, GuardConfig{AllowedCIDRs: []string{"127.0.0.0/8"}})
	g = g.WithResolver(func(ctx context.Context, host string) ([]net.IP, error) {
		calls = append(calls, host)
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	})
	if err := g.CheckURL("http://tracked.example/"); err != nil {
		t.Fatalf("CheckURL: %v", err)
	}
	dial := g.DialContext(nil)
	conn, err := dial(context.Background(), "tcp", "tracked.example:1")
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("dial to closed port unexpectedly succeeded")
	}
	if len(calls) < 2 || calls[0] != "tracked.example" || calls[1] != "tracked.example" {
		t.Fatalf("resolver not used by both paths: %v", calls)
	}
}
