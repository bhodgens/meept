package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

func fakeAgentBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakeagent")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/fakeagent")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fakeagent: %v\n%s", err, out)
	}
	return bin
}

func startEcho(t *testing.T, mode string) *Session {
	t.Helper()
	bin := fakeAgentBin(t)
	// Generous ceilings: these tests exercise handshake/session logic, not
	// timing. The 5s DialTimeout failed under full-suite parallel load
	// (subprocess spawn + handshake took >5s on a busy box) and produced
	// flakes; 30s still fails fast on a genuine deadlock.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	s, err := Start(ctx, SessionConfig{
		AgentID:     "echo",
		Command:     []string{bin, "-mode", mode},
		Cwd:         t.TempDir(),
		DialTimeout: 30 * time.Second,
		CallTimeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	return s
}

func TestSession_HandshakeReady(t *testing.T) {
	s := startEcho(t, "echo")
	if s.State() != StateReady {
		t.Fatalf("state = %v, want ready", s.State())
	}
}

func TestSession_HandshakeTimeout(t *testing.T) {
	bin := fakeAgentBin(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := Start(ctx, SessionConfig{
		AgentID:     "slow",
		Command:     []string{bin, "-mode", "slow"},
		Cwd:         t.TempDir(),
		DialTimeout: 200 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestSession_CommandNotFound(t *testing.T) {
	_, err := Start(context.Background(), SessionConfig{
		AgentID: "missing",
		Command: []string{"/no/such/acp-agent-bin"},
		Cwd:     t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSession_BadHandshake(t *testing.T) {
	bin := fakeAgentBin(t)
	_, err := Start(context.Background(), SessionConfig{
		AgentID:     "bad",
		Command:     []string{bin, "-mode", "badhandshake"},
		Cwd:         t.TempDir(),
		DialTimeout: 3 * time.Second,
	})
	if err == nil {
		t.Fatal("expected handshake error")
	}
}

func TestSession_EchoSend(t *testing.T) {
	s := startEcho(t, "echo")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	got, err := s.Send(ctx, "hello")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestSession_CloseIdempotent(t *testing.T) {
	s := startEcho(t, "echo")
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestSession_DieMidFlight(t *testing.T) {
	bin := fakeAgentBin(t)
	_, err := Start(context.Background(), SessionConfig{
		AgentID:     "die",
		Command:     []string{bin, "-mode", "die"},
		Cwd:         t.TempDir(),
		DialTimeout: 2 * time.Second,
	})
	if err == nil {
		t.Fatal("expected die during handshake")
	}
}

func TestSession_PermissionPermissive(t *testing.T) {
	bin := fakeAgentBin(t)
	s, err := Start(context.Background(), SessionConfig{
		AgentID:        "perm",
		Command:        []string{bin, "-mode", "permission"},
		Cwd:            t.TempDir(),
		DialTimeout:    5 * time.Second,
		CallTimeout:    8 * time.Second,
		PermissionMode: "permissive",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	got, err := s.Send(ctx, "hi")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got != "hi" {
		t.Fatalf("got %q", got)
	}
}

func TestSession_PermissionDeny(t *testing.T) {
	bin := fakeAgentBin(t)
	s, err := Start(context.Background(), SessionConfig{
		AgentID:        "perm",
		Command:        []string{bin, "-mode", "permission"},
		Cwd:            t.TempDir(),
		DialTimeout:    5 * time.Second,
		CallTimeout:    8 * time.Second,
		PermissionMode: "deny",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := s.Send(ctx, "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Decision lane (acp-permission-decision leaf 02, master.md Contract B)
// ---------------------------------------------------------------------------

// stubDecisioner records Ask calls and replays a scripted result/error.
// It satisfies the Decisioner seam without touching llm.Resolver plumbing.
type stubDecisioner struct {
	mu        sync.Mutex
	available bool
	res       *llm.DecisionResult
	err       error
	questions []llm.DecisionQuestion
	deadlines []time.Time
	hasDL     []bool
}

func (d *stubDecisioner) Available() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.available
}

func (d *stubDecisioner) Ask(ctx context.Context, q llm.DecisionQuestion) (*llm.DecisionResult, error) {
	dl, ok := ctx.Deadline()
	d.mu.Lock()
	d.questions = append(d.questions, q)
	d.deadlines = append(d.deadlines, dl)
	d.hasDL = append(d.hasDL, ok)
	d.mu.Unlock()
	if d.err != nil {
		return nil, d.err
	}
	return d.res, nil
}

func (d *stubDecisioner) calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.questions)
}

func (d *stubDecisioner) lastQuestion() llm.DecisionQuestion {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.questions[len(d.questions)-1]
}

func (d *stubDecisioner) lastDeadline() (time.Time, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deadlines[len(d.deadlines)-1], d.hasDL[len(d.hasDL)-1]
}

func allowResult(conf float64) *llm.DecisionResult {
	return &llm.DecisionResult{Answer: "allow", Confidence: conf}
}

func denyResult() *llm.DecisionResult {
	return &llm.DecisionResult{Answer: "deny", Confidence: 1.0}
}

// decisionHarnessSession wires a Session over io.Pipes (same pattern as
// transport_test.go) so reply frames are asserted directly on what the
// Transport writes — the agent-side "stdin/stdout" of the fake session.
// handlePermission's Reply is synchronous inside Serve's dispatch loop, so
// writing one inbound requestPermission frame and decoding one outbound
// response frame is a complete round trip.
func decisionHarnessSession(t *testing.T, cfg SessionConfig) (*Session, *io.PipeReader, *io.PipeWriter) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	tr := NewTransport(inR, outW)
	s := &Session{
		cfg:    cfg,
		tr:     tr,
		state:  StateReady,
		events: make(chan SessionEvent, 32),
	}
	tr.OnNotification(s.onNotice)
	go tr.Serve()
	t.Cleanup(func() {
		if err := tr.Close(); err != nil {
			t.Logf("transport close: %v", err)
		}
		if err := inW.Close(); err != nil {
			t.Logf("inW close: %v", err)
		}
		if err := outR.Close(); err != nil {
			t.Logf("outR close: %v", err)
		}
	})
	return s, outR, inW
}

// sendPermissionRequest writes one inbound session/requestPermission frame
// with the given raw params (or a default sessionId payload when empty).
func sendPermissionRequest(t *testing.T, toClient io.Writer, params string) {
	t.Helper()
	sendPermissionRequestID(t, toClient, 7, params)
}

// sendPermissionRequestID writes one inbound session/requestPermission frame
// with an explicit JSON-RPC id (multi-request pins correlate replies by id).
func sendPermissionRequestID(t *testing.T, toClient io.Writer, id int64, params string) {
	t.Helper()
	if params == "" {
		params = `{"sessionId":"sess-h","toolCall":{"title":"rm -rf scratch"}}`
	}
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  MethodSessionRequestPermission,
		"params":  json.RawMessage(params),
	}
	if err := json.NewEncoder(toClient).Encode(msg); err != nil {
		t.Fatalf("write inbound requestPermission: %v", err)
	}
}

// readReplyFrame decodes the reply Transport writes for the pending request.
func readReplyFrame(t *testing.T, fromClient io.Reader) Response {
	t.Helper()
	type reply struct {
		resp Response
		err  error
	}
	ch := make(chan reply, 1)
	go func() {
		var resp Response
		err := json.NewDecoder(fromClient).Decode(&resp)
		ch <- reply{resp, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("read reply frame: %v", r.err)
		}
		return r.resp
	case <-time.After(5 * time.Second):
		t.Fatal("no reply frame written within 5s")
		return Response{}
	}
}

// outcomeOf parses the ACP result.outcome object out of a reply frame.
func outcomeOf(t *testing.T, resp Response) (outcome, optionID string) {
	t.Helper()
	var res struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		t.Fatalf("decode reply result %s: %v", resp.Result, err)
	}
	return res.Outcome.Outcome, res.Outcome.OptionID
}

// drainEvents collects buffered session events without blocking past a beat.
func drainEvents(s *Session) []SessionEvent {
	var out []SessionEvent
	for {
		select {
		case ev := <-s.events:
			out = append(out, ev)
		case <-time.After(200 * time.Millisecond):
			return out
		}
	}
}

// findEvent returns the text of the last event with the given kind, or "".
func findEvent(events []SessionEvent, kind string) string {
	text := ""
	for _, ev := range events {
		if ev.Kind == kind {
			text = ev.Text
		}
	}
	return text
}

func decisionCfg(dec Decisioner, threshold float64) SessionConfig {
	return SessionConfig{
		AgentID:           "decider",
		PermissionMode:    "decision",
		Decision:          dec,
		DecisionThreshold: threshold,
	}
}

func TestSession_DecisionAllow(t *testing.T) {
	stub := &stubDecisioner{available: true, res: allowResult(1.0)}
	s, fromClient, toClient := decisionHarnessSession(t, decisionCfg(stub, 0.5))

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	outcome, optionID := outcomeOf(t, resp)
	if outcome != "selected" || optionID != "allow-once" {
		t.Fatalf("reply outcome = %q/%q, want selected/allow-once", outcome, optionID)
	}
	events := drainEvents(s)
	if got := findEvent(events, "permission_decision"); got != "allow" {
		t.Fatalf("permission_decision text = %q, want allow", got)
	}
	// Contract B: the existing permission_request event stays, now carrying
	// the decision text on the decision lane.
	if got := findEvent(events, "permission_request"); got != "allow" {
		t.Fatalf("permission_request text = %q, want allow (decision text)", got)
	}
	if stub.calls() != 1 {
		t.Fatalf("Ask calls = %d, want 1", stub.calls())
	}
	q := stub.lastQuestion()
	if q.Type != "noul" || q.Prompt != "May this action run?" {
		t.Fatalf("question = %+v, want noul / \"may this action run?\"", q)
	}
	if len(q.Options) != 2 || q.Options[0] != "allow" || q.Options[1] != "deny" {
		t.Fatalf("options = %v, want [allow deny]", q.Options)
	}
	if !strings.Contains(q.State, "decider") || !strings.Contains(q.State, "rm -rf scratch") {
		t.Fatalf("state %q must name the agent and the request", q.State)
	}
	if q.Threshold != 0.5 {
		t.Fatalf("question threshold = %v, want 0.5", q.Threshold)
	}
	// bounded ctx per drift: Ask must run under decisionAskTimeout.
	dl, ok := stub.lastDeadline()
	if !ok {
		t.Fatal("Ask ctx has no deadline — decisionAskTimeout not applied")
	}
	if d := time.Until(dl); d > decisionAskTimeout+time.Second || d < decisionAskTimeout-2*time.Second {
		t.Fatalf("Ask ctx deadline in %v, want ~%v", d, decisionAskTimeout)
	}
}

func TestSession_DecisionAnswerDeny(t *testing.T) {
	stub := &stubDecisioner{available: true, res: denyResult()}
	s, fromClient, toClient := decisionHarnessSession(t, decisionCfg(stub, 0.5))

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	outcome, _ := outcomeOf(t, resp)
	if outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled", outcome)
	}
	if got := findEvent(drainEvents(s), "permission_decision"); got != "deny" {
		t.Fatalf("permission_decision text = %q, want deny", got)
	}
}

func TestSession_DecisionAnswerOtherDenies(t *testing.T) {
	// An answer outside {allow, deny} (e.g. parse-shape noise from a stub)
	// is a reason-token deny: answer_deny.
	stub := &stubDecisioner{available: true, res: &llm.DecisionResult{Answer: "maybe", Confidence: 1.0}}
	s, fromClient, toClient := decisionHarnessSession(t, decisionCfg(stub, 0.5))

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled", outcome)
	}
	if got := findEvent(drainEvents(s), "permission_decision"); got != "answer_deny" {
		t.Fatalf("permission_decision text = %q, want answer_deny", got)
	}
}

func TestSession_DecisionNilDecisionerDenies(t *testing.T) {
	// Fail CLOSED with no decisioner — the deliberate OPPOSITE polarity of
	// builtin's tool-call decision gate, which fails OPEN on an absent
	// client (internal/tools/builtin/decision_gate.go). A permission
	// request guards an external side effect inside someone else's agent
	// session; a missing decider must not silently grant it
	// (master.md decision 3).
	s, fromClient, toClient := decisionHarnessSession(t, decisionCfg(nil, 0.5))

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled", outcome)
	}
	if got := findEvent(drainEvents(s), "permission_decision"); got != "no_decisioner" {
		t.Fatalf("permission_decision text = %q, want no_decisioner", got)
	}
}

func TestSession_DecisionUnavailableDenies(t *testing.T) {
	// Fail-closed: an unavailable decision layer cancels (opposite of the
	// fail-open builtin gate — see master.md decision 3).
	stub := &stubDecisioner{available: false, res: allowResult(1.0)}
	s, fromClient, toClient := decisionHarnessSession(t, decisionCfg(stub, 0.5))

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled", outcome)
	}
	if got := findEvent(drainEvents(s), "permission_decision"); got != "unavailable" {
		t.Fatalf("permission_decision text = %q, want unavailable", got)
	}
	if stub.calls() != 0 {
		t.Fatalf("Ask calls = %d, want 0 (unavailable must not be asked)", stub.calls())
	}
}

func TestSession_DecisionAskErrorDenies(t *testing.T) {
	// Fail-closed on ANY Ask error — transport failure, ErrDecisionUnavailable,
	// ErrDecisionParse — the deliberate inverse of the builtin decision gate,
	// which logs Warn and ALLOWS on the same errors (decision_gate.go
	// BeforeToolCall). Pinned per master.md decision 3.
	stub := &stubDecisioner{available: true, err: errors.New("decision model call failed")}
	s, fromClient, toClient := decisionHarnessSession(t, decisionCfg(stub, 0.5))

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled", outcome)
	}
	if got := findEvent(drainEvents(s), "permission_decision"); got != "ask_error" {
		t.Fatalf("permission_decision text = %q, want ask_error", got)
	}
}

func TestSession_DecisionLowConfidenceDenies(t *testing.T) {
	// Confidence note (pinned): today's DecisionClient degrades to
	// Confidence=1.0 argmax-only, so the ANSWER is the live signal; the
	// threshold is a real second bar only once logprobs land
	// (decision-model leaf 03/04). Both checks stay in the contract.
	stub := &stubDecisioner{available: true, res: allowResult(0.4)}
	s, fromClient, toClient := decisionHarnessSession(t, decisionCfg(stub, 0.5))

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled", outcome)
	}
	if got := findEvent(drainEvents(s), "permission_decision"); got != "low_confidence" {
		t.Fatalf("permission_decision text = %q, want low_confidence", got)
	}
}

func TestSession_DecisionStateBoundedTo300Runes(t *testing.T) {
	// The render budget mirrors builtin.summarizeToolArgs (300): the raw
	// params are truncated, never dumped whole.
	big := `{"sessionId":"sess-h","blob":"` + strings.Repeat("x", 1000) + `"}`
	stub := &stubDecisioner{available: true, res: allowResult(1.0)}
	_, fromClient, toClient := decisionHarnessSession(t, decisionCfg(stub, 0.5))

	sendPermissionRequest(t, toClient, big)
	readReplyFrame(t, fromClient)
	q := stub.lastQuestion()
	if strings.Contains(q.State, strings.Repeat("x", 301)) {
		t.Fatal("state leaked more than the 300-rune params budget")
	}
	if !strings.Contains(q.State, "...(truncated)") {
		t.Fatalf("truncated state must carry the truncation marker, got %q", q.State)
	}
}

func TestSession_DecisionSubprocessAllowEndToEnd(t *testing.T) {
	// The full lane through a real fakeagent subprocess: the agent blocks
	// its prompt until the reply frame lands, so Send succeeding proves the
	// decision lane answered.
	bin := fakeAgentBin(t)
	stub := &stubDecisioner{available: true, res: allowResult(1.0)}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := Start(ctx, SessionConfig{
		AgentID:           "perm-dec",
		Command:           []string{bin, "-mode", "permission"},
		Cwd:               t.TempDir(),
		DialTimeout:       30 * time.Second,
		CallTimeout:       20 * time.Second,
		PermissionMode:    "decision",
		Decision:          stub,
		DecisionThreshold: 0.5,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	sendCtx, sendCancel := context.WithTimeout(ctx, 20*time.Second)
	defer sendCancel()
	got, err := s.Send(sendCtx, "hi")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got != "hi" {
		t.Fatalf("got %q", got)
	}
	if stub.calls() != 1 {
		t.Fatalf("Ask calls = %d, want 1", stub.calls())
	}
	if text := findEvent(drainEvents(s), "permission_decision"); text != "allow" {
		t.Fatalf("permission_decision text = %q, want allow", text)
	}
}

func TestSession_DecisionSubprocessDenyEndToEnd(t *testing.T) {
	bin := fakeAgentBin(t)
	stub := &stubDecisioner{available: true, res: denyResult()}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := Start(ctx, SessionConfig{
		AgentID:           "perm-dec",
		Command:           []string{bin, "-mode", "permission"},
		Cwd:               t.TempDir(),
		DialTimeout:       30 * time.Second,
		CallTimeout:       20 * time.Second,
		PermissionMode:    "decision",
		Decision:          stub,
		DecisionThreshold: 0.5,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	sendCtx, sendCancel := context.WithTimeout(ctx, 20*time.Second)
	defer sendCancel()
	if _, err := s.Send(sendCtx, "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if text := findEvent(drainEvents(s), "permission_decision"); text != "deny" {
		t.Fatalf("permission_decision text = %q, want deny", text)
	}
}

// ---------------------------------------------------------------------------
// Human permission relay (acp-human-permission-relay leaf 02,
// master.md Contract B)
// ---------------------------------------------------------------------------

// stubNotifier records the PermissionNotifier seam calls and replays a
// fixed listener-availability answer for NotifyPermissionRequested.
type stubNotifier struct {
	mu        sync.Mutex
	listener  bool // return value of NotifyPermissionRequested
	requested []PendingPermission
	resolved  []resolvedCall
}

type resolvedCall struct {
	p        PendingPermission
	decision string
	reason   string
	approver Approver
}

func (n *stubNotifier) NotifyPermissionRequested(p PendingPermission) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.requested = append(n.requested, p)
	return n.listener
}

func (n *stubNotifier) NotifyPermissionResolved(p PendingPermission, decision, reason string, approver Approver) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.resolved = append(n.resolved, resolvedCall{p: p, decision: decision, reason: reason, approver: approver})
}

func (n *stubNotifier) counts() (requested, resolved int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.requested), len(n.resolved)
}

func (n *stubNotifier) lastRequested() PendingPermission {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.requested[len(n.requested)-1]
}

func (n *stubNotifier) lastResolved() resolvedCall {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.resolved[len(n.resolved)-1]
}

// waitForEvent reads session events until one with the given kind arrives
// (or the deadline fails the test), returning the matched event.
func waitForEvent(t *testing.T, s *Session, kind string, timeout time.Duration) SessionEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-s.events:
			if ev.Kind == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %q event within %v", kind, timeout)
			return SessionEvent{}
		}
	}
}

func humanRelayCfg(rl *Relay, notif PermissionNotifier, timeout time.Duration) SessionConfig {
	return SessionConfig{
		AgentID:        "human",
		PermissionMode: "human",
		Relay:          rl,
		Notifier:       notif,
		RelayTimeout:   timeout,
	}
}

func escalationCfg(dec Decisioner, rl *Relay, notif PermissionNotifier, timeout time.Duration) SessionConfig {
	cfg := decisionCfg(dec, 0.5)
	cfg.Relay = rl
	cfg.Notifier = notif
	cfg.RelayTimeout = timeout
	cfg.DecisionEscalateToHuman = true
	return cfg
}

func TestSession_HumanRelayAllow(t *testing.T) {
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 5*time.Second))

	sendPermissionRequest(t, toClient, "")
	// The pending id surfaces on the frozen permission_requested event
	// (drift 7: Text = the pending id, summary stays inside PendingPermission).
	reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
	if reqEv.Text == "" {
		t.Fatal("permission_requested event must carry the pending id")
	}
	if err := rl.Resolve(reqEv.Text, "allow"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	resp := readReplyFrame(t, fromClient)
	outcome, optionID := outcomeOf(t, resp)
	if outcome != "selected" || optionID != "allow-once" {
		t.Fatalf("reply outcome = %q/%q, want selected/allow-once", outcome, optionID)
	}
	events := drainEvents(s)
	if got := findEvent(events, "permission_resolved"); got != "allow" {
		t.Fatalf("permission_resolved text = %q, want allow", got)
	}
	// The relay lane uses its own frozen events; the parent-lane
	// permission_decision must stay absent.
	if got := findEvent(events, "permission_decision"); got != "" {
		t.Fatalf("relay lane must not emit permission_decision, got %q", got)
	}

	nReq, nRes := notif.counts()
	if nReq != 1 || nRes != 1 {
		t.Fatalf("notifier calls = %d/%d, want 1/1", nReq, nRes)
	}
	p := notif.lastRequested()
	if p.ID != reqEv.Text || p.AgentID != "human" || p.RequestID != int64(7) {
		t.Fatalf("pending = %+v, want id=%q agent=human requestID=int64(7)", p, reqEv.Text)
	}
	if !strings.Contains(p.Summary, "human") || !strings.Contains(p.Summary, "rm -rf scratch") {
		t.Fatalf("summary %q must reuse the bounded render (agent + request)", p.Summary)
	}
	if p.CreatedAt.IsZero() {
		t.Fatal("pending CreatedAt must be stamped")
	}
	rc := notif.lastResolved()
	if rc.decision != "allow" || rc.reason != "answered" {
		t.Fatalf("resolved call = %+v, want allow/answered", rc)
	}
	// Answered pendings leave the registry (leaf 03's list must shrink).
	if got := rl.Pending(); len(got) != 0 {
		t.Fatalf("Pending after resolve = %d, want 0", len(got))
	}
}

