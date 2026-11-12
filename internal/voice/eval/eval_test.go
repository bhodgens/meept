package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- scorer correctness on known pairs ----

func TestWERKnownPair(t *testing.T) {
	// Port spot-checks of accuracy.py wer(): norm() lowercases, '-'->
	// space, strips punctuation, splits on whitespace.
	if got := WER("I need to add a work order for Dakota Drywall.", "I need to add a work order for Dakota Drywall!"); got != 0 {
		t.Fatalf("WER identical: got %v, want 0", got)
	}
	// ref norm: move the drywall delivery to friday at eight a m (9 words)
	// hyp norm: move the drywall delivery to friday at 8 am        (9 words)
	// distance: eight->8 (sub), "a m"->"am" (sub) = 2 -> 2/9.
	if got := WER("Move the drywall delivery to Friday at eight a.m.", "Move the drywall delivery to Friday at 8am."); got != 2.0/9.0 {
		t.Fatalf("WER 8am pair: got %v, want %v", got, 2.0/9.0)
	}
	// "what's" normalizes to ONE token ("whats"): no insertion.
	// ref: whats open on the meadow ridge job (7)
	// hyp: what is open on the meadow ridge job (8): sub whats->what + ins is = 2 -> 2/7.
	if got := WER("what's open on the Meadow Ridge job?", "what is open on the Meadow Ridge job?"); got != 2.0/7.0 {
		t.Fatalf("WER contraction pair: got %v, want %v", got, 2.0/7.0)
	}
	// One substitution -> 1/8 (the recorded 'Meadow Ridge'->'metal ridge' class).
	if got := WER("The roof flashing at Meadow Ridge is finished.", "The roof flashing at metal ridge is finished."); got != 1.0/8.0 {
		t.Fatalf("WER substitution pair: got %v, want %v", got, 1.0/8.0)
	}
	// Empty hypothesis vs 5-word ref -> 1.0 (5/5).
	if got := WER("log forty sheets of drywall", ""); got != 1.0 {
		t.Fatalf("WER empty hyp: got %v, want 1", got)
	}
	// Empty ref AND empty hyp -> 0 (max(1,0) denominator port).
	if got := WER("", ""); got != 0 {
		t.Fatalf("WER empty/empty: got %v, want 0", got)
	}
}

func TestKeyHits(t *testing.T) {
	ref := "Mark the siding work order done for Meadow Ridge."
	// The degraded hypothesis keeps NO full key term ("work order" is
	// broken to "work", "siding"/"meadow ridge" are substituted): 0/3.
	hits, total := KeyHits(ref, "mark the fighting work done for metal ridge")
	if total != 3 {
		t.Fatalf("KeyHits total: got %d, want 3", total)
	}
	if hits != 0 {
		t.Fatalf("KeyHits hits: got %d, want 0", hits)
	}
	hits, total = KeyHits(ref, ref)
	if hits != total || total != 3 {
		t.Fatalf("KeyHits perfect: got %d/%d, want 3/3", hits, total)
	}
	// Ref without any key term -> total 0 (never divides by zero).
	hits, total = KeyHits("hello there", "hello there")
	if hits != 0 || total != 0 {
		t.Fatalf("KeyHits no-key ref: got %d/%d, want 0/0", hits, total)
	}
}

// ---- leak detector (ported prompt_leak.py needles) ----

func TestLeakDetectorPlantedNeedle(t *testing.T) {
	for _, needle := range LeakNeedles {
		out := "sure thing, the " + needle + " job is queued"
		found := FindLeaks(out)
		if len(found) != 1 || found[0] != needle {
			t.Fatalf("FindLeaks(%q): got %v, want [%s]", out, found, needle)
		}
		res := ScoreLeakTranscript(&LeakCase{WAV: "nonspeech.wav"}, out)
		if res.Pass {
			t.Fatalf("planted needle %q passed the leak scorer", needle)
		}
		if len(res.Leaks) != 1 || res.Leaks[0] != needle {
			t.Fatalf("ScoreLeakTranscript leaks: got %v, want [%s]", res.Leaks, needle)
		}
		if !strings.Contains(res.FailReason, needle) {
			t.Fatalf("FailReason must NAME the needle: %q", res.FailReason)
		}
	}
}

