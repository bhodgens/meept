package daemon

// Leaf 03-voice/00T §1 + §4 test gate: the per-call brief builder
// (briefForCall + usableCheck + the refusal rule) and the ReleaseHook
// isolation contract. This file is TESTS ONLY.
//
// Seam expectations (the impl leaf 03-01 picks its concrete binding, but
// MUST satisfy these shapes, or the gate stays RED — report the conflict,
// do not silently change the seam):
//
//   - func briefForCall(ctx context.Context, llm func(prompt string) (string, error),
//         lines []voice.TranscriptLine) (*briefResult, error)
//     The second parameter MUST accept a plain func value of the form
//     func(prompt string) (string, error) (an unnamed func-type parameter,
//     or a named func type with that exact underlying type — NOT an
//     interface with methods; the impl may wrap/adapt internally).
//     Give-up path after ONE parse-failure retry: (nil, errBriefUnavailable).
//   - func usableCheck(lines []voice.TranscriptLine) bool
//     Frozen refusal rule (port of the uptractor heardEnough rule):
//     usable speech = >= 3 non-filler words across the transcript.
//     Filler list (case-insensitive): um, uh, okay, yeah, hmm, like.
//   - var errBriefUnavailable
//   - VoiceEmployeeWiring gains fields ledgerStore (*ledger.Store) and
//     llm (the same func form) per leaf 03-01's contract "w gains fields
//     (ledgerStore, llm) via its constructor"; nil ledgerStore = brief
//     no-ops (feature off by absence).
//   - ReleaseHook brief path (inside the `if seen` block, AFTER
//     ReleaseForCall + the release log line): EndedCall -> usableCheck ->
//     briefForCall -> appendBriefRecordsFn; every failure WARN-only,
//     NEVER delaying or failing the teardown/relay (notifyCallRelay
//     contract mirrored).
//
// The briefResult type is owned by leaf 03-02; this file only READS its
// frozen fields (Summary, ActionItems[].{Text,Owner,Due}, Commitments,
// FollowUpMessage, RiskFlags) and never constructs one from a struct
// literal — instances come from json.Unmarshal of the frozen brief JSON
// contract, so the item type name is the impl's choice.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/voice"
)

// ---------------------------------------------------------------------------
// FROZEN: the brief system prompt lines, pinned HERE (leaf 03-01: "system
// prompt EXACTLY as 00T §1 pins — frozen text lives in that test"). The
// prompt handed to the llm seam must contain every line verbatim.
// ---------------------------------------------------------------------------

var vbBriefPromptLines = []string{
	"You turn one employee phone call into a ledger brief.",
	"Return only JSON with keys summary, action_items, commitments, follow_up_message, risk_flags.",
	"Each action_item has text, owner (me or them), and due (may be empty).",
	"Do not invent facts.",
	"action_items are only things still undone. commitments are only promises actually made.",
	"follow_up_message is a draft text in the caller's voice of the job, specific to what changed. Empty string if there is nothing to send. Never a generic 'let me know'.",
	"risk_flags is an array, empty if none.",
}

// vbBriefJSON is the frozen brief JSON contract populated from a siding
// call — the canned "valid" LLM response.
const vbBriefJSON = `{"summary":"Caller needs the siding repaired before friday","action_items":[{"text":"order siding materials","owner":"them","due":"friday"},{"text":"send the written quote","owner":"me","due":""}],"commitments":["will email the quote today"],"follow_up_message":"Confirming the siding order — quote goes out today.","risk_flags":["weather delay"]}`

// vbParseBrief turns a frozen brief JSON string into a briefResult via the
// frozen snake_case tags, avoiding any dependence on the nested item type's
// name.
func vbParseBrief(t *testing.T, raw string) *briefResult {
	t.Helper()
	var br briefResult
	if err := json.Unmarshal([]byte(raw), &br); err != nil {
		t.Fatalf("unmarshal frozen brief contract: %v", err)
	}
	return &br
}

// vbRecordingLLM is the §1 fake LLM seam: a func(prompt) (string, error)
// with a scripted response queue and a call counter. Callers pass the
// .ask method VALUE (plain func form) to briefForCall / wiring seams.
type vbRecordingLLM struct {
	mu        sync.Mutex
	responses []string // consumed in order; last entry repeats if exhausted
	errs      []error
	calls     []string
}

func (f *vbRecordingLLM) ask(_ context.Context, prompt string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, prompt)
	i := len(f.calls) - 1
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	if i < len(f.responses) {
		return f.responses[i], err
	}
	if len(f.responses) > 0 {
		return f.responses[len(f.responses)-1], err
	}
	return "", err
}

