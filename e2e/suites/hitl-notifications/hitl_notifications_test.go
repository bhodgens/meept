//go:build e2e

// Package hitlnotifications is the hermetic e2e suite for the HITL
// notifications tree (docs/plans/20261031-hitl-notifications, leaf 05).
// Four scenarios, one daemon per scenario:
//
//	hitl-slack-01   Slack round trip incl. late-click fail-closed: a
//	                signed interaction POSTed to the daemon's
//	                /api/v1/slack/interactions route resolves a pending
//	                ACP permission with approver slack:<uid>; a click
//	                arriving after the (disableable) relay timeout
//	                fail-closes - no pending request, reason timeout.
//	hitl-gate-02    gate park -> restart -> resume: a ParkKindGate
//	                checkpoint (plan.submitting) survives a daemon
//	                restart over the SAME MEEPT_HOME and resumes exactly
//	                once when the approval arrives out-of-band.
//	hitl-deep-03    deep-link web fallback substrate: the running
//	                daemon's internal/comm/web server serves the session
//	                surface for the C6 fallback URL's session id (the
//	                Dart-side resolver is leaf 03's flutter tests; this
//	                proves the Go-side substrate only).
//	hitl-regime-04  regime report generation: the internal/eval/hitl
//	                Runner produces a HitlRegimeReport across all three
//	                regimes with every C7 field populated; the markdown
//	                renders.
//
// Hermetic Slack: a local httptest server doubles the Slack Web API and
// [comm.slack] api_base_url points at it. Signing steps per
// internal/comm/slack/interactions.go: v0 HMAC-SHA256 over
// "v0:<timestamp>:<raw body>" keyed with the signing secret, hex digest
// with the "v0=" prefix, timestamp within the 5-minute replay window,
// body = application/x-www-form-urlencoded with a single `payload`
// parameter. The relay stays the ONLY timeout authority; the scenario
// makes it disableable by arming the relay with a short [acp]
// relay_timeout and simply not answering within it.
package hitlnotifications

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/eval/hitl"
)

// scratch TLS config (self-signed scratch cert, harness convention).
func insecureHTTP() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			// InsecureSkipVerify: the scratch daemon uses a self-signed cert.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}
}

// stubAgentBin builds the fake ACP agent (internal/acp testdata) - the
// same subprocess the acp-wire suite uses, exercised over the daemon.
func stubAgentBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakeagent")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/fakeagent")
	cmd.Dir = filepath.Join(repoRoot(), "internal", "acp")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakeagent: %v\n%s", err, out)
	}
	return bin
}

func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0) //nolint:dogsled // identity probe
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

// signSlack renders the v0 signature for a raw body per
// internal/comm/slack/interactions.go verifyRequest.
func signSlack(secret string, ts int64, raw []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + strconv.FormatInt(ts, 10) + ":"))
	mac.Write(raw)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

// slackInteractionBody builds the form-urlencoded interaction callback
// body for one button click (C3 grammar: perm:<requestID>:<verb>).
func slackInteractionBody(userID, requestID, verb string) []byte {
	payload := fmt.Sprintf(`{"type":"block_actions","user":{"id":%q},"actions":[{"action_id":"b","value":"perm:%s:%s"}]}`,
		userID, requestID, verb)
	return []byte("payload=" + urlEncode(payload))
}

