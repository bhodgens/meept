package secrets

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Task 1: helpers -------------------------------------------------------

func TestHostMatches(t *testing.T) {
	tests := []struct {
		name  string
		host  string
		list  []string
		match bool
	}{
		{"exact match", "api.openai.com", []string{"api.openai.com"}, true},
		{"suffix match", "api.openai.com", []string{"openai.com"}, true},
		{"second entry matches", "api.openai.com", []string{"other.test", "openai.com"}, true},
		{"no match", "evil.example", []string{"openai.com"}, false},
		{"prefix is not suffix", "openai.com.evil.example", []string{"openai.com"}, false},
		{"partial label no match", "notopenai.com", []string{"openai.com"}, false},
		{"port stripped for matching", "api.github.com:443", []string{"github.com"}, true},
		{"empty list never matches", "github.com", nil, false},
		{"empty host never matches", "", []string{"github.com"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostMatches(tt.host, tt.list)
			if got != tt.match {
				t.Fatalf("hostMatches(%q, %v) = %v, want %v", tt.host, tt.list, got, tt.match)
			}
		})
	}
}

func TestScanHeader_FindsPlaceholders(t *testing.T) {
	tests := []struct {
		name  string
		value string
		names []string
	}{
		{"single placeholder", "MEEPT_SECRET:gh_token", []string{"gh_token"}},
		{"placeholder inside value", "Bearer MEEPT_SECRET:gh_token extra", []string{"gh_token"}},
		{"two placeholders", "a MEEPT_SECRET:one b MEEPT_SECRET:two c", []string{"one", "two"}},
		{"no placeholder", "Bearer realvalue", nil},
		{"bare prefix only", "MEEPT_SECRET:", nil},
		{"empty value", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanPlaceholderNames(tt.value)
			if len(got) != len(tt.names) {
				t.Fatalf("scanPlaceholderNames(%q) = %v, want %v", tt.value, got, tt.names)
			}
			for i := range tt.names {
				if got[i] != tt.names[i] {
					t.Fatalf("scanPlaceholderNames(%q)[%d] = %q, want %q", tt.value, i, got[i], tt.names[i])
				}
			}
		})
	}
}

// --- shared fixtures -------------------------------------------------------

const (
	testTokenName = "gh_tok"
	testTokenVal  = "REALSECRETVALUE"
)

// newTestBroker returns a broker with one env-backed secret whose source may
// be customized per-test (Hosts/Header/Format fields).
func newTestBroker(t *testing.T) *Broker {
	t.Helper()
	t.Setenv("TEST_PROXY_TOKEN", testTokenVal)
	b, err := NewBroker(Config{
		testTokenName: {Kind: "env", Name: "TEST_PROXY_TOKEN"},
	}, newDiscardLogger())
	if err != nil {
		t.Fatalf("NewBroker failed: %v", err)
	}
	return b
}

// newTestBrokerWithLoader builds the fixture broker with a caller-supplied
// logger (used by the never-log test so broker logs are captured too).
func newTestBrokerWithLogger(t *testing.T, logger *slog.Logger) *Broker {
	t.Helper()
	t.Setenv("TEST_PROXY_TOKEN", testTokenVal)
	b, err := NewBroker(Config{
		testTokenName: {Kind: "env", Name: "TEST_PROXY_TOKEN"},
	}, logger)
	if err != nil {
		t.Fatalf("NewBroker failed: %v", err)
	}
	return b
}

// setSource overrides the fixture secret's routing metadata.
func setSource(b *Broker, hosts []string, header, format string) {
	src := b.sources[testTokenName]
	src.Hosts = hosts
	src.Header = header
	src.Format = format
	b.sources[testTokenName] = src
}

// stubTransport is the proxy's downstream transport under test: it records
// the post-Rewrite request and forwards it to a local httptest stub, so no
// request can ever escape to real network even if Rewrite misroutes.
type stubTransport struct {
	mu     sync.Mutex
	target string // host:port of local stub; every request is retargeted here
	last   *http.Request
}

