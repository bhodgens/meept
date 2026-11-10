package signalleg

// CallLeg chain-migration pin (tree 20261110-voice-callleg, leaf 03).
// NOT a gate file — this pins the migrated contact-chain caller:
// signalVoiceAttempt now Places through the voice.CallLeg contract
// (voice.NewSignalAdapter(leg), constructed at the single daemon site
// in internal/daemon/contact_wiring.go). The C9 effect-key semantics
// from 20261109 leaf 02 must not regress: a stable key per requestID
// (retry-context reuse), a distinct key per fresh per-attempt id. The
// adapter's Events() lane stays UNCONSUMED by the chain (leaf-03
// behavior rule) — terminal classification still rides the outcome
// observer, pinned here too.
//
// Rigs reused from the gate files (newTestLeg / fakeEffects /
// handleCallEvent / signalCallLegTimeout), the pattern
// callleg_signal_gate_test.go follows.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/signal"
	"github.com/caimlas/meept/internal/voice"
)

// recordingCaller is a spy voice.CallLeg: records every PlaceRequest,
// returns a canned handle. Pins the request shape the migrated chain
// emits (same fields the pre-migration direct leg.Place carried).
type recordingCaller struct {
	requests []voice.PlaceRequest
	handle   voice.CallHandle
}

func (r *recordingCaller) Place(_ context.Context, req voice.PlaceRequest) (voice.CallHandle, error) {
	r.requests = append(r.requests, req)
	return r.handle, nil
}

func (r *recordingCaller) Accept(context.Context, string) (voice.CallHandle, error) {
	panic("chain never Accepts")
}
func (r *recordingCaller) Hangup(context.Context, voice.CallHandle, string) error {
	panic("chain never Hangups")
}
func (r *recordingCaller) Events() <-chan voice.CallEvent {
	ch := make(chan voice.CallEvent) // unconsumed lane; never written
	return ch
}

// TestChainSignalVoiceAttemptPlacesThroughCallLeg pins (a) exactly ONE
// leg Place per chain attempt, with the SAME fields the pre-migration
// direct call carried (EmployeeID from the leg Deps snapshot, Target =
// the record's SignalUUID, FirstLine = the chain message, Leg=signal),
// and (b) per-attempt request ids stay DISTINCT — the chain's repeated
// outreach to one contact never collides on a completed receipt.
func TestChainSignalVoiceAttemptPlacesThroughCallLeg(t *testing.T) {
	leg, _, _, _ := newTestLeg(t, func(string) bool { return true })
	spy := &recordingCaller{handle: voice.CallHandle{CCID: "signal-pin-1", Leg: voice.LegSignal}}
	attempt := leg.VoiceAttempt(spy, nil) // nil outcome: fail-soft ANSWERED arm

	rec := contact.Record{SignalUUID: "a1111111-1111-1111-1111-111111111111"}
	first, err := attempt.Attempt(context.Background(), rec, "hello one")
	if err != nil || first != string(voice.OutcomeAnswered) {
		t.Fatalf("attempt 1 = (%q, %v), want (answered, nil)", first, err)
	}
	second, err := attempt.Attempt(context.Background(), rec, "hello two")
	if err != nil || second != string(voice.OutcomeAnswered) {
		t.Fatalf("attempt 2 = (%q, %v), want (answered, nil)", second, err)
	}

	if len(spy.requests) != 2 {
		t.Fatalf("leg Places = %d, want 2 (exactly one per chain attempt)", len(spy.requests))
	}
	for i, req := range spy.requests {
		if req.Target != rec.SignalUUID {
			t.Errorf("request %d: Target = %q, want the record's SignalUUID %q", i, req.Target, rec.SignalUUID)
		}
		if req.Leg != voice.LegSignal {
			t.Errorf("request %d: Leg = %q, want %q", i, req.Leg, voice.LegSignal)
		}
		if req.RequestID == "" {
			t.Errorf("request %d: empty RequestID — the per-attempt identity (leaf 02) must ride Place", i)
		}
	}
	if spy.requests[0].RequestID == spy.requests[1].RequestID {
		t.Errorf("two chain attempts reused requestID %q — fresh attempts need distinct ids",
			spy.requests[0].RequestID)
	}
	if spy.requests[0].FirstLine != "hello one" || spy.requests[1].FirstLine != "hello two" {
		t.Errorf("FirstLine not carried per attempt: %q / %q", spy.requests[0].FirstLine, spy.requests[1].FirstLine)
	}
}

