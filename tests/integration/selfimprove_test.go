package integration

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/selfimprove"
)

// TestController_Detect_Integration verifies that the controller can run
// detection against a real (temporary) project directory.
func TestController_Detect_Integration(t *testing.T) {
	projectDir := t.TempDir()

	// Create a small Go file with a TODO that the detector should find.
	src := filepath.Join(projectDir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n\nfunc main() {\n\t// TODO: fix this\n}\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	cfg := selfimprove.DefaultConfig()
	cfg.DataPath = filepath.Join(t.TempDir(), "si-data")
	cfg.Validate()

	ctrl := selfimprove.NewController(cfg, nil, nil, projectDir, slog.New(slog.NewTextHandler(io.Discard, nil)))

	issues, err := ctrl.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	// The detector scans for various patterns; we just verify it runs without
	// error and returns a slice (may be empty depending on detector logic).
	t.Logf("detected %d issues", len(issues))
}

// TestController_StatusRoundTrip verifies Initialize -> GetStatus -> Stop
// lifecycle works correctly.
func TestController_StatusRoundTrip(t *testing.T) {
	projectDir := t.TempDir()

	cfg := selfimprove.DefaultConfig()
	cfg.DataPath = filepath.Join(t.TempDir(), "si-data")
	cfg.Validate()

	ctrl := selfimprove.NewController(cfg, nil, nil, projectDir, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := ctrl.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	status := ctrl.GetStatus()
	if status == nil {
		t.Fatal("GetStatus returned nil")
	}
	if status.CyclesCompleted != 0 {
		t.Errorf("CyclesCompleted = %d, want 0", status.CyclesCompleted)
	}

	if err := ctrl.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestScheduler_StopVerifiesIdempotent verifies that the scheduler's Stop
// method is safe to call multiple times.
func TestScheduler_StopVerifiesIdempotent(t *testing.T) {
	projectDir := t.TempDir()

	cfg := selfimprove.DefaultConfig()
	cfg.DataPath = filepath.Join(t.TempDir(), "si-data")
	cfg.Validate()

	ctrl := selfimprove.NewController(cfg, nil, nil, projectDir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	sched := selfimprove.NewScheduler(ctrl, 1*time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Stop before start should be safe.
	sched.Stop()
	// Second stop should also be safe.
	sched.Stop()
}

// TestScheduler_StartStop verifies the scheduler starts and stops cleanly.
func TestScheduler_StartStop(t *testing.T) {
	projectDir := t.TempDir()

	cfg := selfimprove.DefaultConfig()
	cfg.DataPath = filepath.Join(t.TempDir(), "si-data")
	cfg.Validate()

	ctrl := selfimprove.NewController(cfg, nil, nil, projectDir, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Use a very short interval so we can observe at least one tick in tests.
	sched := selfimprove.NewScheduler(ctrl, 50*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		sched.Start(ctx)
		close(done)
	}()

	// Let it run briefly then stop.
	time.Sleep(200 * time.Millisecond)
	sched.Stop()

	select {
	case <-done:
		// OK: scheduler exited
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not exit after Stop")
	}
}

// TestController_ProgressCallback verifies that the progress callback is
// invoked during a cycle.
func TestController_ProgressCallback(t *testing.T) {
	projectDir := t.TempDir()

	cfg := selfimprove.DefaultConfig()
	cfg.DataPath = filepath.Join(t.TempDir(), "si-data")
	cfg.Validate()

	ctrl := selfimprove.NewController(cfg, nil, nil, projectDir, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var phases []string
	var mu sync.Mutex
	ctrl.SetProgressCallback(func(phase string, progress float64, message string) {
		mu.Lock()
		phases = append(phases, phase)
		mu.Unlock()
	})

	// RunFullCycle will invoke the callback at each phase even if there are no
	// issues (the "completed" phase will be emitted).
	_, err := ctrl.RunFullCycle(context.Background(), false)
	if err != nil {
		t.Fatalf("RunFullCycle: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(phases) == 0 {
		t.Fatal("expected at least one progress callback invocation, got none")
	}

	// Must at least have "started" and "completed".
	foundStart := false
	foundComplete := false
	for _, p := range phases {
		if p == "started" {
			foundStart = true
		}
		if p == "completed" {
			foundComplete = true
		}
	}
	if !foundStart {
		t.Error("expected 'started' phase in progress callbacks")
	}
	if !foundComplete {
		t.Error("expected 'completed' phase in progress callbacks")
	}
}

// TestController_BusIntegration verifies that status updates are published on
// the message bus during a cycle.
func TestController_BusIntegration(t *testing.T) {
	projectDir := t.TempDir()

	msgBus := bus.New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer msgBus.Close()

	cfg := selfimprove.DefaultConfig()
	cfg.DataPath = filepath.Join(t.TempDir(), "si-data")
	cfg.Validate()

	ctrl := selfimprove.NewController(cfg, msgBus, nil, projectDir, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Subscribe to the status topic.
	sub := msgBus.Subscribe("test-si", "selfimprove.status")
	defer msgBus.Unsubscribe(sub)

	_, err := ctrl.RunFullCycle(context.Background(), false)
	if err != nil {
		t.Fatalf("RunFullCycle: %v", err)
	}

	// Collect messages with a timeout.
	timeout := time.After(2 * time.Second)
	var received int
	for {
		select {
		case <-sub.Channel:
			received++
		case <-timeout:
			goto done
		}
	}
done:

	if received == 0 {
		t.Fatal("expected at least one bus message from selfimprove.status")
	}
	t.Logf("received %d bus messages", received)
}

// TestApplier_ApproveReject_Integration verifies the approval workflow
// end-to-end with a pending fix.
func TestApplier_ApproveReject_Integration(t *testing.T) {
	projectDir := t.TempDir()

	// Create the file we'll modify.
	targetFile := filepath.Join(projectDir, "hello.go")
	original := []byte("package main\n\nfunc hello() string {\n\treturn \"hello\"\n}\n")
	if err := os.WriteFile(targetFile, original, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg := selfimprove.DefaultConfig()
	cfg.DataPath = filepath.Join(t.TempDir(), "si-data")
	cfg.Safety.RequireHumanApproval = true
	cfg.Safety.AutoApplyLowRisk = false
	cfg.Validate()

	ctrl := selfimprove.NewController(cfg, nil, nil, projectDir, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Manually create a pending fix via the applier.
	fix := &selfimprove.ProposedFix{
		ID:          "fix-test-1",
		IssueID:     "issue-1",
		Type:        selfimprove.FixTypeCodeChange,
		Description: "fix hello",
		Diff:        "<<<<<<< ORIGINAL\nreturn \"hello\"\n=======\nreturn \"world\"\n>>>>>>> FIXED",
		FilePath:    "hello.go",
		Risk:        "low",
	}
	validation := &selfimprove.ValidationResult{
		FixID:   "fix-test-1",
		Success: true,
		Status:  selfimprove.ValidationPassed,
	}

	// Apply should return ErrApprovalRequired since RequireHumanApproval is true.
	_, err := ctrl.GetApplier().Apply(context.Background(), fix, validation, "auto")
	if err == nil {
		t.Fatal("expected ErrApprovalRequired, got nil")
	}
	if err.Error() != "fix requires human approval" {
		t.Fatalf("unexpected error: %v", err)
	}

	// Reject it.
	if err := ctrl.RejectFix("fix-test-1", "test rejection"); err != nil {
		t.Fatalf("RejectFix: %v", err)
	}
}

// sync is needed for TestController_ProgressCallback.
// (import moved to top of file)
