package agent

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

func TestNewWatchdog(t *testing.T) {
	cfg := config.WatchdogConfig{
		Enabled:              true,
		TimeoutMinutes:       5,
		HeartbeatIntervalSec: 10,
		MaxIterations:        30,
		StuckIterationCount:  3,
	}

	w := NewWatchdog(cfg, nil)
	if w == nil {
		t.Fatal("expected non-nil watchdog")
	}
	if w.ActiveWorkerCount() != 0 {
		t.Errorf("expected 0 active workers, got %d", w.ActiveWorkerCount())
	}
}

func TestWatchdog_RegisterUnregisterWorker(t *testing.T) {
	cfg := config.WatchdogConfig{Enabled: true, TimeoutMinutes: 10}
	w := NewWatchdog(cfg, nil)

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.RegisterWorker("w1", "task1", "step1", cancel)
	if w.ActiveWorkerCount() != 1 {
		t.Errorf("expected 1 active worker, got %d", w.ActiveWorkerCount())
	}

	state, ok := w.GetWorkerState("w1")
	if !ok {
		t.Fatal("expected worker state to exist")
	}
	if state.WorkerID != "w1" {
		t.Errorf("expected worker ID w1, got %s", state.WorkerID)
	}
	if state.TaskID != "task1" {
		t.Errorf("expected task ID task1, got %s", state.TaskID)
	}
	if state.StepID != "step1" {
		t.Errorf("expected step ID step1, got %s", state.StepID)
	}

	w.UnregisterWorker("w1")
	if w.ActiveWorkerCount() != 0 {
		t.Errorf("expected 0 active workers after unregister, got %d", w.ActiveWorkerCount())
	}

	_, ok = w.GetWorkerState("w1")
	if ok {
		t.Error("expected worker state to be gone after unregister")
	}
}

func TestWatchdog_UpdateHeartbeat(t *testing.T) {
	cfg := config.WatchdogConfig{Enabled: true, TimeoutMinutes: 10}
	w := NewWatchdog(cfg, nil)

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.RegisterWorker("w1", "task1", "step1", cancel)

	// Update heartbeat
	w.UpdateHeartbeat("w1", 5, StageExecuting)

	state, ok := w.GetWorkerState("w1")
	if !ok {
		t.Fatal("expected worker state")
	}
	if state.Iteration != 5 {
		t.Errorf("expected iteration 5, got %d", state.Iteration)
	}
	if state.Stage != StageExecuting {
		t.Errorf("expected stage executing, got %s", state.Stage)
	}

	// Update non-existent worker should not panic
	w.UpdateHeartbeat("nonexistent", 1, StageThinking)
}

func TestWatchdog_TimeoutDetection(t *testing.T) {
	cfg := config.WatchdogConfig{
		Enabled:              true,
		TimeoutMinutes:       0, // Will use 1ns effectively via direct check
		HeartbeatIntervalSec: 1,
		MaxIterations:        50,
		StuckIterationCount:  5,
	}
	w := NewWatchdog(cfg, nil)

	_, cancel := context.WithCancel(context.Background())

	// Register a worker
	w.RegisterWorker("w1", "task1", "step1", cancel)

	// Manually set start time to past to simulate timeout
	w.mu.Lock()
	state := w.workers["w1"]
	state.StartTime = time.Now().Add(-11 * time.Minute) // Exceeds 10 min default
	w.mu.Unlock()

	// Run check
	w.checkWorkers()

	// Worker should be removed due to timeout
	if w.ActiveWorkerCount() != 0 {
		t.Errorf("expected 0 active workers after timeout, got %d", w.ActiveWorkerCount())
	}

	// Check alert was sent
	select {
	case alert := <-w.Alerts():
		if alert.Type != AlertTimeout {
			t.Errorf("expected timeout alert, got %s", alert.Type)
		}
		if alert.WorkerID != "w1" {
			t.Errorf("expected worker w1, got %s", alert.WorkerID)
		}
	case <-time.After(time.Second):
		t.Error("expected alert but none received")
	}
}

func TestWatchdog_MaxIterationsDetection(t *testing.T) {
	cfg := config.WatchdogConfig{
		Enabled:              true,
		TimeoutMinutes:       10,
		HeartbeatIntervalSec: 1,
		MaxIterations:        5,
		StuckIterationCount:  10,
	}
	w := NewWatchdog(cfg, nil)

	_, cancel := context.WithCancel(context.Background())

	w.RegisterWorker("w1", "task1", "step1", cancel)

	// Set iteration past max
	w.mu.Lock()
	state := w.workers["w1"]
	state.Iteration = 6 // Exceeds MaxIterations=5
	w.mu.Unlock()

	w.checkWorkers()

	if w.ActiveWorkerCount() != 0 {
		t.Errorf("expected 0 workers after max iterations, got %d", w.ActiveWorkerCount())
	}

	select {
	case alert := <-w.Alerts():
		if alert.Type != AlertMaxIter {
			t.Errorf("expected max iterations alert, got %s", alert.Type)
		}
	case <-time.After(time.Second):
		t.Error("expected alert")
	}
}

func TestWatchdog_CaptureReport(t *testing.T) {
	cfg := config.WatchdogConfig{Enabled: true}
	w := NewWatchdog(cfg, nil)

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.RegisterWorker("w1", "task1", "step1", cancel)
	w.UpdateHeartbeat("w1", 3, StageValidating)

	// CaptureReport called from outside checkWorkers uses its own locking
	report := w.CaptureReport("w1", "partial output here")
	if report == nil {
		t.Fatal("expected report")
	}
	if report.WorkerID != "w1" {
		t.Errorf("expected worker w1, got %s", report.WorkerID)
	}
	if report.TaskID != "task1" {
		t.Errorf("expected task task1, got %s", report.TaskID)
	}
	if report.Iterations != 3 {
		t.Errorf("expected 3 iterations, got %d", report.Iterations)
	}
	if report.Stage != StageValidating {
		t.Errorf("expected validating stage, got %s", report.Stage)
	}
	if report.PartialResult != "partial output here" {
		t.Errorf("expected partial result, got %s", report.PartialResult)
	}

	// Check report was sent to channel
	select {
	case r := <-w.Reports():
		if r.WorkerID != "w1" {
			t.Errorf("expected report for w1, got %s", r.WorkerID)
		}
	case <-time.After(time.Second):
		t.Error("expected report in channel")
	}

	// Capture for non-existent worker
	nonExistent := w.CaptureReport("nonexistent", "")
	if nonExistent != nil {
		t.Error("expected nil for non-existent worker")
	}
}

func TestWatchdog_StartStop(t *testing.T) {
	cfg := config.WatchdogConfig{
		Enabled:              true,
		TimeoutMinutes:       10,
		HeartbeatIntervalSec: 1,
	}
	w := NewWatchdog(cfg, nil)

	w.Start(context.Background())

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	w.Stop()

	if w.ActiveWorkerCount() != 0 {
		t.Errorf("expected 0 workers, got %d", w.ActiveWorkerCount())
	}
}

func TestWatchdog_Disabled(t *testing.T) {
	cfg := config.WatchdogConfig{Enabled: false}
	w := NewWatchdog(cfg, nil)

	// Start should be a no-op
	w.Start(context.Background())
	w.Stop()

	// No background goroutine should have started
	// This just verifies no panics or errors
}
