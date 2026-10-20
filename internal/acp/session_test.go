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
	if params == "" {
		params = `{"sessionId":"sess-h","toolCall":{"title":"rm -rf scratch"}}`
	}
	id := int64(7)
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
}

func (n *stubNotifier) NotifyPermissionRequested(p PendingPermission) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.requested = append(n.requested, p)
	return n.listener
}

func (n *stubNotifier) NotifyPermissionResolved(p PendingPermission, decision, reason string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.resolved = append(n.resolved, resolvedCall{p, decision, reason})
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
