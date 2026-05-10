package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// stubChatter implements llm.Chatter for testing. It returns a preset
// response or error from Chat calls, ignoring the input messages entirely.
type stubChatter struct {
	resp *llm.Response
	err  error
}

func (s *stubChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return s.resp, s.err
}

func (s *stubChatter) ChatWithProgress(_ context.Context, _ []llm.ChatMessage, _ llm.ProgressCallback, _ ...llm.ChatOption) (*llm.Response, error) {
	return s.resp, s.err
}

func (s *stubChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "stub"}
}

// makeMemories creates n MemoryResult entries with sequential IDs.
func makeMemories(n int, baseTime time.Time) []MemoryResult {
	results := make([]MemoryResult, n)
	for i := range n {
		results[i] = MemoryResult{
			Memory: Memory{
				ID:        fmt.Sprintf("mem-%d", i),
				Content:   "test content for memory " + fmt.Sprintf("mem-%d", i),
				CreatedAt: baseTime.Add(time.Duration(i) * time.Hour),
			},
		}
	}
	return results
}

func TestSummarizeWithLLM_Success(t *testing.T) {
	jsonResponse := `[{"topic":"testing","summary":"all test memories","ids":["mem-0","mem-1","mem-2"]}]`
	chatter := &stubChatter{
		resp: &llm.Response{Content: jsonResponse},
	}

	consolidator := NewConsolidator(ConsolidatorConfig{
		Logger: slog.Default(),
		LLM:    chatter,
	})

	memories := makeMemories(3, time.Now().Add(-48*time.Hour))
	summaries, err := consolidator.summarizeWithLLM(context.Background(), memories)

	if err != nil {
		t.Fatalf("summarizeWithLLM returned error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary group, got %d", len(summaries))
	}
	if summaries[0].Topic != "testing" {
		t.Errorf("expected topic %q, got %q", "testing", summaries[0].Topic)
	}
	if len(summaries[0].IDs) != 3 {
		t.Errorf("expected 3 IDs, got %d", len(summaries[0].IDs))
	}
}

