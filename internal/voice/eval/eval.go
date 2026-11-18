// Package eval ports the uptractor sidecar-cloud voice eval harness
// (phone/core/sidecar-cloud/eval/{accuracy.py,prompt_leak.py}, branch
// fix/coordinator-missed-hangup-watchdog) into a persistent Go test lane:
// STT accuracy scoring plus prompt-leak detection, driven by
// `make test-voice-eval` against committed WAV fixtures and the real
// local whisper path (internal/voicechild/providers, vendor "whisper").
//
// IMPORT NOTE: this package is internal/voice/eval — name-similar to but
// distinct from the top-level internal/eval (classifier/trajectory eval).
// Import paths must be written in full.
//
// Port fidelity (pinned verbatim, see the constants below):
//   - accuracy.py norm()/wer(): normalize to [a-z0-9 ] words (lowercase,
//     '-' becomes space, every other non-alphanumeric dropped), then
//     Levenshtein word distance over max(1, len(ref)).
//   - accuracy.py KEY / keyhits(): key-term hit list and counting rule.
//   - accuracy.py PROMPT: the sidecar vocabulary prompt string.
//   - prompt_leak.py: its needle list — the PROMPT vocabulary Whisper
//     could hallucinate on non-speech audio. Any needle substring in a
//     transcript produced from NON-SPEECH input is a leak = FAIL.
//     (Accuracy-case hypotheses are exempt: a caller legitimately saying
//     "Dakota Drywall" is the site name, not a leak — prompt_leak.py's
//     domain is non-speech audio only.)
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Norm-space rewrite of accuracy.py norm(): lowercase, '-' to space,
// strip everything that is not a-z 0-9 or space, split on whitespace.
func normWords(s string) []string {
	s = strings.ReplaceAll(strings.ToLower(s), "-", " ")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == ' ':
			b.WriteRune(r)
		}
	}
	return strings.Fields(b.String())
}

// WER ports accuracy.py wer(): word-level Levenshtein distance divided
// by max(1, len(ref)).
func WER(ref, hyp string) float64 {
	r, h := normWords(ref), normWords(hyp)
	d := make([][]int, len(r)+1)
	for i := range d {
		d[i] = make([]int, len(h)+1)
		d[i][0] = i
	}
	for j := 0; j <= len(h); j++ {
		d[0][j] = j
	}
	for i := 1; i <= len(r); i++ {
		for j := 1; j <= len(h); j++ {
			cost := 1
			if r[i-1] == h[j-1] {
				cost = 0
			}
			m := min(d[i][j-1]+1, d[i-1][j]+1, d[i-1][j-1]+cost)
			d[i][j] = m
		}
	}
	den := max(len(r), 1)
	return float64(d[len(r)][len(h)]) / float64(den)
}

// joinNorm is accuracy.py's " ".join(norm(s)) — the normalized string a
// key term must appear in to be counted.
func joinNorm(s string) string {
	return strings.Join(normWords(s), " ")
}

// KeyTerms ports accuracy.py KEY verbatim.
var KeyTerms = []string{
	"meadow ridge",
	"dakota drywall",
	"siding",
	"drywall",
	"work order",
	"roof flashing",
	"friday",
}

// KeyHits ports accuracy.py keyhits(): a key counts toward the total when
// it appears in the normalized reference; it counts as a hit when it also
// appears in the normalized hypothesis. Returns (hits, total).
func KeyHits(ref, hyp string) (hits, total int) {
	refJoin, hypJoin := joinNorm(ref), joinNorm(hyp)
	for _, k := range KeyTerms {
		if strings.Contains(refJoin, k) {
			total++
			if strings.Contains(hypJoin, k) {
				hits++
			}
		}
	}
	return hits, total
}

// VocabPrompt ports accuracy.py PROMPT verbatim: the sidecar's vocabulary
// prompt (also the source of the prompt-leak needles).
const VocabPrompt = "Uptractor site coordinator. Jobs: Meadow Ridge, Dakota Drywall. Work order, siding, drywall, roof flashing, delivery, crew, message."

// LeakNeedles is the prompt_leak.py needle list, pinned verbatim: the
// distinct vocabulary the PROMPT can seed. Any needle substring in a
// transcript produced from NON-SPEECH input is a leak = FAIL.
// (prompt_leak.py judges the prompt safe when feeding non-speech audio —
// silence, hiss, hum, rumble, hold music — produces NO text; this port
// hardens that into a substring check so a future STT/prompt swap that
// hallucinates prompt vocabulary on real line noise fails the lane.
// Generic non-speech annotations like "(music)" or a stray "you" are NOT
// needles: prompt_leak.py's bar is prompt vocabulary, not any text.)
var LeakNeedles = []string{
	"uptractor site coordinator",
	"meadow ridge",
	"dakota drywall",
	"roof flashing",
}

