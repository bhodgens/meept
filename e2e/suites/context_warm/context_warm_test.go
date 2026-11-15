//go:build e2e

// Package contextwarm is the phase2-context-warm 00T gate suite: the
// partial context-warm contract (root C4 + branch C6) proven hermetically
// against a real scratch daemon with a live local voice call.
//
// Contract rows pinned here (branch orchestrator Test Gate matrix):
//   - "Warm payload shape": a live partial, once the debounce window
//     settles, produces ONE max_tokens=1 warm request whose prefix (system
//     message + prompt_cache_key/session-affinity lane) is identical to
//     the real turn that follows; the warm request precedes the turn
//     request in the FakeLLM arrival order.
//   - "Disabled = byte-identical (opt-in)": voice.context_warm default
//     false ⇒ partials produce ZERO warm requests (only the final turn
//     request lands).
//   - "Debounce + supersede": 4 rapid partials inside one
//     voice.turn_confirm_ms window ⇒ at most 2 warm requests, and the
//     newest partial's text is the one warmed (latest wins).
//   - "R8 no-tools/no-effect/no-quota-park": a warm against a
//     quota-blocked (429) fake upstream never delays or poisons the real
//     turn — the final turn still runs and the reply still plays.
//   - token floor: with a huge voice.context_warm_min_tokens, partials
//     produce zero warm traffic.
//
// RED note (00T gate): warm traffic only exists once the daemon routes
// partials/finals/ends through the voice context-warm seam
// (voice.RoutePartial/RouteFinal/RouteEnd, internal/voice/contextwarm.go)
// at the routeFrame + call-release sites (leaf 02 integration). The
// disabled-flag baseline and the token-floor skip hold regardless; the
// warm-traffic tests are the RED set until the integration wiring lands.
//
// Style: rides the voice-bargein suite's established local-leg harness
// (per-suite helper duplication; see fake_telnyx_test.go /
// warm_stack_test.go headers). Partials and finals are injected through
// the fake child's control socket exactly as the real STT pump emits them.
package contextwarm

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// Warm-request discriminator: a warm carries max_tokens=1 and one of the
// injected nonce texts. A real turn carries the config max_output (4096)
// and the final prompt text, so the two lanes are separable on the wire
// body alone.
func bodyMaxTokens(body map[string]any) (int, bool) {
	mt, ok := body["max_tokens"].(float64)
	if !ok {
		return 0, false
	}
	return int(mt), true
}

// bodyHasNonce reports whether any message in the request carries substr.
func bodyHasNonce(body map[string]any, substr string) bool {
	raw, err := json.Marshal(body["messages"])
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), substr)
}

type fakeReq struct {
	body map[string]any
	idx  int
}

// warmRequests returns the FakeLLM requests that look like warm traffic:
// max_tokens=1 and carrying the nonce family (never the health probe or a
// classifier call, which share neither property).
func warmRequests(s *harness.Stack, nonceFamily string) []fakeReq {
	var out []fakeReq
	for i, body := range s.Fake.Requests() {
		mt, ok := bodyMaxTokens(body)
		if !ok || mt != 1 {
			continue
		}
		if !bodyHasNonce(body, nonceFamily) {
			continue
		}
		out = append(out, fakeReq{body: body, idx: i})
	}
	return out
}

// turnRequest returns the first request carrying substr that is NOT warm
// traffic (max_tokens != 1), with its arrival index.
func turnRequest(s *harness.Stack, substr string) (fakeReq, int, bool) {
	for i, body := range s.Fake.Requests() {
		if mt, ok := bodyMaxTokens(body); ok && mt == 1 {
			continue
		}
		if bodyHasNonce(body, substr) {
			return fakeReq{body: body, idx: i}, i, true
		}
	}
	return fakeReq{}, -1, false
}

