package voicechild

// Voice-peragent leaf 01: the short-final gate IMPROVEMENTS (exempt
// phrases, reply window, energy gate, fragment decay), their additive
// keys-frame wire, and the master invariants:
//
//   - ALL improvements are strictly inert while min_turn_words is 0
//     (zero-config = byte-identical legacy — the pin);
//   - an absent-on-wire field decodes to the today-shape default
//     (no exemptions, energy off, decay off, reply window 15);
//   - telemetry: INFO 'short final dropped' gains reason=wordcount|
//     energy; INFO 'short final exempted' with reason=phrase|reply|
//     energy fires when an improvement saves a would-be drop.

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// applyGate drives one raw keys line through the PRODUCTION decode
// seam (handleKeysFrame -> applyKeys) onto a live pipeline.
func applyGate(t *testing.T, c *Child, ccid, sel string) {
	t.Helper()
	line := `{"call_id":"` + ccid + `","event":"keys","stt_key":"stt-secret","tts_key":"tts-secret"` + sel + `}`
	if err := c.handleKeysFrame(line); err != nil {
		t.Fatalf("handleKeysFrame: %v", err)
	}
}

// TestGateKeysFrameAdditiveSelectors (wire test): every leaf-01
// selector decodes through handleKeysFrame into the pipeline fields,
// and an OLD-DAEMON line (only the pre-leaf keys) decodes to the
// gate's today-shape defaults.
func TestGateKeysFrameAdditiveSelectors(t *testing.T) {
	const ccid = "gate-wire"
	win := 40
	minW := 3
	_, child, p, _ := newAggChildFull(t, ccid, &win, &minW)

	// Part 1: the fixture's old-daemon line carried only
	// turn_confirm_ms + min_turn_words: absent fields landed on the
	// today-shape defaults.
	p.playMu.Lock()
	if p.exemptPhrases != nil {
		p.playMu.Unlock()
		t.Fatal("absent exempt_phrases decoded non-nil (must be nil = no exemptions = the legacy gate shape)")
	}
	if p.energyGate || p.minRMS != 0 || p.fragmentDecayS != 0 {
		p.playMu.Unlock()
		t.Fatalf("absent energy/decay decoded ON: gate=%v minRMS=%d decay=%d", p.energyGate, p.minRMS, p.fragmentDecayS)
	}
	if p.replyExemptS != defaultReplyExemptS {
		p.playMu.Unlock()
		t.Fatalf("absent reply_exempt_s = %d, want the child default %d", p.replyExemptS, defaultReplyExemptS)
	}
	p.playMu.Unlock()

	// Part 2: the full leaf-01 line decodes every selector.
	applyGate(t, child, ccid, `,"min_turn_words":5,"exempt_phrases":["stop","go ahead"],`+
		`"energy_gate":true,"min_rms":1234,"fragment_decay_s":45,"reply_exempt_s":9`)
	waitFor(t, func() bool {
		p.playMu.Lock()
		defer p.playMu.Unlock()
		return p.minTurnWords == 5 && p.energyGate && p.minRMS == 1234 &&
			p.fragmentDecayS == 45 && p.replyExemptS == 9 &&
			len(p.exemptPhrases) == 2 && p.exemptPhrases[1] == "go ahead"
	})

	// Part 3: an explicit empty exemption list decodes non-nil-empty
	// (the daemon's "no exemptions" override).
	applyGate(t, child, ccid, `,"min_turn_words":2,"exempt_phrases":[]`)
	waitFor(t, func() bool {
		p.playMu.Lock()
		defer p.playMu.Unlock()
		return p.exemptPhrases != nil && len(p.exemptPhrases) == 0
	})

	// Part 4: negatives clamp to the disabled value on every numeric.
	applyGate(t, child, ccid, `,"min_turn_words":-1,"min_rms":-5,"fragment_decay_s":-2,"reply_exempt_s":-3`)
	waitFor(t, func() bool {
		p.playMu.Lock()
		defer p.playMu.Unlock()
		return p.minTurnWords == 0 && p.minRMS == 0 && p.fragmentDecayS == 0 && p.replyExemptS == 0
	})
}