func TestSession_HumanRelayDeny(t *testing.T) {
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 5*time.Second))

	sendPermissionRequest(t, toClient, "")
	reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
	if err := rl.Resolve(reqEv.Text, "deny"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	resp := readReplyFrame(t, fromClient)
	if outcome, optionID := outcomeOf(t, resp); outcome != "cancelled" || optionID != "" {
		t.Fatalf("reply outcome = %q/%q, want cancelled/\"\"", outcome, optionID)
	}
	if got := findEvent(drainEvents(s), "permission_resolved"); got != "deny" {
		t.Fatalf("permission_resolved text = %q, want deny", got)
	}
	rc := notif.lastResolved()
	if rc.decision != "deny" || rc.reason != "answered" {
		t.Fatalf("resolved call = %+v, want deny/answered", rc)
	}
}

func TestSession_HumanRelayTimeout(t *testing.T) {
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 60*time.Millisecond))

	sendPermissionRequest(t, toClient, "")
	// Nobody answers: the fail-closed timer fires, cancelling the request.
	resp := readReplyFrame(t, fromClient)
	if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled", outcome)
	}
	events := drainEvents(s)
	if got := findEvent(events, "permission_requested"); got == "" {
		t.Fatal("timeout lane must still surface permission_requested")
	}
	if got := findEvent(events, "permission_resolved"); got != "timeout" {
		t.Fatalf("permission_resolved text = %q, want timeout", got)
	}
	rc := notif.lastResolved()
	if rc.decision != "deny" || rc.reason != "timeout" {
		t.Fatalf("resolved call = %+v, want deny/timeout", rc)
	}
	if got := rl.Pending(); len(got) != 0 {
		t.Fatalf("timed-out pending must leave the registry, got %d", len(got))
	}
}