// urlEncode is the minimal form encoder for the payload parameter
// (percent-encode everything outside the unreserved set that appears).
func urlEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// postInteraction POSTs a signed Slack interaction callback to the daemon.
func postInteraction(t *testing.T, s *harness.Stack, secret string, body []byte) (int, string) {
	t.Helper()
	ts := time.Now().Unix()
	req, err := http.NewRequest(http.MethodPost, s.HTTPBaseURL()+"/api/v1/slack/interactions",
		strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("build interaction request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Slack-Signature", signSlack(secret, ts, body))
	resp, err := insecureHTTP().Do(req)
	if err != nil {
		t.Fatalf("POST interaction: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// waitForPendingPermission polls acp.permissions.list over RPC until the
// relay reports a pending request, returning its id.
func waitForPendingPermission(t *testing.T, s *harness.Stack) string {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		sess := dialRPCS(t, s.SocketPath)
		raw, err := sess.call("acp.permissions.list", map[string]any{})
		sess.Close()
		if err == nil {
			var res struct {
				Enabled bool `json:"enabled"`
				Pending []struct {
					ID string `json:"id"`
				} `json:"pending"`
			}
			if json.Unmarshal(raw, &res) == nil && res.Enabled && len(res.Pending) > 0 {
				return res.Pending[0].ID
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no pending ACP permission within 60s; daemon log tail:\n%s", s.Daemon.LogTail())
	return ""
}

// acpStack boots the daemon with the human relay armed + the Slack approval
// channel pointed at the local Slack-API double (the acp-wire stack shape
// plus [comm.slack]).
func acpStack(t *testing.T, bin, slackBase string) *harness.Stack {
	t.Helper()
	return harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"transport.http.websocket":           true,
			"security.require_confirmation_high": false,
			"acp": map[string]any{
				"enabled":         true,
				"permission_mode": "human",
				"relay_timeout":   3, // disableable-in-practice: the late click waits past this
				"dial_timeout":    30,
				"call_timeout":    20,
				"max_agents":      2,
			},
			"slack": map[string]any{
				"enabled":          true,
				"bot_token":        "xoxb-e2e-scratch",
				"signing_secret":   "e2e-signing-secret",
				"channel_id":       "CE2ECHAN",
				"allowed_user_ids": []string{"UE2EUSER"},
				"api_base_url":     slackBase,
			},
		}),
		harness.WithPreBootHook(func(s *harness.Stack) error {
			catalog := `{"agents":[{"id":"perm-agent","command":["` + bin +
				`","-mode","permission"],"enabled":true}]}`
			if err := os.WriteFile(filepath.Join(s.MeeptHome, "acp_agents.json5"), []byte(catalog), 0o600); err != nil {
				return err
			}
			chatSpec := filepath.Join(s.MeeptHome, "agents", "chat", "AGENT.md")
			data, err := os.ReadFile(chatSpec)
			if err != nil {
				return err
			}
			patched := strings.Replace(string(data),
				"additional_tools:\n", "additional_tools:\n  - acp_agent\n", 1)
			if patched == string(data) {
				return errNoAdditionalTools
			}
			return os.WriteFile(chatSpec, []byte(patched), 0o644)
		}),
	)
}

type staticError string

func (e staticError) Error() string { return string(e) }

const errNoAdditionalTools staticError = "chat AGENT.md has no additional_tools block to extend"

// launchPermAgent drives the acp_agent launch turn (spawns the fakeagent
// subprocess inside the daemon).
func launchPermAgent(t *testing.T, s *harness.Stack, sessionID string) {
	t.Helper()
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat for hitl slack"}`)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "acp_agent",
		Arguments: `{"agent":"perm-agent","verb":"launch"}`,
	})
	s.ChatTurn(t, sessionID, "launch the perm-agent external coding agent", 120*time.Second)
}

// ---------------------------------------------------------------------------
// Scenario hitl-slack-01: signed interaction resolves; late click fails
// closed.
// ---------------------------------------------------------------------------

func TestSlackRoundTripAndLateClickFailClosed(t *testing.T) {
	// Slack Web API double: chat.postMessage answers ok with a ts.
	slackDouble := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(io.Discard, r.Body)                             //nolint:errcheck // drain the request
		_, _ = w.Write([]byte(`{"ok":true,"ts":"1700000000.000100"}`)) //nolint:errcheck // httptest handler
	}))
	t.Cleanup(slackDouble.Close)

	bin := stubAgentBin(t)
	s := acpStack(t, bin, slackDouble.URL)

	// Wiring proof: the enabled lines landed (full log file - LogTail is
	// a 4KB window).
	logBytes, err := os.ReadFile(s.Daemon.LogPath())
	if err != nil {
		t.Fatalf("read daemon log: %v", err)
	}
	if !strings.Contains(string(logBytes), "slack approval channel enabled") {
		t.Fatalf("slack channel not enabled at boot; daemon log:\n%s", logBytes)
	}
	if !strings.Contains(string(logBytes), "slack interactions endpoint enabled") {
		t.Fatalf("slack interactions endpoint not registered; daemon log:\n%s", logBytes)
	}

	s.RegisterProject(t, "hitl-project")
	sessionID := s.CreateSession(t, "hitl-slack", s.ProjectDir)
	launchPermAgent(t, s, sessionID)

	// LEG 1: the signed ALLOW click resolves with slack:<uid> attribution.
	turnDone := make(chan string, 1)
	go func() {
		s.Fake.EnqueueToolCalls(harness.ToolCall{
			Name:      "acp_agent",
			Arguments: `{"agent":"perm-agent","verb":"send","message":"grant"}`,
		})
		out, _ := s.RunCLI(t, 150*time.Second, true,
			"chat", "--session", sessionID, "ask perm-agent to do: grant")
		turnDone <- out
	}()

	pendID := waitForPendingPermission(t, s)

	// Negative auth probe first: a signature from a NON-allowed user must
	// not resolve (allowed_user_ids is the authorization boundary).
	_, _ = postInteraction(t, s, "e2e-signing-secret",
		slackInteractionBody("UNAUTHORIZED-USER", pendID, "allow"))

	// The signed allow from the allowed user.
	status, body := postInteraction(t, s, "e2e-signing-secret",
		slackInteractionBody("UE2EUSER", pendID, "allow"))
	if status != http.StatusOK {
		t.Fatalf("signed allow interaction status = %d: %s", status, body)
	}

	// The resolved permission carries approver slack:<uid>: the WS frame
	// and the acp.permissions surface both derive from the same resolved
	// record; the daemon log names the approver (interactions.go logs
	// "slack: permission resolved" with approver slack:UE2EUSER).
	assertSlackApprover(t, s, "slack:UE2EUSER", 60*time.Second)
	<-turnDone

	// LEG 2: late click fail-closed. The relay timeout is 3s: register a
	// request, wait past the timeout (reason timeout), then click.
	turnDone2 := make(chan string, 1)
	go func() {
		s.Fake.EnqueueToolCalls(harness.ToolCall{
			Name:      "acp_agent",
			Arguments: `{"agent":"perm-agent","verb":"send","message":"ignore"}`,
		})
		out, _ := s.RunCLI(t, 150*time.Second, true,
			"chat", "--session", sessionID, "ask perm-agent to do: ignore")
		turnDone2 <- out
	}()

	lateID := waitForPendingPermission(t, s)
	// Wait past the 3s relay timeout: the relay fail-closes the request.
	time.Sleep(5 * time.Second)
	status, body = postInteraction(t, s, "e2e-signing-secret",
		slackInteractionBody("UE2EUSER", lateID, "allow"))
	// The HTTP ack is still 200 (Slack demands it); the resolve is the
	// no-pending path (interactions.go logs debug "no pending permission"
	// and the relay records reason timeout).
	if status != http.StatusOK {
		t.Fatalf("late-click interaction status = %d: %s (Slack acks 200 even on no-pending)", status, body)
	}
	assertTimedOutNoPending(t, s, lateID, 30*time.Second)
	<-turnDone2
}