// TestExemptPhrasePassesAtAbsurdN: the master pin — an exempt phrase
// dispatches at N=99 (no real utterance has 99 words; the exemption is
// the whole point), with the operator-facing exempted log.
func TestExemptPhrasePassesAtAbsurdN(t *testing.T) {
	const ccid = "gate-phrase"
	win := 40
	d, child, p, stt := newAggChild(t, ccid, &win)
	applyGate(t, child, ccid, `,"turn_confirm_ms":40,"min_turn_words":99,"exempt_phrases":["stop","go ahead"],"reply_exempt_s":0`)
	logger := newCaptureLogger()
	p.logger = logger.logger()

	stt.emit(Transcript{Text: "Stop!", Final: true})
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Last == nil || !*f.Last {
		t.Fatalf("exempt phrase did not dispatch: %+v", f)
	}
	text := logger.everything()
	if !strings.Contains(text, "short final exempted") || !strings.Contains(text, "reason=phrase") {
		t.Fatalf("exempt log missing/wrong: %s", text)
	}
	if strings.Contains(text, "short final dropped") {
		t.Fatalf("exempted final ALSO logged a drop: %s", text)
	}
	// A NON-exempt short final still drops (reason=wordcount). The
	// exempted prompt was already popped by waitFrame, so the drop
	// must leave the queue EMPTY.
	stt.emit(Transcript{Text: "the other", Final: true})
	time.Sleep(150 * time.Millisecond)
	if n := countQueuedPrompts(d); n != 0 {
		t.Fatalf("non-exempt short final passed the gate: %d queued prompts, want 0", n)
	}
	if !strings.Contains(logger.everything(), "reason=wordcount") {
		t.Fatal("drop log lacks reason=wordcount (master telemetry)")
	}
}

// TestMatchExemptPhraseSemantics: whole-residue only, case- and
// punctuation-insensitive, multi-word phrase support.
func TestMatchExemptPhraseSemantics(t *testing.T) {
	set := []string{"stop", "thank you", "go ahead"}
	cases := []struct {
		residue string
		want    bool
	}{
		{"Stop!", true},        // outer punctuation trimmed
		{"THANK YOU.", true},   // case-insensitive, two words
		{"thank  you", true},   // STT double-space collapses
		{"go ahead", true},     // exact
		{"stop that", false},   // whole-residue only
		{"please stop", false}, // not a whole match
		{"", false},            // empty residue
		{"... ---", false},     // punctuation-only (never an exemption)
	}
	for _, c := range cases {
		if got := matchExemptPhrase(c.residue, set); got != c.want {
			t.Errorf("matchExemptPhrase(%q) = %v, want %v", c.residue, got, c.want)
		}
	}
}

// TestReplyExemptNearSelfSayActivity: a short final right after the
// CHILD's own spoken audio is a reply: exempted with reason=reply, and
// it dispatches. Far outside the window it drops.
func TestReplyExemptNearSelfSayActivity(t *testing.T) {
	const ccid = "gate-reply"
	win := 40
	d, child, p, stt := newAggChild(t, ccid, &win)
	applyGate(t, child, ccid, `,"turn_confirm_ms":40,"min_turn_words":3,"exempt_phrases":[],"reply_exempt_s":15`)
	logger := newCaptureLogger()
	p.logger = logger.logger()

	// The agent just spoke (the production stamp site is
	// playbackStream's chunk write; drive the seam directly).
	p.markSayActivity()
	stt.emit(Transcript{Text: "hmm okay", Final: true}) // 2 words < 3, not in the (empty) phrase set
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt {
		t.Fatalf("reply-window short final did not dispatch: %+v", f)
	}
	if !strings.Contains(logger.everything(), "reason=reply") {
		t.Fatal("reply exemption lacks reason=reply telemetry")
	}

	// Outside the window: drops.
	p.playMu.Lock()
	p.lastSayActivity = time.Now().Add(-time.Hour)
	p.playMu.Unlock()
	stt.emit(Transcript{Text: "the other", Final: true})
	time.Sleep(150 * time.Millisecond)
	if n := countQueuedPrompts(d); n != 0 {
		t.Fatalf("stale say-activity exempted a background fragment: %d queued prompts, want 0", n)
	}
}