func TestSession_HumanRelayNoListenerStillWaits(t *testing.T) {
	// Reconnect contract (drift 5): a false return means NO listener right
	// now — it must NOT cancel early. The wait continues to the deadline so
	// a reconnecting surface can still answer; here it times out.
	rl := NewRelay()
	notif := &stubNotifier{listener: false}
	s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 100*time.Millisecond))

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled", outcome)
	}
	if got := findEvent(drainEvents(s), "permission_resolved"); got != "timeout" {
		t.Fatalf("permission_resolved text = %q, want timeout (no early cancel)", got)
	}
	nReq, _ := notif.counts()
	if nReq != 1 {
		t.Fatalf("requested notifications = %d, want 1 (listener=false still registers+publishes)", nReq)
	}
}

func TestSession_HumanNilRelayFailsClosed(t *testing.T) {
	// Fail-closed pin (like the parent's TestSession_DecisionNilDecisioner-
	// Denies): human mode with the relay OR notifier seam missing must
	// cancel immediately with a WARN, never wait, never hang the lane.
	for _, tc := range []struct {
		name  string
		relay *Relay
		notfn PermissionNotifier
	}{
		{"nil relay", nil, &stubNotifier{listener: true}},
		{"nil notifier", NewRelay(), nil},
		{"both nil", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(tc.relay, tc.notfn, 5*time.Second))
			sendPermissionRequest(t, toClient, "")
			resp := readReplyFrame(t, fromClient)
			if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
				t.Fatalf("reply outcome = %q, want cancelled (fail closed)", outcome)
			}
			if got := findEvent(drainEvents(s), "permission_resolved"); got != "deny" {
				t.Fatalf("permission_resolved text = %q, want deny", got)
			}
		})
	}
}

