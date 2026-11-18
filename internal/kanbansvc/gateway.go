// Package kanbansvc holds the sidecar gateway for the kanban board: a thin
// REST/WS front that forwards every read and mutation to the daemon's
// kanban.* RPC over the CLI's transport client. The gateway holds NO logic and
// NO database handle (master.md C7) — it is the only process the browser ever
// talks to, so the daemon stays loopback/socket-bound.
//
// Grant model (C5): reads need board.read, mutations board.mutate, and
// takeover is conjunctive — it needs board.mutate AND board.takeover, so a
// takeover-only token cannot move cards in. Browsers cannot set headers on a
// WebSocket, so the WS handshake carries the bearer token in
// Sec-WebSocket-Protocol as "bearer.<token>" — the same convention
// internal/comm/web/websocket.go uses.
package kanbansvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// C5 grants. The strings are the contract — keep them byte-identical to
// internal/kanban/authz.go.
const (
	GrantRead     = "board.read"
	GrantMutate   = "board.mutate"
	GrantTakeover = "board.takeover"
)

// ErrNoStaticBundle distinguishes "the web bundle is not installed" from a
// real serving failure so the placeholder path and logs stay honest.
var ErrNoStaticBundle = errors.New("kanban web bundle not installed")

// RPCCaller is the seam to the daemon RPC. transport.Client satisfies it; the
// tests substitute a fake so the gateway can be exercised with no daemon.
type RPCCaller interface {
	Call(method string, params any) (json.RawMessage, error)
	Connect() error
	Close() error
}

// EventSource supplies board deltas for the /api/v1/events relay. The
// production implementation is BusPump (pump.go): it bridges the daemon's
// bus.subscribe/bus.poll RPC pair. IdleSource remains as an inert
// placeholder for embedders/tests that want a relay with no feed.
type EventSource interface {
	Subscribe(ctx context.Context) (<-chan json.RawMessage, error)
}

// GrantChecker maps a bearer token to its grant set. TokenStore (pairing,
// 04/03) is the production implementation; StaticGrantStore remains the
// in-process/test helper. The gateway only ever asks this seam.
type GrantChecker interface {
	Grants(token string) ([]string, bool)
}

// pairExchangePath is the single intentionally UNGRANTED route: the
// single-use pairing code in the body IS the credential, so authorize
// never runs on it. Everything else under /api/v1/ stays grant-gated.
const pairExchangePath = "/api/v1/pair/exchange"

// pairRateLimiterWindow is the sliding window for the per-IP pair/exchange
// rate limit (M3). Requests outside the window are allowed.
const pairRateLimiterWindow = 30 * time.Second

// pairRateLimiterMax is the maximum number of exchange attempts per window.
const pairRateLimiterMax = 5

// ipRateLimitEntry tracks one IP's window state for the pair/exchange
// rate limit.
type ipRateLimitEntry struct {
	windowStart time.Time
	count       int
}

// pairRateLimiter is a small in-process per-IP fixed-window rate limiter.
// No external dependency — a fixed 30s window keyed by client IP, used here
// for the pair/exchange route that is reachable in TLS-proxy deployments.
type pairRateLimiter struct {
	mu sync.Mutex
	m  map[string]ipRateLimitEntry
}

// allowed reports whether the client at key has budget left in the current
// window. If the window has expired, the counter resets and the request is
// allowed. A nil receiver is a no-op pass.
func (r *pairRateLimiter) allowed(key string) bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	entry, ok := r.m[key]
	if !ok || now.Sub(entry.windowStart) >= pairRateLimiterWindow {
		// New window — allow the first request.
		r.m[key] = ipRateLimitEntry{windowStart: now, count: 1}
		return true
	}
	// Same window.
	entry.count++
	r.m[key] = entry
	return entry.count <= pairRateLimiterMax
}

// route describes one REST endpoint and the RPC method it forwards to.
type route struct {
	verb string
	rpc  string
	// grants lists every grant the route requires; a token must hold ALL
	// of them (takeover needs board.mutate AND board.takeover, C5).
	grants []string
	// body marks endpoints whose params come from the JSON request body
	// (mutations) versus the query string (reads).
	body bool
}