func TestLeakDetectorCaseInsensitive(t *testing.T) {
	if got := FindLeaks("Welcome to UPTRACTOR SITE COORDINATOR service"); len(got) != 1 {
		t.Fatalf("FindLeaks uppercase: got %v", got)
	}
	if got := FindLeaks("the crew is at dakota DRYWALL"); len(got) != 1 || got[0] != "dakota drywall" {
		t.Fatalf("FindLeaks mixed case: got %v", got)
	}
}

func TestLeakDetectorCleanAndAnnotations(t *testing.T) {
	// Clean text passes.
	if got := FindLeaks("move the delivery to Friday at eight a m"); got != nil {
		t.Fatalf("FindLeaks clean text: got %v, want none", got)
	}
	// Whisper.cpp's generic non-speech annotations are NOT prompt
	// vocabulary (prompt_leak.py's recorded clean run showed the same
	// class of non-prompt artifacts): they pass.
	for _, ann := range []string{"you", "(blender whirring)", "[MUSIC PLAYING]", "(water running)"} {
		if got := FindLeaks(ann); got != nil {
			t.Fatalf("FindLeaks(%q): got %v, want none (not prompt vocabulary)", ann, got)
		}
	}
	res := ScoreLeakTranscript(&LeakCase{WAV: "nonspeech.wav"}, "(music)")
	if !res.Pass {
		t.Fatalf("generic annotation failed the leak scorer: %s", res.FailReason)
	}
	res = ScoreLeakTranscript(&LeakCase{WAV: "leak-silence.wav", ExpectEmpty: true}, "")
	if !res.Pass {
		t.Fatalf("empty transcript on ExpectEmpty failed: %s", res.FailReason)
	}
	res = ScoreLeakTranscript(&LeakCase{WAV: "leak-silence.wav", ExpectEmpty: true}, "meadow ridge")
	if res.Pass || len(res.Leaks) != 1 {
		t.Fatalf("ExpectEmpty with a transcript must fail: %+v", res)
	}
}

// ---- fixture loader + clean fixture passes ----

func fixtureDir(t *testing.T) string {
	t.Helper()
	// The fixtures live at the REPO ROOT testdata/voice-eval (the lane
	// contract), not the package-local testdata; walk up from the
	// package dir exactly as the lane runner does.
	root, err := repoRootFrom(".")
	if err != nil {
		t.Skipf("fixture dir not reachable: %v", err)
	}
	return filepath.Join(root, "testdata", "voice-eval")
}

func TestLoadCaseSet(t *testing.T) {
	cs, err := LoadCaseSet(fixtureDir(t))
	if err != nil {
		t.Fatalf("LoadCaseSet: %v", err)
	}
	if cs.Prompt != VocabPrompt {
		t.Fatalf("expected.json prompt drifted from the pinned VocabPrompt")
	}
	if len(cs.Cases) < 2 {
		t.Fatalf("accuracy case set too small: %d cases", len(cs.Cases))
	}
	if len(cs.LeakCases) < 2 {
		t.Fatalf("leak case set too small: %d cases", len(cs.LeakCases))
	}
	for _, c := range cs.Cases {
		if c.WAV == "" || c.Expected == "" || c.MaxWER <= 0 || c.MaxWER >= 1 {
			t.Fatalf("accuracy case %q malformed: %+v", c.WAV, c)
		}
		if _, err := os.Stat(filepath.Join(fixtureDir(t), c.WAV)); err != nil {
			t.Fatalf("accuracy case wav missing: %v", err)
		}
	}
	for _, lc := range cs.LeakCases {
		if lc.WAV == "" {
			t.Fatalf("leak case with empty wav: %+v", lc)
		}
		if _, err := os.Stat(filepath.Join(fixtureDir(t), lc.WAV)); err != nil {
			t.Fatalf("leak case wav missing: %v", err)
		}
	}
}