func TestSession_HumanRelayFullRejectsFailsClosed(t *testing.T) {
	// Drift 8: registry capped at maxPendingPermissions. When full, a new
	// request is rejected with a WARN and cancelled immediately — the
	// oldest pendings stay answerable.
	rl := NewRelay()
	for i := 0; i < maxPendingPermissions; i++ {
		if _, err := rl.Register(PendingPermission{ID: fmt.Sprintf("perm-full-%d", i), AgentID: "pre"}); err != nil {
			t.Fatalf("prefill register %d: %v", i, err)
		}
	}
	notif := &stubNotifier{listener: true}
	s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 2*time.Second))

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled (full = fail closed)", outcome)
	}
	events := drainEvents(s)
	if got := findEvent(events, "permission_requested"); got != "" {
		t.Fatalf("full relay must not emit permission_requested, got %q", got)
	}
	if got := findEvent(events, "permission_resolved"); got != "deny" {
		t.Fatalf("permission_resolved text = %q, want deny", got)
	}
	nReq, _ := notif.counts()
	if nReq != 0 {
		t.Fatalf("requested notifications = %d, want 0 (rejected before publish)", nReq)
	}
	if got := len(rl.Pending()); got != maxPendingPermissions {
		t.Fatalf("Pending after rejection = %d, want %d (prefills untouched)", got, maxPendingPermissions)
	}
}

func TestSession_DecisionEscalationAllow(t *testing.T) {
	// Master Contract B escalation: decider denies, but the human relay is
	// armed and answers allow -> selected/allow-once + escalated_human_allow.
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	stub := &stubDecisioner{available: true, res: denyResult()}
	s, fromClient, toClient := decisionHarnessSession(t, escalationCfg(stub, rl, notif, 5*time.Second))

	sendPermissionRequest(t, toClient, "")
	reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
	if err := rl.Resolve(reqEv.Text, "allow"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	resp := readReplyFrame(t, fromClient)
	if outcome, optionID := outcomeOf(t, resp); outcome != "selected" || optionID != "allow-once" {
		t.Fatalf("reply outcome = %q/%q, want selected/allow-once", outcome, optionID)
	}
	events := drainEvents(s)
	if got := findEvent(events, "permission_decision"); got != "escalated_human_allow" {
		t.Fatalf("permission_decision text = %q, want escalated_human_allow", got)
	}
	if got := findEvent(events, "permission_resolved"); got != "allow" {
		t.Fatalf("permission_resolved text = %q, want allow", got)
	}
}

func TestSession_DecisionEscalationDeny(t *testing.T) {
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	stub := &stubDecisioner{available: true, res: &llm.DecisionResult{Answer: "maybe", Confidence: 1.0}}
	s, fromClient, toClient := decisionHarnessSession(t, escalationCfg(stub, rl, notif, 5*time.Second))

	sendPermissionRequest(t, toClient, "")
	reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
	if err := rl.Resolve(reqEv.Text, "deny"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	resp := readReplyFrame(t, fromClient)
	if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled", outcome)
	}
	if got := findEvent(drainEvents(s), "permission_decision"); got != "escalated_human_deny" {
		t.Fatalf("permission_decision text = %q, want escalated_human_deny", got)
	}
}

func TestSession_DecisionEscalationTimeout(t *testing.T) {
	// Relay timeout on the escalation lane cancels with the parent's
	// "deny" token (drift 6), not an escalated token.
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	stub := &stubDecisioner{available: true, res: allowResult(0.4)} // low_confidence without escalation
	s, fromClient, toClient := decisionHarnessSession(t, escalationCfg(stub, rl, notif, 60*time.Millisecond))

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled", outcome)
	}
	events := drainEvents(s)
	if got := findEvent(events, "permission_requested"); got == "" {
		t.Fatal("armed escalation must run the relay wait (permission_requested missing)")
	}
	if got := findEvent(events, "permission_decision"); got != "deny" {
		t.Fatalf("permission_decision text = %q, want deny (timeout -> parent semantics)", got)
	}
}

func TestSession_DecisionEscalationDeciderAllowSkipsRelay(t *testing.T) {
	// Leaf task: escalate=true + decider allow -> NO relay wait.
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	stub := &stubDecisioner{available: true, res: allowResult(1.0)}
	s, fromClient, toClient := decisionHarnessSession(t, escalationCfg(stub, rl, notif, 5*time.Second))

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	if outcome, optionID := outcomeOf(t, resp); outcome != "selected" || optionID != "allow-once" {
		t.Fatalf("reply outcome = %q/%q, want selected/allow-once", outcome, optionID)
	}
	events := drainEvents(s)
	if got := findEvent(events, "permission_decision"); got != "allow" {
		t.Fatalf("permission_decision text = %q, want allow", got)
	}
	if got := findEvent(events, "permission_requested"); got != "" {
		t.Fatalf("allow outcome must not touch the relay, got permission_requested %q", got)
	}
	if nReq, nRes := notif.counts(); nReq != 0 || nRes != 0 {
		t.Fatalf("notifier calls = %d/%d, want 0/0", nReq, nRes)
	}
}

func TestSession_DecisionEscalationNoRelayPlainCancel(t *testing.T) {
	// Drift 6: escalate armed but the relay seam is nil -> plain cancel
	// with the parent's outcome text + WARN, no relay events.
	notif := &stubNotifier{listener: true}
	stub := &stubDecisioner{available: true, res: denyResult()}
	s, fromClient, toClient := decisionHarnessSession(t, escalationCfg(stub, nil, notif, 5*time.Second))

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled", outcome)
	}
	events := drainEvents(s)
	if got := findEvent(events, "permission_decision"); got != "deny" {
		t.Fatalf("permission_decision text = %q, want deny (plain parent cancel)", got)
	}
	if got := findEvent(events, "permission_requested"); got != "" {
		t.Fatalf("no relay wired -> no permission_requested, got %q", got)
	}
	if got := findEvent(events, "permission_resolved"); got != "" {
		t.Fatalf("no relay wired -> no permission_resolved, got %q", got)
	}
}

func TestSession_DecisionEscalateFalseByteIdentical(t *testing.T) {
	// escalate=false decision behavior stays byte-identical to the parent
	// tree pins — even when relay+notifier are present on the config, the
	// deny outcome must cancel immediately with the original text and
	// never touch the relay.
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	stub := &stubDecisioner{available: true, res: denyResult()}
	cfg := escalationCfg(stub, rl, notif, 5*time.Second)
	cfg.DecisionEscalateToHuman = false
	s, fromClient, toClient := decisionHarnessSession(t, cfg)

	sendPermissionRequest(t, toClient, "")
	resp := readReplyFrame(t, fromClient)
	if outcome, _ := outcomeOf(t, resp); outcome != "cancelled" {
		t.Fatalf("reply outcome = %q, want cancelled", outcome)
	}
	events := drainEvents(s)
	if got := findEvent(events, "permission_decision"); got != "deny" {
		t.Fatalf("permission_decision text = %q, want deny (parent token)", got)
	}
	if got := findEvent(events, "permission_requested"); got != "" {
		t.Fatalf("escalate=false must never run the relay, got permission_requested %q", got)
	}
	if nReq, nRes := notif.counts(); nReq != 0 || nRes != 0 {
		t.Fatalf("notifier calls = %d/%d, want 0/0", nReq, nRes)
	}
}

// ---------------------------------------------------------------------------
// Relay registry unit surface (leaf 02 task 1, final bullets)
// ---------------------------------------------------------------------------

