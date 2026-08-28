package secrets

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// LeakAttemptMetricName is the canonical metric name counting placeholder
// traffic toward hosts outside the secret's declared Hosts allowlist (or
// placeholders naming unknown secrets). Exported so the daemon status surface
// can report it alongside other counters.
const LeakAttemptMetricName = "secrets.leak_attempt"

// maxBodyScan bounds how much of a request body is scanned for placeholders.
// Bytes past the limit pass through unscanned.
const maxBodyScan = 1 << 20 // 1MiB

// defaultListen is the bind address used when ProxyConfig.Listen is empty:
// loopback with an OS-assigned ephemeral port.
const defaultListen = "127.0.0.1:0"

// shutdownGrace bounds how long in-flight requests may finish after ctx
// cancellation before listeners are force-closed.
const shutdownGrace = 5 * time.Second

// ProxyConfig configures the egress secret-injection proxy.
type ProxyConfig struct {
	Enabled bool   `json:"enabled" toml:"enabled"`
	Listen  string `json:"listen"  toml:"listen"` // default "127.0.0.1:0"; MUST be loopback
}

// Proxy is the loopback reverse proxy that rewrites MEEPT_SECRET:<name>
// placeholders into real credential material for allowlisted hosts only.
//
// Security properties:
//   - Binds loopback exclusively; non-loopback Listen values are REFUSED
//     (hard error, not a warning) so misconfiguration cannot expose resolved
//     credentials on an external interface.
//   - Real values live only inside this process: injection rewrites requests
//     in flight; nothing is persisted, logged, or reflected in responses.
//   - Requests carrying placeholders toward non-allowlisted hosts pass
//     through UNMODIFIED and increment the secrets.leak_attempt counter.
//
// Scope note: this leaf forwards plain HTTP only. CONNECT/TLS interception is
// explicitly out of scope (see plans/containment-and-computer-use/master.md).
type Proxy struct {
	broker   *Broker
	cfg      ProxyConfig
	logger   *slog.Logger
	addr     atomic.Value // string, bound address after Start
	leaks    atomic.Int64
	listener net.Listener
	server   *http.Server

	// rewriteTransport overrides the downstream transport (tests inject a
	// recording/local-only transport here; nil means http.DefaultTransport).
	rewriteTransport http.RoundTripper

	// policy is consulted before secret injection when non-nil: deny -> 403,
	// ask -> approver/timeout, allow -> continue. Resolved IPs are
	// double-checked against CIDR rules (DNS rebinding defense). Nil means no
	// egress policy (default mode=allow behavior).
	policy *EgressPolicy
}

// NewProxy builds a proxy. Start must be called to begin serving.
func NewProxy(b *Broker, cfg ProxyConfig, logger *slog.Logger) *Proxy {
	if logger == nil {
		logger = slog.Default()
	}
	return &Proxy{broker: b, cfg: cfg, logger: logger.With("component", "secrets-proxy")}
}

// Addr returns the bound "host:port" after Start succeeds; empty before.
func (p *Proxy) Addr() string {
	s, _ := p.addr.Load().(string)
	return s
}

// LeakAttempts reports the secrets.leak_attempt counter: requests observed
// carrying placeholders toward non-allowlisted hosts or naming unknown
// secrets.
func (p *Proxy) LeakAttempts() int64 {
	return p.leaks.Load()
}

// Start binds the configured loopback address and serves until ctx is
// cancelled. Cancellation triggers graceful shutdown; the listener is closed
// and in-flight requests get shutdownGrace to finish. Returns the bound
// "host:port".
func (p *Proxy) Start(ctx context.Context) (string, error) {
	if p.listener != nil {
		return "", fmt.Errorf("secrets proxy already started")
	}
	ln, err := loopbackListen(p.cfg.Listen)
	if err != nil {
		return "", err
	}

	srv := &http.Server{
		Handler:           http.HandlerFunc(p.serveHTTP),
		ReadHeaderTimeout: 10 * time.Second,
	}
	p.listener = ln
	p.server = srv
	p.addr.Store(ln.Addr().String())

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			p.logger.Debug("secrets proxy shutdown", "error", err)
		}
	}()

	go func() {
		// ErrServerClosed after Shutdown is expected; anything else is
		// logged (listener failures are operational faults).
		if serr := srv.Serve(ln); serr != nil && !errorsIsServerClosed(serr) {
			p.logger.Error("secrets proxy serve failed", "error", serr)
		}
	}()

	p.logger.Info("secrets egress proxy listening",
		"addr", ln.Addr().String(),
		"metric", LeakAttemptMetricName,
	)
	return ln.Addr().String(), nil
}

// Stop forces immediate graceful shutdown (used by daemon wiring on paths
// where the lifecycle ctx is not cancelled automatically).
func (p *Proxy) Stop() {
	if p.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := p.server.Shutdown(ctx); err != nil {
			p.logger.Debug("secrets proxy stop", "error", err)
		}
	}
}

