package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// recordingDistillChatter captures the ChatOption slice each call receives.
type recordingDistillChatter struct {
	opts   []llm.ChatOption
	called int
}

func (c *recordingDistillChatter) Chat(_ context.Context, _ []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	c.called++
	c.opts = opts
	return &llm.Response{Content: `{"principle":"p","because":"b","evidence_ids":[]}`}, nil
}

func (c *recordingDistillChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *recordingDistillChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "recording"}
}

// TestLLMDistillSummarizer_InjectsGrammar pins the distill wiring: the
// SummarizeForDistill call must carry llm.WithRawGrammar with the lesson
// grammar so llama.cpp-style local endpoints constrain output to the shape
// DecodeLesson accepts. The attach itself is endpoint-gated inside the client
// (attachRawGrammar / isLocalEndpoint), so cloud chatters never see the field.
func TestLLMDistillSummarizer_InjectsGrammar(t *testing.T) {
	chatter := &recordingDistillChatter{}
	s := &llmDistillSummarizer{client: chatter}
	_, err := s.SummarizeForDistill(context.Background(), DomainLesson, []Memory{
		{Category: "ops", Content: "always check replica lag before deploys"},
	})
	if err != nil {
		t.Fatalf("SummarizeForDistill: %v", err)
	}
	if chatter.called != 1 {
		t.Fatalf("called %d times, want 1", chatter.called)
	}
	if got := llm.RawGrammarOf(chatter.opts); got != llm.LessonGrammar() {
		t.Errorf("RawGrammarOf(opts) = %q, want LessonGrammar()", got)
	}

	// The grammar must force a single bare JSON object — not the ambient
	// bare-array shape — and must not wrap the lesson in an outer key.
	g := llm.LessonGrammar()
	if !strings.Contains(g, `root ::= "{"`) {
		t.Error("lesson grammar does not force a single JSON object")
	}
	if !strings.Contains(g, "principle") || !strings.Contains(g, "evidence_ids") {
		t.Error("lesson grammar missing required member rules")
	}
	for _, banned := range []string{"root ::= \"[\"", "lessons", "result", "wrapper"} {
		if strings.Contains(g, banned) {
			t.Errorf("grammar references banned token %q", banned)
		}
	}
}