func TestRelay_ResolveSentinels(t *testing.T) {
	rl := NewRelay()
	if err := rl.Resolve("perm-nope", "allow"); !errors.Is(err, ErrRelayNotFound) {
		t.Fatalf("unknown id error = %v, want ErrRelayNotFound", err)
	}
	if _, err := rl.Register(PendingPermission{ID: "perm-x"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := rl.Resolve("perm-x", "allow"); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if err := rl.Resolve("perm-x", "allow"); !errors.Is(err, ErrRelayAnswered) {
		t.Fatalf("double resolve error = %v, want ErrRelayAnswered", err)
	}
	// A non-allow verb is fail-closed deny, still a valid single answer.
	if _, err := rl.Register(PendingPermission{ID: "perm-y"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := rl.Resolve("perm-y", "maybe"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := rl.Resolve("perm-y", "allow"); !errors.Is(err, ErrRelayAnswered) {
		t.Fatalf("second resolve error = %v, want ErrRelayAnswered", err)
	}
}

func TestRelay_RegisterCap(t *testing.T) {
	rl := NewRelay()
	for i := 0; i < maxPendingPermissions; i++ {
		if _, err := rl.Register(PendingPermission{ID: fmt.Sprintf("perm-%d", i)}); err != nil {
			t.Fatalf("Register %d: %v", i, err)
		}
	}
	if _, err := rl.Register(PendingPermission{ID: "perm-over"}); !errors.Is(err, ErrRelayFull) {
		t.Fatalf("over-cap Register = %v, want ErrRelayFull", err)
	}
	if got := len(rl.Pending()); got != maxPendingPermissions {
		t.Fatalf("Pending = %d, want %d", got, maxPendingPermissions)
	}
	// One answered slot frees room for a new registration.
	if err := rl.Resolve("perm-0", "deny"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := rl.Register(PendingPermission{ID: "perm-over"}); err != nil {
		t.Fatalf("Register after free: %v", err)
	}
}

func TestRelay_PendingSnapshots(t *testing.T) {
	var rl *Relay
	if got := rl.Pending(); got != nil {
		t.Fatalf("nil receiver Pending = %v, want nil (nil-safe)", got)
	}
	rl = NewRelay()
	now := time.Now()
	if _, err := rl.Register(PendingPermission{ID: "perm-b", AgentID: "b", CreatedAt: now.Add(time.Second)}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := rl.Register(PendingPermission{ID: "perm-a", AgentID: "a", CreatedAt: now}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got := rl.Pending()
	if len(got) != 2 {
		t.Fatalf("Pending len = %d, want 2", len(got))
	}
	if got[0].ID != "perm-a" || got[1].ID != "perm-b" {
		t.Fatalf("Pending order = %q,%q, want created-asc perm-a,perm-b", got[0].ID, got[1].ID)
	}
	// Mutating the snapshot must not touch registry state.
	got[0].AgentID = "mutated"
	if again := rl.Pending(); again[0].AgentID != "a" {
		t.Fatalf("Pending leaked internal state: %+v", again)
	}
}

// TestSession_PermissionLanesUnchanged pins that permissive/deny reply and
// event shapes are byte-identical to the pre-decision lane (the subprocess
// cases TestSession_PermissionPermissive / TestSession_PermissionDeny cover
// the happy path; this harness asserts the exact wire frames + event text).
func TestSession_PermissionLanesUnchanged(t *testing.T) {
	for _, tc := range []struct {
		mode        string
		wantOutcome string
		wantOption  string
		wantKind    string
	}{
		{"permissive", "selected", "allow-once", "approved"},
		{"deny", "cancelled", "", "denied"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			s, fromClient, toClient := decisionHarnessSession(t, SessionConfig{
				AgentID:        "lane",
				PermissionMode: tc.mode,
			})
			sendPermissionRequest(t, toClient, "")
			resp := readReplyFrame(t, fromClient)
			outcome, optionID := outcomeOf(t, resp)
			if outcome != tc.wantOutcome || optionID != tc.wantOption {
				t.Fatalf("outcome = %q/%q, want %q/%q", outcome, optionID, tc.wantOutcome, tc.wantOption)
			}
			events := drainEvents(s)
			if got := findEvent(events, "permission_request"); got != tc.wantKind {
				t.Fatalf("permission_request text = %q, want %q", got, tc.wantKind)
			}
			if got := findEvent(events, "permission_decision"); got != "" {
				t.Fatalf("non-decision lanes must not emit permission_decision, got %q", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Head-of-line blocking fix: permission waits must not block the read loop
// ---------------------------------------------------------------------------

// TestSession_PermissionWaitDoesNotBlockUpdates is the head-of-line pin:
// while a human-relay permission request is unresolved (the relay wait
// would formerly block Transport.Serve for up to relay_timeout), a burst
// of session/update notifications must still be read and delivered, and
// the permission reply must still arrive correctly after Resolve.
func TestSession_PermissionWaitDoesNotBlockUpdates(t *testing.T) {
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 30*time.Second))

	sendPermissionRequest(t, toClient, "")
	reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
	if reqEv.Text == "" {
		t.Fatal("permission_requested event must carry the pending id")
	}

	// Burst of session/update notifications while the permission is
	// unresolved. The old synchronous dispatch never read these: the read
	// loop sat inside relayPermissionWait.
	const burst = 25
	enc := json.NewEncoder(toClient)
	for i := range burst {
		chunk, err := json.Marshal(map[string]any{
			"sessionId": "sess-h",
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]string{"type": "text", "text": fmt.Sprintf("chunk-%d ", i)},
			},
		})
		if err != nil {
			t.Fatalf("marshal chunk %d: %v", i, err)
		}
		notif := Notification{JSONRPC: "2.0", Method: MethodSessionUpdate, Params: chunk}
		if err := enc.Encode(notif); err != nil {
			t.Fatalf("write update %d: %v", i, err)
		}
	}

	// All chunks must be delivered WITHOUT resolving the permission.
	deadline := time.After(5 * time.Second)
	got := 0
	for got < burst {
		select {
		case ev := <-s.events:
			if ev.Kind == "message_chunk" {
				got++
			}
		case <-deadline:
			t.Fatalf("only %d/%d session/update chunks delivered while permission pending — read loop blocked", got, burst)
		}
	}

	// No reply frame may have been written yet (permission still open),
	// and after Resolve the SAME reader gets the reply — one decoder on
	// the pipe, so no frame can be lost to a competing reader.
	type peek struct {
		resp Response
		err  error
	}
	peekCh := make(chan peek, 1)
	go func() {
		var resp Response
		peekCh <- peek{resp, json.NewDecoder(fromClient).Decode(&resp)}
	}()
	select {
	case p := <-peekCh:
		if p.err == nil {
			t.Fatalf("reply frame written before resolve: %+v", p.resp)
		}
		t.Fatalf("reply reader failed before resolve: %v", p.err)
	case <-time.After(200 * time.Millisecond):
		// expected: nothing written yet; the goroutine stays blocked on
		// the pipe and will deliver the post-resolve reply below.
	}

	// Now resolve: the reply must still arrive correctly, correlated by id.
	if err := rl.Resolve(reqEv.Text, "allow"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var resp Response
	select {
	case p := <-peekCh:
		if p.err != nil {
			t.Fatalf("read reply frame: %v", p.err)
		}
		resp = p.resp
	case <-time.After(5 * time.Second):
		t.Fatal("no reply frame within 5s of resolve")
	}
	if resp.ID != 7 {
		t.Fatalf("reply id = %d, want 7 (JSON-RPC correlation)", resp.ID)
	}
	if outcome, optionID := outcomeOf(t, resp); outcome != "selected" || optionID != "allow-once" {
		t.Fatalf("reply outcome = %q/%q, want selected/allow-once", outcome, optionID)
	}
}

// TestSession_SecondPermissionWhileFirstPending proves a second
// session/requestPermission arriving while the first is still waiting is
// handled, not dropped: both register as pendings and both get replies
// correlated to their own ids.
func TestSession_SecondPermissionWhileFirstPending(t *testing.T) {
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 30*time.Second))

	sendPermissionRequestID(t, toClient, 11, `{"sessionId":"sess-h","toolCall":{"title":"first"}}`)
	first := waitForEvent(t, s, "permission_requested", 5*time.Second)
	sendPermissionRequestID(t, toClient, 12, `{"sessionId":"sess-h","toolCall":{"title":"second"}}`)
	second := waitForEvent(t, s, "permission_requested", 5*time.Second)
	if first.Text == second.Text {
		t.Fatalf("second permission request reused the first pending id %q", first.Text)
	}
	if got := len(rl.Pending()); got != 2 {
		t.Fatalf("Pending = %d, want 2 (second request must not be dropped)", got)
	}

	if err := rl.Resolve(second.Text, "allow"); err != nil {
		t.Fatalf("Resolve second: %v", err)
	}
	if err := rl.Resolve(first.Text, "deny"); err != nil {
		t.Fatalf("Resolve first: %v", err)
	}

	// Both replies arrive (order free — JSON-RPC correlates by id).
	seen := map[int64]string{}
	for range 2 {
		resp := readReplyFrame(t, fromClient)
		outcome, _ := outcomeOf(t, resp)
		seen[resp.ID] = outcome
	}
	if seen[11] != "cancelled" {
		t.Fatalf("reply for id 11 = %q, want cancelled (deny)", seen[11])
	}
	if seen[12] != "selected" {
		t.Fatalf("reply for id 12 = %q, want selected (allow)", seen[12])
	}
}

// TestTransport_CloseCancelsPermissionWait: closing the transport ends an
// in-flight human relay wait instead of letting the goroutine linger for
// the full relay timeout.
func TestTransport_CloseCancelsPermissionWait(t *testing.T) {
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	s, _, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 5*time.Minute))

	sendPermissionRequest(t, toClient, "")
	waitForEvent(t, s, "permission_requested", 5*time.Second)

	start := time.Now()
	if err := s.tr.Close(); err != nil {
		t.Logf("close: %v", err)
	}
	// The wait must exit promptly with a fail-closed deny.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-s.events:
			if ev.Kind == "permission_resolved" {
				if ev.Text != relayDecisionDeny {
					t.Fatalf("permission_resolved text = %q, want deny on close", ev.Text)
				}
				if d := time.Since(start); d > 3*time.Second {
					t.Fatalf("relay wait took %v to notice Close", d)
				}
				if got := len(rl.Pending()); got != 0 {
					t.Fatalf("pending registry after close = %d, want 0", got)
				}
				return
			}
		case <-deadline:
			t.Fatal("Close did not cancel the in-flight relay wait within 3s")
		}
	}
}

// ---------------------------------------------------------------------------
// Offered-option resolution (item 2): allow replies must name an optionId
// the agent actually offered.
// ---------------------------------------------------------------------------

func TestResolveAllowOptionID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params string
		want   string
	}{
		{"no options -> legacy literal", `{"sessionId":"s"}`, "allow-once"},
		{"empty params -> legacy literal", ``, "allow-once"},
		{"kind allow_once wins", `{"sessionId":"s","options":[{"optionId":"deny-it","name":"Reject","kind":"reject_once"},{"optionId":"custom-yes","name":"Proceed","kind":"allow_once"}]}`, "custom-yes"},
		{"kind containing allow", `{"sessionId":"s","options":[{"optionId":"nope","name":"No","kind":"reject_once"},{"optionId":"go","name":"Go","kind":"allow_always"}]}`, "go"},
		{"name fallback allow once", `{"sessionId":"s","options":[{"optionId":"r","name":"Reject","kind":"reject_once"},{"optionId":"a1","name":"Allow Once","kind":"other"}]}`, "a1"},
		{"name fallback allow", `{"sessionId":"s","options":[{"optionId":"opt-a","name":"Allow","kind":""}]}`, "opt-a"},
		{"no match -> legacy literal", `{"sessionId":"s","options":[{"optionId":"x","name":"Something","kind":"weird"}]}`, "allow-once"},
		{"empty optionId skipped", `{"sessionId":"s","options":[{"optionId":"","name":"Allow","kind":"allow_once"},{"optionId":"real","name":"Allow","kind":"allow_once"}]}`, "real"},
		{"malformed params -> legacy literal", `{not json`, "allow-once"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveAllowOptionID([]byte(tc.params)); got != tc.want {
				t.Fatalf("resolveAllowOptionID(%s) = %q, want %q", tc.params, got, tc.want)
			}
		})
	}
}

// TestSession_PermissionAllowUsesOfferedOptionID: an agent offering custom
// option ids gets a reply naming the offered allow-once id — on the
// permissive, human-relay, and decision lanes.
func TestSession_PermissionAllowUsesOfferedOptionID(t *testing.T) {
	params := `{"sessionId":"sess-h","toolCall":{"title":"rm -rf scratch"},"options":[` +
		`{"optionId":"reject-once","name":"Reject","kind":"reject_once"},` +
		`{"optionId":"my-allow","name":"Proceed once","kind":"allow_once"}]}`

	t.Run("permissive lane", func(t *testing.T) {
		s, fromClient, toClient := decisionHarnessSession(t, SessionConfig{
			AgentID:        "lane",
			PermissionMode: "permissive",
		})
		sendPermissionRequest(t, toClient, params)
		resp := readReplyFrame(t, fromClient)
		if outcome, optionID := outcomeOf(t, resp); outcome != "selected" || optionID != "my-allow" {
			t.Fatalf("reply = %q/%q, want selected/my-allow", outcome, optionID)
		}
		drainEvents(s)
	})

	t.Run("human relay lane", func(t *testing.T) {
		rl := NewRelay()
		notif := &stubNotifier{listener: true}
		s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 5*time.Second))
		sendPermissionRequest(t, toClient, params)
		reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
		if err := rl.Resolve(reqEv.Text, "allow"); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		resp := readReplyFrame(t, fromClient)
		if outcome, optionID := outcomeOf(t, resp); outcome != "selected" || optionID != "my-allow" {
			t.Fatalf("reply = %q/%q, want selected/my-allow", outcome, optionID)
		}
	})

	t.Run("decision lane", func(t *testing.T) {
		stub := &stubDecisioner{available: true, res: allowResult(1.0)}
		s, fromClient, toClient := decisionHarnessSession(t, decisionCfg(stub, 0.5))
		sendPermissionRequest(t, toClient, params)
		resp := readReplyFrame(t, fromClient)
		if outcome, optionID := outcomeOf(t, resp); outcome != "selected" || optionID != "my-allow" {
			t.Fatalf("reply = %q/%q, want selected/my-allow", outcome, optionID)
		}
		drainEvents(s)
	})
}

