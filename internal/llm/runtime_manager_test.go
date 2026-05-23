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

func TestRuntimeManager_Status(t *testing.T) {
	mgr := llm.NewRuntimeManager(slog.Default())

	pidFile := filepath.Join(t.TempDir(), "status.pid")
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

	err := mgr.RegisterConfig("test-status", cfg, "http://localhost:8080")
	if err != nil {
		t.Fatalf("RegisterConfig error: %v", err)
	}

	statuses := mgr.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].ProviderID != "test-status" {
		t.Errorf("expected provider test-status, got %s", statuses[0].ProviderID)
	}
	if statuses[0].Runtime != "llama-cpp" {
		t.Errorf("expected runtime llama-cpp, got %s", statuses[0].Runtime)
	}

	// Test single provider lookup
	status, ok := mgr.StatusForProvider("test-status")
	if !ok {
		t.Fatal("expected to find provider test-status")
	}
	if status.ProviderID != "test-status" {
		t.Errorf("expected provider test-status, got %s", status.ProviderID)
	}

	// Test nonexistent provider
	_, ok = mgr.StatusForProvider("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent provider")
	}
}

func TestRuntimeManager_StopAll_RespectsAutoStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := llm.NewRuntimeManager(slog.Default())

	// Provider with AutoStop=true
	pidFile1 := filepath.Join(t.TempDir(), "autostop.pid")
	cfg1 := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		PIDFile:         pidFile1,
		AutoStart:       true,
		AutoStop:        true,
		SpawnCommand:    []string{"sleep", "0.1"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  100 * time.Millisecond,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}

	// Provider with AutoStop=false
	pidFile2 := filepath.Join(t.TempDir(), "noautostop.pid")
	cfg2 := &llm.RuntimeConfig{
		Type:            llm.RuntimeMLX,
		ModelPath:       createTempModelFile(t),
		PIDFile:         pidFile2,
		AutoStart:       true,
		AutoStop:        false,
		SpawnCommand:    []string{"sleep", "0.1"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  100 * time.Millisecond,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}

	mgr.RegisterConfig("stop-me", cfg1, server.URL)
	mgr.RegisterConfig("keep-me", cfg2, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll error: %v", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := mgr.StopAll(stopCtx); err != nil {
		t.Fatalf("StopAll error: %v", err)
	}

	// autostop PID file should be removed
	if _, err := os.Stat(pidFile1); !os.IsNotExist(err) {
		t.Error("autostop PID file should be removed")
	}

	// noautostop PID file should still exist
	if _, err := os.Stat(pidFile2); os.IsNotExist(err) {
		t.Error("noautostop PID file should still exist")
	}

	// Clean up leftover process
	mgr.StopProvider(stopCtx, "keep-me")
}

func TestRuntimeManager_StartStopProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := llm.NewRuntimeManager(slog.Default())

	pidFile := filepath.Join(t.TempDir(), "individual.pid")
	cfg := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		PIDFile:         pidFile,
		AutoStart:       false,
		AutoStop:        true,
		SpawnCommand:    []string{"sleep", "0.1"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  100 * time.Millisecond,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}

	mgr.RegisterConfig("individual", cfg, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start individual provider
	if err := mgr.StartProvider(ctx, "individual"); err != nil {
		t.Fatalf("StartProvider error: %v", err)
	}

	status, _ := mgr.StatusForProvider("individual")
	if !status.Running {
		t.Error("expected running after StartProvider")
	}

	// Stop individual provider
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := mgr.StopProvider(stopCtx, "individual"); err != nil {
		t.Fatalf("StopProvider error: %v", err)
	}

	status, _ = mgr.StatusForProvider("individual")
	if status.Running {
		t.Error("expected not running after StopProvider")
	}

	// Test nonexistent provider
	err := mgr.StartProvider(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent provider")
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
