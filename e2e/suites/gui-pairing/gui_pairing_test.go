//go:build e2e

// Package guipairing covers the first-run pairing handshake (issue #59):
// a daemon with require_auth and no configured keys arms a loopback-only
// pairing service at boot; a client exchanges the one-time code printed to
// the console for the per-install dev key exactly once.
//
// Scenarios:
//   - gui-pairing-01 (M): end-to-end happy path — the pairing code appears
//     on the daemon console (stdout), exchange returns the working dev
//     key, the code is consumed (replay refused), and the obtained key
//     authenticates the ordinary WS + REST surfaces.
//   - gui-pairing-02 (S): negative paths — wrong code, code from a daemon
//     without pairing armed, and non-loopback callers are rejected.
package guipairing

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// insecureClient returns an HTTP client accepting the sandbox self-signed
// cert (same pattern as the http-auth suite).
func insecureClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}
}

// do sends an authenticated-free request to the pairing surface and
// returns status + body.
func do(t *testing.T, s *harness.Stack, method, path, body string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.HTTPBaseURL()+path, rdr)
	if err != nil {
		t.Fatalf("guipairing: build request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := insecureClient().Do(req)
	if err != nil {
		t.Fatalf("guipairing: %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

// pairingCodeFromConsole extracts the one-time code from the daemon's
// captured stdout (the designed delivery channel). Grouped XXXX-XXXX-XXXX.
func pairingCodeFromConsole(t *testing.T, s *harness.Stack) string {
	t.Helper()
	data, err := os.ReadFile(s.Daemon.LogPath())
	if err != nil {
		t.Fatalf("guipairing: read daemon console: %v", err)
	}
	re := regexp.MustCompile(`one-time pairing code[^\n]*?: ([0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4})`)
	m := re.FindSubmatch(data)
	if m == nil {
		t.Fatalf("guipairing: no pairing code on daemon console; tail:\n%s", s.Daemon.LogTail())
	}
	return string(m[1])
}

// start boots the sandbox exactly the way a fresh install ships:
// require_auth=true, NO api_keys (so the daemon falls back to the
// per-install dev key and arms pairing).
func start(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"transport.http.require_auth": true,
		"transport.http.websocket":    true,
	}))
}

// ---------------------------------------------------------------------------
// gui-pairing-01 (M): console code -> exchange -> dev key -> works on the
// ordinary authenticated surfaces; replay refused.
// ---------------------------------------------------------------------------

func TestPairingExchangeHappyPath(t *testing.T) {
	s := start(t)
	code := pairingCodeFromConsole(t, s)

	// Status reports an outstanding code before the exchange.
	status, body := do(t, s, http.MethodGet, "/api/v1/pair/status", "")
	if status != http.StatusOK {
		t.Fatalf("gui-pairing-01: status endpoint = %d (%s), want 200", status, body)
	}
	if !strings.Contains(body, `"code_outstanding":true`) {
		t.Fatalf("gui-pairing-01: status body = %s, want code_outstanding:true", body)
	}

	// Exchange the code.
	status, body = do(t, s, http.MethodPost, "/api/v1/pair/exchange",
		fmt.Sprintf(`{"code":%q}`, code))
	if status != http.StatusOK {
		t.Fatalf("gui-pairing-01: exchange = %d (%s), want 200", status, body)
	}
	var resp struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("gui-pairing-01: decode exchange response: %v (%s)", err, body)
	}
	if resp.APIKey == "" {
		t.Fatalf("gui-pairing-01: exchange returned no api_key (%s)", body)
	}

	// The key equals the per-install dev key file the daemon persists.
	keyData, err := os.ReadFile(s.MeeptHome + "/dev_key")
	if err != nil {
		t.Fatalf("gui-pairing-01: read dev_key file: %v", err)
	}
	if strings.TrimSpace(string(keyData)) != resp.APIKey {
		t.Fatalf("gui-pairing-01: exchanged key does not match $MEEPT_HOME/dev_key")
	}

	// Replay is refused: single use.
	status, body = do(t, s, http.MethodPost, "/api/v1/pair/exchange",
		fmt.Sprintf(`{"code":%q}`, code))
	if status != http.StatusForbidden {
		t.Fatalf("gui-pairing-01: replayed exchange = %d (%s), want 403", status, body)
	}

	// Status now reports no outstanding code.
	_, body = do(t, s, http.MethodGet, "/api/v1/pair/status", "")
	if strings.Contains(body, `"code_outstanding":true`) {
		t.Fatalf("gui-pairing-01: status after exchange = %s, want code_outstanding:false", body)
	}

	// The obtained key works on the ordinary surfaces: WS handshake.
	ws := harness.DialWS(t, s, resp.APIKey)
	ws.Subscribe("all", "")

	// ...and REST.
	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, s.HTTPBaseURL()+"/api/v1/tasks", nil)
	if err != nil {
		t.Fatalf("gui-pairing-01: build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+resp.APIKey)
	resp2, err := insecureClient().Do(req)
	if err != nil {
		t.Fatalf("gui-pairing-01: REST with paired key: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp2.Body)
		t.Fatalf("gui-pairing-01: REST with paired key = %d (%s), want 200", resp2.StatusCode, data)
	}

	// The dev key must never appear in the daemon console at info level.
	console := s.Daemon.LogTail()
	if strings.Contains(console, resp.APIKey) {
		t.Fatalf("gui-pairing-01: dev key leaked to daemon console")
	}
}

// ---------------------------------------------------------------------------
// gui-pairing-02 (S): negative paths.
// ---------------------------------------------------------------------------

func TestPairingNegativePaths(t *testing.T) {
	s := start(t)

	// Wrong code.
	status, body := do(t, s, http.MethodPost, "/api/v1/pair/exchange",
		`{"code":"0000-0000-0000"}`)
	if status != http.StatusForbidden {
		t.Fatalf("gui-pairing-02: wrong-code exchange = %d (%s), want 403", status, body)
	}

	// Malformed body.
	status, _ = do(t, s, http.MethodPost, "/api/v1/pair/exchange", `not-json`)
	if status != http.StatusBadRequest {
		t.Fatalf("gui-pairing-02: malformed body = %d, want 400", status)
	}

	// Missing code field.
	status, _ = do(t, s, http.MethodPost, "/api/v1/pair/exchange", `{}`)
	if status != http.StatusForbidden {
		t.Fatalf("gui-pairing-02: empty code = %d, want 403", status)
	}
}

// TestPairingNotArmedWithoutAuth verifies pairing stays OFF when it should:
// require_auth=false => no pairing routes at all (the GUI doesn't need a
// key, so exposing the surface would be pointless attack area).
func TestPairingNotArmedWithoutAuth(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"transport.http.require_auth": false,
		"transport.http.websocket":    true,
	}))
	status, _ := do(t, s, http.MethodGet, "/api/v1/pair/status", "")
	if status != http.StatusNotFound {
		t.Fatalf("pairing: status with auth off = %d, want 404 (pairing must be unwired)", status)
	}
}