func (f *vbRecordingLLM) seam() func(string) (string, error) {
	return func(prompt string) (string, error) { return f.ask(context.Background(), prompt) }
}

func (f *vbRecordingLLM) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// vbLevelHandler records (level, message) pairs so the gate can assert
// WARN-only failure posture without pinning message text (the impl owns
// wording; the contract pins the LEVEL: brief failures warn, never error,
// never fail teardown).
type vbLevelHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *vbLevelHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *vbLevelHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *vbLevelHandler) WithGroup(string) slog.Handler            { return h }

func (h *vbLevelHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *vbLevelHandler) countAtLevel(l slog.Level) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level == l {
			n++
		}
	}
	return n
}

func (f *vbRecordingLLM) lastPrompt() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return ""
	}
	return f.calls[len(f.calls)-1]
}

// vbUsableLines is a transcript that clears the >=3 non-filler-word rule.
func vbUsableLines() []voice.TranscriptLine {
	now := time.Now().UTC()
	return []voice.TranscriptLine{
		{Role: "user", Text: "we need the siding repaired before friday please", At: now},
		{Role: "assistant", Text: "i will order the materials and send the quote", At: now},
	}
}

// ---------------------------------------------------------------------------
// §1 — brief builder against the fake LLM
// ---------------------------------------------------------------------------

func TestVoiceBriefParsesValidJSON(t *testing.T) {
	llm := &vbRecordingLLM{responses: []string{vbBriefJSON}}

	got, err := briefForCall(context.Background(), llm.seam(), vbUsableLines())
	if err != nil {
		t.Fatalf("briefForCall(valid json): %v", err)
	}
	if got == nil {
		t.Fatal("briefForCall(valid json): nil briefResult")
	}
	if got.Summary != "Caller needs the siding repaired before friday" {
		t.Errorf("Summary = %q", got.Summary)
	}
	if len(got.ActionItems) != 2 {
		t.Fatalf("len(ActionItems) = %d, want 2", len(got.ActionItems))
	}
	if got.ActionItems[0].Text != "order siding materials" || got.ActionItems[0].Owner != "them" || got.ActionItems[0].Due != "friday" {
		t.Errorf("ActionItems[0] = %+v, want {order siding materials, them, friday}", got.ActionItems[0])
	}
	if got.ActionItems[1].Owner != "me" {
		t.Errorf("ActionItems[1].Owner = %q, want me", got.ActionItems[1].Owner)
	}
	if len(got.Commitments) != 1 || got.Commitments[0] != "will email the quote today" {
		t.Errorf("Commitments = %v", got.Commitments)
	}
	if got.FollowUpMessage != "Confirming the siding order — quote goes out today." {
		t.Errorf("FollowUpMessage = %q", got.FollowUpMessage)
	}
	if len(got.RiskFlags) != 1 || got.RiskFlags[0] != "weather delay" {
		t.Errorf("RiskFlags = %v", got.RiskFlags)
	}
	if llm.count() != 1 {
		t.Errorf("llm calls = %d, want 1", llm.count())
	}

	// Frozen prompt posture: every pinned system line present verbatim,
	// and the transcript text reached the model.
	prompt := llm.lastPrompt()
	for _, line := range vbBriefPromptLines {
		if !strings.Contains(prompt, line) {
			t.Errorf("prompt missing frozen line %q", line)
		}
	}
	if !strings.Contains(prompt, "siding repaired before friday") {
		t.Error("prompt does not carry the transcript")
	}
}

func TestVoiceBriefParsesFencedJSON(t *testing.T) {
	llm := &vbRecordingLLM{responses: []string{"```json\n" + vbBriefJSON + "\n```"}}

	got, err := briefForCall(context.Background(), llm.seam(), vbUsableLines())
	if err != nil {
		t.Fatalf("briefForCall(fenced json): %v", err)
	}
	if got == nil || got.Summary == "" {
		t.Fatalf("fenced ```json block must parse; got %+v", got)
	}
	if llm.count() != 1 {
		t.Errorf("llm calls = %d, want 1 (fencing is not a parse failure)", llm.count())
	}
}

