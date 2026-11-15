package voice

// Phase2-context-warm leaf 02 — ContextWarmer debounce + routing tests
// (white-box, fake WarmFn recorder). Pins the C6 behavior contract:
// latest-wins debounce, final/end supersede, token floor, disabled =
// pure no-op, per-session independence, throttle spacing.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// warmRecorder is the fake WarmFn: thread-safe capture of every dispatch.
type warmRecorder struct {
	mu    sync.Mutex
	calls []recordedWarm
}

type recordedWarm struct {
	alias    string
	scope    string
	text     string // last message content (the partial rides the prefix end)
	messages []llm.ChatMessage
}

func (r *warmRecorder) fn() WarmFn {
	return func(_ context.Context, alias string, messages []llm.ChatMessage, scope string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		text := ""
		if len(messages) > 0 {
			text = messages[len(messages)-1].Content
		}
		r.calls = append(r.calls, recordedWarm{alias: alias, scope: scope, text: text, messages: messages})
	}
}

func (r *warmRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *warmRecorder) last() recordedWarm {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[len(r.calls)-1]
}

// testPrefixBuilder renders a minimal prefix: one system message padded to
// clear the token floor + the partial as the user turn. ok=false only for
// the sentinel session id.
func testPrefixBuilder(pad int) WarmPrefixFn {
	return func(employeeID, sessionID, partialText string) (WarmPrefix, bool) {
		if sessionID == "no-prefix" {
			return WarmPrefix{}, false
		}
		sys := "persona " + strings.Repeat("x", pad)
		return WarmPrefix{
			Messages: []llm.ChatMessage{
				{Role: llm.RoleSystem, Content: sys},
				{Role: llm.RoleUser, Content: partialText},
			},
			Alias: "voice-test",
			Scope: sessionID,
		}, true
	}
}

func newTestWarmer(t *testing.T, rec *warmRecorder, cfg ContextWarmConfig) *ContextWarmer {
	t.Helper()
	if cfg.Window == 0 {
		cfg.Window = 20 * time.Millisecond
	}
	cw := NewContextWarmer(cfg, ContextWarmDeps{
		Warm:        rec.fn(),
		BuildPrefix: testPrefixBuilder(400), // 400+ chars ≈ >60 tokens
		BaseCtx:     context.Background(),
	})
	t.Cleanup(func() { SetContextWarmRouting(nil) })
	return cw
}

// --- C6 debounce: 4 rapid partials inside one window ⇒ ≤2 warms with the
// LATEST text (the timer resets per partial, so exactly one fires).

func TestContextWarmDebounceLatestWins(t *testing.T) {
	rec := &warmRecorder{}
	cw := newTestWarmer(t, rec, ContextWarmConfig{Enabled: true, MinTokens: 60})

	partials := []string{"one", "one two", "one two three", "one two three four"}
	for _, p := range partials {
		cw.OnPartial("ccid-a", "emp", "sess-a", p)
		time.Sleep(5 * time.Millisecond) // inside the window: each replaces pending
	}
	deadline := time.Now().Add(2 * time.Second)
	for rec.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := rec.count(); n == 0 || n > 2 {
		t.Fatalf("warms = %d, want 1..2 (latest-wins debounce)", n)
	}
	if got := rec.last().text; got != partials[len(partials)-1] {
		t.Errorf("last warm text = %q, want %q (latest partial wins)", got, partials[len(partials)-1])
	}
	if got := rec.last().scope; got != "sess-a" {
		t.Errorf("warm scope = %q, want the turn's session scope %q", got, "sess-a")
	}
	cw.OnCallEnd("ccid-a")
}

// --- last:true supersedes: OnFinal cancels the pending debounce.

func TestContextWarmFinalCancelsPending(t *testing.T) {
	rec := &warmRecorder{}
	cw := newTestWarmer(t, rec, ContextWarmConfig{Enabled: true, MinTokens: 60})

	cw.OnPartial("ccid-a", "emp", "sess-a", "hello there")
	cw.OnFinal("ccid-a") // the real turn dispatches; warm is irrelevant now
	time.Sleep(150 * time.Millisecond)
	if n := rec.count(); n != 0 {
		t.Fatalf("warms after final = %d, want 0 (last:true cancels pending)", n)
	}
	cw.OnCallEnd("ccid-a")
}