func TestCleanFixturePassesScorer(t *testing.T) {
	cs, err := LoadCaseSet(fixtureDir(t))
	if err != nil {
		t.Fatalf("LoadCaseSet: %v", err)
	}
	// A perfect transcript must pass: WER 0, key terms all hit, clean.
	for i := range cs.Cases {
		res := ScoreCase(&cs.Cases[i], cs.Cases[i].Expected)
		if !res.Pass {
			t.Fatalf("clean fixture %s: scorer failed a perfect transcript (%s)", cs.Cases[i].WAV, res.FailReason)
		}
	}
}

// ---- loud-skip path: weights renamed away -> skip, exit 0, named path ----

func TestWeightsMissingSkipsLoud(t *testing.T) {
	dir := t.TempDir()
	// Stage a weights dir, then RENAME the model away: the exact lane
	// state on a machine without cached whisper weights.
	model := filepath.Join(dir, "ggml-base.en.bin")
	if err := os.WriteFile(model, []byte{}, 0o644); err != nil {
		t.Fatalf("stage model: %v", err)
	}
	gone := filepath.Join(dir, "renamed-away.bin")
	if err := os.Rename(model, gone); err != nil {
		t.Fatalf("rename away: %v", err)
	}
	t.Setenv(WeightsDirOverrideEnv, dir)

	_, err := weightsStatus()
	if err == nil {
		t.Fatalf("weightsStatus: nil error with model renamed away")
	}
	if !strings.Contains(err.Error(), errWeightsMissing.Error()) {
		t.Fatalf("weightsStatus error does not name the sentinel: %v", err)
	}
	if !strings.Contains(err.Error(), "ggml-base.en.bin") {
		t.Fatalf("weightsStatus error does not NAME the missing path: %v", err)
	}

	// RunLane must report Skipped=true (the exit-0 policy), never a
	// silent pass and never a hard failure.
	root, rerr := repoRootFrom(".")
	if rerr != nil {
		t.Skipf("fixture dir not reachable: %v", rerr)
	}
	rep, err := RunLane(root)
	if err != nil {
		t.Fatalf("RunLane with weights absent returned an error: %v", err)
	}
	if !rep.Skipped {
		t.Fatalf("RunLane with weights absent did not skip loudly (Skipped=false, failures=%d)", rep.Failures)
	}
	if rep.Failures != 0 {
		t.Fatalf("loud skip must not count failures: %d", rep.Failures)
	}
}

// ---- WER parity with the python reference on one real fixture ----

func TestScorerParityOnFixtureTranscript(t *testing.T) {
	// The lane-recorded hypothesis for work-order-02.wav through the
	// real path scored by hand: identical -> WER 0, key 2/2.
	cs, err := LoadCaseSet(fixtureDir(t))
	if err != nil {
		t.Fatalf("LoadCaseSet: %v", err)
	}
	var c *AccuracyCase
	for i := range cs.Cases {
		if cs.Cases[i].WAV == "work-order-02.wav" {
			c = &cs.Cases[i]
			break
		}
	}
	if c == nil {
		t.Skip("work-order-02 case absent")
	}
	res := ScoreCase(c, "I need to add a work order for Dakota Drywall.")
	if !res.Pass || res.WER != 0 || res.KeyHits != res.KeyTotal {
		t.Fatalf("parity case: %+v", res)
	}
	// A cross-line hypothesis (different utterance text entirely) must
	// FAIL the bar — the scorer discriminates, it is not a rubber stamp.
	other := ScoreCase(c, "Lock 40 sheets of drywall for Dakota drywall.")
	if other.Pass {
		t.Fatalf("cross-line transcript must not pass work-order-02's bar (WER %v)", other.WER)
	}
}