// ---------------------------------------------------------------------------
// Item 5: s.sessionID data race — an agent may emit session/requestPermission
// before session/new's reply lands (Start writes the id after Serve starts).
// Run under -race. The stub agent (mode earlyperm) writes requestPermission
// BEFORE replying to session/new, so the human-relay lane reads the session
// id concurrently with Start's write.
// ---------------------------------------------------------------------------

func TestSession_EarlyPermissionNoSessionIDRace(t *testing.T) {
	bin := fakeAgentBin(t)
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := Start(ctx, SessionConfig{
		AgentID:        "earlyperm",
		Command:        []string{bin, "-mode", "earlyperm"},
		Cwd:            t.TempDir(),
		DialTimeout:    30 * time.Second,
		CallTimeout:    20 * time.Second,
		PermissionMode: "human",
		Relay:          rl,
		Notifier:       notif,
		RelayTimeout:   20 * time.Second,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})

	// The early permission request registers before Start returns; answer
	// every pending so the agent finishes its turn.
	deadline := time.After(20 * time.Second)
	for {
		pendings := rl.Pending()
		if len(pendings) > 0 {
			for _, p := range pendings {
				if err := rl.Resolve(p.ID, "allow"); err != nil && !errors.Is(err, ErrRelayAnswered) {
					t.Logf("resolve %s: %v", p.ID, err)
				}
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("early permission request never registered")
		case <-time.After(20 * time.Millisecond):
		}
	}
	// The surfaced SessionID must be the one Start eventually set — read
	// through the same guarded path the race detector watches.
	if got := s.getSessionID(); got != "sess-fake" {
		t.Fatalf("sessionID = %q, want sess-fake", got)
	}
}

// ---------------------------------------------------------------------------
// Always-allow relay (acp-always-allow leaf 01, master.md Contract A)
// ---------------------------------------------------------------------------

func TestResolveAlwaysOptionID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params string
		want   string
	}{
		{"exact kind allow_always", `{"sessionId":"s","options":[{"optionId":"deny-it","name":"Reject","kind":"reject_once"},{"optionId":"stay-once","name":"Allow once","kind":"allow_once"},{"optionId":"stay-always","name":"Allow always","kind":"allow_always"}]}`, "stay-always"},
		{"kind contains allow+always", `{"sessionId":"s","options":[{"optionId":"go","name":"Keep going","kind":"allow-always"}]}`, "go"},
		{"no always option", `{"sessionId":"s","options":[{"optionId":"once","name":"Allow once","kind":"allow_once"}]}`, ""},
		{"reject_always does not match", `{"sessionId":"s","options":[{"optionId":"never","name":"Always reject","kind":"reject_always"}]}`, ""},
		{"no options", `{"sessionId":"s"}`, ""},
		{"empty params", ``, ""},
		{"malformed params", `{not json`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveAlwaysOptionID([]byte(tc.params)); got != tc.want {
				t.Fatalf("resolveAlwaysOptionID(%s) = %q, want %q", tc.params, got, tc.want)
			}
		})
	}
}