// TestEnergyGateLoudPassesQuietDrops: with energy_gate + min_rms, the
// SAME short final passes over a loud mic window and drops over a
// quiet one; drop reason=energy, loud pass logged exempted.
func TestEnergyGateLoudPassesQuietDrops(t *testing.T) {
	const ccid = "gate-energy"
	win := 40
	d, child, p, stt := newAggChild(t, ccid, &win)
	applyGate(t, child, ccid, `,"turn_confirm_ms":40,"min_turn_words":3,"exempt_phrases":[],"energy_gate":true,"min_rms":3000,"reply_exempt_s":0`)
	logger := newCaptureLogger()
	p.logger = logger.logger()

	loud := constantChunk(loudSample)
	quiet := constantChunk(quietSample)

	// Loud utterance: "Thank you." (2 words < 3) at near-field
	// loudness -> exempted reason=energy, dispatches.
	for range 12 {
		p.noteUtteranceEnergy(loud)
	}
	stt.emit(Transcript{Text: "hi there", Final: true})
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt {
		t.Fatalf("loud short final dropped: %+v", f)
	}
	if !strings.Contains(logger.everything(), "short final exempted") ||
		!strings.Contains(logger.everything(), "reason=energy") {
		t.Fatal("loud pass lacks exempted reason=energy telemetry")
	}

	// Quiet utterance: same length, silence-level mic energy -> drop.
	for range 12 {
		p.noteUtteranceEnergy(quiet)
	}
	stt.emit(Transcript{Text: "this is", Final: true})
	time.Sleep(150 * time.Millisecond)
	if n := countQueuedPrompts(d); n != 0 {
		t.Fatalf("quiet short final dispatched: %d queued prompts, want 0", n)
	}
	if !strings.Contains(logger.everything(), "short final dropped") {
		t.Fatal("quiet drop lacks dropped telemetry")
	}
	// The energy-line reason tag must appear on the drop line too.
	if !strings.Contains(logger.everything(), "reason=energy") {
		t.Fatal("quiet drop lacks reason=energy")
	}
}

// constantChunk builds one 640-byte L16 chunk of a constant sample.
func constantChunk(sample int) []byte {
	b := make([]byte, 640)
	for i := 0; i+1 < len(b); i += 2 {
		b[i] = byte(sample)
		b[i+1] = byte(sample >> 8)
	}
	return b
}