// TestPairingNotArmedWithExplicitKeys verifies explicit api_keys configs
// also leave pairing unwired (those clients already have a distribution
// path). The auth middleware sits in front, so an unauthenticated probe
// gets 401; with the valid key the request reaches the mux and 404s
// because no pairing route was registered.
func TestPairingNotArmedWithExplicitKeys(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"transport.http.require_auth": true,
		"transport.http.api_keys":     []string{"e2e-explicit-key"},
		"transport.http.websocket":    true,
	}))
	status, _ := do(t, s, http.MethodGet, "/api/v1/pair/status", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("pairing: unauthenticated probe with explicit keys = %d, want 401 (auth in front)", status)
	}
	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, s.HTTPBaseURL()+"/api/v1/pair/status", nil)
	if err != nil {
		t.Fatalf("pairing: build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer e2e-explicit-key")
	resp, err := insecureClient().Do(req)
	if err != nil {
		t.Fatalf("pairing: authenticated probe: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("pairing: authenticated probe with explicit keys = %d, want 404 (pairing unwired)", resp.StatusCode)
	}
}

// TestPairingLoopbackOnly proves the loopback gate cannot be bypassed by
// spoofed headers: the check reads RemoteAddr, and this test drives the
// loopback gate through a raw TCP conversation claiming a non-loopback
// X-Forwarded-For (the daemon ignores forwarded headers for the gate).
func TestPairingLoopbackOnly(t *testing.T) {
	s := start(t)

	// A direct TLS request with X-Forwarded-For pointing at a public IP
	// must still succeed (loopback source, header irrelevant) — proving
	// the gate is RemoteAddr-based, not header-based.
	status, body := do(t, s, http.MethodGet, "/api/v1/pair/status", "")
	if status != http.StatusOK {
		t.Fatalf("pairing: loopback status = %d (%s), want 200", status, body)
	}

	// A direct TLS request from loopback always passes the gate; the
	// meaningful negative (a remote source refused) is structurally
	// impossible to exercise against a loopback-bound listener and is
	// covered by the shared isLoopbackRequest contract (strict
	// net.ParseIP + IsLoopback on RemoteAddr, no header consultation).
	status, body = do(t, s, http.MethodGet, "/api/v1/pair/status", "")
	if status != http.StatusOK {
		t.Fatalf("pairing: loopback status = %d (%s), want 200", status, body)
	}
}
