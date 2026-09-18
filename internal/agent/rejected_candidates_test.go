package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
)

// fakeExtractor records calls and returns canned candidates.
type fakeExtractorRej struct {
	cands []memory.AmbientCandidate
	wrote []memory.AmbientCandidate
}

func (f *fakeExtractorRej) Extract(ctx context.Context, messages []string) ([]memory.AmbientCandidate, error) {
	return f.cands, nil
}
func (f *fakeExtractorRej) WriteCandidates(ctx context.Context, candidates []memory.AmbientCandidate) ([]string, error) {
	f.wrote = candidates
	ids := make([]string, len(candidates))
	for i := range candidates {
		ids[i] = "claim-x"
	}
	return ids, nil
}

func TestSplitFiltered_ReasonsAndOrder(t *testing.T) {
	in := []memory.AmbientCandidate{
		{Text: "passes 1", Confidence: 0.8},
		{Text: "passes 2", Confidence: 0.85},
		{Text: "low conf decoy", Confidence: 0.2},
		{Text: "excluded cat", Confidence: 0.8, Category: "opinion"},
		{Text: "over max", Confidence: 0.95},
	}
	excluded := map[string]struct{}{"opinion": {}}
	passed, rejected := splitFiltered(in, 0.7, excluded, 2)
	if len(passed) != 2 || passed[0].Text != "passes 1" || passed[1].Text != "passes 2" {
		t.Fatalf("passed = %v", passed)
	}
	wantReasons := map[string]string{
		"low conf decoy": "confidence",
		"excluded cat":   "category",
		"over max":       "max_per_turn",
	}
	if len(rejected) != len(wantReasons) {
		t.Fatalf("rejected = %d, want %d", len(rejected), len(wantReasons))
	}
	for _, r := range rejected {
		want, ok := wantReasons[r.Text]
		if !ok {
			t.Errorf("unexpected rejected candidate %q", r.Text)
			continue
		}
		if r.Reason != want {
			t.Errorf("%s: reason = %q, want %q", r.Text, r.Reason, want)
		}
	}
}

// TestRejectedCandidateLogger_Append pins the JSONL calibration-log contract:
// every gated-out candidate lands as one JSON line with its reason.
func TestRejectedCandidateLogger_Append(t *testing.T) {
	dir := t.TempDir()
	l := NewRejectedCandidateLogger(dir, nil)
	if l == nil {
		t.Fatal("logger must not be nil for a non-empty dir")
	}
	recs := []rejectedCandidateRecord{
		{Text: "joke extracted as claim", Type: "claim", Confidence: 0.1, Reason: "confidence"},
		{Text: "quoted opinion", Type: "claim", Confidence: 0.5, Reason: "category"},
	}
	l.LogRejected("chat", 0.7, recs)
	// second batch appends (not truncates)
	l.LogRejected("chat", 0.7, []rejectedCandidateRecord{
		{Text: "over max", Type: "claim", Confidence: 0.95, Reason: "max_per_turn"},
	})

	data, err := os.ReadFile(filepath.Join(dir, "rejected_candidates.jsonl"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3:\n%s", len(lines), data)
	}
	var rec rejectedCandidateRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec.Reason != "confidence" || rec.Confidence != 0.1 || rec.TS == "" {
		t.Errorf("line0 = %+v", rec)
	}
}

// TestEpistemicHook_RejectedLogged: the hook logs gate rejections while still
// writing the passing candidates.
func TestEpistemicHook_RejectedLogged(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeExtractorRej{cands: []memory.AmbientCandidate{
		{Text: "real claim", Confidence: 0.9, Type: "claim"},
		{Text: "junk", Confidence: 0.1, Type: "claim"},
	}}
	cfg := config.EpistemicConfig{
		AmbientExtraction: config.AmbientExtractionConfig{
			Enabled:             true,
			ConfidenceThreshold: 0.7,
			MaxPerTurn:          3,
		},
	}
	hook := NewEpistemicHook(EpistemicHookConfig{
		Cfg:            cfg,
		Extractor:      fake,
		RejectedLogger: NewRejectedCandidateLogger(dir, nil),
	})
	ids, err := hook.AfterTurn(context.Background(), "chat", []string{"user: hello"})
	if err != nil {
		t.Fatalf("AfterTurn: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("ids = %v, want 1 written claim", ids)
	}
	data, err := os.ReadFile(filepath.Join(dir, "rejected_candidates.jsonl"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), `"junk"`) || !strings.Contains(string(data), `"confidence"`) {
		t.Errorf("rejected log missing junk/confidence:\n%s", data)
	}
}