// TestFragmentDecayEscalatesAndResets: base N=3 with decay on —
// consecutive drops escalate the effective threshold 3 -> 4 -> 5 ->
// 6 (capped at +3), and a final that clears the effective N resets
// the streak back to base. Driven directly against evaluateShortFinal
// (word/verdict semantics are the contract; the consume path is
// pinned by the suites above).
func TestFragmentDecayEscalatesAndResets(t *testing.T) {
	p := &pipeline{ccid: "decay"}
	p.applyKeys(KeysFrame{MinTurnWords: 3, FragmentDecayS: 60,
		ExemptPhrases: []string{}, ReplyExemptS: 0})

	words := func(n int) string {
		s := ""
		for i := range n {
			if i > 0 {
				s += " "
			}
			s += "w"
		}
		return s
	}
	// drop 1: effN 3, 2 words -> dropped, streak=1.
	if v, _, r := p.evaluateShortFinal(words(2)); v != gateDrop || r != "wordcount" {
		t.Fatalf("first: %v/%q, want drop wordcount", v, r)
	}
	// streak 1: effN 4 — 3 words now drops (it passed at base).
	if v, _, _ := p.evaluateShortFinal(words(3)); v != gateDrop {
		t.Fatal("no escalation at streak 1 (3 words must drop at effN 4)")
	}
	// streak 2: effN 5 — 4 words drops.
	if v, _, _ := p.evaluateShortFinal(words(4)); v != gateDrop {
		t.Fatal("no escalation at streak 2 (4 words must drop at effN 5)")
	}
	// streak 3: effN 6 — 5 words drops.
	if v, _, _ := p.evaluateShortFinal(words(5)); v != gateDrop {
		t.Fatal("no escalation at streak 3 (5 words must drop at effN 6)")
	}
	// streak 4: cap min(4,3)=3 -> effN stays 6 — 5 words still drops,
	// 6 words PASSES and resets the streak.
	if v, _, _ := p.evaluateShortFinal(words(5)); v != gateDrop {
		t.Fatal("escalation beyond the +3 cap (effN must stay 6)")
	}
	if v, _, _ := p.evaluateShortFinal(words(6)); v != gatePass {
		t.Fatal("6 words must pass at effN 6")
	}
	// Reset check: back at base effN 3 — 3 words passes again.
	if v, _, _ := p.evaluateShortFinal(words(3)); v != gatePass {
		t.Fatal("streak did not reset on the passing dispatch")
	}
	// Decay window expiry: a drop, then age gateDropAt past the
	// window -> back to base behavior.
	if v, _, _ := p.evaluateShortFinal(words(2)); v != gateDrop {
		t.Fatal("expected a drop to seed the streak")
	}
	p.playMu.Lock()
	p.gateDropAt = time.Now().Add(-2 * time.Minute)
	p.playMu.Unlock()
	if v, _, _ := p.evaluateShortFinal(words(3)); v != gatePass {
		t.Fatal("expired decay window must fall back to base N")
	}

	// Decay OFF: escalation never binds.
	q := &pipeline{ccid: "decay-off"}
	q.applyKeys(KeysFrame{MinTurnWords: 3, ReplyExemptS: 0, ExemptPhrases: []string{}})
	if v, _, _ := q.evaluateShortFinal(words(2)); v != gateDrop {
		t.Fatal("expected drop at base")
	}
	if v, _, _ := q.evaluateShortFinal(words(3)); v != gatePass {
		t.Fatal("decay off: escalation must NOT bind (3 words pass at base N=3)")
	}
}

// TestImprovementsInertAtZeroWords: the master pin, end to end —
// min_turn_words 0 with every improvement armed on the keys line:
// short finals dispatch exactly as the pre-leaf gate-off pipeline did
// and NEITHER gate log line ever appears.
func TestImprovementsInertAtZeroWords(t *testing.T) {
	const ccid = "gate-inert"
	win := 40
	d, child, p, stt := newAggChild(t, ccid, &win)
	applyGate(t, child, ccid, `,"turn_confirm_ms":40,"min_turn_words":0,"exempt_phrases":["zzz"],`+
		`"energy_gate":true,"min_rms":100000,"fragment_decay_s":60,"reply_exempt_s":15`)
	logger := newCaptureLogger()
	p.logger = logger.logger()

	for range 12 {
		p.noteUtteranceEnergy(constantChunk(quietSample))
	}
	// Both finals are 1-2 words: the PRE-LEAF gate-off pipeline
	// aggregates them into ONE dispatch (the aggregation window is
	// on) and the energy floor (min_rms 100000 = everything quiet)
	// must NOT drop either — proof every improvement is inert at
	// min_turn_words 0.
	stt.emit(Transcript{Text: "hi", Final: true})
	stt.emit(Transcript{Text: "stop", Final: true})
	waitFor(t, func() bool { return countQueuedPrompts(d) >= 1 })
	time.Sleep(100 * time.Millisecond)
	text := logger.everything()
	if strings.Contains(text, "short final") {
		t.Fatalf("gate-off path logged gate telemetry: %s", text)
	}
	for _, f := range popPrompts(d) {
		if f.Event != voiceframe.EventPrompt {
			t.Fatalf("unexpected frame %+v", f)
		}
	}
}
