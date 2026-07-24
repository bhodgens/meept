package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPipeline records submitted proposals for test assertions.
type mockPipeline struct {
	mu        sync.Mutex
	proposals []selfimprove.SkillProposal
}

func (m *mockPipeline) SubmitProposal(_ context.Context, p selfimprove.SkillProposal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proposals = append(m.proposals, p)
	return nil
}

func TestFeedbackInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval int
		turns    int
		wantFire bool
	}{
		{"fires at interval", 3, 3, true},
		{"does not fire before interval", 3, 2, false},
		{"fires at second interval", 3, 6, true},
		{"interval of 1 fires every turn", 1, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline := &mockPipeline{}
			classified := make(chan struct{}, 10)
			classifyFn := func(_ context.Context, _ string, _ []string) (string, error) {
				classified <- struct{}{}
				return "NO_IMPROVEMENT", nil
			}

			trigger := NewInlineFeedbackTrigger(tt.interval, pipeline, classifyFn)
			trigger.SetActiveSkill("test-skill")

			for i := 0; i < tt.turns; i++ {
				trigger.PrepareNextTurn(context.Background(), TurnState{})
			}

			if tt.wantFire {
				select {
				case <-classified:
					// Classification was invoked as expected.
				case <-time.After(time.Second):
					t.Fatal("expected classification to fire but it did not")
				}
			} else {
				select {
				case <-classified:
					t.Fatal("classification fired unexpectedly")
				case <-time.After(50 * time.Millisecond):
					// No classification — correct.
				}
			}
		})
	}
}

func TestFeedbackNoSkill(t *testing.T) {
	pipeline := &mockPipeline{}
	classified := make(chan struct{}, 1)
	classifyFn := func(_ context.Context, _ string, _ []string) (string, error) {
		classified <- struct{}{}
		return "NO_IMPROVEMENT", nil
	}

	trigger := NewInlineFeedbackTrigger(1, pipeline, classifyFn)
	// No active skill set — should never fire.

	trigger.PrepareNextTurn(context.Background(), TurnState{})

	select {
	case <-classified:
		t.Fatal("classification fired with no active skill")
	case <-time.After(50 * time.Millisecond):
		// Correct: no classification without an active skill.
	}
}

func TestParseFeedbackResult(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		activeSkill string
		wantSkill   string
		wantChange  string
		wantOK      bool
	}{
		{
			name:        "full result",
			input:       "IMPROVEMENT\nSKILL: debugging\nCHANGE: Add retry logic for transient errors",
			activeSkill: "fallback",
			wantSkill:   "debugging",
			wantChange:  "Add retry logic for transient errors",
			wantOK:      true,
		},
		{
			name:        "uses active skill when SKILL missing",
			input:       "IMPROVEMENT\nCHANGE: Use structured logging",
			activeSkill: "logging",
			wantSkill:   "logging",
			wantChange:  "Use structured logging",
			wantOK:      true,
		},
		{
			name:        "missing CHANGE returns false",
			input:       "IMPROVEMENT\nSKILL: testing",
			activeSkill: "testing",
			wantOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proposal, ok := parseFeedbackResult(tt.input, tt.activeSkill)
			require.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantSkill, proposal.SkillName)
				assert.Equal(t, tt.wantChange, proposal.Change)
				assert.Equal(t, "inline-feedback", proposal.Source)
			}
		})
	}
}

func TestParseFeedbackNoImprovement(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"explicit NO_IMPROVEMENT", "NO_IMPROVEMENT"},
		{"empty string", ""},
		{"whitespace only", "   \n  "},
		{"unrelated text", "Everything looks fine"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := parseFeedbackResult(tt.input, "some-skill")
			assert.False(t, ok)
		})
	}
}

func TestFeedbackNonBlocking(t *testing.T) {
	pipeline := &mockPipeline{}
	blockCh := make(chan struct{})
	classifyFn := func(_ context.Context, _ string, _ []string) (string, error) {
		<-blockCh // Block until released.
		return "NO_IMPROVEMENT", nil
	}

	trigger := NewInlineFeedbackTrigger(1, pipeline, classifyFn)
	trigger.SetActiveSkill("test-skill")

	state := TurnState{
		Messages: []llm.ChatMessage{
			{Role: llm.RoleUser, Content: "hello"},
		},
	}

	// PrepareNextTurn must return immediately even though classifyFn blocks.
	done := make(chan TurnModification, 1)
	go func() {
		done <- trigger.PrepareNextTurn(context.Background(), state)
	}()

	select {
	case mod := <-done:
		assert.False(t, mod.Modified)
		assert.Empty(t, mod.ExtraMessages)
		assert.Empty(t, mod.ModelOverride)
		assert.False(t, mod.SkipTools)
		assert.Empty(t, mod.Reason)
	case <-time.After(time.Second):
		t.Fatal("PrepareNextTurn blocked — must be non-blocking")
	}

	close(blockCh) // Release the goroutine.
}