func TestVoiceBriefParsesProseThenJSON(t *testing.T) {
	// Extraction rule (frozen): first '{' to last '}' — prose around the
	// JSON object is tolerated on both sides.
	llm := &vbRecordingLLM{responses: []string{"Sure, here is the brief:\n" + vbBriefJSON + "\nHope that helps!"}}

	got, err := briefForCall(context.Background(), llm.seam(), vbUsableLines())
	if err != nil {
		t.Fatalf("briefForCall(prose+json): %v", err)
	}
	if got == nil || len(got.ActionItems) != 2 {
		t.Fatalf("prose-wrapped JSON must parse; got %+v", got)
	}
	if llm.count() != 1 {
		t.Errorf("llm calls = %d, want 1", llm.count())
	}
}

func TestVoiceBriefGivesUpAfterOneRetry(t *testing.T) {
	// (d) invalid JSON twice -> exactly ONE retry, then give-up:
	// (nil, errBriefUnavailable); NO brief. The "writes NOTHING" ledger
	// side of this path is gated in the hook tests below.
	llm := &vbRecordingLLM{responses: []string{"that was a nice call.", "still not json"}}

	got, err := briefForCall(context.Background(), llm.seam(), vbUsableLines())
	if got != nil {
		t.Errorf("give-up must return nil brief, got %+v", got)
	}
	if !errors.Is(err, errBriefUnavailable) {
		t.Errorf("give-up err = %v, want errBriefUnavailable", err)
	}
	if llm.count() != 2 {
		t.Errorf("llm calls = %d, want 2 (initial + ONE retry)", llm.count())
	}
}

func TestVoiceBriefRecoversOnRetry(t *testing.T) {
	// One parse failure, then valid JSON on the retry -> success.
	llm := &vbRecordingLLM{responses: []string{"not json at all", vbBriefJSON}}

	got, err := briefForCall(context.Background(), llm.seam(), vbUsableLines())
	if err != nil {
		t.Fatalf("retry-recover: %v", err)
	}
	if got == nil || got.Summary == "" {
		t.Fatalf("retry-recover: nil/empty brief")
	}
	if llm.count() != 2 {
		t.Errorf("llm calls = %d, want 2", llm.count())
	}
}

// ---------------------------------------------------------------------------
// §1 — refusal rule (usableCheck)
// ---------------------------------------------------------------------------