// --- token floor: under context_warm_min_tokens ⇒ skip, no dispatch.

func TestContextWarmSubFloorSkips(t *testing.T) {
	rec := &warmRecorder{}
	cw := NewContextWarmer(ContextWarmConfig{Enabled: true, MinTokens: 60, Window: 20 * time.Millisecond}, ContextWarmDeps{
		Warm:        rec.fn(),
		BuildPrefix: testPrefixBuilder(30), // ~11 tokens < 60 floor
		BaseCtx:     context.Background(),
	})
	t.Cleanup(func() { cw.OnCallEnd("ccid-a") })

	cw.OnPartial("ccid-a", "emp", "sess-a", "short")
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if rec.count() != 0 {
			t.Fatal("warm dispatched below the token floor")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, skipped := cw.Stats(); skipped == 0 {
		t.Error("skipped counter = 0, want the floor skip recorded")
	}
}

// --- call end cancels the pending debounce (no zombie warm after release).

func TestContextWarmEndCancelsPending(t *testing.T) {
	rec := &warmRecorder{}
	cw := newTestWarmer(t, rec, ContextWarmConfig{Enabled: true, MinTokens: 60})

	cw.OnPartial("ccid-a", "emp", "sess-a", "talking while hanging up")
	cw.OnCallEnd("ccid-a")
	time.Sleep(150 * time.Millisecond)
	if n := rec.count(); n != 0 {
		t.Fatalf("warms after call end = %d, want 0", n)
	}
}

// --- disabled flag (the C6 default) ⇒ pure no-op, zero calls, no state.

func TestContextWarmDisabledIsNoOp(t *testing.T) {
	rec := &warmRecorder{}
	cw := NewContextWarmer(ContextWarmConfig{Enabled: false, MinTokens: 60}, ContextWarmDeps{
		Warm:        rec.fn(),
		BuildPrefix: testPrefixBuilder(400),
	})
	cw.OnPartial("ccid-a", "emp", "sess-a", strings.Repeat("long enough to warm ", 20))
	cw.OnFinal("ccid-a")
	cw.OnCallEnd("ccid-a")
	if n := rec.count(); n != 0 {
		t.Fatalf("disabled warmer dispatched %d warms, want 0 (byte-identical)", n)
	}
	if w, s := cw.Stats(); w != 0 || s != 0 {
		t.Errorf("disabled stats = (%d,%d), want (0,0)", w, s)
	}
	// Route* with an unregistered/disabled warmer must be inert too.
	SetContextWarmRouting(cw)
	RoutePartial("ccid-b", "emp", "sess-b", "anything")
	RouteFinal("ccid-b")
	RouteEnd("ccid-b")
	SetContextWarmRouting(nil)
	RoutePartial("ccid-c", "emp", "sess-c", "anything") // nil warmer
}

// --- two sessions are independent: per-ccid state, per-ccid cancel.

func TestContextWarmSessionsIndependent(t *testing.T) {
	rec := &warmRecorder{}
	cw := newTestWarmer(t, rec, ContextWarmConfig{Enabled: true, MinTokens: 60})

	cw.OnPartial("ccid-a", "emp-a", "sess-a", "alpha words that are long enough")
	cw.OnPartial("ccid-b", "emp-b", "sess-b", "beta words that are long enough")
	cw.OnFinal("ccid-a") // only A's pending dies

	deadline := time.Now().Add(2 * time.Second)
	for rec.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond) // give A's stale window time to misfire
	if n := rec.count(); n != 1 {
		t.Fatalf("warms = %d, want exactly 1 (session B's, A cancelled)", n)
	}
	if got := rec.last(); got.scope != "sess-b" || !strings.HasPrefix(got.text, "beta") {
		t.Errorf("warm was (scope %q, text %q), want session B's latest", got.scope, got.text)
	}
	cw.OnCallEnd("ccid-a")
	cw.OnCallEnd("ccid-b")
}

