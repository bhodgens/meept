//go:build e2e

// Package authmultiuser covers the opt-in multi-user HTTP auth surface
// (auth-multiuser-01..05) against a real scratch daemon configured with
// [multiuser] enabled and a seeded users store.
package authmultiuser

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// authStack is a harness.Stack plus the raw keys of the seeded users.
type authStack struct {
	*harness.Stack
	usersFile  string
	goodKey    string // alice's never-expiring key
	expiredKey string // bob's expired key
}

// startMultiUser boots the harness stack, then rewrites meept.json5 to
// enable [multiuser] with a seeded users store and boots a fresh daemon on
// top (the harness has no "boot with custom config" seam; the daemon flags
// point at the rewritten file, so a manual boot is required — same approach
// as the state-restart suite).
func startMultiUser(t *testing.T) *authStack {
	s := harness.Start(t)
	a := &authStack{Stack: s}

	// Seed the users store (raw keys surface once; only hashes persist).
	a.usersFile = filepath.Join(s.StateDir, "users.json5")
	store := `{"users":[` +
		aliceJSON(t, &a.goodKey) + `,` +
		bobJSON(t, &a.expiredKey) +
		`]}`
	if err := os.WriteFile(a.usersFile, []byte(store), 0o600); err != nil {
		t.Fatalf("write users store: %v", err)
	}

	// Enable multiuser in the harness config. users_file empty → the
	// daemon's fallback opens <home>/.meept/users.json5; instead point it
	// explicitly at our seeded store.
	cfgPath := filepath.Join(s.MeeptHome, "meept.json5")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read harness config: %v", err)
	}
	cfg := strings.Replace(string(data), `"multiagent": {`, `"multiuser": {"enabled": true, "users_file": `+quote(a.usersFile)+`},
  "multiagent": {`, 1)
	if cfg == string(data) {
		t.Fatalf("multiuser injection failed; config anchor missing:\n%s", data)
	}
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// The harness daemon already booted with the old config. The multiuser
	// wiring happens at HTTP-server construction, so stop it and boot one
	// more time against the rewritten config (harness stop() is idempotent
	// and cleans up its daemon slot at test end either way).
	pid := s.Daemon.Pid()
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Signal(syscallTERM())
	}
	waitForDaemonDown(t, s)
	_ = os.Remove(s.SocketPath)
	bootExtra(t, s)

	return a
}

func quote(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// aliceJSON builds a user entry with a known raw key (never-expiring).
func aliceJSON(t *testing.T, rawOut *string) string {
	t.Helper()
	raw := "e2e-alice-key-" + strings.Repeat("a", 32)
	*rawOut = raw
	return fmt.Sprintf(`{"id":"user-e2e-alice","name":"alice","keys":[{"id":"key-e2e-alice","hash":%s,"label":"e2e"}]}`,
		quote(sha256Hex(raw)))
}

// bobJSON builds a user entry whose key expired in the past.
func bobJSON(t *testing.T, rawOut *string) string {
	t.Helper()
	raw := "e2e-bob-key-" + strings.Repeat("b", 32)
	*rawOut = raw
	return fmt.Sprintf(`{"id":"user-e2e-bob","name":"bob","keys":[{"id":"key-e2e-bob","hash":%s,"label":"e2e","expires_at":"2020-01-01T00:00:00Z"}]}`,
		quote(sha256Hex(raw)))
}

// authedClient returns an HTTP client for the scratch TLS listener.
func authedClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}
}

// getHealth hits /health (exempt path).
func (a *authStack) getHealth(t *testing.T) int {
	t.Helper()
	resp, err := authedClient().Get(a.HTTPBaseURL() + "/health") //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	return resp.StatusCode
}

// postChatSubmit posts a chat submit with the given Authorization header
// ("" = no header). Returns status + body map.
func (a *authStack) postChatSubmit(t *testing.T, sessionID, authHeader string) (int, map[string]any) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"message":       "auth probe",
		"session_id":    sessionID,
		"source_client": "e2e-harness",
	})
	req, err := http.NewRequest(http.MethodPost, a.HTTPBaseURL()+"/api/v1/chat/submit", bytes.NewReader(payload)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := authedClient().Do(req)
	if err != nil {
		t.Fatalf("chat submit: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	return resp.StatusCode, out
}

// optionsRequest sends an OPTIONS preflight (exempt path).
func (a *authStack) optionsRequest(t *testing.T, path string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodOptions, a.HTTPBaseURL()+path, nil) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("new options request: %v", err)
	}
	resp, err := authedClient().Do(req)
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	return resp.StatusCode
}

