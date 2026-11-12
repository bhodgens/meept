//go:build e2e

package voicelocal

// voice_events_test.go — suite voice-local, scenario voice-local-events-01:
// the operator telemetry feed (voice-events tree, commit d0657f4b) proven
// hermetically end to end: a real local call's lifecycle — heard,
// prompt_dispatch, turn_final, barge_in, call_ended — surfaces through the
// voice.local.events RPC with cursor semantics, over the real ring written
// by the talk loop's OnEvent hook and routeFrame, read through the real
// daemon adapter.

import (
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// eventAccum drives voice.local.events polls from an internal cursor and
// remembers every row ever seen, keyed by kind (first occurrence wins: the
// kinds this suite asserts fire once per turn). Cursor handling is
// BATCH-AWARE: a poll returns every event after the cursor, so advancing
// past a batch and then hunting a kind inside that batch would replay-
// miss it — the accumulator keeps the full map instead of per-kind cursors.
type eventAccum struct {
	t      *testing.T
	c      *harness.RPCClient
	ccid   string
	cursor uint64
	seen   map[string]map[string]any // kind -> first row of that kind
	order  []string                  // kinds in first-seen order
}

func newEventAccum(t *testing.T, c *harness.RPCClient, ccid string) *eventAccum {
	return &eventAccum{t: t, c: c, ccid: ccid, seen: map[string]map[string]any{}}
}

// pollOnce advances the cursor over whatever the daemon returns.
func (a *eventAccum) pollOnce() {
	a.t.Helper()
	res := a.c.CallResult("voice.local.events", map[string]any{"call_id": a.ccid, "cursor": a.cursor})
	rows, _ := res["events"].([]any)
	cur, _ := res["cursor"].(float64)
	for _, r := range rows {
		e, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if seq, ok := e["seq"].(float64); uint64(seq) > a.cursor && ok {
			a.cursor = uint64(seq)
		}
		k, _ := e["kind"].(string)
		if _, dup := a.seen[k]; !dup {
			a.seen[k] = e
			a.order = append(a.order, k)
		}
	}
	if uint64(cur) > a.cursor {
		a.cursor = uint64(cur)
	}
}

// await blocks until kind k was observed (fails after within).
func (a *eventAccum) await(k string, within time.Duration) map[string]any {
	a.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		a.pollOnce()
		if e, ok := a.seen[k]; ok {
			return e
		}
		time.Sleep(100 * time.Millisecond)
	}
	a.t.Fatalf("event kind %q never surfaced via voice.local.events for %s within %v (seen so far: %v)",
		k, a.ccid, within, a.order)
	return nil
}

// rawPoll calls the RPC directly for the cursor-discipline assertions.
func rawPoll(t *testing.T, c *harness.RPCClient, ccid string, cursor uint64) ([]map[string]any, uint64, bool) {
	t.Helper()
	res := c.CallResult("voice.local.events", map[string]any{"call_id": ccid, "cursor": cursor})
	found, _ := res["found"].(bool)
	rows, _ := res["events"].([]any)
	cur, _ := res["cursor"].(float64)
	var out []map[string]any
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, uint64(cur), found
}

