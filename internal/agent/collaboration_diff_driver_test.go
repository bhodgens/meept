package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDifferentialDriver_Name(t *testing.T) {
	d := NewDifferentialDriver(DifferentialDriverDeps{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	if d.Name() != "differential" {
		t.Errorf("Name() = %q, want differential", d.Name())
	}
}

func TestDifferentialDriver_CanInitiate(t *testing.T) {
	d := NewDifferentialDriver(DifferentialDriverDeps{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	if !d.CanInitiate("coder", "test") {
		t.Error("CanInitiate(coder) should be true")
	}
	if !d.CanInitiate("planner", "test") {
		t.Error("CanInitiate(planner) should be true")
	}
	if d.CanInitiate("chat", "test") {
		t.Error("CanInitiate(chat) should be false")
	}
}

func TestValidateCheckpointResult_Fallbacks(t *testing.T) {
	r1 := &ValidateCheckpointResult{AnyOK: true, BranchAConverged: true, BranchBConverged: false}
	if r1.AnyOK != true {
		t.Error("AnyOK should be true when A converged")
	}

	r2 := &ValidateCheckpointResult{AnyOK: true, BranchAConverged: false, BranchBConverged: true}
	if r2.AnyOK != true {
		t.Error("AnyOK should be true when B converged")
	}

	r3 := &ValidateCheckpointResult{AnyOK: false, BranchAConverged: false, BranchBConverged: false}
	if r3.AnyOK {
		t.Error("AnyOK should be false when both failed")
	}
}

func TestDifferentialDriver_buildDifferentiatorPrompt(t *testing.T) {
	d := NewDifferentialDriver(DifferentialDriverDeps{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	sess := NewCollaborationSession("differential", "task-42", []string{"agent-a", "agent-b"}, DefaultSessionConfig())

	prompt := d.buildDifferentiatorPrompt(sess, true, false)
	if prompt == "" {
		t.Error("prompt should not be empty")
	}
	if !strings.Contains(prompt, "CONVERGED") {
		t.Error("prompt should mention CONVERGED")
	}

	prompt2 := d.buildDifferentiatorPrompt(sess, false, true)
	if !strings.Contains(prompt2, "FAILED") {
		t.Error("prompt should mention FAILED for branch A")
	}
}

func TestDifferentialDriver_phaseFork(t *testing.T) {
	d := NewDifferentialDriver(DifferentialDriverDeps{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	sess := NewCollaborationSession("differential", "task-42", []string{"agent-a", "agent-b"}, DefaultSessionConfig())

	ctx := t.Context()
	err := d.phaseFork(ctx, sess)
	if err != nil {
		t.Fatalf("phaseFork failed: %v", err)
	}
	if sess.Workspace == "" {
		t.Error("workspace should be set after fork")
	}

	expectedDirs := []string{"branch-a", "branch-b", "combined", "meta"}
	for _, dir := range expectedDirs {
		path := filepath.Join(sess.Workspace, dir)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected directory %s to exist", dir)
		}
	}

	planPath := filepath.Join(sess.Workspace, "meta", "plan.md")
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		t.Error("expected plan.md to exist")
	}

	os.RemoveAll(sess.Workspace)
}