func TestSummarizeWithLLM_FiltersInvalidIDs(t *testing.T) {
	// LLM returns one valid ID and one that does not exist in the input.
	jsonResponse := `[{"topic":"mixed","summary":"some memories","ids":["mem-0","nonexistent"]}]`
	chatter := &stubChatter{
		resp: &llm.Response{Content: jsonResponse},
	}

	consolidator := NewConsolidator(ConsolidatorConfig{
		Logger: slog.Default(),
		LLM:    chatter,
	})

	memories := makeMemories(1, time.Now().Add(-48*time.Hour))
	summaries, err := consolidator.summarizeWithLLM(context.Background(), memories)

	if err != nil {
		t.Fatalf("summarizeWithLLM returned error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if len(summaries[0].IDs) != 1 || summaries[0].IDs[0] != "mem-0" {
		t.Errorf("expected only [mem-0], got %v", summaries[0].IDs)
	}
}

func TestSummarizeWithLLM_DropsEmptyGroups(t *testing.T) {
	// LLM returns a group with only invalid IDs — it should be dropped entirely.
	jsonResponse := `[{"topic":"empty","summary":"no valid ids","ids":["bogus-1","bogus-2"]}]`
	chatter := &stubChatter{
		resp: &llm.Response{Content: jsonResponse},
	}

	consolidator := NewConsolidator(ConsolidatorConfig{
		Logger: slog.Default(),
		LLM:    chatter,
	})

	memories := makeMemories(2, time.Now().Add(-48*time.Hour))
	summaries, err := consolidator.summarizeWithLLM(context.Background(), memories)

	if err != nil {
		t.Fatalf("summarizeWithLLM returned error: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries (all invalid IDs), got %d", len(summaries))
	}
}

func TestSummarizeWithLLM_LLMError(t *testing.T) {
	chatter := &stubChatter{
		err: errors.New("LLM unavailable"),
	}

	consolidator := NewConsolidator(ConsolidatorConfig{
		Logger: slog.Default(),
		LLM:    chatter,
	})

	memories := makeMemories(3, time.Now().Add(-48*time.Hour))
	_, err := consolidator.summarizeWithLLM(context.Background(), memories)

	if err == nil {
		t.Fatal("expected error when LLM returns error, got nil")
	}
}

func TestSummarizeWithLLM_EmptyResponse(t *testing.T) {
	chatter := &stubChatter{
		resp: &llm.Response{Content: ""},
	}

	consolidator := NewConsolidator(ConsolidatorConfig{
		Logger: slog.Default(),
		LLM:    chatter,
	})

	memories := makeMemories(3, time.Now().Add(-48*time.Hour))
	_, err := consolidator.summarizeWithLLM(context.Background(), memories)

	if err == nil {
		t.Fatal("expected error when LLM returns empty content, got nil")
	}
}

func TestSummarizeWithLLM_UnparseableResponse(t *testing.T) {
	chatter := &stubChatter{
		resp: &llm.Response{Content: "this is not json at all"},
	}

	consolidator := NewConsolidator(ConsolidatorConfig{
		Logger: slog.Default(),
		LLM:    chatter,
	})

	memories := makeMemories(3, time.Now().Add(-48*time.Hour))
	_, err := consolidator.summarizeWithLLM(context.Background(), memories)

	if err == nil {
		t.Fatal("expected error when LLM returns unparseable content, got nil")
	}
}

func TestSummarizeWithLLM_MarkdownFencedResponse(t *testing.T) {
	fencedResponse := "```json\n[{\"topic\":\"code\",\"summary\":\"coding memories\",\"ids\":[\"mem-0\"]}]\n```"
	chatter := &stubChatter{
		resp: &llm.Response{Content: fencedResponse},
	}

	consolidator := NewConsolidator(ConsolidatorConfig{
		Logger: slog.Default(),
		LLM:    chatter,
	})

	memories := makeMemories(1, time.Now().Add(-48*time.Hour))
	summaries, err := consolidator.summarizeWithLLM(context.Background(), memories)

	if err != nil {
		t.Fatalf("summarizeWithLLM returned error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Topic != "code" {
		t.Errorf("expected topic %q, got %q", "code", summaries[0].Topic)
	}
}

func TestConsolidateEpisodic_FallbackWhenLLMNil(t *testing.T) {
	// Without an LLM, consolidateEpisodic should fall back to date-based
	// grouping. We cannot easily call consolidateEpisodic without a real
	// Manager, so we test the branching logic via the public MergeRelated
	// method instead.
	consolidator := NewConsolidator(ConsolidatorConfig{
		Logger: slog.Default(),
		// LLM is nil
	})

	// Use a fixed noon timestamp so all memories land on the same calendar date.
	baseTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	memories := makeMemories(3, baseTime)
	summaries, err := consolidator.MergeRelated(context.Background(), memories)

	if err != nil {
		t.Fatalf("MergeRelated returned error: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected at least one date-based summary, got 0")
	}
	// All three memories share the same CreatedAt date, so they should be in one group.
	if len(summaries) != 1 {
		t.Errorf("expected 1 date-based group, got %d", len(summaries))
	}
}

func TestConsolidateEpisodic_FallbackOnLLMError(t *testing.T) {
	chatter := &stubChatter{
		err: errors.New("LLM down"),
	}

	consolidator := NewConsolidator(ConsolidatorConfig{
		Logger: slog.Default(),
		LLM:    chatter,
	})

	memories := makeMemories(3, time.Now().Add(-48*time.Hour))
	summaries, err := consolidator.MergeRelated(context.Background(), memories)

	if err != nil {
		t.Fatalf("MergeRelated returned error: %v", err)
	}
	// Should fall back to date-based summarization.
	if len(summaries) == 0 {
		t.Fatal("expected fallback date-based summaries, got 0")
	}
}

func TestNewConsolidator_WithLLM(t *testing.T) {
	chatter := &stubChatter{}
	consolidator := NewConsolidator(ConsolidatorConfig{
		Logger: slog.Default(),
		LLM:    chatter,
	})
	if consolidator.llm == nil {
		t.Error("expected LLM to be set on Consolidator, got nil")
	}
}

func TestNewConsolidator_WithoutLLM(t *testing.T) {
	consolidator := NewConsolidator(ConsolidatorConfig{
		Logger: slog.Default(),
	})
	if consolidator.llm != nil {
		t.Error("expected LLM to be nil on Consolidator")
	}
}

func TestParseSummarizeResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
		wantErr bool
	}{
		{
			name:    "clean JSON array",
			input:   `[{"topic":"test","summary":"s","ids":["1"]}]`,
			wantLen: 1,
		},
		{
			name:    "fenced with json tag",
			input:   "```json\n[{\"topic\":\"test\",\"summary\":\"s\",\"ids\":[\"1\"]}]\n```",
			wantLen: 1,
		},
		{
			name:    "prose before fence",
			input:   "Here are the summaries:\n\n```json\n[{\"topic\":\"test\",\"summary\":\"s\",\"ids\":[\"1\"]}]\n```\n",
			wantLen: 1,
		},
		{
			name:    "prose before and after fence",
			input:   "Sure, here you go:\n```json\n[{\"topic\":\"test\",\"summary\":\"s\",\"ids\":[\"1\"]}]\n```\nHope that helps!",
			wantLen: 1,
		},
		{
			name:    "bare JSON in prose",
			input:   "The result is [{\"topic\":\"test\",\"summary\":\"s\",\"ids\":[\"1\"]}] as requested.",
			wantLen: 1,
		},
		{
			name:    "multiple summaries",
			input:   "```json\n[{\"topic\":\"a\",\"summary\":\"sa\",\"ids\":[\"1\"]},{\"topic\":\"b\",\"summary\":\"sb\",\"ids\":[\"2\"]}]\n```",
			wantLen: 2,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "no JSON at all",
			input:   "I couldn't summarize those memories.",
			wantErr: true,
		},
		{
			name:    "generic fence without language tag",
			input:   "```\n[{\"topic\":\"test\",\"summary\":\"s\",\"ids\":[\"1\"]}]\n```",
			wantLen: 1,
		},
		{
			name:    "fenced with trailing whitespace",
			input:   "```json\n[{\"topic\":\"test\",\"summary\":\"s\",\"ids\":[\"1\"]}]  \n```  ",
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSummarizeResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error for invalid input")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result) != tt.wantLen {
				t.Errorf("got %d summaries, want %d", len(result), tt.wantLen)
			}
		})
	}
}
