package agent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func newTestCollector(t *testing.T) (*ReflectionCollector, string) {
	t.Helper()
	queuePath := filepath.Join(t.TempDir(), "improvements.md")
	loader := newPlannerTemplateLoader() // empty tiers; will use fallbacks
	// Register fallbacks (mirrors NewDaemonPlannerTemplateLoader for tests)
	loader.fallbacks["reflection/turn.md"] = reflectionTurnFallbackBody
	loader.fallbacks["reflection/session.md"] = reflectionSessionFallbackBody
	rc := NewReflectionCollector(
		config.ReflectionCollectorConfig{
			Enabled:           true,
			AutoQueue:         true,
			TurnConfidenceMin: 0.6,
		},
		nil, // classifier — unused because classifierRunOnce is set
		"",  // classifierModel
		loader,
		queuePath,
		nil, // logger — NewReflectionCollector defaults to slog.Default()
	)
	return rc, queuePath
}

func TestReflectionCollector_ReflectTurn_DropsLowConfidence(t *testing.T) {
	rc, queuePath := newTestCollector(t)
	rc.classifierRunOnce = func(ctx context.Context, prompt, convID string) (string, error) {
		return `{"proposal":{"type":"skill_create","target":"x","change":"y","justification":"z","confidence":0.4}}`, nil
	}
	traj := ReflectionTrajectory{UserInput: "x", Outcome: "success"}
	if err := rc.ReflectTurn(context.Background(), traj); err != nil {
		t.Fatalf("ReflectTurn: %v", err)
	}
	pending, _ := newProposalQueue(queuePath).ListPending()
	if len(pending) != 0 {
		t.Errorf("low-confidence proposal was queued: %d", len(pending))
	}
}

func TestReflectionCollector_ReflectTurn_QueuesValidProposal(t *testing.T) {
	rc, queuePath := newTestCollector(t)
	rc.classifierRunOnce = func(ctx context.Context, prompt, convID string) (string, error) {
		return `{"proposal":{"type":"skill_create","target":".meept/skills/x/SKILL.md","change":"content","justification":"because","confidence":0.8}}`, nil
	}
	traj := ReflectionTrajectory{UserInput: "x", Outcome: "success"}
	if err := rc.ReflectTurn(context.Background(), traj); err != nil {
		t.Fatalf("ReflectTurn: %v", err)
	}
	pending, _ := newProposalQueue(queuePath).ListPending()
	if len(pending) != 1 {
		t.Errorf("pending = %d; want 1", len(pending))
	}
}

func TestReflectionCollector_ReflectTurn_NullProposal(t *testing.T) {
	rc, _ := newTestCollector(t)
	rc.classifierRunOnce = func(ctx context.Context, prompt, convID string) (string, error) {
		return `{"proposal": null}`, nil
	}
	if err := rc.ReflectTurn(context.Background(), ReflectionTrajectory{}); err != nil {
		t.Fatalf("ReflectTurn: %v", err)
	}
}

func TestReflectionCollector_ReflectTurn_Disabled(t *testing.T) {
	rc, queuePath := newTestCollector(t)
	rc.cfg.Enabled = false
	rc.classifierRunOnce = func(ctx context.Context, prompt, convID string) (string, error) {
		t.Error("classifier should not be called when disabled")
		return "", nil
	}
	if err := rc.ReflectTurn(context.Background(), ReflectionTrajectory{}); err != nil {
		t.Fatalf("ReflectTurn: %v", err)
	}
	pending, _ := newProposalQueue(queuePath).ListPending()
	if len(pending) != 0 {
		t.Errorf("pending = %d; want 0 when disabled", len(pending))
	}
}

func TestReflectionCollector_ReflectTurn_NoJSON(t *testing.T) {
	rc, _ := newTestCollector(t)
	rc.classifierRunOnce = func(ctx context.Context, prompt, convID string) (string, error) {
		return "I cannot extract a lesson from this turn.", nil
	}
	if err := rc.ReflectTurn(context.Background(), ReflectionTrajectory{}); err != nil {
		t.Fatalf("ReflectTurn: %v", err)
	}
}

func TestReflectionCollector_IsAlwaysProposeOnly_Exported(t *testing.T) {
	if !IsAlwaysProposeOnly("CLAUDE.md") {
		t.Error("CLAUDE.md should be always propose-only")
	}
	if IsAlwaysProposeOnly(".meept/skills/x/SKILL.md") {
		t.Error("skill path should not be always propose-only")
	}
}

func TestReflectionCollector_ReflectInactiveSessions_Stub(t *testing.T) {
	rc, _ := newTestCollector(t)
	// Should not panic and should return immediately.
	rc.ReflectInactiveSessions(context.Background())
}