func (s *stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	out := r.Clone(r.Context())
	out.URL.Scheme = "http"
	out.URL.Host = s.target
	s.mu.Lock()
	s.last = out
	s.mu.Unlock()
	return http.DefaultTransport.RoundTrip(out)
}

func (s *stubTransport) lastHeader(key string) string {
	s.mu.Lock()
	var h http.Header
	if s.last != nil {
		h = s.last.Header
	}
	s.mu.Unlock()
	// Header.Get is called OUTSIDE the mutex scope (mutexio rule); it is
	// nil-receiver safe.
	return h.Get(key)
}

// attachStub routes p's upstream traffic to the given local stub URL.
func attachStub(t *testing.T, p *Proxy, stubURL string) *stubTransport {
	t.Helper()
	u, err := url.Parse(stubURL)
	if err != nil {
		t.Fatal(err)
	}
	tr := &stubTransport{target: u.Host}
	p.rewriteTransport = tr
	return tr
}

// startStub starts a local upstream stub.
func startStub(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// echoAuthHandler echoes the Authorization header back in a response header,
// letting tests observe exactly what the upstream received.
func echoAuthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Saw-Authorization", r.Header.Get("Authorization"))
	w.WriteHeader(http.StatusOK)
}

// captureHandler records the received body into dest.
func captureHandler(dest *string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, maxBodyScan))
		*dest = string(b)
		w.WriteHeader(http.StatusOK)
	}
}

// startProxy starts p and fails the test on bind error.
func startProxy(t *testing.T, ctx context.Context, p *Proxy) string {
	t.Helper()
	addr, err := p.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	return addr
}

// do sends one POST through the running proxy. Empty body/ctype omit them.
func do(t *testing.T, p *Proxy, host, header, val, body, ctype string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+p.Addr()+"/path", rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	if header != "" {
		req.Header.Set(header, val)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request through proxy failed: %v", err)
	}
	return resp
}

// --- Task 2: proxy behaviors ----------------------------------------------

// Behavior 1: placeholder header + matching host -> ENTIRE header value
// replaced with Format(value); placeholder must NOT reach upstream.
func TestProxy_HeaderInjectionOnMatchedHost(t *testing.T) {
	stub := startStub(t, echoAuthHandler)
	b := newTestBroker(t)
	setSource(b, []string{"github.com"}, "Authorization", "Bearer {}")

	p := NewProxy(b, ProxyConfig{Enabled: true}, newDiscardLogger())
	tr := attachStub(t, p, stub.URL)
	startProxy(t, context.Background(), p)

	resp := do(t, p, "github.com", "Authorization", Placeholder(testTokenName), "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := tr.lastHeader("Authorization"); got != "Bearer "+testTokenVal {
		t.Fatalf("upstream Authorization = %q, want injected %q", got, "Bearer "+testTokenVal)
	}
	if got := resp.Header.Get("X-Saw-Authorization"); strings.Contains(got, PlaceholderPrefix) {
		t.Fatalf("placeholder leaked upstream: %q", got)
	}
}

// Behavior 1b: mismatched host -> passthrough UNMODIFIED + leak counter.
func TestProxy_HostMismatchPassthroughAndLeakCounter(t *testing.T) {
	stub := startStub(t, echoAuthHandler)
	b := newTestBroker(t)
	setSource(b, []string{"github.com"}, "Authorization", "Bearer {}")

	p := NewProxy(b, ProxyConfig{Enabled: true}, newDiscardLogger())
	tr := attachStub(t, p, stub.URL)
	startProxy(t, context.Background(), p)

	before := p.LeakAttempts()
	resp := do(t, p, "evil.example", "Authorization", Placeholder(testTokenName), "", "")
	resp.Body.Close()

	if got := tr.lastHeader("Authorization"); got != Placeholder(testTokenName) {
		t.Fatalf("mismatched-host upstream header = %q, want untouched %q", got, Placeholder(testTokenName))
	}
	if after := p.LeakAttempts(); after != before+1 {
		t.Fatalf("leak attempts = %d, want %d (+1)", after, before+1)
	}
}