// errorsIsServerClosed reports whether err is http.ErrServerClosed without
// importing errors just for this identity check.
func errorsIsServerClosed(err error) bool {
	type unwrapper interface{ Unwrap() error }
	for e := err; e != nil; {
		if e == http.ErrServerClosed {
			return true
		}
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// loopbackListen validates that listen targets a loopback IP literal and
// binds it. Non-loopback targets are refused outright: the proxy holds the
// power to mint authenticated requests, so binding it broadly would turn one
// config typo into a network-exposed credential oracle.
func loopbackListen(listen string) (*net.TCPListener, error) {
	if listen == "" {
		listen = defaultListen
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return nil, fmt.Errorf("secrets proxy listen %q invalid: %w", listen, err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return nil, fmt.Errorf("secrets proxy listen %q: host %q must be an IP literal: %w", listen, host, err)
	}
	if !ip.IsLoopback() {
		return nil, fmt.Errorf("secrets proxy refuses non-loopback listen %q (credential-injection boundary)", listen)
	}
	ln, lerr := net.Listen("tcp", listen)
	if lerr != nil {
		return nil, fmt.Errorf("secrets proxy bind %q failed: %w", listen, lerr)
	}
	tcp, ok := ln.(*net.TCPListener)
	if !ok {
		// Returning the error below; no logger in scope here, and the
		// listener is discarded with the error path either way.
		ln.Close()
		return nil, fmt.Errorf("secrets proxy bind %q produced non-TCP listener", listen)
	}
	return tcp, nil
}

// serveHTTP is the proxy entry point. Chunked transfer-encoding is rejected
// before anything else: scanning/de-chunking streamed bodies without content
// framing invites smuggling bugs (duckagent lesson).
func (p *Proxy) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if len(r.TransferEncoding) > 0 {
		p.logger.Warn("rejected unsupported transfer encoding",
			"encoding", strings.Join(r.TransferEncoding, ","),
			"dest_host", requestDest(r),
		)
		http.Error(w, "transfer-encoding not supported by secrets proxy", http.StatusBadRequest)
		return
	}

	dest := requestDest(r)
	if p.policy != nil {
		action, _, _, err := p.policy.CheckAndResolve(dest)
		if err != nil {
			p.logger.Warn("egress policy check failed", "dest_host", dest, "error", err)
			http.Error(w, "blocked by egress policy", http.StatusForbidden)
			return
		}
		switch action {
		case EgressDeny:
			p.logger.Warn("blocked by egress policy", "dest_host", dest,
				"metric", EgressDecisionMetricName, "action", EgressDeny)
			http.Error(w, "blocked by egress policy", http.StatusForbidden)
			return
		case EgressAsk:
			// resolveAsk already ran inside Decide; a surviving ask (no
			// approver) resolves to deny there, so this branch is allow.
		}
	}
	tr := p.rewriteTransport
	if tr == nil {
		tr = http.DefaultTransport
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			out := pr.Out
			out.URL.Scheme = "http"
			out.URL.Host = dest
			out.Host = dest
			p.rewriteOutbound(out, dest)
		},
		Transport: tr,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Never surface upstream error text that might echo request
			// material; a bare status keeps the failure opaque.
			p.logger.Warn("upstream request failed", "dest_host", dest, "error", err)
			http.Error(w, "upstream unreachable", http.StatusBadGateway)
		},
	}
	rp.ServeHTTP(w, r)
}

// requestDest extracts the destination authority for routing and host-matching.
// Absolute-form proxy requests carry it in URL.Host; origin-form in r.Host.
func requestDest(r *http.Request) string {
	if r.URL != nil && r.URL.Host != "" {
		return r.URL.Host
	}
	return r.Host
}

// rewriteOutbound performs header and body placeholder resolution on the
// OUTBOUND clone. Counters/log fields carry names and hosts only — never
// values.
func (p *Proxy) rewriteOutbound(out *http.Request, dest string) {
	var unknownSeen, mismatchSeen bool

	for key, vals := range out.Header {
		for i, v := range vals {
			newV, unk, mism := p.injectString(v, dest)
			unknownSeen = unknownSeen || unk
			mismatchSeen = mismatchSeen || mism
			if newV != v && !mism {
				out.Header[key][i] = newV
				p.logger.Debug("placeholder injected", "header", key, "dest_host", dest)
			}
		}
	}

	if out.Body != nil && scannableBody(out.Header.Get("Content-Type")) {
		head, rerr := io.ReadAll(io.LimitReader(out.Body, maxBodyScan))
		if rerr == nil {
			newHead, unk, mism := p.injectString(string(head), dest)
			unknownSeen = unknownSeen || unk
			mismatchSeen = mismatchSeen || mism
			if newHead != string(head) {
				out.Body = io.NopCloser(io.MultiReader(
					strings.NewReader(newHead),
					out.Body, // remainder beyond the scanned prefix
				))
				p.adjustContentLength(out, len(string(head)), len(newHead))
				p.logger.Debug("body placeholder injected", "dest_host", dest)
			} else {
				out.Body = io.NopCloser(io.MultiReader(strings.NewReader(newHead), out.Body))
			}
		} else {
			// Body unreadable: fail closed for scannable types by dropping
			// the body rather than risking an unscanned passthrough.
			p.logger.Warn("scannable body unreadable; dropping body", "dest_host", dest, "error", rerr)
			out.Body = io.NopCloser(strings.NewReader(""))
		}
	}

	if unknownSeen || mismatchSeen {
		p.leaks.Add(1)
		reason := "unknown_secret"
		if mismatchSeen {
			reason = "host_mismatch"
		}
		p.logger.Warn(LeakAttemptMetricName, "reason", reason, "dest_host", dest)
	}
}