// restRoutes is the 1:1 REST-to-C4 map. Keys are paths; the verb is checked
// separately so a wrong verb returns 405 rather than 404.
var restRoutes = map[string]route{
	"/api/v1/board/list":     {verb: http.MethodGet, rpc: "kanban.board.list", grants: []string{GrantRead}},
	"/api/v1/board/create":   {verb: http.MethodPost, rpc: "kanban.board.create", grants: []string{GrantMutate}, body: true},
	"/api/v1/column/list":    {verb: http.MethodGet, rpc: "kanban.column.list", grants: []string{GrantRead}},
	"/api/v1/column/set_wip": {verb: http.MethodPost, rpc: "kanban.column.set_wip", grants: []string{GrantMutate}, body: true},
	"/api/v1/card/list":      {verb: http.MethodGet, rpc: "kanban.card.list", grants: []string{GrantRead}},
	"/api/v1/card/get":       {verb: http.MethodGet, rpc: "kanban.card.get", grants: []string{GrantRead}},
	"/api/v1/card/create":    {verb: http.MethodPost, rpc: "kanban.card.create", grants: []string{GrantMutate}, body: true},
	"/api/v1/card/move":      {verb: http.MethodPost, rpc: "kanban.card.move", grants: []string{GrantMutate}, body: true},
	"/api/v1/card/takeover":  {verb: http.MethodPost, rpc: "kanban.card.takeover", grants: []string{GrantMutate, GrantTakeover}, body: true},
	"/api/v1/card/assign":    {verb: http.MethodPost, rpc: "kanban.card.assign", grants: []string{GrantMutate}, body: true},
	"/api/v1/card/comment":   {verb: http.MethodPost, rpc: "kanban.card.comment", grants: []string{GrantMutate}, body: true},
	"/api/v1/card/timeline":  {verb: http.MethodGet, rpc: "kanban.card.timeline", grants: []string{GrantRead}},
	"/api/v1/reconcile/now":  {verb: http.MethodPost, rpc: "kanban.reconcile.now", grants: []string{GrantMutate}, body: true},
}

// Gateway serves the REST/WS surface. It is stateless apart from its
// dependencies, so a single instance backs the whole listener.
type Gateway struct {
	rpc    RPCCaller
	events EventSource
	authz  GrantChecker

	// pairing, when set, arms POST /api/v1/pair/exchange. It is optional:
	// a gateway built without it answers 503 on the pair route but keeps
	// serving every grant-gated route through authz. main.go sets it right
	// after NewGateway (leaf 07/01 may fold it into the constructor).
	pairing *TokenStore

	// static serves /web/ from the installed bundle, or is nil when no
	// bundle exists. Built once in NewGateway so every request rides the
	// canonical StripPrefix+FileServer pair rather than a hand-forged
	// http.Request.
	static http.Handler

	// allowedOrigins is the EXACT-MATCH WS Origin allow-list ([kanban].
	// allowed_origins, plumbed as repeated -allow-origin flags by the
	// daemon boot — the sidecar stays config-blind). Empty = the current
	// same-origin/loopback-only posture, byte-identical. Entries are
	// normalized scheme://host[:port] lowercase; a browser Origin is
	// accepted only on full-string equality — no wildcard, no suffix or
	// prefix fuzzy matching (origin trust is exact; a subdomain-suffix
	// match is how an XSS'd sibling hands your board tokens to an
	// attacker). This exists for the documented Caddy/nginx TLS-proxy
	// deployment: the proxy presents the PUBLIC Origin, which the
	// loopback-only check otherwise rejects.
	allowedOrigins []string

	// pairRateLimiter is the per-IP rate limiter for the pair/exchange
	// route (M3). Set via SetPairRateLimiter. A nil limiter is a no-op —
	// every request is allowed.
	pairRateLimiter *pairRateLimiter
}

// NewGateway wires the sidecar gateway. Every dependency is required: a nil
// here is a boot-time misconfiguration, so it returns an error rather than
// panicking or silently serving unauthenticated routes.
func NewGateway(rpc RPCCaller, events EventSource, authz GrantChecker, staticDir string) (*Gateway, error) {
	if rpc == nil {
		return nil, errors.New("kanbansvc: rpc caller is required")
	}
	if events == nil {
		return nil, errors.New("kanbansvc: event source is required")
	}
	if authz == nil {
		return nil, errors.New("kanbansvc: grant checker is required")
	}
	g := &Gateway{rpc: rpc, events: events, authz: authz,
		pairRateLimiter: &pairRateLimiter{m: make(map[string]ipRateLimitEntry)}}
	if staticDir != "" {
		g.static = http.StripPrefix("/web", http.FileServer(http.Dir(staticDir)))
	}
	return g, nil
}

