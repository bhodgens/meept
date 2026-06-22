package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
)

func TestEpistemicHookDisabledByDefault(t *testing.T) {
	hook := NewEpistemicHook(EpistemicHookConfig{
		Cfg: config.EpistemicConfig{}, // AmbientExtraction.Enabled = false
	})
	// Should return immediately with no action.
	written, err := hook.AfterTurn(context.Background(), "chat", []string{"hello"})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if len(written) != 0 {
		t.Errorf("expected 0 written, got %d", len(written))
	}
}

func TestEpistemicHookExcludesIntent(t *testing.T) {
	hook := NewEpistemicHook(EpistemicHookConfig{
		Cfg: config.EpistemicConfig{
			AmbientExtraction: config.AmbientExtractionConfig{
				Enabled:        true,
				ExcludeIntents: []string{"chat"},
			},
		},
		// Extractor is non-nil so we verify the intent filter is the gate.
		Extractor: &fakeAmbientExtractor{},
	})
	written, _ := hook.AfterTurn(context.Background(), "chat", []string{"hello"})
	if len(written) != 0 {
		t.Errorf("chat intent should be excluded, got %d written", len(written))
	}
}

func TestEpistemicHookNilExtractor(t *testing.T) {
	hook := NewEpistemicHook(EpistemicHookConfig{
		Cfg: config.EpistemicConfig{
			AmbientExtraction: config.AmbientExtractionConfig{Enabled: true},
		},
		Extractor: nil,
	})
	written, err := hook.AfterTurn(context.Background(), "research", []string{"hello"})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if len(written) != 0 {
		t.Errorf("nil extractor should yield 0 writes, got %d", len(written))
	}
}

func TestEpistemicHookHappyPath(t *testing.T) {
	fake := &fakeAmbientExtractor{
		candidates: []memory.AmbientCandidate{
			{Type: "claim", Text: "x", Confidence: 0.9},
			{Type: "claim", Text: "low conf", Confidence: 0.1}, // below threshold 0.7
			{Type: "claim", Text: "excluded", Category: "joke", Confidence: 0.95},
		},
		writtenIDs: []string{"id1"},
	}
	hook := NewEpistemicHook(EpistemicHookConfig{
		Cfg: config.EpistemicConfig{
			AmbientExtraction: config.AmbientExtractionConfig{
				Enabled:             true,
				ConfidenceThreshold: 0.7,
				MaxPerTurn:          3,
				ExcludeCategories:   []string{"joke"},
			},
		},
		Extractor: fake,
	})
	written, err := hook.AfterTurn(context.Background(), "research", []string{"hello"})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if len(written) != 1 || written[0] != "id1" {
		t.Errorf("expected [id1], got %v", written)
	}
	if len(fake.writeCalls) != 1 {
		t.Errorf("expected 1 WriteCandidates call, got %d", len(fake.writeCalls))
	}
	if len(fake.writeCalls[0]) != 1 {
		t.Errorf("expected 1 filtered candidate, got %d", len(fake.writeCalls[0]))
	}
}

func TestIntentExcluded(t *testing.T) {
	if !intentExcluded("chat", []string{"chat", "recall"}) {
		t.Error("chat should be excluded")
	}
	if intentExcluded("research", []string{"chat"}) {
		t.Error("research should not be excluded when list=[chat]")
	}
	if intentExcluded("chat", nil) {
		t.Error("nil exclude list should exclude nothing")
	}
}

type fakeAmbientExtractor struct {
	candidates []memory.AmbientCandidate
	writtenIDs []string
	writeCalls [][]memory.AmbientCandidate
}

func (f *fakeAmbientExtractor) Extract(ctx context.Context, messages []string) ([]memory.AmbientCandidate, error) {
	return f.candidates, nil
}

func (f *fakeAmbientExtractor) WriteCandidates(ctx context.Context, candidates []memory.AmbientCandidate) ([]string, error) {
	f.writeCalls = append(f.writeCalls, candidates)
	return f.writtenIDs, nil
}
