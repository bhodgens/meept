package llm_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

func TestRuntimeManager_New(t *testing.T) {
	mgr := llm.NewRuntimeManager(slog.Default())
	if mgr == nil {
		t.Fatal("expected non-nil RuntimeManager")
	}

	// A freshly created manager should return false for any provider lookup.
	_, ok := mgr.GetHealthChecker("nonexistent")
	if ok {
		t.Error("expected no health checker in fresh manager")
	}
}

func TestRuntimeManager_RegisterAndGetHealthChecker(t *testing.T) {
	mgr := llm.NewRuntimeManager(slog.Default())

	pidFile := filepath.Join(t.TempDir(), "test.pid")
	cfg := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		PIDFile:         pidFile,
		AutoStart:       false,
		AutoStop:        true,
		SpawnCommand:    []string{"sleep", "1"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  1 * time.Second,
		HealthTimeout:   2 * time.Second,
		HealthThreshold: 1,
	}

	err := mgr.RegisterConfig("test-provider", cfg, "http://localhost:8080")
	if err != nil {
		t.Fatalf("RegisterConfig returned error: %v", err)
	}

	hc, ok := mgr.GetHealthChecker("test-provider")
	if !ok {
		t.Fatal("expected health checker to be found after registration")
	}
	if hc == nil {
		t.Fatal("expected non-nil health checker")
	}
}

func TestRuntimeManager_GetHealthChecker_NotFound(t *testing.T) {
	mgr := llm.NewRuntimeManager(slog.Default())

	hc, ok := mgr.GetHealthChecker("unknown-provider")
	if ok {
		t.Error("expected ok=false for unknown provider")
	}
	if hc != nil {
		t.Error("expected nil health checker for unknown provider")
	}
}

func TestRuntimeManager_StartStop_ShortLivedProcess(t *testing.T) {
	// Set up a fake health endpoint via httptest.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := llm.NewRuntimeManager(slog.Default())

	pidFile := filepath.Join(t.TempDir(), "shortlived.pid")
	cfg := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		PIDFile:         pidFile,
		AutoStart:       true,
		AutoStop:        true,
		SpawnCommand:    []string{"sleep", "0.1"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  100 * time.Millisecond,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}

	err := mgr.RegisterConfig("shortlived-provider", cfg, server.URL)
	if err != nil {
		t.Fatalf("RegisterConfig returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = mgr.StartAll(ctx)
	if err != nil {
		t.Fatalf("StartAll returned error: %v", err)
	}

	// Verify the PID file was created (process was started).
	if _, statErr := os.Stat(pidFile); statErr != nil {
		t.Errorf("expected PID file at %s after StartAll: %v", pidFile, statErr)
	}

	// Stop all runtimes.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := mgr.StopAll(stopCtx); err != nil {
		t.Fatalf("StopAll returned error: %v", err)
	}
}

func TestRuntimeManager_StartAll_SkipNonAutoStart(t *testing.T) {
	mgr := llm.NewRuntimeManager(slog.Default())

	pidFile := filepath.Join(t.TempDir(), "noautostart.pid")
	cfg := &llm.RuntimeConfig{
		Type:            llm.RuntimeMLX,
		ModelPath:       createTempModelFile(t),
		PIDFile:         pidFile,
		AutoStart:       false,
		AutoStop:        false,
		SpawnCommand:    []string{"sleep", "10"},
		SpawnTimeout:    5 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  1 * time.Second,
		HealthTimeout:   2 * time.Second,
		HealthThreshold: 1,
	}

	err := mgr.RegisterConfig("noautostart-provider", cfg, "http://localhost:8080")
	if err != nil {
		t.Fatalf("RegisterConfig returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = mgr.StartAll(ctx)
	if err != nil {
		t.Fatalf("StartAll returned error: %v", err)
	}

	// PID file should NOT exist since AutoStart is false.
	if _, statErr := os.Stat(pidFile); statErr == nil {
		t.Error("expected no PID file for non-auto-start provider")
	}
}