// waitFor polls cond until it holds or the shared gate timeout passes.
func waitFor(t *testing.T, cond func() bool, why string) {
	t.Helper()
	deadline := time.Now().Add(signalCallLegTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting: %s", why)
}

// reserves snapshots fakeEffects' reservation keys.
func reserves(t *testing.T, fx *fakeEffects) []string {
	t.Helper()
	fx.mu.Lock()
	defer fx.mu.Unlock()
	out := make([]string, len(fx.reserve))
	copy(out, fx.reserve)
	return out
}

// runChainAttempt runs one chain Attempt on a goroutine (the outcome
// observer blocks until the terminal lands), waits for the leg to hold
// the call, injects CONNECTED then ENDED("hangup") for the stub call id
// of sequence n, and returns the chain's classification. hangup maps to
// voice.OutcomeHangup -> the chain's CONNECTED-terminal ANSWERED arm.
func runChainAttempt(t *testing.T, attempt contact.ChannelAttempt, leg *Leg, rec contact.Record, msg string, n int) string {
	t.Helper()
	type res struct {
		outcome string
		err     error
	}
	ch := make(chan res, 1)
	go func() {
		o, err := attempt.Attempt(context.Background(), rec, msg)
		ch <- res{o, err}
	}()
	// Wait for StartCall to land: l.calls gets its entry BEFORE
	// Client.StartCall in Place, so len(Live()) alone could race the
	// stub call id; stateByCallID needs st.callID set.
	waitFor(t, func() bool {
		leg.mu.Lock()
		defer leg.mu.Unlock()
		for _, st := range leg.calls {
			if st.callID != "" {
				return true
			}
		}
		return false
	}, "chain attempt to place the call (StartCall landed)")
	callID := "call-stub-" + itoaChain(n)
	leg.handleCallEvent(signal.CallEvent{CallID: callID, State: signal.CallEventStateConnected})
	leg.handleCallEvent(signal.CallEvent{CallID: callID, State: signal.CallEventStateEnded, Reason: "hangup"})
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("chain attempt (%s) = (%q, %v), want a nil-error terminal", msg, r.outcome, r.err)
		}
		waitFor(t, func() bool { return len(leg.Live()) == 0 }, "terminal teardown")
		return r.outcome
	case <-time.After(signalCallLegTimeout):
		t.Fatalf("chain attempt (%s) never returned after the injected terminal", msg)
	}
	return ""
}