// SetPairing arms the pairing-exchange route. Store is never nil here: a
// nil store means the route was never meant to be armed, and SetPairing
// returns an error instead of silently serving a route that cannot work.
// Additive seam — leaf 07/01 may fold this into NewGateway.
func (g *Gateway) SetPairing(store *TokenStore) error {
	if store == nil {
		return errors.New("kanbansvc: token store is required")
	}
	g.pairing = store
	return nil
}

// ServeHTTP routes the surfaces: REST (auth + RPC forward), the ungranted
// pairing exchange, the WS event relay, and the static bundle.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == pairExchangePath:
		g.handlePairExchange(w, r)
	case r.URL.Path == "/api/v1/events":
		g.handleEvents(w, r)
	case r.URL.Path == "/web":
		http.Redirect(w, r, "/web/", http.StatusMovedPermanently)
	case strings.HasPrefix(r.URL.Path, "/web/"):
		g.serveStatic(w, r)
	case r.URL.Path == "/":
		g.serveIndex(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/v1/"):
		g.serveAPI(w, r)
	default:
		writeJSONError(w, http.StatusNotFound, "not found")
	}
}

// serveAPI enforces the grant, builds params, forwards one RPC call, and
// passes the reply or the upstream error through.
func (g *Gateway) serveAPI(w http.ResponseWriter, r *http.Request) {
	spec, ok := restRoutes[r.URL.Path]
	if !ok {
		writeJSONError(w, http.StatusNotFound, "unknown api path")
		return
	}
	if r.Method != spec.verb {
		w.Header().Set("Allow", spec.verb)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !g.authorize(w, r, spec.grants...) {
		return
	}

	params, err := g.buildParams(r, spec.body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := g.rpc.Call(spec.rpc, params)
	if err != nil {
		g.writeRPCError(w, spec.rpc, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(resp); err != nil { //nolint:gosec // G705: resp is a JSON-RPC reply written with Content-Type application/json — no HTML rendering surface, no XSS
		slog.Warn("kanban gateway: reply write failed", "rpc", spec.rpc, "error", err)
	}
}

// handlePairExchange serves POST /api/v1/pair/exchange — the one
// deliberately UNGRANTED route. The single-use pairing code in the body IS
// the credential (the code was minted in-process and printed once to the
// daemon console), so authorize never runs here; everything else under
// /api/v1/ stays grant-gated. A wrong, consumed, or missing code is 401
// with ErrInvalidPairCode's message — one error for all three shapes, so a
// caller cannot oracle the store. The code and the minted token are never
// logged. Per-IP rate limit (M3) is enforced before the store is touched.
func (g *Gateway) handlePairExchange(w http.ResponseWriter, r *http.Request) {
	if g.pairing == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "pairing is not armed on this sidecar")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// Per-IP rate limit (M3): reject rapid exchange attempts before the
	// code is consumed, so a remote attacker cannot brute-force codes in
	// volume. The limiter is keyed by the request's client IP, which is
	// either the direct RemoteAddr (loopback-only bind) or X-Forwarded-For
	// (TLS-proxy deployment).
	if !g.pairRateLimiter.allowed(requestIP(r)) {
		writeJSONError(w, http.StatusTooManyRequests, "too many pair exchange attempts")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "decode request body: expected {\"code\": string}")
		return
	}
	token, grants, err := g.pairing.ExchangePairCode(body.Code)
	if err != nil {
		if !errors.Is(err, ErrInvalidPairCode) {
			// Mint/persist failure: the code is already consumed, so a
			// plain 500 with no detail is the honest answer.
			writeJSONError(w, http.StatusInternalServerError, "exchange failed")
			return
		}
		writeJSONError(w, http.StatusUnauthorized, ErrInvalidPairCode.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"token": token, "grants": grants}); err != nil {
		slog.Warn("kanban gateway: exchange reply write failed", "error", err)
	}
}

// requestIP extracts the client IP from the request. In TLS-proxy
// deployments the proxy sets X-Forwarded-For; otherwise it returns the
// direct RemoteAddr. The pair/exchange rate limiter (M3) is keyed by this
// value, so the limiter distinguishes independent attackers in a proxy.
func requestIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// The leftmost entry is the original client; strip it for the
		// limiter key.
		if before, _, ok := strings.Cut(fwd, ","); ok {
			return strings.TrimSpace(before)
		}
		return strings.TrimSpace(fwd)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// buildParams turns the request into RPC params: query values for reads (a
// single-valued map), the decoded JSON body for mutations.
func (g *Gateway) buildParams(r *http.Request, fromBody bool) (any, error) {
	if !fromBody {
		q := r.URL.Query()
		params := make(map[string]string, len(q))
		for k, v := range q {
			if len(v) > 0 {
				params[k] = v[0]
			}
		}
		return params, nil
	}
	var params any
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil && !errors.Is(err, io.EOF) {
		// An empty body is a legal mutation call (e.g. reconcile/now) and
		// forwards as nil params; anything else is a client error.
		return nil, fmt.Errorf("decode request body: %w", err)
	}
	return params, nil
}

// authorize answers 401 for missing OR unknown tokens (bad credentials) and
// 403 for a known token that lacks any of the required grants. The
// distinction matters to operators: 403 means "pair this sidecar and grant
// the lane", 401 means "the token you presented was never issued". The grant
// requirement is never relaxed for convenience; when a route requires
// multiple grants (takeover: board.mutate AND board.takeover) the token must
// hold all of them, and the 403 names the first missing one.
func (g *Gateway) authorize(w http.ResponseWriter, r *http.Request, grants ...string) bool {
	token := bearerToken(r)
	if token == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing bearer token")
		return false
	}
	held, known := g.authz.Grants(token)
	if !known {
		writeJSONError(w, http.StatusUnauthorized, "unknown bearer token")
		return false
	}
	for _, grant := range grants {
		if !hasGrantName(held, grant) {
			writeJSONError(w, http.StatusForbidden, "missing grant "+grant)
			return false
		}
	}
	return true
}

func hasGrantName(grants []string, grant string) bool {
	return slices.Contains(grants, grant)
}

// bearerToken reads the token from the Authorization header, then falls back
// to the subprotocol lane browsers are forced to use.
func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if rest, ok := strings.CutPrefix(header, "Bearer "); ok {
		return strings.TrimSpace(rest)
	}
	return wsBearer(r)
}