// awaitWarmRequests polls until at least n warm requests have landed.
func awaitWarmRequests(t *testing.T, s *harness.Stack, nonce string, n int, within time.Duration) []fakeReq {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		got := warmRequests(s, nonce)
		if len(got) >= n {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
	return warmRequests(s, nonce)
}

// openWarmCall boots the local-leg voice stack with context_warm on and
// the given overrides, opens one live call, and returns everything the
// scenario needs (stack, child handle, ccid). The mic keeps the splice
// join gate satisfied exactly like the bargein scenario.
type warmCall struct {
	s        *harness.Stack
	fake     *fakeTelnyx
	q        *fakeQwen3Bar
	ccid     string
	ctrlPath string
}

func openWarmCall(t *testing.T, voiceExtras map[string]any) *warmCall {
	t.Helper()
	q := newFakeQwen3Bar(t)
	fake := newFakeTelnyx(t)
	merged := map[string]any{}
	for k, v := range voiceExtras {
		merged[k] = v
	}
	s, ctrlPath := bootLocalVoiceStack(t, fake, q, merged)

	const warmCcid = "cc-warm-boot"
	fake.deliverCallInitiated(s, warmCcid, "incoming")
	fake.awaitAnswered(warmCcid, 10*time.Second)
	fake.dialMedia(s, warmCcid)
	awaitLogLine(t, s, "voice media: start frame", 15*time.Second)
	fake.deliverHangup(s, warmCcid)
	awaitLogLine(t, s, "voice call ended", 15*time.Second)

	awaitFile(t, ctrlPath, 10*time.Second)

	const employee = "emp-ctxwarm"
	open := rpcOpenLocalCall(t, s, employee, "ctxwarmbox")
	ccid, _ := open["call_id"].(string)
	awaitLogLine(t, s, "voice wiring: talk loop armed", 15*time.Second)

	fake.connectChild(ctrlPath, 15*time.Second)
	fake.childDialAudio(ccid)
	client := dialLocalClient(t, open)
	// SHORT burst: just enough mic flow for the local splice's join gate.
	// Continuous loud audio would keep the RMS barge-in lane firing and
	// clear every say before the TTS lane could synth it (the
	// voice-bargein scenario uses the same 2 s burst shape).
	go client.speakMic(2000)

	return &warmCall{s: s, fake: fake, q: q, ccid: ccid, ctrlPath: ctrlPath}
}

func awaitFile(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if fileExists(path) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("path %s never appeared within %s", path, within)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// endLocalCall tears the call down through the real RPC surface (same
// shape as the voice-local suite's endLocalCall, minus the calls.db row
// poll: this suite asserts on the FakeLLM request lane, not the
// transcript store).
func endLocalCall(t *testing.T, s *harness.Stack, ccid string) {
	t.Helper()
	endClient := harness.DialRPC(t, s.SocketPath)
	end := endClient.CallResult("voice.local.end", map[string]any{"call_id": ccid})
	if end["ok"] != true {
		t.Fatalf("voice.local.end(%s) result = %v, want ok:true", ccid, end)
	}
	awaitLogLine(t, s, "voice call ended", 15*time.Second)
}

// TestContextWarmPartialThenFinal pins the core R7 flow: with
// voice.context_warm on and a low token floor, rapid partials settle into
// at most 2 warm requests (debounce, latest wins) BEFORE the final turn,
// sharing the turn's prefix (first system message byte-equal) and its
// session-affinity lane (prompt_cache_key equal).
func TestContextWarmPartialThenFinal(t *testing.T) {
	w := openWarmCall(t, map[string]any{
		"context_warm":            true,
		"context_warm_min_tokens": 1,
		"turn_confirm_ms":         900,
		"supervisor_enabled":      false,
	})

	s := w.s
	// Script the final turn's reply by prompt text (executor lane).
	s.Fake.Script(
		harness.And(harness.IsExecutorRequest(), harness.LastUserMessageContains("ctxwarmfinal what is the denver forecast")),
		harness.TextResponse("CTXWARMTURN the forecast is sunny."),
	)

	partials := []string{
		"ctxwarmnonce one tell me about the weather outside to",
		"ctxwarmnonce two tell me about the weather outside today",
		"ctxwarmnonce three in denver please tell me the weather today",
		"ctxwarmnonce four denver please what is the weather today",
	}
	for _, p := range partials {
		w.fake.childSendsPrompt(w.ccid, p, false)
		time.Sleep(40 * time.Millisecond)
	}

	// The debounce window settles with NO final: warms must fire on the
	// partials alone (this is the whole feature — prefill off the turn's
	// critical path).
	warms := awaitWarmRequests(t, s, "ctxwarmnonce", 1, 10*time.Second)
	if len(warms) == 0 {
		t.Fatalf("no warm requests after partials with context_warm on: "+
			"daemon routeFrame->voice.RoutePartial integration missing (FakeLLM saw %d requests total)",
			s.Fake.RequestCount())
	}
	if len(warms) > 2 {
		t.Errorf("4 rapid partials produced %d warm requests, want <= 2 (debounce: per-session window, at most 1 in-flight)", len(warms))
	}
	// Latest wins: the LAST warm request must carry the newest partial.
	last := warms[len(warms)-1]
	if !bodyHasNonce(last.body, "ctxwarmnonce four") {
		t.Errorf("latest warm request does not carry the newest partial text: %s", mustJSON(last.body["messages"]))
	}

	// Now the final: the real turn must run and its reply must play.
	w.fake.childSendsPrompt(w.ccid, "ctxwarmfinal what is the denver forecast", true)
	_, turnIdx, ok := awaitTurn(t, s, "ctxwarmfinal what is the denver forecast", 30*time.Second)
	if !ok {
		t.Fatalf("final turn request never reached FakeLLM")
	}
	if warms[0].idx >= turnIdx {
		t.Errorf("warm request arrived at %d, turn at %d: warm must precede the real turn", warms[0].idx, turnIdx)
	}
	w.fake.awaitSayText(w.ccid, "CTXWARMTURN", 20*time.Second)

	// Prefix identity: same system message, same prompt_cache_key lane.
	turn, _, _ := turnRequest(s, "ctxwarmfinal what is the denver forecast")
	if got, want := firstSystem(warms[0].body), firstSystem(turn.body); got == "" || got != want {
		t.Errorf("warm/turn prefix mismatch:\n warm sys=%q\n turn sys=%q", got, want)
	}
	if pk, _ := warms[0].body["prompt_cache_key"].(string); pk == "" {
		t.Error("warm request carries no prompt_cache_key (must share the turn's session-affinity lane)")
	} else if tk, _ := turn.body["prompt_cache_key"].(string); tk != pk {
		t.Errorf("prompt_cache_key mismatch: warm=%q turn=%q (must be the SAME scope)", tk, pk)
	}

	endLocalCall(t, s, w.ccid)
}

// awaitSayContainingText polls the fake child's mirrored C1 frames until
// a say frame for ccid carries substr (in plain fake-child mode the say
// TEXT is the playback observable: the child synthesizes only in
// realAudio mode, so the TTS endpoint stays un-asked in this suite).
func (f *fakeTelnyx) awaitSayText(ccid, substr string, within time.Duration) {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		f.childMu.Lock()
		for _, fr := range f.frames[ccid] {
			if fr.Event == "say" && strings.Contains(fr.Text, substr) {
				f.childMu.Unlock()
				return
			}
		}
		f.childMu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("no say frame for %s containing %q within %s", ccid, substr, within)
}

func awaitTurn(t *testing.T, s *harness.Stack, substr string, within time.Duration) (fakeReq, int, bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if req, idx, ok := turnRequest(s, substr); ok {
			return req, idx, true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fakeReq{}, -1, false
}

func firstSystem(body map[string]any) string {
	msgs, _ := body["messages"].([]any)
	if len(msgs) == 0 {
		return ""
	}
	m, _ := msgs[0].(map[string]any)
	if role, _ := m["role"].(string); role != "system" {
		return ""
	}
	content, _ := m["content"].(string)
	return content
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<unmarshalable>"
	}
	return string(b)
}

// TestContextWarmDisabledZeroWarms pins the C6 opt-in row: flag off
// (the shipped default) ⇒ partials fire ZERO warm requests and the final
// turn runs exactly once — byte-identical legacy behavior.
func TestContextWarmDisabledZeroWarms(t *testing.T) {
	w := openWarmCall(t, map[string]any{
		"turn_confirm_ms":    900,
		"supervisor_enabled": false,
		// context_warm deliberately unset: default false.
	})
	s := w.s
	s.Fake.Script(
		harness.And(harness.IsExecutorRequest(), harness.LastUserMessageContains("ctxwarmofffinal forecast")),
		harness.TextResponse("CTXWARMOFFTURN done."),
	)

	for _, p := range []string{
		"ctxwarmoffnonce one what is the weather looking like today",
		"ctxwarmoffnonce two what is the weather in denver today",
	} {
		w.fake.childSendsPrompt(w.ccid, p, false)
		time.Sleep(100 * time.Millisecond)
	}
	// Settle past any possible debounce window: nothing may warm.
	time.Sleep(2 * time.Second)
	if n := len(warmRequests(s, "ctxwarmoffnonce")); n != 0 {
		t.Errorf("context_warm OFF produced %d warm requests, want 0 (opt-in default false)", n)
	}

	w.fake.childSendsPrompt(w.ccid, "ctxwarmofffinal forecast for tomorrow", true)
	if _, _, ok := awaitTurn(t, s, "ctxwarmofffinal forecast", 30*time.Second); !ok {
		t.Fatalf("final turn never ran on the disabled path")
	}
	w.fake.awaitSayText(w.ccid, "CTXWARMOFFTURN", 20*time.Second)

	if n := len(warmRequests(s, "ctxwarmoffnonce")); n != 0 {
		t.Errorf("warm requests appeared mid-turn with the flag off: %d", n)
	}
	endLocalCall(t, s, w.ccid)
}

// TestContextWarmTokenFloorSkips pins voice.context_warm_min_tokens: with
// the floor set above any realistic prefix size, enabled partials produce
// zero warm traffic but the final turn is unaffected.
func TestContextWarmTokenFloorSkips(t *testing.T) {
	w := openWarmCall(t, map[string]any{
		"context_warm":            true,
		"context_warm_min_tokens": 100000,
		"turn_confirm_ms":         900,
		"supervisor_enabled":      false,
	})
	s := w.s
	s.Fake.Script(
		harness.And(harness.IsExecutorRequest(), harness.LastUserMessageContains("ctxwarmfloorfinal forecast")),
		harness.TextResponse("CTXWARMFLOORTURN done."),
	)
	w.fake.childSendsPrompt(w.ccid, "ctxwarmfloornonce what is the weather today please", false)
	time.Sleep(2500 * time.Millisecond)
	if n := len(warmRequests(s, "ctxwarmfloornonce")); n != 0 {
		t.Errorf("sub-floor partial produced %d warm requests, want 0 (context_warm_min_tokens skip)", n)
	}
	w.fake.childSendsPrompt(w.ccid, "ctxwarmfloorfinal forecast for tomorrow", true)
	if _, _, ok := awaitTurn(t, s, "ctxwarmfloorfinal forecast", 30*time.Second); !ok {
		t.Fatalf("final turn never ran with the floor set")
	}
	endLocalCall(t, s, w.ccid)
}

// TestContextWarmQuotaUpstreamTurnUnaffected pins R8 end to end: when the
// warm lane's upstream answers 429 quota-blocked, the real final turn must
// still run and still play — a failed warm never parks, retries into, or
// poisons the turn.
func TestContextWarmQuotaUpstreamTurnUnaffected(t *testing.T) {
	w := openWarmCall(t, map[string]any{
		"context_warm":            true,
		"context_warm_min_tokens": 1,
		"turn_confirm_ms":         900,
		"supervisor_enabled":      false,
	})
	s := w.s
	// Every warm-shaped request (max_tokens=1 + nonce) is quota-blocked.
	s.Fake.Script(
		harness.And(
			func(body map[string]any) bool {
				mt, ok := bodyMaxTokens(body)
				return ok && mt == 1
			},
			harness.LastUserMessageContains("ctxwarmquotanonce"),
		),
		harness.QuotaResponse("usage_limit_reached", time.Now().Add(24*time.Hour)),
	)
	s.Fake.Script(
		harness.And(harness.IsExecutorRequest(), harness.LastUserMessageContains("ctxwarmquotafinal forecast")),
		harness.TextResponse("CTXWARMQUOTATURN done."),
	)

	start := time.Now()
	w.fake.childSendsPrompt(w.ccid, "ctxwarmquotanonce one what is the denver weather today", false)
	// The warm attempt must be OBSERVED for this row to mean anything:
	// without warm traffic there is no quota-blocked warm to survive.
	// (RED pre-integration: this is the failing assertion.)
	if got := awaitWarmRequests(t, s, "ctxwarmquotanonce", 1, 8*time.Second); len(got) == 0 {
		t.Fatalf("quota-row scenario: no warm request after a partial with context_warm on "+
			"(daemon RoutePartial integration missing; FakeLLM saw %d requests total)", s.Fake.RequestCount())
	}

	w.fake.childSendsPrompt(w.ccid, "ctxwarmquotafinal forecast for tomorrow", true)
	if _, _, ok := awaitTurn(t, s, "ctxwarmquotafinal forecast", 30*time.Second); !ok {
		t.Fatalf("quota-blocked warm delayed or poisoned the real turn")
	}
	w.fake.awaitSayText(w.ccid, "CTXWARMQUOTATURN", 20*time.Second)
	if el := time.Since(start); el > 45*time.Second {
		t.Errorf("quota-blocked warm path took %s end to end — warm must never park a turn", el)
	}
	endLocalCall(t, s, w.ccid)
}