// TestSession_HumanRelayAlways: the third human verb echoes the agent's OWN
// allow_always-kind optionId (design decision 1 — meept is a transport, it
// persists nothing), the resolved event text carries "always", and the
// pending snapshot surfaces the offered options.
func TestSession_HumanRelayAlways(t *testing.T) {
	params := `{"sessionId":"sess-h","toolCall":{"title":"rm -rf scratch"},"options":[` +
		`{"optionId":"reject-once","name":"Reject","kind":"reject_once"},` +
		`{"optionId":"my-once","name":"Proceed once","kind":"allow_once"},` +
		`{"optionId":"my-always","name":"Proceed always","kind":"allow_always"}]}`
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 5*time.Second))

	sendPermissionRequest(t, toClient, params)
	reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
	if err := rl.Resolve(reqEv.Text, "always"); err != nil {
		t.Fatalf("Resolve always: %v", err)
	}

	resp := readReplyFrame(t, fromClient)
	if outcome, optionID := outcomeOf(t, resp); outcome != "selected" || optionID != "my-always" {
		t.Fatalf("reply = %q/%q, want selected/my-always", outcome, optionID)
	}
	events := drainEvents(s)
	if got := findEvent(events, "permission_resolved"); got != "always" {
		t.Fatalf("permission_resolved text = %q, want always", got)
	}
	rc := notif.lastResolved()
	if rc.decision != "always" || rc.reason != "answered" {
		t.Fatalf("resolved call = %+v, want always/answered", rc)
	}
	p := notif.lastRequested()
	if len(p.Options) != 3 {
		t.Fatalf("pending Options = %+v, want the 3 offered entries", p.Options)
	}
	if p.Options[2].OptionID != "my-always" || p.Options[2].Kind != "allow_always" || p.Options[2].Name != "Proceed always" {
		t.Fatalf("always option passthrough = %+v, want raw agent labels", p.Options[2])
	}
}

// TestSession_HumanRelayAlwaysFallsBackToAllow: an agent that offers no
// allow_always option must NEVER error or deny on "always" — it resolves
// like "allow" through the allow_once echo chain (design decision 2).
func TestSession_HumanRelayAlwaysFallsBackToAllow(t *testing.T) {
	params := `{"sessionId":"sess-h","toolCall":{"title":"rm -rf scratch"},"options":[` +
		`{"optionId":"reject-once","name":"Reject","kind":"reject_once"},` +
		`{"optionId":"my-once","name":"Proceed once","kind":"allow_once"}]}`
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 5*time.Second))

	sendPermissionRequest(t, toClient, params)
	reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
	if err := rl.Resolve(reqEv.Text, "always"); err != nil {
		t.Fatalf("Resolve always: %v", err)
	}
	resp := readReplyFrame(t, fromClient)
	if outcome, optionID := outcomeOf(t, resp); outcome != "selected" || optionID != "my-once" {
		t.Fatalf("reply = %q/%q, want selected/my-once (allow_once fallback)", outcome, optionID)
	}
	if got := findEvent(drainEvents(s), "permission_resolved"); got != "always" {
		t.Fatalf("permission_resolved text = %q, want always", got)
	}
}

// TestSession_HumanRelayAlwaysNoOptions pins the legacy literal tail of the
// chain: no options offered at all -> defaultAllowOptionID, still no error.
func TestSession_HumanRelayAlwaysNoOptions(t *testing.T) {
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 5*time.Second))

	sendPermissionRequest(t, toClient, "")
	reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
	if err := rl.Resolve(reqEv.Text, "always"); err != nil {
		t.Fatalf("Resolve always: %v", err)
	}
	resp := readReplyFrame(t, fromClient)
	if outcome, optionID := outcomeOf(t, resp); outcome != "selected" || optionID != defaultAllowOptionID {
		t.Fatalf("reply = %q/%q, want selected/%s", outcome, optionID, defaultAllowOptionID)
	}
	if p := notif.lastRequested(); p.Options != nil {
		t.Fatalf("pending Options = %+v, want nil when the agent offered none", p.Options)
	}
}

// TestSession_DecisionEscalationHumanAlways: the escalation relay inherits
// human answers, so it gains "always" via the shared wait — the reply names
// the allow_always option while the decision lane's own model options stay
// allow/deny (design decision 4).
func TestSession_DecisionEscalationHumanAlways(t *testing.T) {
	params := `{"sessionId":"sess-h","toolCall":{"title":"rm -rf scratch"},"options":[` +
		`{"optionId":"my-once","name":"Proceed once","kind":"allow_once"},` +
		`{"optionId":"my-always","name":"Proceed always","kind":"allow_always"}]}`
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	stub := &stubDecisioner{available: true, res: denyResult()}
	s, fromClient, toClient := decisionHarnessSession(t, escalationCfg(stub, rl, notif, 5*time.Second))

	sendPermissionRequest(t, toClient, params)
	reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
	if err := rl.Resolve(reqEv.Text, "always"); err != nil {
		t.Fatalf("Resolve always: %v", err)
	}
	resp := readReplyFrame(t, fromClient)
	if outcome, optionID := outcomeOf(t, resp); outcome != "selected" || optionID != "my-always" {
		t.Fatalf("reply = %q/%q, want selected/my-always", outcome, optionID)
	}
	events := drainEvents(s)
	if got := findEvent(events, "permission_decision"); got != "escalated_human_allow" {
		t.Fatalf("permission_decision text = %q, want escalated_human_allow", got)
	}
	if got := findEvent(events, "permission_resolved"); got != "always" {
		t.Fatalf("permission_resolved text = %q, want always", got)
	}
}

func TestRelay_ResolveAlwaysVerb(t *testing.T) {
	rl := NewRelay()
	ch, err := rl.Register(PendingPermission{ID: "perm-always"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := rl.Resolve("perm-always", "always"); err != nil {
		t.Fatalf("Resolve always: %v", err)
	}
	d := <-ch
	if !d.allow || d.text != "always" || !d.always {
		t.Fatalf("decision = %+v, want allow/text=always/always=true", d)
	}
	// Second answer still lands as already-answered (exactly-once unchanged).
	if err := rl.Resolve("perm-always", "allow"); !errors.Is(err, ErrRelayAnswered) {
		t.Fatalf("double resolve = %v, want ErrRelayAnswered", err)
	}
	// Normalization unchanged: anything not allow/always is deny, and the
	// verb match is exact (upper-case "ALWAYS" never reaches the relay from
	// the API layers, which lowercase first).
	other := make(chan relayDecision, 1)
	_ = other
	if _, err := rl.Register(PendingPermission{ID: "perm-shout"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := rl.Resolve("perm-shout", "ALWAYS"); err != nil {
		t.Fatalf("Resolve ALWAYS: %v", err)
	}
	if got := len(rl.Pending()); got != 0 {
		t.Fatalf("Pending after answers = %d, want 0", got)
	}
}

// TestRelay_OptionsCappedAtRegister: the offered-options snapshot is bounded
// at maxPermissionOptions entries (Contract A: cap at register time).
func TestRelay_OptionsCappedAtRegister(t *testing.T) {
	opts := make([]PermissionOption, maxPermissionOptions+4)
	for i := range opts {
		opts[i] = PermissionOption{OptionID: fmt.Sprintf("opt-%d", i), Name: "Opt", Kind: "allow_once"}
	}
	rl := NewRelay()
	if _, err := rl.Register(PendingPermission{ID: "perm-cap", Options: opts}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got := rl.Pending()
	if len(got) != 1 || len(got[0].Options) != maxPermissionOptions {
		t.Fatalf("Options len = %d, want %d", len(got[0].Options), maxPermissionOptions)
	}
	// The caller's slice must not be mutated by the cap.
	if len(opts) != maxPermissionOptions+4 {
		t.Fatalf("caller options mutated: len = %d", len(opts))
	}
}

// TestSession_HumanRelayOptionsCapped: the lane parses the raw params into
// the pending snapshot and caps over-long offers before publish.
func TestSession_HumanRelayOptionsCapped(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"sessionId":"sess-h","toolCall":{"title":"rm -rf scratch"},"options":[`)
	for i := 0; i < maxPermissionOptions+4; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"optionId":"opt-%d","name":"Opt %d","kind":"allow_once"}`, i, i)
	}
	sb.WriteString(`]}`)

	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	s, _, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 5*time.Second))
	sendPermissionRequest(t, toClient, sb.String())
	waitForEvent(t, s, "permission_requested", 5*time.Second)

	p := notif.lastRequested()
	if len(p.Options) != maxPermissionOptions {
		t.Fatalf("published Options = %d, want cap %d", len(p.Options), maxPermissionOptions)
	}
}

// TestSession_HumanRelayApproverAttribution pins audit-M5 attribution: the
// approver passed to ResolveWithApprover rides the decision into the
// permission_resolved notification, so surfaces (and the bus event built
// from it) can show WHO answered. Mirrors TestSession_HumanRelayAllow with
// an attributed resolve.
func TestSession_HumanRelayApproverAttribution(t *testing.T) {
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	s, fromClient, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 5*time.Second))

	sendPermissionRequest(t, toClient, "")
	reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
	if reqEv.Text == "" {
		t.Fatal("permission_requested event must carry the pending id")
	}
	approver := Approver{Kind: "peercred", PID: 4321, UID: 501}
	if err := rl.ResolveWithApprover(reqEv.Text, "allow", approver); err != nil {
		t.Fatalf("ResolveWithApprover: %v", err)
	}

	resp := readReplyFrame(t, fromClient)
	outcome, _ := outcomeOf(t, resp)
	if outcome != "selected" {
		t.Fatalf("reply outcome = %q, want selected", outcome)
	}

	rc := notif.lastResolved()
	if rc.decision != "allow" || rc.reason != "answered" {
		t.Fatalf("resolved call = %+v, want allow/answered", rc)
	}
	if rc.approver != approver {
		t.Fatalf("resolved approver = %+v, want %+v", rc.approver, approver)
	}
	if got, want := rc.approver.String(), "peercred:pid=4321,uid=501"; got != want {
		t.Fatalf("approver render = %q, want %q", got, want)
	}
}