// assertSlackApprover polls the daemon log for the slack approver
// attribution on the resolved permission.
func assertSlackApprover(t *testing.T, s *harness.Stack, approver string, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(s.Daemon.LogPath())
		if err == nil && strings.Contains(string(data), "permission resolved") &&
			strings.Contains(string(data), approver) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	data, _ := os.ReadFile(s.Daemon.LogPath())
	t.Fatalf("no %s approver attribution within %s; daemon log tail:\n%s", approver, budget, tail(string(data), 4096))
}

// assertTimedOutNoPending proves the late click found no pending request:
// the daemon log carries the interactions handler's no-pending debug lane
// or the relay's timeout resolution for the id, and the id is gone from
// the pending registry.
func assertTimedOutNoPending(t *testing.T, s *harness.Stack, id string, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		sess := dialRPCS(t, s.SocketPath)
		raw, err := sess.call("acp.permissions.list", map[string]any{})
		sess.Close()
		pending := true
		if err == nil {
			var res struct {
				Pending []struct {
					ID string `json:"id"`
				} `json:"pending"`
			}
			if json.Unmarshal(raw, &res) == nil {
				pending = false
				for _, p := range res.Pending {
					if p.ID == id {
						pending = true
					}
				}
			}
		}
		data, _ := os.ReadFile(s.Daemon.LogPath())
		logHasTimeout := strings.Contains(string(data), "human relay timed out") ||
			strings.Contains(string(data), "no pending permission") ||
			strings.Contains(string(data), `"reason":"timeout"`) ||
			strings.Contains(string(data), "reason=timeout")
		if !pending && logHasTimeout {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	data, _ := os.ReadFile(s.Daemon.LogPath())
	t.Fatalf("late click was not fail-closed within %s (id still pending or no timeout record); log tail:\n%s",
		budget, tail(string(data), 4096))
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// ---------------------------------------------------------------------------
// Scenario hitl-gate-02: gate park -> restart -> resume.
// ---------------------------------------------------------------------------

func TestGateParkRestartResume(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"plans.mode":                      "always",
		"plans.approval.require_approval": true,
	}))
	s.RegisterProject(t, "hitl-gate-project")
	sessionID := s.CreateSession(t, "hitl-gate", s.ProjectDir)

	// Submit a planning turn: plans.mode always parks it at
	// pending_approval; the plan.submitting bus event checkpoints the gate
	// (GateParker over parks.db).
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned for gate park"}`)
	ack := s.SubmitChatHTTP(t, sessionID, "Create a small plan for organizing the project docs. GATE-MARKER")

	// plan.list_by_session keys on the plan's SourceSession, which is the
	// chat CONVERSATION id (the bus events carry conversation_id per the
	// session_id/conversation_id contract) - resolve it from the session
	// record.
	sess0 := dialRPCS(t, s.SocketPath)
	got0, err := sess0.call("session.get", map[string]any{"id": sessionID})
	sess0.Close()
	if err != nil {
		t.Fatalf("session.get: %v", err)
	}
	var sessRec struct {
		ConversationID string `json:"conversation_id"`
	}
	if json.Unmarshal(got0, &sessRec) != nil || sessRec.ConversationID == "" {
		t.Fatalf("session %s has no conversation_id: %s", sessionID, got0)
	}

	// The pending plan appears (the gate).
	planID := waitForPendingPlan(t, s, sessRec.ConversationID, 90*time.Second)
	if planID == "" {
		t.Fatal("no pending_approval plan appeared")
	}
	// The gate checkpoint persisted: parks.db carries a ParkKindGate row.
	gateRows := waitForGateParkRow(t, filepath.Join(s.StateDir, "parks.db"), planID, 30*time.Second)
	if gateRows == 0 {
		t.Fatalf("no ParkKindGate persistence row for plan %s (checkpoint missing)", planID)
	}

	// RESTART over the SAME MEEPT_HOME: the TurnParker.Start re-arm reloads
	// surviving gate rows.
	removeStalePidfile(t, s)
	stop := bootRestart(t, s)
	defer stop()

	// Out-of-band approve via RPC (the plan.approved event pumps
	// ResumeByKey -> the at-most-once resume).
	sess := dialRPCS(t, s.SocketPath)
	defer sess.Close()
	if _, err := sess.call("plan.approve", map[string]any{
		"plan_id": planID, "session_id": sessRec.ConversationID, "by": "e2e-out-of-band",
	}); err != nil {
		t.Fatalf("plan.approve: %v", err)
	}

	// Resume exactly once: the gate checkpoint log fires
	// ("gate checkpoint resumed" / "gate resumed on plan approval") and a
	// SECOND approve finds nothing (at-most-once).
	assertGateResumedOnce(t, s, planID, 60*time.Second)
	if _, err := sess.call("plan.approve", map[string]any{
		"plan_id": planID, "session_id": sessRec.ConversationID, "by": "e2e-duplicate",
	}); err != nil {
		t.Logf("duplicate plan.approve returned %v (the plan is already approved; harmless)", err)
	}
	time.Sleep(2 * time.Second)
	data, _ := os.ReadFile(s.Daemon.LogPath())
	if n := strings.Count(string(data), "gate checkpoint resumed"); n > 1 {
		t.Fatalf("gate resumed %d times, want exactly once (at-most-once resume violated)", n)
	}
	_ = ack
}