// wsBearer extracts "bearer.<token>" from Sec-WebSocket-Protocol (RFC 6455
// §4.2.2 lane, same convention as the daemon's own hub).
func wsBearer(r *http.Request) string {
	for proto := range strings.SplitSeq(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(proto), "bearer."); ok {
			return rest
		}
	}
	return ""
}

// pickSubprotocol returns the offered protocol the server must echo back. A
// browser that offered "bearer.<token>" and got NO Sec-WebSocket-Protocol
// response header aborts the upgrade, so the gateway echoes the bearer lane
// verbatim, exactly as the daemon hub does (websocket.go:243-261).
func pickSubprotocol(r *http.Request) string {
	offered := r.Header.Get("Sec-WebSocket-Protocol")
	if offered == "" {
		return ""
	}
	picked := ""
	for proto := range strings.SplitSeq(offered, ",") {
		proto = strings.TrimSpace(proto)
		if strings.HasPrefix(proto, "bearer.") {
			return proto
		}
		if picked == "" {
			picked = proto
		}
	}
	return picked
}

// writeJSONError is the single JSON error writer. Hand-assembled error bodies
// break on any value that needs escaping (a grant name, upstream message) and
// ship invalid JSON, so every error path goes through here instead.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	body, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		// Only a non-UTF-8 message can do this, and Go strings are
		// UTF-8-clean by contract, so fall back to a fixed body.
		body = []byte(`{"error":"error encoding failed"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, writeErr := w.Write(body); writeErr != nil {
		slog.Warn("kanban gateway: error reply write failed", "status", status, "error", writeErr)
	}
}

// writeRPCError maps an upstream RPC failure onto an HTTP status. A method or
// record the daemon does not know is a client-visible 404; anything else means
// the upstream is the thing that failed (502).
func (g *Gateway) writeRPCError(w http.ResponseWriter, rpc string, err error) {
	status := http.StatusBadGateway
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "unknown method") || strings.Contains(lower, "not found") || strings.Contains(lower, "no handler") {
		status = http.StatusNotFound
	}
	body, marshalErr := json.Marshal(map[string]string{"rpc": rpc, "error": msg})
	if marshalErr != nil {
		body = []byte(`{"error":"error encoding upstream failure"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, writeErr := w.Write(body); writeErr != nil {
		slog.Warn("kanban gateway: error reply write failed", "rpc", rpc, "error", writeErr)
	}
}

// handleEvents serves the WS relay. Auth happens in the Handshake so a bad
// token never completes an upgrade.
func (g *Gateway) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	srv := &websocket.Server{
		Handshake: func(cfg *websocket.Config, req *http.Request) error {
			if err := g.checkWSOrigin(req); err != nil {
				return err
			}
			token := bearerToken(req)
			if token == "" {
				return errors.New("missing bearer token")
			}
			grants, known := g.authz.Grants(token)
			if !known {
				return errors.New("unknown bearer token")
			}
			if !hasGrantName(grants, GrantRead) {
				return fmt.Errorf("missing grant %s", GrantRead)
			}
			// Echo the offered subprotocol or the browser aborts the
			// upgrade after the server accepts it.
			if picked := pickSubprotocol(req); picked != "" {
				cfg.Protocol = []string{picked}
			}
			return nil
		},
		Handler: g.relayEvents,
	}
	srv.ServeHTTP(w, r)
}

