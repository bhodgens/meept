package skills

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// capturingChatter is a test double for llm.Chatter that records the
// messages it receives and returns a canned response.
type capturingChatter struct {
	messages []llm.ChatMessage
	response *llm.Response
}

func (c *capturingChatter) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.messages = messages
	return c.response, nil
}

func (c *capturingChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *capturingChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{
		ModelID:    "model-b",
		ProviderID: "provider1",
	}
}

func newCapturingChatter() *capturingChatter {
	return &capturingChatter{
		response: &llm.Response{
			Content: "ok",
			Model:   "provider1/model-b",
			Usage:   llm.TokenUsage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
		},
	}
}

// TestExecutor_InjectsSkillDir verifies that a directory-layout skill's
// execution input contains the skill_dir context line and has SKILL_DIR
// literals substituted, while flat skills are untouched.
func TestExecutor_InjectsSkillDir(t *testing.T) {
	skillDir := t.TempDir()
	body := "run python3 $SKILL_DIR/scripts/x.py"

	t.Run("Execute", func(t *testing.T) {
		mock := newCapturingChatter()
		exec := NewExecutor(testResolver(), WithClient(mock))

		skill := &Skill{
			Name:     "linked",
			Requires: []string{"code"},
			Dir:      skillDir,
			Body:     body,
		}

		if _, err := exec.Execute(context.Background(), skill, "go"); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if len(mock.messages) == 0 {
			t.Fatal("no messages captured")
		}

		system := mock.messages[0]
		if system.Role != llm.RoleSystem {
			t.Fatalf("first message role = %q, want system", system.Role)
		}
		if !strings.Contains(system.Content, "skill_dir: "+skillDir) {
			t.Errorf("system message missing skill_dir line, got: %q", system.Content)
		}
		if !strings.Contains(system.Content, skillDir+"/scripts/x.py") {
			t.Errorf("SKILL_DIR not substituted in system message, got: %q", system.Content)
		}
		if strings.Contains(system.Content, "SKILL_DIR") {
			t.Errorf("literal SKILL_DIR still present in system message: %q", system.Content)
		}
	})

	t.Run("ExecuteWithMessages", func(t *testing.T) {
		mock := newCapturingChatter()
		exec := NewExecutor(testResolver(), WithClient(mock))

		skill := &Skill{
			Name:     "linked",
			Requires: []string{"code"},
			Dir:      skillDir,
			Body:     body,
		}

		msgs := []llm.ChatMessage{{Role: llm.RoleUser, Content: "go"}}
		if _, err := exec.ExecuteWithMessages(context.Background(), skill, msgs); err != nil {
			t.Fatalf("ExecuteWithMessages failed: %v", err)
		}
		if len(mock.messages) == 0 {
			t.Fatal("no messages captured")
		}

		system := mock.messages[0]
		if system.Role != llm.RoleSystem {
			t.Fatalf("first message role = %q, want system", system.Role)
		}
		if !strings.Contains(system.Content, "skill_dir: "+skillDir) {
			t.Errorf("system message missing skill_dir line, got: %q", system.Content)
		}
		if !strings.Contains(system.Content, skillDir+"/scripts/x.py") {
			t.Errorf("SKILL_DIR not substituted in system message, got: %q", system.Content)
		}
		if strings.Contains(system.Content, "SKILL_DIR") {
			t.Errorf("literal SKILL_DIR still present in system message: %q", system.Content)
		}
	})

	t.Run("FlatSkillUntouched", func(t *testing.T) {
		mock := newCapturingChatter()
		exec := NewExecutor(testResolver(), WithClient(mock))

		skill := &Skill{
			Name:     "flat",
			Requires: []string{"code"},
			Dir:      "",
			Body:     body,
		}

		if _, err := exec.Execute(context.Background(), skill, "go"); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if len(mock.messages) == 0 {
			t.Fatal("no messages captured")
		}

		system := mock.messages[0]
		if strings.Contains(system.Content, "skill_dir:") {
			t.Errorf("flat skill must not get a skill_dir line, got: %q", system.Content)
		}
		if !strings.Contains(system.Content, "SKILL_DIR") {
			t.Errorf("flat skill body must keep the SKILL_DIR literal, got: %q", system.Content)
		}
	})
}