// TestRelay_ResolveLegacyUnattributed pins the compatibility wrapper: the
// 2-arg Resolve records an empty (unattributed) approver, rendering as
// "unknown" — legacy callers keep working and stay visibly unattributed.
func TestRelay_ResolveLegacyUnattributed(t *testing.T) {
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	s, _, toClient := decisionHarnessSession(t, humanRelayCfg(rl, notif, 5*time.Second))

	sendPermissionRequest(t, toClient, "")
	reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
	if err := rl.Resolve(reqEv.Text, "deny"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	waitForEvent(t, s, "permission_resolved", 5*time.Second)

	rc := notif.lastResolved()
	if rc.approver.Kind != "" {
		t.Fatalf("legacy Resolve approver = %+v, want zero (unattributed)", rc.approver)
	}
	if got := rc.approver.String(); got != "unknown" {
		t.Fatalf("zero approver render = %q, want unknown", got)
	}
}

// ---------------------------------------------------------------------------
// Persona lane (acp-employees dim 2B)
// ---------------------------------------------------------------------------

// TestSession_CommandArgsAppendedToSpawnArgv pins that Start concatenates
// Command + CommandArgs into the subprocess argv (asserted end-to-end via
// the fakeagent argv mode, which echoes its own os.Args tail).
func TestSession_CommandArgsAppendedToSpawnArgv(t *testing.T) {
	bin := fakeAgentBin(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := Start(ctx, SessionConfig{
		AgentID:     "hermes",
		Persona:     "bob",
		Command:     []string{bin, "-mode", "argv"},
		CommandArgs: []string{"-p", "borg"},
		Cwd:         t.TempDir(),
		DialTimeout: 30 * time.Second,
		CallTimeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	got, err := s.Send(ctx, "argv?")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	// fakeagent echoes "argv=<os.Args[1:] joined by ,>" — the CommandArgs
	// must ride AFTER the catalog command tail, untouched by the flag pkg
	// (unknown flags after flag.Parse are left in os.Args).
	want := "argv=-mode,argv,-p,borg"
	if got != want {
		t.Fatalf("spawn argv echo = %q, want %q", got, want)
	}
}

// TestSession_PersonaFlowsIntoRelayPending pins that a persona-stamped
// session carries the persona on the PendingPermission record and renders
// it into the bounded summary as "agent/persona".
func TestSession_PersonaFlowsIntoRelayPending(t *testing.T) {
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	cfg := humanRelayCfg(rl, notif, 5*time.Second)
	cfg.Persona = "bob"
	s, fromClient, toClient := decisionHarnessSession(t, cfg)

	sendPermissionRequest(t, toClient, "")
	reqEv := waitForEvent(t, s, "permission_requested", 5*time.Second)
	p := notif.lastRequested()
	if p.Persona != "bob" {
		t.Fatalf("PendingPermission.Persona = %q, want bob", p.Persona)
	}
	if p.AgentID != "human" {
		t.Fatalf("PendingPermission.AgentID = %q, want the catalog id", p.AgentID)
	}
	if !strings.Contains(p.Summary, "human/bob") {
		t.Fatalf("summary %q must render the persona (human/bob)", p.Summary)
	}
	// resolve so the wait goroutine exits cleanly
	if err := rl.Resolve(reqEv.Text, "deny"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	readReplyFrame(t, fromClient)
}

// TestRenderDecisionState_Persona pins the summary render: the persona
// suffix appears when set, the shape is unchanged when empty, and the
// 300-rune truncation budget still applies to the params body.
func TestRenderDecisionState_Persona(t *testing.T) {
	t.Parallel()

	got := renderDecisionState("hermes", "bob", []byte(`{"x":1}`))
	if !strings.Contains(got, "Agent: hermes/bob\n") {
		t.Fatalf("render = %q, want the agent/persona header", got)
	}
	plain := renderDecisionState("hermes", "", []byte(`{"x":1}`))
	if !strings.Contains(plain, "Agent: hermes\n") || strings.Contains(plain, "hermes/") {
		t.Fatalf("no-persona render = %q, want the bare agent header", plain)
	}
	// truncation budget unchanged: >300 runes get the ...(truncated) tail
	long := strings.Repeat("あ", decisionStateBudget+50)
	tr := renderDecisionState("hermes", "bob", []byte(long))
	if !strings.HasSuffix(tr, "...(truncated)") {
		t.Fatalf("truncation lost: tail = %q", tr[len(tr)-30:])
	}
	if strings.Count(tr, "あ") != decisionStateBudget {
		t.Fatalf("rune budget changed: got %d runes, want %d", strings.Count(tr, "あ"), decisionStateBudget)
	}
}

// TestSession_DecisionLanePersonaInQuestionState pins that the decision
// lane's question State carries the persona render too (shared helper).
func TestSession_DecisionLanePersonaInQuestionState(t *testing.T) {
	stub := &stubDecisioner{available: true, res: allowResult(1.0)}
	cfg := decisionCfg(stub, 0.5)
	cfg.AgentID = "hermes"
	cfg.Persona = "bob"
	_, fromClient, toClient := decisionHarnessSession(t, cfg)

	sendPermissionRequest(t, toClient, "")
	readReplyFrame(t, fromClient)
	q := stub.lastQuestion()
	if !strings.Contains(q.State, "Agent: hermes/bob\n") {
		t.Fatalf("decision question state = %q, want the hermes/bob header", q.State)
	}
}

// TestResolveRankTargetExtraction pins the live defect fix 2026-10-25:
// the rank state must carry the toolCall target even when the params JSON
// is large/truncated — the 300-rune Summary cut (V2 vetoed a benign edit
// it literally could not see the path of). Covers: structured parse
// (title + content path), regex fallback on truncated input, and the
// empty-params miss.
func TestResolveRankTargetExtraction(t *testing.T) {
	// Structured: hermes edit-approval shape (title + content location).
	full := []byte(`{"sessionId":"s","toolCall":{"title":"Approve edit: /tmp/a/note.txt","kind":"edit","content":[{"type":"diff","location":{"path":"/tmp/a/note.txt","kind":"file"}}],"rawInput":{"tool":"patch"}}}`)
	line := rankTargetLine(full)
	if !strings.Contains(line, "path=/tmp/a/note.txt") || !strings.Contains(line, "title=Approve edit:") {
		t.Fatalf("structured extract missing fields: %q", line)
	}

	// Truncated params (invalid JSON): fallback sweep still finds the path.
	trunc := full[:len(full)-12]
	line = rankTargetLine(trunc)
	if !strings.Contains(line, "/tmp/a/note.txt") {
		t.Fatalf("fallback sweep missed path in truncated params: %q", line)
	}

	// Empty params: no line, and the state renders without it.
	if got := rankTargetLine(nil); got != "" {
		t.Fatalf("empty params must yield no target line, got %q", got)
	}
	state := renderResolveRankState(Approver{Kind: "test"}, "allow", "summary", nil)
	if strings.Contains(state, "EXTRACTED_TARGET") {
		t.Fatal("state carries an empty target fence")
	}
	state = renderResolveRankState(Approver{Kind: "test"}, "allow", "summary", full)
	if !strings.Contains(state, "path=/tmp/a/note.txt") {
		t.Fatalf("state missing extracted target: %q", state)
	}
}
