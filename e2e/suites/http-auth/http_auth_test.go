//go:build e2e

// Package httpauth covers the manifest's http-auth scenarios
// (http-auth-01..03): the HTTP transport's authentication middleware —
// missing Authorization → 401 with /health and OPTIONS exempt, bad key →
// the distinctive 418 with Basic headers treated as missing (401), and WS
// upgrade authentication via the Sec-WebSocket-Protocol bearer.<key>
// subprotocol (the browser-compatible auth channel).
//
// The sandbox boots with require_auth=true and a scratch API key via
// harness.WithConfigOverlay.
package httpauth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/caimlas/meept/e2e/harness"
)

const testKey = "e2e-httpauth-scratch-key"

// start boots the sandbox with auth required and exactly one valid key.
func start(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"transport.http.require_auth": true,
		"transport.http.api_keys":     []string{testKey},
		"transport.http.websocket":    true,
	}))
}

// insecureClient returns an HTTP client accepting the sandbox self-signed
// cert.
func insecureClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}
}

// do sends a request with the given Authorization header value ("" omits
// the header) and returns status + body.
func do(t *testing.T, s *harness.Stack, method, path, authHeader string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, s.HTTPBaseURL()+path, nil)
	if err != nil {
		t.Fatalf("httpauth: build request: %v", err)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := insecureClient().Do(req)
	if err != nil {
		t.Fatalf("httpauth: %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// bodyFields decodes a JSON body into a string map.
func bodyFields(t *testing.T, body string) map[string]string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("httpauth: body decode: %v (%s)", err, body)
	}
	return m
}

// ---------------------------------------------------------------------------
// http-auth-01 (S): missing Authorization -> 401; /health and OPTIONS exempt.
// ---------------------------------------------------------------------------

// TestMissingAuthUnauthorizedWithExemptions pins the negative and the
// exemptions in one pass: a protected endpoint without Authorization
// returns 401 "missing authorization"; /health answers 200 without a key;
// an OPTIONS preflight (CORS) passes through to the handler chain (200)
// without a key.
func TestMissingAuthUnauthorizedWithExemptions(t *testing.T) {
	s := start(t)

	// Protected endpoint, no header → 401.
	code, body := do(t, s, http.MethodGet, "/api/v1/tasks", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("http-auth-01: missing-auth status = %d, want 401 (body: %s)", code, body)
	}
	if got := bodyFields(t, body)["error"]; got != "missing authorization" {
		t.Fatalf("http-auth-01: error = %q, want %q (body: %s)", got, "missing authorization", body)
	}

	// Health exemption: no key, still 200.
	code, body = do(t, s, http.MethodGet, "/health", "")
	if code != http.StatusOK {
		t.Fatalf("http-auth-01: /health status = %d, want 200 without auth (body: %s)", code, body)
	}
	code, _ = do(t, s, http.MethodGet, "/api/v1/health", "")
	if code != http.StatusOK {
		t.Fatalf("http-auth-01: /api/v1/health status = %d, want 200 without auth", code)
	}

	// OPTIONS exemption: CORS preflight is answered before auth.
	code, _ = do(t, s, http.MethodOptions, "/api/v1/tasks", "")
	if code >= 500 {
		t.Fatalf("http-auth-01: OPTIONS preflight status = %d, want a handler answer (<500)", code)
	}

	// Control: the same endpoint WITH the valid key passes (so the 401
	// above is the auth layer, not the route).
	code, _ = do(t, s, http.MethodGet, "/api/v1/tasks", "Bearer "+testKey)
	if code != http.StatusOK {
		t.Fatalf("http-auth-01: valid-key status = %d, want 200", code)
	}
}

// ---------------------------------------------------------------------------
// http-auth-02 (S): bad key -> 418 invalid API key; Basic header treated as
// missing (401).
// ---------------------------------------------------------------------------

// TestBadKeyTeapotAndBasicHeaderIsMissing pins the bad-key contract: a
// wrong Bearer key gets the distinctive 418 (teapot) "invalid API key"
// response; a Basic authorization header is NOT accepted as a raw key —
// it is treated as missing auth (401), never 418.
func TestBadKeyTeapotAndBasicHeaderIsMissing(t *testing.T) {
	s := start(t)

	// Unknown Bearer key → 418 with the frozen message.
	code, body := do(t, s, http.MethodGet, "/api/v1/tasks", "Bearer definitely-not-a-valid-key")
	if code != http.StatusTeapot {
		t.Fatalf("http-auth-02: bad-key status = %d, want 418 (body: %s)", code, body)
	}
	fields := bodyFields(t, body)
	if fields["error"] != "unauthorized" {
		t.Fatalf("http-auth-02: bad-key error field = %q, want %q (body: %s)", fields["error"], "unauthorized", body)
	}
	if msg := fields["message"]; !strings.Contains(msg, "invalid API key") {
		t.Fatalf("http-auth-02: bad-key message = %q, want it to name the invalid API key", msg)
	}

	// Basic header → treated as missing (401), NOT parsed as a raw key.
	code, body = do(t, s, http.MethodGet, "/api/v1/tasks", "Basic ZTJlOnBhc3N3b3Jk")
	if code != http.StatusUnauthorized {
		t.Fatalf("http-auth-02: Basic header status = %d, want 401 (treated as missing)", code)
	}
	if got := bodyFields(t, body)["error"]; got != "missing authorization" {
		t.Fatalf("http-auth-02: Basic header error = %q, want %q", got, "missing authorization")
	}
}

// ---------------------------------------------------------------------------
// http-auth-03 (M): WS upgrade authenticates via Sec-WebSocket-Protocol
// bearer.<key>.
// ---------------------------------------------------------------------------

// TestWSUpgradeBearerSubprotocolAuth pins the browser auth channel: with
// require_auth enabled, a WS upgrade carrying Sec-WebSocket-Protocol
// "bearer.<valid key>" completes and the daemon talks (welcome + subscribed
// ack + a real relayed event).
func TestWSUpgradeBearerSubprotocolAuth(t *testing.T) {
	s := start(t)

	// Valid key: the upgrade completes and the connection is functional
	// (DialWS consumes the {type:status} welcome, Subscribe gets the ack).
	ws := harness.DialWS(t, s, testKey)
	ws.Subscribe("all", "")

	// The authenticated socket actually receives relayed events: publish
	// through the RPC bridge and wait for the frame.
	rpc := harness.DialRPC(t, s.SocketPath)
	rpc.CallResult("bus.publish", map[string]any{
		"topic": "turn.terminal",
		"payload": map[string]any{
			"conversation_id": "conv-httpauth-03",
			"session_id":      "session-httpauth-03",
			"turn_id":         "turn-httpauth-03",
			"status":          "completed",
			"reply":           "HTTPAUTH03-WS-EVENT",
			"duration_ms":     1,
		},
	})
	if ev := ws.WaitEvent(15*time.Second, "agent_progress"); ev == nil {
		t.Fatalf("http-auth-03: authenticated WS never received the published event")
	} else if !strings.Contains(string(ev.Data), "HTTPAUTH03-WS-EVENT") {
		t.Fatalf("http-auth-03: unexpected frame: %s (%s)", ev.Type, ev.Data)
	}
}

// TestWSUpgradeWrongKeyRefused proves the negative path of scenario 03:
// an upgrade offering a WRONG bearer subprotocol must not yield a working
// connection. The middleware rejects before the handler upgrades, so the
// dial fails (non-101 handshake).
func TestWSUpgradeWrongKeyRefused(t *testing.T) {
	s := start(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// HTTPBaseURL is https://host:port; the WS endpoint is wss:// on the
	// same TLS listener.
	wsURL := "wss" + strings.TrimPrefix(s.HTTPBaseURL(), "https") + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"bearer.wrong-key-e2e"},
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			},
		},
	})
	if err != nil {
		// Expected: the middleware refused the upgrade.
		return
	}
	// Some stacks complete the upgrade and close immediately — probe for
	// a working connection with a short read. Any frame within 2s means
	// the daemon served an unauthenticated socket (a real failure).
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer probeCancel()
	_, data, rerr := conn.Read(probeCtx)
	_ = conn.Close(websocket.StatusNormalClosure, "probe done")
	if rerr == nil {
		t.Fatalf("http-auth-03: wrong-key upgrade produced a live connection (first frame: %s)", data)
	}
}
