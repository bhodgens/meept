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

// TestRuntimeManager_SharedProcess_Merge verifies that two providers targeting
// the same (runtime, host, port) share one subprocess.
func TestRuntimeManager_SharedProcess_Merge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := llm.NewRuntimeManager(slog.Default())
	pidDir := t.TempDir()
	pidFile := filepath.Join(pidDir, "shared.pid")

	cfg1 := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		ModelPaths:      map[string]string{"alpha": createTempModelFile(t)},
		EndpointKey:     "llama-cpp:127.0.0.1:9999",
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
	cfg2 := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		ModelPaths:      map[string]string{"beta": createTempModelFile(t)},
		EndpointKey:     "llama-cpp:127.0.0.1:9999", // Same endpoint.
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

	// Both providers point at the same httptest URL but EndpointKey
	// is explicitly set to force the shared endpoint.
	if err := mgr.RegisterConfig("provider-a", cfg1, server.URL); err != nil {
		t.Fatalf("RegisterConfig provider-a: %v", err)
	}
	if err := mgr.RegisterConfig("provider-b", cfg2, server.URL); err != nil {
		t.Fatalf("RegisterConfig provider-b: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// Status reports both providers but they share the same PID via the endpoint.
	statuses := mgr.Status()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 status entries, got %d", len(statuses))
	}

	// Find the two provider statuses; both should report the same PID (non-zero).
	pids := make(map[int]struct{})
	for _, s := range statuses {
		if s.PID != 0 {
			pids[s.PID] = struct{}{}
		}
	}
	if len(pids) == 0 {
		t.Fatal("no PID reported from either provider (process not running)")
	}
	if len(pids) > 1 {
		t.Errorf("expected both providers to share one PID, got %d distinct PIDs: %v", len(pids), pids)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := mgr.StopAll(stopCtx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
}

// TestRuntimeManager_InUseGate verifies that StartAll skips endpoints with no
// model in the in-use set.
func TestRuntimeManager_InUseGate(t *testing.T) {
	mgr := llm.NewRuntimeManager(slog.Default())
	pidFile := filepath.Join(t.TempDir(), "gated.pid")
	cfg := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		ModelPaths:      map[string]string{"unused": createTempModelFile(t)},
		EndpointKey:     "llama-cpp:127.0.0.1:8765",
		PIDFile:         pidFile,
		AutoStart:       true,
		AutoStop:        true,
		SpawnCommand:    []string{"sleep", "10"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  1 * time.Second,
		HealthTimeout:   2 * time.Second,
		HealthThreshold: 1,
	}
	if err := mgr.RegisterConfig("gated-provider", cfg, "http://127.0.0.1:8765"); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}

	// Set an in-use set that does NOT include our provider/model.
	mgr.SetModelsInUse(map[string]struct{}{"other/model": {}})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// PID file should NOT exist because the endpoint was gated.
	if _, err := os.Stat(pidFile); err == nil {
		t.Error("PID file should not exist; provider was not in in-use set")
	}

	// Status should reflect WouldStart=false and InUseModels empty.
	status, ok := mgr.StatusForProvider("gated-provider")
	if !ok {
		t.Fatal("provider not found in status")
	}
	if status.WouldStart {
		t.Error("expected WouldStart=false for gated provider")
	}
	if len(status.InUseModels) != 0 {
		t.Errorf("expected no InUseModels, got %v", status.InUseModels)
	}
	if status.ProcessGroup == "" {
		t.Error("expected non-empty ProcessGroup")
	}
}

// TestRuntimeManager_InUseGate_IncludesModel verifies that when the in-use set
// includes the provider's model, StartAll spawns.
func TestRuntimeManager_InUseGate_IncludesModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := llm.NewRuntimeManager(slog.Default())
	pidFile := filepath.Join(t.TempDir(), "included.pid")
	cfg := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		ModelPaths:      map[string]string{"alpha": createTempModelFile(t)},
		EndpointKey:     "llama-cpp:127.0.0.1:8766",
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
	if err := mgr.RegisterConfig("included-provider", cfg, server.URL); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}

	mgr.SetModelsInUse(map[string]struct{}{"included-provider/alpha": {}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	if _, err := os.Stat(pidFile); err != nil {
		t.Errorf("expected PID file after gated start: %v", err)
	}

	status, _ := mgr.StatusForProvider("included-provider")
	if !status.WouldStart {
		t.Error("expected WouldStart=true for in-use provider")
	}
	if len(status.InUseModels) != 1 || status.InUseModels[0] != "alpha" {
		t.Errorf("expected InUseModels=['alpha'], got %v", status.InUseModels)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	mgr.StopAll(stopCtx)
}