// adjustContentLength fixes Content-Length after in-place body substitution.
func (p *Proxy) adjustContentLength(out *http.Request, oldLen, newLen int) {
	cl := out.Header.Get("Content-Length")
	if cl == "" {
		return
	}
	n, err := strconv.ParseInt(cl, 10, 64)
	if err != nil {
		return
	}
	delta := int64(newLen - oldLen)
	out.Header.Set("Content-Length", strconv.FormatInt(n+delta, 10))
	if out.ContentLength > 0 {
		out.ContentLength += delta
	}
}

// injectString rewrites every MEEPT_SECRET:<name> occurrence in s whose
// secret is declared AND whose source allows dest. Returns the (possibly
// unchanged) string plus flags: whether unknown names were referenced and
// whether any declared secret was blocked by the host allowlist. Blocked
// values fail CLOSED: if any referenced secret mismatches the allowlist, the
// whole string is returned untouched.
func (p *Proxy) injectString(s, dest string) (out string, unknownSeen, mismatchSeen bool) {
	out = s
	names := scanPlaceholderNames(s)
	if len(names) == 0 {
		return out, false, false
	}
	blocked := false
	for _, name := range names {
		val, err := p.resolve(name)
		if err != nil {
			unknownSeen = true
			continue
		}
		src, ok := p.source(name)
		if !ok {
			unknownSeen = true
			continue
		}
		if !hostMatches(dest, src.Hosts) {
			blocked = true
			mismatchSeen = true
			continue
		}
		repl := val
		if src.Format != "" {
			repl = strings.ReplaceAll(src.Format, "{}", val)
		}
		out = strings.ReplaceAll(out, Placeholder(name), repl)
	}
	if blocked {
		return s, unknownSeen, mismatchSeen // fail closed: no partial injection
	}
	return out, unknownSeen, mismatchSeen
}

// resolve wraps broker.resolve with a nil-broker guard (treated as unknown).
func (p *Proxy) resolve(name string) (string, error) {
	if p.broker == nil {
		return "", fmt.Errorf("unknown secret %q (no broker)", name)
	}
	return p.broker.resolve(name)
}

// source wraps broker.Source with a nil-broker guard.
func (p *Proxy) source(name string) (Source, bool) {
	if p.broker == nil {
		return Source{}, false
	}
	return p.broker.Source(name)
}

// --- helpers ---------------------------------------------------------------

// scannableBody reports whether ct is text/* or application/json — the only
// content types whose bodies are scanned for placeholders.
func scannableBody(ct string) bool {
	mt := ct
	if i := strings.IndexByte(mt, ';'); i >= 0 {
		mt = mt[:i]
	}
	mt = strings.ToLower(strings.TrimSpace(mt))
	return strings.HasPrefix(mt, "text/") || mt == "application/json"
}

// hostMatches reports whether rawHost ("host" or "host:port") equals or ends
// with dot + one of the allowed suffixes. Empty inputs never match. Ports are
// stripped first; scheme handling comes from net/url parsing of r.Host
// upstream (requestDest).
func hostMatches(rawHost string, allowed []string) bool {
	if len(allowed) == 0 || rawHost == "" {
		return false
	}
	h := rawHost
	if u, err := url.Parse("http://" + rawHost); err == nil && u.Hostname() != "" {
		h = u.Hostname()
	}
	h = strings.ToLower(h)
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		a = strings.TrimSuffix(a, ".") // FQDN-form entries match bare form
		if h == a || strings.HasSuffix(h, "."+a) {
			return true
		}
	}
	return false
}

// scanPlaceholderNames returns every secret name referenced by
// MEEPT_SECRET:<name> tokens inside value, in first-seen order. Bare prefixes
// without a name yield nothing.
func scanPlaceholderNames(value string) []string {
	var names []string
	for start := 0; start <= len(value); {
		i := strings.Index(value[start:], PlaceholderPrefix)
		if i < 0 {
			break
		}
		i += start + len(PlaceholderPrefix)
		end := len(value)
		if j := strings.IndexAny(value[i:], " \t\r\n\"'`;(){}[],"); j >= 0 {
			end = i + j
		}
		if end > i {
			names = append(names, value[i:end])
		}
		start = end
	}
	return dedupe(names)
}

// dedupe preserves order while dropping repeated names.
func dedupe(names []string) []string {
	if len(names) == 0 {
		return names
	}
	seen := make(map[string]struct{}, len(names))
	out := names[:0]
	for _, n := range names {
		if _, dup := seen[n]; !dup {
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	return out
}
