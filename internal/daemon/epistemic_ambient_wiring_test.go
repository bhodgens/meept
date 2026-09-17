package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// recordingChatter captures the ChatOption slice each call receives.
type recordingChatter struct {
	opts   []llm.ChatOption
	called int
}

func (c *recordingChatter) Chat(_ context.Context, _ []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	c.called++
	c.opts = opts
	return &llm.Response{Content: "[]"}, nil
}

func (c *recordingChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *recordingChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "recording"}
}

// TestAmbientClassifierAdapter_InjectsGrammar pins the Part B wiring: the
// ambient extraction call must carry llm.WithRawGrammar with the bare-array
// candidate grammar so llama.cpp-style endpoints constrain output to the shape
// memory.ParseAmbientCandidates accepts.
func TestAmbientClassifierAdapter_InjectsGrammar(t *testing.T) {
	chatter := &recordingChatter{}
	adapter := newAmbientClassifierAdapter(chatter)
	if adapter == nil {
		t.Fatal("adapter nil for non-nil chatter")
	}
	if _, err := adapter.ExtractCandidates(context.Background(), "prompt"); err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}
	if chatter.called != 1 {
		t.Fatalf("called %d times, want 1", chatter.called)
	}

	// The option slice must include a WithRawGrammar option carrying the
	// ambient candidate grammar (asserted via llm.RawGrammarOf, the test
	// seam for callers that stub the Chatter), and the temperature option.
	if got := llm.RawGrammarOf(chatter.opts); got != llm.AmbientCandidateGrammar() {
		t.Errorf("RawGrammarOf(opts) = %q, want AmbientCandidateGrammar()", got)
	}

	// Grammar must force a bare JSON array: root opens with a "[" literal,
	// and no object-wrapper key appears anywhere in the grammar body.
	if !strings.HasPrefix(llm.AmbientCandidateGrammar(), "root ::= \"[\"") {
		t.Error("grammar root does not force a bare JSON array")
	}
	for _, banned := range []string{"candidates", "results", "items"} {
		if strings.Contains(llm.AmbientCandidateGrammar(), banned) {
			t.Errorf("grammar references wrapper key %q", banned)
		}
	}
}

// TestAmbientClassifierAdapter_NilChatterNil guards the constructor.
func TestAmbientClassifierAdapter_NilChatterNil(t *testing.T) {
	if newAmbientClassifierAdapter(nil) != nil {
		t.Fatal("expected nil adapter for nil chatter")
	}
}