func TestVoiceLocalEventTelemetry(t *testing.T) {
	_, modelDir := shimPath(t)
	modelPath := writePiperModel(t, modelDir)
	fake := newFakeTelnyx(t)
	s, ctrlPath := bootLocalStack(t, fake, modelPath)

	const (
		employee = "emp-local-events"
		label    = "eventsbox"
	)
	rpcCreateEmployee(t, s, employee)

	s.Fake.Script(
		harness.And(harness.IsExecutorRequest(), harness.LastUserMessageContains("local utterance one")),
		harness.TextResponse("PIPERMARKER telemetry reply for the events suite."),
	)

	const warmCcid = "cc-events-warm"
	spawnChildViaPhoneWarmCall(t, s, fake, warmCcid)
	fake.connectChild(ctrlPath, 15*time.Second)

	open := rpcOpenLocalCall(t, s, employee, label)
	ccid, _ := open["call_id"].(string)
	awaitLogLine(t, s, "voice wiring: talk loop armed", 15*time.Second)

	eventsClient := harness.DialRPC(t, s.SocketPath)
	acc := newEventAccum(t, eventsClient, ccid)

	// 1. heard + prompt_dispatch after the mic utterance transcribes.
	//    heard is routeFrame's FINAL-prompt record; prompt_dispatch is the
	//    talk loop's own. Both carry the shim's fixed transcript.
	client := dialLocalClient(t, open)
	go client.speakMic()
	fake.childCommand(ctrlMsg{Ctrl: "speak-mic", CallID: ccid, Note: scriptedUtteranceB64()})

	heard := acc.await("heard", 25*time.Second)
	if txt, _ := heard["text"].(string); txt == "" {
		t.Fatalf("heard event carries no text preview: %+v", heard)
	}
	if seq, _ := heard["seq"].(float64); uint64(seq) <= 0 {
		t.Fatalf("heard event seq must be 1-based: %+v", heard)
	}
	acc.await("prompt_dispatch", 20*time.Second)

	// 2. turn_final with words + latency once the reply streams.
	awaitSayFrame(t, fake, ccid, "PIPERMARKER", 20*time.Second)
	final := acc.await("turn_final", 20*time.Second)
	if w, _ := final["words"].(float64); w <= 0 {
		t.Fatalf("turn_final event must carry reply words: %+v", final)
	}

	// 3. barge_in: the fake child emits a real C1 interrupt frame; the
	//    talk loop records it on the ring.
	fake.childCommand(ctrlMsg{Ctrl: "interrupt", CallID: ccid})
	acc.await("barge_in", 15*time.Second)

	// 4. cursor discipline: a poll at the accumulator's advanced cursor
	//    returns only strictly-newer rows (monotonic, no replay of the
	//    lifecycle already consumed).
	rows, _, found := rawPoll(t, eventsClient, ccid, acc.cursor)
	if !found {
		t.Fatalf("ring must report found for a live call: ccid=%s", ccid)
	}
	for _, e := range rows {
		if seq, _ := e["seq"].(float64); uint64(seq) <= acc.cursor {
			t.Fatalf("events must be strictly after the cursor: seq=%v cursor=%d", e["seq"], acc.cursor)
		}
	}

	// 5. call_ended on voice.local.end, with the reason.
	endClient := harness.DialRPC(t, s.SocketPath)
	end := endClient.CallResult("voice.local.end", map[string]any{"call_id": ccid})
	if end["ok"] != true {
		t.Fatalf("voice.local.end result = %v, want ok:true", end)
	}
	ended := acc.await("call_ended", 15*time.Second)
	if rsn, _ := ended["reason"].(string); rsn == "" {
		t.Fatalf("call_ended event must carry a reason: %+v", ended)
	}

	// 6. Full lifecycle: every kind landed, and the first-seen order is
	//    the causal order (heard precedes dispatch precedes final).
	want := []string{"heard", "prompt_dispatch", "turn_final", "barge_in", "call_ended"}
	pos := map[string]int{}
	for i, k := range acc.order {
		pos[k] = i
	}
	for _, k := range want {
		if _, ok := pos[k]; !ok {
			t.Fatalf("lifecycle missing kind %q; ring order = %v", k, acc.order)
		}
	}
	if !(pos["heard"] < pos["prompt_dispatch"] && pos["prompt_dispatch"] < pos["turn_final"] && pos["turn_final"] < pos["barge_in"] && pos["barge_in"] < pos["call_ended"]) {
		t.Fatalf("lifecycle order wrong: %v", acc.order)
	}
}

func TestVoiceLocalEventsUnknownCall(t *testing.T) {
	_, modelDir := shimPath(t)
	modelPath := writePiperModel(t, modelDir)
	fake := newFakeTelnyx(t)
	s, _ := bootLocalStack(t, fake, modelPath)

	c := harness.DialRPC(t, s.SocketPath)
	rows, cur, found := rawPoll(t, c, "local-nonexistent", 0)
	if found {
		t.Fatalf("unknown call must report found:false, got rows=%v", rows)
	}
	if len(rows) != 0 || cur != 0 {
		t.Fatalf("unknown call must answer the empty envelope (never an error mid-teardown): rows=%v cur=%d", rows, cur)
	}
}