// waitForPendingPlan polls plan.list_by_session for a pending_approval plan.
func waitForPendingPlan(t *testing.T, s *harness.Stack, sessionID string, budget time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		sess := dialRPCS(t, s.SocketPath)
		raw, err := sess.call("plan.list_by_session", map[string]any{"session_id": sessionID})
		sess.Close()
		if err == nil {
			var res struct {
				Plans []struct {
					ID    string `json:"id"`
					State string `json:"state"`
				} `json:"plans"`
			}
			if json.Unmarshal(raw, &res) == nil {
				for _, p := range res.Plans {
					if p.State == "pending_approval" && p.ID != "" {
						return p.ID
					}
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return ""
}

// waitForGateParkRow counts parked_turns rows whose persistence key names
// the plan (ParkKindGate rows live in parks.db under kind "gate").
func waitForGateParkRow(t *testing.T, parksDB, planID string, budget time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		n := countGateRows(t, parksDB, planID)
		if n > 0 {
			return n
		}
		time.Sleep(250 * time.Millisecond)
	}
	return 0
}

// assertGateResumedOnce polls the daemon log for the resume of planID.
func assertGateResumedOnce(t *testing.T, s *harness.Stack, planID string, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(s.Daemon.LogPath())
		if err == nil &&
			strings.Contains(string(data), planID) &&
			(strings.Contains(string(data), "gate checkpoint resumed") ||
				strings.Contains(string(data), "gate resumed on plan approval")) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	data, _ := os.ReadFile(s.Daemon.LogPath())
	t.Fatalf("gate %s never resumed within %s; log tail:\n%s", planID, budget, tail(string(data), 4096))
}

// ---------------------------------------------------------------------------
// Scenario hitl-deep-03: the web fallback substrate.
// ---------------------------------------------------------------------------

func TestDeepLinkFallbackSubstrate(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"web": map[string]any{
			"enabled":    true,
			"host":       "127.0.0.1",
			"port":       harness.FreePort(t),
			"secret_key": "e2e-web-secret",
		},
	}))
	s.RegisterProject(t, "hitl-web-project")
	sessionID := s.CreateSession(t, "hitl-web", s.ProjectDir)

	// The C6 fallback URL is <web base>/#/activity?session=<id>: the
	// fragment is CLIENT-side, so the Go-side substrate contract is that
	// the daemon's web server is up and serves the session SURFACE for
	// the id - GET /api/v1/sessions/<id> returns the session record
	// (the payload the GUI session page renders for the fallback target).
	// The Dart-side fallback navigation is leaf 03's flutter tests; this
	// scenario proves the Go substrate only (leaf Notes).
	host := webBaseHost(t, s)
	// The web substrate requires bearer auth (C-04 fail-closed default);
	// the fallback URL the GUI opens carries the same secret the client
	// was provisioned with. Send it as the GUI does.
	client := &http.Client{Timeout: 15 * time.Second}
	get := func(path string) (int, []byte) {
		req, err := http.NewRequest(http.MethodGet, "http://"+host+path, nil) //nolint:nosec // fixed test URL
		if err != nil {
			t.Fatalf("build probe request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer e2e-web-secret")
		resp, err := client.Do(req) //nolint:noctx // bounded probe
		if err != nil {
			t.Fatalf("web substrate unreachable for %s: %v", path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, raw
	}
	// The substrate is UP: health + status answer 200 through the same
	// authenticated mux the fallback URL's session page rides.
	status, raw := get("/health")
	if status != http.StatusOK {
		t.Fatalf("web substrate /health = %d: %s", status, raw)
	}
	status, raw = get("/api/v1/status")
	if status != http.StatusOK {
		t.Fatalf("web substrate /api/v1/status = %d: %s", status, raw)
	}
	// The session surface EXISTS at the fallback route family:
	// GET /api/v1/sessions/{id} answers (200 with the record when the
	// session manager is bound, 503 "session management not configured"
	// when wired out - both prove the route family is served, never a
	// bare 404 mux miss).
	status, raw = get("/api/v1/sessions/" + sessionID)
	if status != http.StatusOK && status != http.StatusServiceUnavailable {
		t.Fatalf("web substrate GET /api/v1/sessions/%s = %d (route family not served): %s",
			sessionID, status, raw)
	}
	if status == http.StatusOK && !strings.Contains(string(raw), sessionID) {
		t.Fatalf("web substrate response for session %s did not carry the session id:\n%s", sessionID, raw)
	}
	// Cross-check: the session identity the fallback URL names is REAL on
	// the daemon's unified HTTP server (the TLS surface the GUI pairs
	// with) - the same session store backs both web surfaces.
	tlsReq, err := http.NewRequest(http.MethodGet, s.HTTPBaseURL()+"/api/v1/sessions/"+sessionID, nil)
	if err != nil {
		t.Fatalf("build tls probe: %v", err)
	}
	tlsResp, err := insecureHTTP().Do(tlsReq) //nolint:noctx // bounded probe
	if err != nil {
		t.Fatalf("tls session probe: %v", err)
	}
	defer tlsResp.Body.Close()
	tlsRaw, _ := io.ReadAll(tlsResp.Body)
	if tlsResp.StatusCode != http.StatusOK || !strings.Contains(string(tlsRaw), sessionID) {
		t.Fatalf("unified HTTP GET /api/v1/sessions/%s = %d: %s", sessionID, tlsResp.StatusCode, tlsRaw)
	}
}

// ---------------------------------------------------------------------------
// Scenario hitl-regime-04: the regime report.
// ---------------------------------------------------------------------------

func TestRegimeReport(t *testing.T) {
	sc := hitl.Scenario{
		ID:          "e2e-regime",
		ChatMessage: "Create the regime scenario artifact and finish.",
		Actions: []hitl.ScenarioAction{
			{Key: "report-step", Action: "file_write", Tool: "file_write"},
		},
	}
	sc = sc.WithDefaults()

	// Drive the Runner over SEAM fakes wired to the real surfaces the
	// leaf pins (ground truth): the decision stream, the terminal feed,
	// and the classified action stream - the same seams leaf 04's own
	// tests drive, exercised here through the exported e2e surface
	// (internal/eval/hitl is importable from the e2e tier by design).
	runner := &hitl.Runner{PollInterval: time.Millisecond}
	var report hitl.HitlRegimeReport
	for _, regime := range hitl.AllRegimes {
		var res *hitl.RunResult
		switch regime {
		case hitl.RegimePerAction:
			res = runner.Drive(hitl.RunConfig{
				Scenario:  sc,
				Regime:    regime,
				SessionID: "sess-regime",
				Perms:     fakePerms{},
				Human:     hitl.NewScriptedHuman(map[string]string{"report-step": "allow"}, "deny"),
				Terminal: func() hitl.TerminalStatus {
					return hitl.TerminalStatus{Done: true, Completed: true, TaskID: "task-regime"}
				},
			})
		case hitl.RegimePlanGateLead:
			res = runner.Drive(hitl.RunConfig{
				Scenario:  sc,
				Regime:    regime,
				SessionID: "sess-regime",
				Gates:     &fakeGates{},
				Human:     hitl.NewScriptedHuman(map[string]string{"regime-plan": "allow"}, "deny"),
				Terminal: func() hitl.TerminalStatus {
					return hitl.TerminalStatus{Done: true, Completed: true, TaskID: "task-regime"}
				},
			})
		default:
			res = runner.Drive(hitl.RunConfig{
				Scenario: sc,
				Regime:   regime,
				Terminal: func() hitl.TerminalStatus {
					return hitl.TerminalStatus{Done: true, Completed: true, TaskID: "task-regime"}
				},
			})
		}
		if res == nil {
			t.Fatalf("no run result for regime %s", regime)
		}
		acts := []hitl.ActionRecord{{Seq: 1, Tool: "file_write", Outcome: "success", Key: "report-step"}}
		oracle := hitl.NewScenarioOracle(nil, nil)
		oracle.ClassifyActions(acts)
		sample := hitl.RunSpotCheck(acts, sc.SampleRate, sc.SampleSeed, oracle.Classify)
		m := hitl.Compute(regime, sc, hitl.ComputeOptions{
			Decisions: hitl.DecisionStream{Events: res.Events},
			Terminal:  hitl.TerminalState{Attempts: res.Attempts, Completeds: 1, Recorded: true},
			WallClock: 250 * time.Millisecond,
			Actions:   acts,
			Sample:    sample,
			Oracle:    hitl.OracleLabel,
		})
		report.Scenarios = append(report.Scenarios, m)
	}
	report.GeneratedAt = time.Now().UTC()

	// Every C7 field populated for all three regimes.
	if len(report.Scenarios) != 3 {
		t.Fatalf("report scenarios = %d, want 3 regimes", len(report.Scenarios))
	}
	regimes := map[string]bool{}
	for _, m := range report.Scenarios {
		regimes[m.Regime] = true
		if m.Regime == "" || m.ScenarioID == "" {
			t.Fatalf("regime/scenario_id empty in %+v", m)
		}
		if !m.TerminalRecorded {
			t.Fatalf("terminal_recorded=false in %+v (the attempt never closed)", m)
		}
		if m.CompletionRate != 1 {
			t.Errorf("completion_rate = %v, want 1 in %+v", m.CompletionRate, m)
		}
		if m.WallClockSeconds <= 0 {
			t.Errorf("wall_clock_seconds = %v, want > 0 in %+v", m.WallClockSeconds, m)
		}
		if m.EscapeOracle == "" || m.MetricProvenance == "" {
			t.Errorf("escape_oracle/metric_provenance empty in %+v", m)
		}
		if m.EscapeRate == nil {
			t.Errorf("escape_rate map nil in %+v", m)
		}
	}
	for _, want := range []string{"per_action", "plan_gate_lead", "autonomous_sampled"} {
		if !regimes[want] {
			t.Fatalf("regime %s missing from the report: %+v", want, report.Scenarios)
		}
	}

	// Interrupt metrics: per_action counts the allow, plan_gate_lead the
	// gate, autonomous_sampled zero interventions.
	byRegime := map[string]hitl.RegimeMetrics{}
	for _, m := range report.Scenarios {
		byRegime[m.Regime] = m
	}
	if got := byRegime["per_action"].InterruptCount; got != 1 {
		t.Errorf("per_action interrupt_count = %d, want 1", got)
	}
	if got := byRegime["plan_gate_lead"].InterruptCount; got != 1 {
		t.Errorf("plan_gate_lead interrupt_count = %d, want 1", got)
	}
	if got := byRegime["autonomous_sampled"].InterruptCount; got != 0 {
		t.Errorf("autonomous_sampled interrupt_count = %d, want 0", got)
	}

	// JSON round trip + markdown render (leaf T4 surfaces).
	dir := t.TempDir()
	jsonPath, mdPath, err := hitl.WriteReport(dir, report)
	if err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read report json: %v", err)
	}
	var round hitl.HitlRegimeReport
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("report json does not parse into HitlRegimeReport: %v (%s)", err, raw)
	}
	if len(round.Scenarios) != 3 {
		t.Fatalf("round-tripped scenarios = %d, want 3", len(round.Scenarios))
	}
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read summary md: %v", err)
	}
	for _, cell := range []string{"per_action", "plan_gate_lead", "autonomous_sampled", "completion_rate", "escape_rate"} {
		if !strings.Contains(string(md), cell) {
			t.Errorf("markdown summary missing %q:\n%s", cell, md)
		}
	}
}

// fakePerms/fakeGates are the harness seams for the Runner (leaf 04's own
// fakes are package-private; these satisfy the same interfaces).
type fakePerms struct{}

func (fakePerms) Pending() ([]hitl.PendingPermission, error) {
	return []hitl.PendingPermission{{
		ID: "perm-regime-1", AgentID: "perm-agent", SessionID: "sess-regime",
		Summary: "report-step: write the regime artifact",
	}}, nil
}

func (fakePerms) Resolve(id, decision string) error { return nil }

type fakeGates struct{ answered bool }

func (f *fakeGates) PendingPlan(string) (string, string, bool, error) {
	if f.answered {
		return "", "", false, nil
	}
	return "plan-regime-1", "regime-plan: organize the docs", true, nil
}

func (f *fakeGates) DecidePlan(planID, sessionID, decision string) error {
	f.answered = true
	return nil
}