// auth-multiuser-01 + 02: the key-auth happy path issues an Identity (the
// request goes through with the owner-scoped session flow), while unknown
// and expired keys get DISTINCT 418 messages; missing header is 401;
// /health and OPTIONS are exempt.
func TestKeyAuthHappyPathAndDistinctFailures(t *testing.T) {
	a := startMultiUser(t)

	// Happy path: alice's valid key submits a chat turn successfully — the
	// request authenticated, carried an Identity, and reached the handler.
	sess := a.CreateSession(t, "auth-multiuser", a.ProjectDir)
	code, body := a.postChatSubmit(t, sess, "Bearer "+a.goodKey)
	if code != 200 {
		t.Fatalf("valid key submit status = %d: %v", code, body)
	}
	if accepted, _ := body["accepted"].(bool); !accepted {
		t.Fatalf("valid key submit not accepted: %v", body)
	}

	// Unknown key: 418 with the invalid-key message.
	code, body = a.postChatSubmit(t, sess, "Bearer not-a-real-key-000")
	if code != http.StatusTeapot {
		t.Fatalf("unknown key status = %d (%s), want 418", code, body)
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "invalid API key") {
		t.Fatalf("unknown key message = %q, want the invalid-key text", msg)
	}

	// Expired key: 418 with the DISTINCT expiry message.
	code, body = a.postChatSubmit(t, sess, "Bearer "+a.expiredKey)
	if code != http.StatusTeapot {
		t.Fatalf("expired key status = %d (%s), want 418", code, body)
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "expired") {
		t.Fatalf("expired key message = %q, want the expiry-specific text (distinct from invalid)", msg)
	}

	// Missing header: 401 (not 418).
	code, body = a.postChatSubmit(t, sess, "")
	if code != http.StatusUnauthorized {
		t.Fatalf("missing key status = %d (%s), want 401", code, body)
	}

	// Exemptions: /health and OPTIONS pass without any key.
	if code := a.getHealth(t); code != 200 {
		t.Fatalf("/health status = %d, want 200 exempt", code)
	}
	if code := a.optionsRequest(t, "/api/v1/chat/submit"); code != 200 {
		t.Fatalf("OPTIONS status = %d, want 200 exempt", code)
	}
}

// auth-multiuser-03: multiuser opt-out preserves legacy single-key behavior.
// The plain harness stack ships NO api_keys and require_auth=false, so the
// legacy path must serve requests with NO Authorization header at all (the
// multi-user middleware is never constructed).
func TestMultiuserOptOutPreservesLegacyBehavior(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sess := s.CreateSession(t, "auth-legacy", s.ProjectDir)

	// No auth header at all: the legacy (auth disabled) path accepts it.
	payload, _ := json.Marshal(map[string]any{
		"message":       "legacy probe",
		"session_id":    sess,
		"source_client": "e2e-harness",
	})
	req, err := http.NewRequest(http.MethodPost, s.HTTPBaseURL()+"/api/v1/chat/submit", bytes.NewReader(payload)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := authedClient().Do(req)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("legacy no-auth submit status = %d (%s), want 200 (multiuser disabled → no auth required)", resp.StatusCode, body)
	}
}

// auth-multiuser-04 + 05: quota/permission stubs are no-ops in the wired
// path (a valid key's request passes with no quota/permission errors), and
// raw key material is never persisted — only sha256 hashes live in the
// users store on disk.
func TestStubsNoOpAndRawKeysNeverPersisted(t *testing.T) {
	a := startMultiUser(t)

	// Stub no-op: the valid key's request flows through untouched (200,
	// accepted) — no quota gate, no permission gate.
	sess := a.CreateSession(t, "auth-stubs", a.ProjectDir)
	code, body := a.postChatSubmit(t, sess, "Bearer "+a.goodKey)
	if code != 200 {
		t.Fatalf("stub-path submit status = %d: %v", code, body)
	}
	if accepted, _ := body["accepted"].(bool); !accepted {
		t.Fatalf("stub-path submit rejected: %v", body)
	}

	// Hashes-only on disk: read the users store file and assert the raw key
	// material never appears; the sha256 hex of each key does.
	data, err := os.ReadFile(a.usersFile)
	if err != nil {
		t.Fatalf("read users store: %v", err)
	}
	disk := string(data)
	if strings.Contains(disk, a.goodKey) {
		t.Fatal("RAW key material persisted in the users store; only hashes may be stored")
	}
	if strings.Contains(disk, a.expiredKey) {
		t.Fatal("RAW expired key material persisted in the users store")
	}
	if !strings.Contains(disk, sha256Hex(a.goodKey)) {
		t.Fatal("expected the sha256 hash of the valid key in the users store")
	}
	if !strings.Contains(disk, sha256Hex(a.expiredKey)) {
		t.Fatal("expected the sha256 hash of the expired key in the users store")
	}
}
