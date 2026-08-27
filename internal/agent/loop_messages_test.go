package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

// mockMessageDrainer provides canned unread messages for injection tests.
type mockMessageDrainer struct {
	drained    []IncomingAgentMessage
	drainCount int
}

func (m *mockMessageDrainer) DrainInbox(to string, limit int) ([]IncomingAgentMessage, error) {
	m.drainCount++
	out := m.drained
	m.drained = nil
	return out, nil
}

func TestBuildMessagesSection_InjectsOncePerTurn(t *testing.T) {
	l := NewAgentLoop("sess", "/tmp", WithAgentID("coder"))
	mock := &mockMessageDrainer{}
	l.SetMessageDrainer(mock)
	now := time.Now().UTC()
	mock.drained = []IncomingAgentMessage{
		{ID: "msg-1", From: "orchestrator", To: "coder", Body: "check the flaky test"},
		{ID: "msg-2", From: "planner", To: "coder", Body: "priority change", CreatedAt: now},
	}
	section := l.buildAgentMessagesSection(context.Background())
	if section == "" {
		t.Fatal("expected a non-empty messages section")
	}
	if !strings.Contains(section, "[message from orchestrator]") ||
		!strings.Contains(section, "[message from planner]") {
		t.Fatalf("section missing sender anchors:\n%s", section)
	}
	for _, id := range []string{"msg-1", "msg-2"} {
		if !strings.Contains(section, id) {
			t.Fatalf("section missing message id %s for reply targeting:\n%s", id, section)
		}
	}

	// Second turn: drainer returned nothing (messages were drained and
	// marked delivered on the first call), so no re-injection.
	second := l.buildAgentMessagesSection(context.Background())
	if second != "" {
		t.Fatalf("second turn must not re-inject; got:\n%s", second)
	}
	if mock.drainCount != 2 {
		t.Fatalf("drain calls = %d; want 2", mock.drainCount)
	}
}

func TestBuildMessagesSection_NoDrainer(t *testing.T) {
	l := NewAgentLoop("sess", "/tmp", WithAgentID("coder"))
	if got := l.buildAgentMessagesSection(context.Background()); got != "" {
		t.Fatalf("expected empty section with no drainer; got %q", got)
	}
}

func TestWiredMessagesSectionAppearsInSystemPrompt(t *testing.T) {
	l := NewAgentLoop("sess", "/tmp", WithAgentID("coder"))
	mock := &mockMessageDrainer{drained: []IncomingAgentMessage{
		{ID: "msg-x", From: "auditor", To: "coder", Body: "hello"},
	}}
	l.SetMessageDrainer(mock)
	discovered := l.buildSystemPromptWithSkills(context.Background(), nil)
	if !strings.Contains(discovered, "[message from auditor]") || !strings.Contains(discovered, "msg-x") {
		t.Fatalf("system prompt missing injected message block:\n%s", discovered)
	}
}