func TestVoiceBriefUsableCheck(t *testing.T) {
	line := func(texts ...string) []voice.TranscriptLine {
		out := make([]voice.TranscriptLine, 0, len(texts))
		for _, tx := range texts {
			out = append(out, voice.TranscriptLine{Role: "user", Text: tx, At: time.Now().UTC()})
		}
		return out
	}

	cases := []struct {
		name  string
		lines []voice.TranscriptLine
		want  bool
	}{
		{"empty", nil, false},
		{"all fillers lowercase", line("um, uh, okay"), false},
		{"all fillers mixed case", line("Yeah LIKE hmm"), false},
		{"two content words", line("send quote"), false},
		{"three content words", line("send the quote"), true},
		{"content plus fillers", line("um okay I will email the contractor"), true},
		{"three content across lines", line("we need it", "by friday"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := usableCheck(tc.lines); got != tc.want {
				t.Errorf("usableCheck(%v) = %v, want %v", tc.lines, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// §1/§4 — ReleaseHook brief path integration (best-effort, zero-call refusal)
// ---------------------------------------------------------------------------

// vbHookWiring builds a release-hook wiring over fakes with the brief
// seams injected directly on the struct (same package, per the 03-01
// contract "w gains fields (ledgerStore, llm) via its constructor" — the
// gate wires them post-construction so the store/llm vary per scenario).
// Mirrors notifyBusWiring (voice_employee_test.go) so the call-end relay
// stays observable.
func vbHookWiring(t *testing.T) (*VoiceEmployeeWiring, *ledger.Store, *vbRecordingLLM) {
	t.Helper()
	logger := testLogger(t)
	sessions := session.NewMemoryStore(logger)
	template := agent.NewAgentLoop("template-voice-brief", t.TempDir())
	mgr := agent.NewManager(agent.ManagerConfig{SessionStore: sessions})
	msgBus := bus.New(nil, logger)

	w, err := WireVoiceEmployee(VoiceEmployeeDeps{
		Sessions:      sessions,
		LoopManager:   mgr,
		TemplateLoop:  template,
		WorkingDir:    t.TempDir(),
		Bus:           msgBus,
		CallStorePath: filepath.Join(t.TempDir(), "calls.db"),
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("WireVoiceEmployee: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	store, err := ledger.Open(filepath.Join(t.TempDir(), "ledger.db"), logger)
	if err != nil {
		t.Fatalf("ledger.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	llm := &vbRecordingLLM{responses: []string{vbBriefJSON}}
	w.ledgerStore = store
	w.llm = llm.seam()
	// msgBus is reachable through w.bus (same package) for relay asserts.
	return w, store, llm
}

func TestVoiceBriefHookAppendsOnUsableCall(t *testing.T) {
	w, store, llm := vbHookWiring(t)
	ctx := context.Background()

	const ccid = "cc-brief-ok"
	if _, err := w.Binding.OnCallStart(ctx, ccid, "+155****9001", "emp-brief", voice.DirectionIncoming, voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}
	for _, l := range vbUsableLines() {
		if err := w.store.AppendTranscript(ctx, ccid, l); err != nil {
			t.Fatalf("AppendTranscript: %v", err)
		}
	}

	release := w.AcquireCallHold(ccid, "emp-brief")
	w.ReleaseHook(ccid, voice.OutcomeHangup)
	release()

	if llm.count() != 1 {
		t.Errorf("llm calls = %d, want 1 for a usable call", llm.count())
	}
	// The brief must land in the ledger (appendBriefRecords ran via the
	// package seam var); exact row shape is gated in §2.
	vbPollFor(t, 2*time.Second, func() bool {
		rows, err := store.QueryLog(ctx, ledger.LogQuery{EmployeeID: "emp-brief", Subject: "call:" + ccid})
		return err == nil && len(rows) > 0
	})
	rows, err := store.QueryLog(ctx, ledger.LogQuery{EmployeeID: "emp-brief", Subject: "call:" + ccid})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if rows[0].Kind != ledger.KindCommittal {
		t.Errorf("first hook row kind = %q, want committal", rows[0].Kind)
	}
	if rows[0].Source != ledger.SourceVoice || rows[0].SourceRef != ccid {
		t.Errorf("first hook row source/ref = %q/%q, want voice/%s", rows[0].Source, rows[0].SourceRef, ccid)
	}
}

func TestVoiceBriefHookGiveUpWarnsAndWritesNothing(t *testing.T) {
	// §1(d) end-to-end: invalid JSON twice -> the give-up path logs WARN
	// and the ledger receives NOTHING (the brief-level half of this is
	// TestVoiceBriefGivesUpAfterOneRetry).
	w, store, llm := vbHookWiring(t)
	llm.responses = []string{"not json", "still not json"}
	handler := &vbLevelHandler{}
	w.logger = slog.New(handler)
	ctx := context.Background()

	const ccid = "cc-brief-giveup"
	if _, err := w.Binding.OnCallStart(ctx, ccid, "+155****9006", "emp-giveup", voice.DirectionIncoming, voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}
	for _, l := range vbUsableLines() {
		if err := w.store.AppendTranscript(ctx, ccid, l); err != nil {
			t.Fatalf("AppendTranscript: %v", err)
		}
	}

	release := w.AcquireCallHold(ccid, "emp-giveup")
	w.ReleaseHook(ccid, voice.OutcomeHangup)
	release()

	if llm.count() != 2 {
		t.Errorf("llm calls = %d, want 2 (initial + ONE retry)", llm.count())
	}
	if handler.countAtLevel(slog.LevelWarn) == 0 {
		t.Error("give-up path logged no WARN record (contract: WARN-only, writes NOTHING)")
	}
	if handler.countAtLevel(slog.LevelError) != 0 {
		t.Error("give-up path logged ERROR; brief failures are WARN-only")
	}
	rows, err := store.QueryLog(ctx, ledger.LogQuery{EmployeeID: "emp-giveup", Subject: "call:" + ccid})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("give-up wrote %d ledger rows, want 0", len(rows))
	}
}

func TestVoiceBriefHookRefusesFillerCall(t *testing.T) {
	// Refusal rule end-to-end: <3 usable non-filler words -> NO llm call
	// at all (the §1 pinned assertion), no brief, no ledger rows; teardown
	// unaffected.
	w, store, llm := vbHookWiring(t)
	ctx := context.Background()

	const ccid = "cc-brief-filler"
	if _, err := w.Binding.OnCallStart(ctx, ccid, "+155****9002", "emp-brief", voice.DirectionIncoming, voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}
	if err := w.store.AppendTranscript(ctx, ccid, voice.TranscriptLine{Role: "user", Text: "um... uh... okay... yeah", At: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}

	release := w.AcquireCallHold(ccid, "emp-brief")
	w.ReleaseHook(ccid, voice.OutcomeHangup)
	release()

	if llm.count() != 0 {
		t.Errorf("llm calls = %d, want 0 for a filler-only transcript (refusal rule)", llm.count())
	}
	rows, err := store.QueryLog(ctx, ledger.LogQuery{EmployeeID: "emp-brief", Subject: "call:" + ccid})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("refused call wrote %d ledger rows, want 0", len(rows))
	}
}

// ---------------------------------------------------------------------------
// §4 — isolation: a store whose Append always errors must not break the
// release contract (mirrors the pinned TestVoiceEmployee release tests —
// hold dropped exactly once, relay published, call row ended — with the
// brief path failing underneath).
// ---------------------------------------------------------------------------

func TestVoiceBriefStoreFailureDoesNotBreakRelease(t *testing.T) {
	w, store, _ := vbHookWiring(t)
	ctx := context.Background()

	// Force every ledger.Append to fail: close the store out from under
	// the wiring (modernc sqlite -> "sql: database is closed" on Append).
	if err := store.Close(); err != nil {
		t.Fatalf("pre-close ledger store: %v", err)
	}

	const ccid = "cc-brief-broken"
	if _, err := w.Binding.OnCallStart(ctx, ccid, "+155****9003", "emp-broken", voice.DirectionIncoming, voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}
	for _, l := range vbUsableLines() {
		if err := w.store.AppendTranscript(ctx, ccid, l); err != nil {
			t.Fatalf("AppendTranscript: %v", err)
		}
	}

	// The hook must complete despite the always-erroring store.
	done := make(chan struct{})
	go func() {
		defer close(done)
		release := w.AcquireCallHold(ccid, "emp-broken")
		w.ReleaseHook(ccid, voice.OutcomeHangup)
		release()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ReleaseHook did not complete with an erroring ledger store")
	}

	// Release contract intact: the call row ended.
	_, _, ok, err := w.store.EndedCall(ctx, ccid)
	if err != nil || !ok {
		t.Fatalf("call row not ended after hook (ok=%v err=%v) — teardown was broken by the brief failure", ok, err)
	}

	// Release contract intact: the employee hold is free — a fresh acquire
	// for the same employee must not block.
	acquired := make(chan struct{})
	go func() {
		r := w.AcquireCallHold("cc-after", "emp-broken")
		close(acquired)
		r()
	}()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("employee hold still held after a brief-path failure (exactly-once release broken)")
	}

	// Release contract intact: the call-end relay still fired despite the
	// failing brief path (best-effort brief must never suppress the
	// notification — mirrors notifyCallRelay's "must never delay or fail
	// the teardown" contract).
	sub := w.bus.Subscribe("brief-broken-relay", agent.SpeakTopicNotify)
	defer w.bus.Unsubscribe(sub)
	const ccid2 = "cc-brief-broken2"
	if _, err := w.Binding.OnCallStart(ctx, ccid2, "+155****9004", "emp-broken", voice.DirectionIncoming, voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart(ccid2): %v", err)
	}
	if err := w.store.AppendTranscript(ctx, ccid2, voice.TranscriptLine{Role: "user", Text: "please take a message for alice", At: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}
	release2 := w.AcquireCallHold(ccid2, "emp-broken")
	w.ReleaseHook(ccid2, voice.OutcomeHangup)
	release2()
	payloads := collectNotify(t, sub.Channel, 2*time.Second)
	if len(payloads) != 1 {
		t.Fatalf("relay payloads = %d, want 1 despite the failing brief", len(payloads))
	}
}

func TestVoiceBriefHookNilStoreNoops(t *testing.T) {
	// Feature-off posture: nil ledgerStore -> the brief path no-ops,
	// teardown unchanged (nil store, llm armed with a valid response).
	w, _, llm := vbHookWiring(t)
	w.ledgerStore = nil
	ctx := context.Background()

	const ccid = "cc-brief-nil"
	if _, err := w.Binding.OnCallStart(ctx, ccid, "+155****9005", "emp-nil", voice.DirectionIncoming, voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}
	for _, l := range vbUsableLines() {
		if err := w.store.AppendTranscript(ctx, ccid, l); err != nil {
			t.Fatalf("AppendTranscript: %v", err)
		}
	}
	release := w.AcquireCallHold(ccid, "emp-nil")
	w.ReleaseHook(ccid, voice.OutcomeHangup)
	release()

	if llm.count() != 0 {
		t.Errorf("llm calls = %d, want 0 with a nil ledger store (off by absence)", llm.count())
	}
}

// vbPollFor waits up to d for cond to hold (the hook may run the brief
// synchronously or on a goroutine; the gate allows either).
func vbPollFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", d)
}