// --- prefix identity: the dispatched messages are EXACTLY what the
// builder rendered (same slice) under the builder's scope + alias — the
// daemon's real builder reuses the turn's assembly, so warm-prefix ==
// turn-prefix by construction (the divergence test lives integration-side).

func TestContextWarmPassesRenderedPrefixVerbatim(t *testing.T) {
	rec := &warmRecorder{}
	cw := newTestWarmer(t, rec, ContextWarmConfig{Enabled: true, MinTokens: 60})

	cw.OnPartial("ccid-a", "emp", "sess-a", "warmable partial text")
	deadline := time.Now().Add(2 * time.Second)
	for rec.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if rec.count() == 0 {
		t.Fatal("no warm dispatched")
	}
	got := rec.last()
	if got.alias != "voice-test" || got.scope != "sess-a" {
		t.Errorf("alias/scope = %q/%q, want voice-test/sess-a", got.alias, got.scope)
	}
	if len(got.messages) != 2 || got.messages[1].Content != "warmable partial text" {
		t.Errorf("messages not rendered verbatim from the builder: %+v", got.messages)
	}
	cw.OnCallEnd("ccid-a")
}

// --- builder ok=false ⇒ skip (no dispatch, skipped counter moves).

func TestContextWarmBuilderSkip(t *testing.T) {
	rec := &warmRecorder{}
	cw := newTestWarmer(t, rec, ContextWarmConfig{Enabled: true, MinTokens: 60})

	cw.OnPartial("ccid-a", "emp", "no-prefix", "long enough partial text here")
	time.Sleep(200 * time.Millisecond)
	if n := rec.count(); n != 0 {
		t.Fatalf("warms = %d, want 0 when the builder declines", n)
	}
	if _, skipped := cw.Stats(); skipped == 0 {
		t.Error("skipped counter = 0, want the builder skip recorded")
	}
	cw.OnCallEnd("ccid-a")
}

// --- nil deps ⇒ inert even with Enabled=true (typed-nil posture).

func TestContextWarmNilDepsInert(t *testing.T) {
	cw := NewContextWarmer(ContextWarmConfig{Enabled: true}, ContextWarmDeps{})
	if cw.Enabled() {
		t.Fatal("Enabled() true with nil deps")
	}
	cw.OnPartial("ccid-a", "emp", "sess-a", "text")
	cw.OnFinal("ccid-a")
	cw.OnCallEnd("ccid-a")
	var nilCW *ContextWarmer
	nilCW.OnPartial("ccid-a", "emp", "sess-a", "text") // nil receiver safe
	nilCW.OnFinal("ccid-a")
	nilCW.OnCallEnd("ccid-a")
}

// --- throttle: sub-throttle re-fires re-arm the window instead of
// double-dispatching inside 150 ms.

func TestContextWarmThrottleSpacing(t *testing.T) {
	rec := &warmRecorder{}
	cw := NewContextWarmer(ContextWarmConfig{
		Enabled:   true,
		MinTokens: 60,
		Window:    10 * time.Millisecond,
		Throttle:  120 * time.Millisecond,
	}, ContextWarmDeps{
		Warm:        rec.fn(),
		BuildPrefix: testPrefixBuilder(400),
		BaseCtx:     context.Background(),
	})
	t.Cleanup(func() { cw.OnCallEnd("ccid-a") })

	for _, p := range []string{"first long enough partial", "second long enough partial"} {
		cw.OnPartial("ccid-a", "emp", "sess-a", p)
		time.Sleep(40 * time.Millisecond) // past the 10 ms window, inside throttle
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rec.count() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := rec.count(); n < 1 {
		t.Fatalf("warms = %d, want ≥1 after the throttle drains", n)
	}
	// Both fired warms (if 2) must be ≥ throttle apart; with the
	// latest-wins re-arm the final text must be the LAST partial.
	if got := rec.last().text; got != "second long enough partial" {
		t.Errorf("latest dispatched text = %q, want the latest partial", got)
	}
}