// SetAllowedOrigins arms the exact-match WS Origin allow-list (the
// TLS-proxy knob, [kanban].allowed_origins via -allow-origin). Entries
// are normalized (lowercase scheme://host[:port]); invalid URLs and the
// empty string are dropped with a warning rather than failing boot — a
// malformed entry must never silently widen or break origin trust, so
// only exact parseable origins are kept. A nil/empty list restores the
// default loopback-only posture. Nil-gate on the receiver (house Set*
// rule).
func (g *Gateway) SetAllowedOrigins(origins []string) {
	if g == nil {
		return
	}
	normalized := make([]string, 0, len(origins))
	for _, raw := range origins {
		if entry, ok := normalizeOrigin(raw); ok {
			normalized = append(normalized, entry)
		} else if raw != "" {
			slog.Warn("kanban gateway: dropping malformed allowed origin", "origin", raw)
		}
	}
	g.allowedOrigins = normalized
}

// SetPairRateLimiter arms the per-IP rate limiter for the pair/exchange
// route (M3). A nil limiter leaves every request allowed; pass one when the
// sidecar is reachable over a TLS proxy.
func (g *Gateway) SetPairRateLimiter(limit *pairRateLimiter) {
	if g == nil {
		return
	}
	g.pairRateLimiter = limit
}

// normalizeOrigin canonicalizes an Origin to lowercase scheme://host with
// any default port trimmed comparison-stable. Accepts only an absolute
// http/https URL with a host and NO path (browsers never send one, and a
// path in an allow-list entry signals a misconfigured operator input).
func normalizeOrigin(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	if strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https" {
		return "", false
	}
	// A bare origin only: any path — even "/" (a trailing slash) — is an
	// operator pasting a URL, not an Origin. Browsers never send one, so
	// matching against a slashed entry could never fire anyway.
	if u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), true
}