// FindLeaks returns the needles present in output (case-insensitive
// substring match). Empty slice = clean.
func FindLeaks(output string) []string {
	low := strings.ToLower(output)
	var found []string
	for _, n := range LeakNeedles {
		if strings.Contains(low, n) {
			found = append(found, n)
		}
	}
	return found
}

// AccuracyCase is one entry of expected.json's "cases": a WAV fixture,
// its expected transcript (the accuracy.py LINES text the audio was
// synthesized from), and the frozen pass threshold for that case.
type AccuracyCase struct {
	WAV       string  `json:"wav"`                    // file name under testdata/voice-eval/
	Expected  string  `json:"expected"`               // reference transcript (accuracy.py LINES text)
	MaxWER    float64 `json:"max_wer"`                // pass bar: WER(Expected, actual) <= MaxWER
	MinKeyHit *int    `json:"min_key_hits,omitempty"` // optional key-terms floor
}

// LeakCase is one entry of expected.json's "leak_cases": a NON-SPEECH
// fixture (prompt_leak.py's CASES shapes) that must yield a transcript
// containing no needle. ExpectEmpty marks fixtures the production RMS
// gate suppresses entirely (true silence never reaches whisper).
type LeakCase struct {
	WAV         string `json:"wav"`
	ExpectEmpty bool   `json:"expect_empty,omitempty"`
}

// CaseSet is the frozen eval case set committed as expected.json.
type CaseSet struct {
	ThresholdNote string         `json:"threshold_note"`
	Prompt        string         `json:"prompt"` // VocabPrompt, frozen from accuracy.py PROMPT
	Cases         []AccuracyCase `json:"cases"`
	LeakCases     []LeakCase     `json:"leak_cases"`
}

// LoadCaseSet reads expected.json from dir.
func LoadCaseSet(dir string) (*CaseSet, error) {
	b, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil {
		return nil, fmt.Errorf("voice eval: read expected.json: %w", err)
	}
	var cs CaseSet
	if err := json.Unmarshal(b, &cs); err != nil {
		return nil, fmt.Errorf("voice eval: parse expected.json: %w", err)
	}
	if len(cs.Cases) == 0 {
		return nil, fmt.Errorf("voice eval: expected.json has no accuracy cases")
	}
	if len(cs.LeakCases) == 0 {
		return nil, fmt.Errorf("voice eval: expected.json has no leak cases")
	}
	return &cs, nil
}

// CaseResult is one accuracy case's lane verdict.
type CaseResult struct {
	WAV        string
	Actual     string
	WER        float64
	KeyHits    int
	KeyTotal   int
	Pass       bool
	FailReason string
}

// ScoreCase scores one accuracy transcript against its case: the ported
// WER + key-term counting. (Leak detection is a separate lane section —
// see ScoreLeakTranscript.)
func ScoreCase(c *AccuracyCase, actual string) CaseResult {
	res := CaseResult{WAV: c.WAV, Actual: actual}
	res.WER = WER(c.Expected, actual)
	res.KeyHits, res.KeyTotal = KeyHits(c.Expected, actual)
	pass := res.WER <= c.MaxWER
	if c.MinKeyHit != nil && res.KeyHits < *c.MinKeyHit {
		pass = false
		res.FailReason = fmt.Sprintf("key terms %d/%d < required %d", res.KeyHits, res.KeyTotal, *c.MinKeyHit)
	}
	if !pass && res.FailReason == "" {
		res.FailReason = fmt.Sprintf("WER %.0f%% > bar %.0f%%", res.WER*100, c.MaxWER*100)
	}
	res.Pass = pass
	return res
}

// LeakResult is one leak case's lane verdict.
type LeakResult struct {
	WAV        string
	Actual     string
	Pass       bool
	Leaks      []string
	FailReason string
}

// ScoreLeakTranscript applies the prompt-leak rule to a transcript
// produced from a NON-SPEECH fixture: any needle = FAIL (named). An
// empty transcript always passes; ExpectEmpty additionally requires it
// (production's RMS gate suppresses true silence before whisper — a
// non-empty transcript there means the gate broke).
func ScoreLeakTranscript(c *LeakCase, actual string) LeakResult {
	res := LeakResult{WAV: c.WAV, Actual: actual}
	if c.ExpectEmpty && actual != "" {
		res.FailReason = fmt.Sprintf("expected NO transcript (RMS-gated silence), got %q", actual)
		res.Leaks = FindLeaks(actual)
		return res
	}
	if leaks := FindLeaks(actual); len(leaks) > 0 {
		res.Leaks = leaks
		res.FailReason = "PROMPT LEAK: needles [" + strings.Join(leaks, ", ") + "] in output " + fmt.Sprintf("%q", actual)
		return res
	}
	res.Pass = true
	return res
}