func itoaChain(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// TestChainSignalVoiceAttemptViaSignalAdapter runs the migrated path
// END-TO-END over the real rig — attempt -> NewSignalAdapter -> *Leg —
// and pins on fakeEffects (the ledger-observation rig from
// callleg_signal_gate_test.go):
//
//   - one chain attempt reserves EXACTLY one effect key per Place, over
//     (deps employee, "signal:"+uuid, per-attempt request id);
//   - two chain attempts (fresh ids) reserve DISTINCT keys;
//   - a retry-context Place with the SAME (employee, target, requestID)
//     reuses one key (the adapter never re-keys);
//   - the hangup terminal classifies ANSWERED via the outcome observer —
//     the Events() lane stays unconsumed; classification still rides the
//     pre-migration seam.
func TestChainSignalVoiceAttemptViaSignalAdapter(t *testing.T) {
	ctx := context.Background()
	leg, fx, _, _ := newTestLeg(t, func(string) bool { return true })
	// The chain snapshot reads Deps.EmployeeID (pre-migration passed
	// l.deps.EmployeeID straight into Place; empty is the rig default
	// and ErrNoEmployee-gates Place). Poke it BEFORE VoiceAttempt so
	// the snapshot carries an employee, exactly like the daemon rig
	// (Deps.EmployeeID from the signal wiring).
	leg.deps.EmployeeID = "emp-chain-pin"
	sad := voice.NewSignalAdapter(leg)
	outcomes := voice.NewOutcomeRegistry()
	// Daemon-parity observer wiring: the release hook feeds the
	// collector; the leg's terminal lane is the rig stand-in.
	leg.AddTerminalObserver(func(ccid string, o voice.Outcome, _ string) { outcomes.Record(ccid, o) })
	attempt := leg.VoiceAttempt(sad, outcomes.Lookup)

	const uuid = "b2222222-2222-2222-2222-222222222222"
	const prefix = "emp-chain-pin\x1fsignal:" + uuid + "\x1f"
	rec := contact.Record{SignalUUID: uuid}

	// Chain attempt 1 (stub-1): exactly one reservation, canonical key parts.
	if out := runChainAttempt(t, attempt, leg, rec, "greeting one", 1); out != string(voice.OutcomeAnswered) {
		t.Errorf("attempt 1 classification = %q, want answered (hangup terminal)", out)
	}
	k := reserves(t, fx)
	if len(k) != 1 {
		t.Fatalf("after attempt 1: effect reserves = %d, want exactly 1 (one leg Place per chain attempt)", len(k))
	}
	if !strings.HasPrefix(k[0], prefix) {
		t.Errorf("attempt 1 key %q lacks the canonical (employee, canonical target) prefix", k[0])
	}

	// Chain attempt 2 (stub-2): fresh per-attempt id => DISTINCT key.
	if out := runChainAttempt(t, attempt, leg, rec, "greeting two", 2); out != string(voice.OutcomeAnswered) {
		t.Errorf("attempt 2 classification = %q, want answered (hangup terminal)", out)
	}
	k = reserves(t, fx)
	if len(k) != 2 {
		t.Fatalf("after attempt 2: effect reserves = %d, want 2", len(k))
	}
	if k[1] == k[0] {
		t.Errorf("two chain attempts collided on one effect key (%q) — fresh per-attempt ids must yield distinct keys", k[0])
	}

	// Retry-context arm (C-V2): the SAME (employee, target, requestID)
	// after full teardown reuses ONE key — Places 3/4 (stubs 3/4).
	reuseReq := voice.PlaceRequest{
		EmployeeID: "emp-chain-pin",
		Target:     uuid,
		Leg:        voice.LegSignal,
		RequestID:  "req-chain-reuse",
		FirstLine:  "retry greeting",
	}
	h, err := sad.Place(ctx, reuseReq)
	if err != nil {
		t.Fatalf("retry-context Place 1: %v", err)
	}
	_ = h
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-3", State: signal.CallEventStateConnected})
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-3", State: signal.CallEventStateEnded, Reason: "busy"})
	waitFor(t, func() bool { return len(leg.Live()) == 0 }, "reuse Place 1 teardown")
	if _, err := sad.Place(ctx, reuseReq); err != nil {
		t.Fatalf("retry-context Place 2 (same requestID): %v", err)
	}
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-4", State: signal.CallEventStateConnected})
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-4", State: signal.CallEventStateEnded, Reason: "hangup"})
	waitFor(t, func() bool { return len(leg.Live()) == 0 }, "reuse Place 2 teardown")

	k = reserves(t, fx)
	if len(k) != 4 {
		t.Fatalf("after the reuse pair: effect reserves = %d, want 4", len(k))
	}
	wantReuse := prefix + "req-chain-reuse"
	if k[2] != wantReuse || k[3] != wantReuse {
		t.Errorf("same-requestID retry reserved %q / %q, want both %q — migrated path must not re-key", k[2], k[3], wantReuse)
	}
}