// Behavior 2: JSON body under 1MiB gets placeholder replacement in place.
func TestProxy_BodyInjectionJSON(t *testing.T) {
	var seenBody string
	stub := startStub(t, captureHandler(&seenBody))
	b := newTestBroker(t)
	setSource(b, []string{"api.test"}, "Authorization", "")

	p := NewProxy(b, ProxyConfig{Enabled: true}, newDiscardLogger())
	attachStub(t, p, stub.URL)
	startProxy(t, context.Background(), p)

	body := `{"note":"hello MEEPT_SECRET:gh_tok bye"}`
	resp := do(t, p, "api.test", "", "", body, "application/json")
	resp.Body.Close()

	want := `{"note":"hello REALSECRETVALUE bye"}`
	if seenBody != want {
		t.Fatalf("body after proxy = %q, want %q", seenBody, want)
	}
}

// Behavior 2b: bodies that are neither text/* nor application/json are
// forwarded byte-for-byte.
func TestProxy_BodyUntouchedForNonScannableTypes(t *testing.T) {
	var seenBody string
	stub := startStub(t, captureHandler(&seenBody))
	b := newTestBroker(t)
	setSource(b, []string{"api.test"}, "", "")

	p := NewProxy(b, ProxyConfig{Enabled: true}, newDiscardLogger())
	attachStub(t, p, stub.URL)
	startProxy(t, context.Background(), p)

	body := "opaque MEEPT_SECRET:gh_tok payload"
	resp := do(t, p, "api.test", "", "", body, "application/octet-stream")
	resp.Body.Close()

	if seenBody != body {
		t.Fatalf("octet-stream body was modified: %q", seenBody)
	}
}

// Behavior 2c: text/* content types ARE scanned.
func TestProxy_BodyInjectionTextPlain(t *testing.T) {
	var seenBody string
	stub := startStub(t, captureHandler(&seenBody))
	b := newTestBroker(t)
	setSource(b, []string{"api.test"}, "", "")

	p := NewProxy(b, ProxyConfig{Enabled: true}, newDiscardLogger())
	attachStub(t, p, stub.URL)
	startProxy(t, context.Background(), p)

	resp := do(t, p, "api.test", "", "", "v=MEEPT_SECRET:gh_tok", "text/plain")
	resp.Body.Close()

	if seenBody != "v="+testTokenVal {
		t.Fatalf("text/plain body after proxy = %q, want %q", seenBody, "v="+testTokenVal)
	}
}

// Behavior 3: chunked transfer-encoding requests rejected with 400 before
// reaching any upstream. Raw bytes over TCP so net/http cannot de-chunk for us.
func TestProxy_ChunkedRejected400(t *testing.T) {
	upstreamHit := false
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamHit = true
		w.WriteHeader(200)
	})
	b := newTestBroker(t)
	setSource(b, []string{"github.com"}, "Authorization", "Bearer {}")

	p := NewProxy(b, ProxyConfig{Enabled: true}, newDiscardLogger())
	attachStub(t, p, stub.URL)
	addr := startProxy(t, context.Background(), p)

	rawBody := `{"a":1}`
	payload := "POST /x HTTP/1.1\r\n" +
		"Host: github.com\r\n" +
		"Transfer-Encoding: chunked\r\n" +
		"Content-Type: application/json\r\n" +
		"\r\n" +
		fmt.Sprintf("%x\r\n%s\r\n0\r\n\r\n", len(rawBody), rawBody)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	if _, writeErr := conn.Write([]byte(payload)); writeErr != nil {
		// Server may have answered 400 before consuming the full body; that
		// is the expected fast path, not a failure here.
		t.Logf("write returned early (expected when server rejects chunked): %v", writeErr)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("chunked status = %d, want 400", resp.StatusCode)
	}
	if upstreamHit {
		t.Fatalf("chunked request reached upstream; want rejection before upstream")
	}
}