// originAllowed reports whether an Origin header value passes the
// allow-list: the exact-match operator entries first, then the
// same-origin/loopback default (mirrors the daemon hub's allow-list).
// ok=false for a non-empty Origin on the default posture with no list.
func (g *Gateway) originAllowed(origin string) bool {
	if origin == "" {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(origin))
	if slices.Contains(g.allowedOrigins, lower) {
		return true
	}
	host := lower
	if u, err := url.Parse(origin); err == nil && u.Hostname() != "" {
		host = strings.ToLower(u.Hostname())
	}
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// checkWSOrigin mirrors the daemon hub's allow-list: same-origin only, with a
// missing Origin (non-browser client) allowed because the bearer token still
// gates the connection. Operator-configured exact origins
// ([kanban].allowed_origins → SetAllowedOrigins) are accepted IN ADDITION
// — required for the documented Caddy/nginx TLS-proxy deployment, where
// the proxy presents the public Origin.
func (g *Gateway) checkWSOrigin(req *http.Request) error {
	origin := req.Header.Get("Origin")
	if g.originAllowed(origin) {
		return nil
	}
	return fmt.Errorf("origin not allowed: %s", origin)
}

// relayEvents pumps EventSource messages to one WS client. A discarded reader
// goroutine notices the client's close and ends the relay, so no subscription
// outlives its connection.
func (g *Gateway) relayEvents(conn *websocket.Conn) {
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Drain inbound frames so the peer's close is observed (a WS server that
	// only writes never sees it).
	go func() {
		for {
			var discard json.RawMessage
			if err := websocket.Message.Receive(conn, &discard); err != nil {
				cancel()
				return
			}
		}
	}()

	stream, err := g.events.Subscribe(ctx)
	if err != nil {
		slog.Warn("kanban gateway: event subscription failed", "error", err)
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-stream:
			if !ok {
				return
			}
			if err := websocket.Message.Send(conn, string(msg)); err != nil {
				return
			}
		}
	}
}

// serveStatic serves the Flutter bundle under /web/. The handler is built
// once in NewGateway, so path cleanup and traversal rejection stay in
// http.FileServer, which owns that contract.
func (g *Gateway) serveStatic(w http.ResponseWriter, r *http.Request) {
	if g.static == nil {
		writeJSONError(w, http.StatusServiceUnavailable, ErrNoStaticBundle.Error())
		return
	}
	g.static.ServeHTTP(w, r)
}

// serveIndex tells the truth about the bundle instead of a 404 stack.
func (g *Gateway) serveIndex(w http.ResponseWriter, r *http.Request) {
	if g.static == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "meept-kanban sidecar is running; the web bundle is not installed (build it with `make build-gui` and point WEBDIR at it).")
		return
	}
	http.Redirect(w, r, "/web/", http.StatusFound)
}

// StaticGrantStore is an in-process token→grant map (tests, embedding
// hosts). The production GrantChecker is TokenStore (pairing.go): it is
// file-backed, survives restarts, and mints tokens through pairing codes.
// NewStaticGrantStore starts empty so a store with no granted tokens
// authorizes nothing. Safe for concurrent read/write.
type StaticGrantStore struct {
	mu     sync.RWMutex
	grants map[string][]string
}

// NewStaticGrantStore returns an empty store: no token is known, so no request
// is authorized.
func NewStaticGrantStore() *StaticGrantStore {
	return &StaticGrantStore{grants: make(map[string][]string)}
}

// Grant associates a token with a grant set, replacing any previous set.
func (s *StaticGrantStore) Grant(token string, grants ...string) {
	if s == nil || token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.grants == nil {
		s.grants = make(map[string][]string)
	}
	s.grants[token] = append([]string(nil), grants...)
}

// Revoke drops a token's grants entirely.
func (s *StaticGrantStore) Revoke(token string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.grants, token)
}

// Grants implements GrantChecker. The bool reports whether the token is known
// at all; an unknown token never carries grants.
func (s *StaticGrantStore) Grants(token string) ([]string, bool) {
	if s == nil || token == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	grants, ok := s.grants[token]
	if !ok {
		return nil, false
	}
	return append([]string(nil), grants...), true
}

// IdleSource is the stand-in EventSource until the daemon board-event feed
// lands. Subscribe returns a channel that closes when ctx ends and never
// yields a board delta.
type IdleSource struct{}

// Subscribe implements EventSource.
func (IdleSource) Subscribe(ctx context.Context) (<-chan json.RawMessage, error) {
	out := make(chan json.RawMessage)
	go func() {
		<-ctx.Done()
		close(out)
	}()
	return out, nil
}

// StaticDir reports whether a web bundle is wired (the root itself stays
// private so no caller can repoint the handler). Used by boot logs and tests.
func (g *Gateway) HasStaticBundle() bool { return g.static != nil }

// ReadIdleTimeout is exported so the sidecar main can align listener tuning
// with the relay's expectations; the gateway itself keeps no timers.
const ReadIdleTimeout = 60 * time.Second