// Behavior 4: unknown secret name -> untouched + leak counter increments.
func TestProxy_UnknownSecretUntouchedAndCounted(t *testing.T) {
	stub := startStub(t, echoAuthHandler)
	b := newTestBroker(t)
	setSource(b, []string{"github.com"}, "Authorization", "Bearer {}")

	p := NewProxy(b, ProxyConfig{Enabled: true}, newDiscardLogger())
	tr := attachStub(t, p, stub.URL)
	startProxy(t, context.Background(), p)

	before := p.LeakAttempts()
	resp := do(t, p, "github.com", "Authorization", Placeholder("no_such_secret"), "", "")
	resp.Body.Close()

	if got := tr.lastHeader("Authorization"); got != Placeholder("no_such_secret") {
		t.Fatalf("unknown-secret upstream header = %q, want untouched", got)
	}
	if after := p.LeakAttempts(); after != before+1 {
		t.Fatalf("leak attempts = %d, want %d (+1)", after, before+1)
	}
}

// Behavior 5: real values NEVER logged. A capturing logger watches every
// record emitted during a full inject + body-scan run.
func TestProxy_NeverLogsRealValues(t *testing.T) {
	var mu sync.Mutex
	var logged strings.Builder
	logger := slog.New(slog.NewTextHandler(&logWriter{mu: &mu, b: &logged}, &slog.HandlerOptions{Level: slog.LevelDebug}))

	b := newTestBrokerWithLogger(t, logger)
	setSource(b, []string{"github.com"}, "Authorization", "Bearer {}")

	stub := startStub(t, echoAuthHandler)
	p := NewProxy(b, ProxyConfig{Enabled: true}, logger)
	attachStub(t, p, stub.URL)
	startProxy(t, context.Background(), p)

	resp := do(t, p, "github.com", "Authorization", Placeholder(testTokenName),
		`{"k":"MEEPT_SECRET:gh_tok"}`, "application/json")
	resp.Body.Close()

	mu.Lock()
	out := logged.String()
	mu.Unlock()
	if strings.Contains(out, testTokenVal) {
		t.Fatalf("real secret value appeared in logs:\n%s", out)
	}
	if !strings.Contains(out, testTokenName) && !strings.Contains(out, PlaceholderPrefix) {
		// Sanity: the logger captured anything at all (names are allowed;
		// silence would also pass, but prove the writer was exercised).
		t.Log("no log output captured; writer may be unexercised")
	}
}

type logWriter struct {
	mu *sync.Mutex
	b  *strings.Builder
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

// --- Start / Addr / lifecycle ----------------------------------------------

func TestProxy_StartBindsLoopbackAndShutsDownCleanly(t *testing.T) {
	b := newTestBroker(t)
	p := NewProxy(b, ProxyConfig{Enabled: true, Listen: "127.0.0.1:0"}, newDiscardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	addr, err := p.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("bound addr %q not loopback", addr)
	}
	if p.Addr() != addr {
		t.Fatalf("Addr() = %q, want %q", p.Addr(), addr)
	}
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	resp.Body.Close()

	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		client := &http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Get("http://" + addr + "/")
		if err != nil {
			return // listener gone — clean shutdown, no leaked listener
		}
		resp.Body.Close()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("listener still accepting 2s after ctx cancel — leaked listener")
}

func TestProxy_NonLoopbackListenIsError(t *testing.T) {
	for _, listen := range []string{"0.0.0.0:0", "192.168.1.5:8888", ":8080"} {
		b := newTestBroker(t)
		p := NewProxy(b, ProxyConfig{Enabled: true, Listen: listen}, newDiscardLogger())
		if _, err := p.Start(context.Background()); err == nil {
			t.Fatalf("non-loopback Listen %q must be refused, got nil error", listen)
		}
	}
}

func TestProxy_EmptyListenDefaultsLoopbackEphemeral(t *testing.T) {
	b := newTestBroker(t)
	p := NewProxy(b, ProxyConfig{Enabled: true}, newDiscardLogger())
	addr := startProxy(t, context.Background(), p)
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("default listen must bind 127.0.0.1 ephemeral, got %q", addr)
	}
}

// LeakAttempts must be safe to call on a nil-lifecycle Proxy (never started).
func TestProxy_LeakAttemptsZeroBeforeStart(t *testing.T) {
	b := newTestBroker(t)
	p := NewProxy(b, ProxyConfig{Enabled: true}, newDiscardLogger())
	if n := p.LeakAttempts(); n != 0 {
		t.Fatalf("fresh proxy leak attempts = %d, want 0", n)
	}
}
