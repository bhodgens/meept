package llm_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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

// capturingHandler is a minimal slog.Handler that records log messages and
// attributes for inspection in tests.
type capturingHandler struct {
	records []string
	mu      sync.Mutex
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var sb strings.Builder
	sb.WriteString(r.Level.String())
	sb.WriteString(" ")
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(" ")
		sb.WriteString(a.Key)
		sb.WriteString("=")
		sb.WriteString(a.Value.String())
		return true
	})
	h.records = append(h.records, sb.String())
	return nil
}

func (h *capturingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *capturingHandler) dump() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.records))
	copy(out, h.records)
	return out
}

// containsSubstring reports whether any captured record contains substr.
func (h *capturingHandler) containsSubstring(substr string) bool {
	for _, r := range h.dump() {
		if strings.Contains(r, substr) {
			return true
		}
	}
	return false
}

// TestRuntimeManager_ConflictingSpawnCommand verifies that registering a second
// provider against an existing endpoint with a DIFFERENT spawn_command logs a
// warning and keeps the first command. Satisfies spec §4 test requirement:
// "Conflicting spawn_command on shared port: warning logged, first wins."
func TestRuntimeManager_ConflictingSpawnCommand(t *testing.T) {
	handler := &capturingHandler{}
	logger := slog.New(handler)
	mgr := llm.NewRuntimeManager(logger)

	cfg1 := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		ModelPaths:      map[string]string{"alpha": createTempModelFile(t)},
		EndpointKey:     "llama-cpp:127.0.0.1:7771",
		PIDFile:         filepath.Join(t.TempDir(), "a.pid"),
		AutoStart:       false,
		SpawnCommand:    []string{"llama-server", "--port", "7771"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  1 * time.Second,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}
	cfg2 := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		ModelPaths:      map[string]string{"beta": createTempModelFile(t)},
		EndpointKey:     "llama-cpp:127.0.0.1:7771", // Same endpoint.
		PIDFile:         filepath.Join(t.TempDir(), "b.pid"),
		AutoStart:       false,
		SpawnCommand:    []string{"mlx_server", "--port", "7771"}, // Different.
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  1 * time.Second,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}

	if err := mgr.RegisterConfig("provider-a", cfg1, "http://127.0.0.1:7771"); err != nil {
		t.Fatalf("RegisterConfig provider-a: %v", err)
	}
	if err := mgr.RegisterConfig("provider-b", cfg2, "http://127.0.0.1:7771"); err != nil {
		t.Fatalf("RegisterConfig provider-b: %v", err)
	}

	if !handler.containsSubstring("Conflicting spawn_command") {
		t.Errorf("expected 'Conflicting spawn_command' warning in logs; got:\n%s",
			strings.Join(handler.dump(), "\n"))
	}
}

// TestRuntimeManager_ConflictingPIDFile verifies the S4-Bug2 fix: when a
// second provider registers against an existing endpoint with a different
// pid_file, a debug log is emitted (first wins). Satisfies spec §4:
// "subsequent providers' pid_file values are ignored (with a debug log if
// they differ)."
func TestRuntimeManager_ConflictingPIDFile(t *testing.T) {
	handler := &capturingHandler{}
	logger := slog.New(handler)
	mgr := llm.NewRuntimeManager(logger)

	cfg1 := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		ModelPaths:      map[string]string{"alpha": createTempModelFile(t)},
		EndpointKey:     "llama-cpp:127.0.0.1:7772",
		PIDFile:         filepath.Join(t.TempDir(), "first.pid"),
		AutoStart:       false,
		SpawnCommand:    []string{"sleep", "1"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  1 * time.Second,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}
	cfg2 := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		ModelPaths:      map[string]string{"beta": createTempModelFile(t)},
		EndpointKey:     "llama-cpp:127.0.0.1:7772",
		PIDFile:         filepath.Join(t.TempDir(), "second.pid"), // Different.
		AutoStart:       false,
		SpawnCommand:    []string{"sleep", "1"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  1 * time.Second,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}

	if err := mgr.RegisterConfig("provider-a", cfg1, "http://127.0.0.1:7772"); err != nil {
		t.Fatalf("RegisterConfig provider-a: %v", err)
	}
	if err := mgr.RegisterConfig("provider-b", cfg2, "http://127.0.0.1:7772"); err != nil {
		t.Fatalf("RegisterConfig provider-b: %v", err)
	}

	if !handler.containsSubstring("Conflicting pid_file") {
		t.Errorf("expected 'Conflicting pid_file' debug log; got:\n%s",
			strings.Join(handler.dump(), "\n"))
	}
}

// TestRuntimeManager_StopAll_OneSIGTERMPerEndpoint verifies that StopAll sends
// exactly one SIGTERM per shared endpoint, not one per provider. Satisfies
// spec §4 test requirement: "StopAll sends exactly one SIGTERM per endpoint."
//
// We verify this indirectly: a merged endpoint has one RuntimeProcess, and
// StopAll iterates m.endpoints (not m.configs). After StopAll, the single
// subprocess must be dead and the single PID file removed. If StopAll had
// sent multiple SIGTERMs, the second would target a dead/reused PID — but
// the observable guarantee is that exactly one subprocess was managed and
// it is no longer running.
func TestRuntimeManager_StopAll_OneSIGTERMPerEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := llm.NewRuntimeManager(slog.Default())
	pidFile := filepath.Join(t.TempDir(), "shared-stop.pid")
	baseCfg := llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		ModelPaths:      map[string]string{"alpha": createTempModelFile(t)},
		EndpointKey:     "llama-cpp:127.0.0.1:7780",
		PIDFile:         pidFile,
		AutoStart:       true,
		AutoStop:        true,
		SpawnCommand:    []string{"sleep", "30"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  100 * time.Millisecond,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}
	cfg1 := baseCfg
	cfg2 := baseCfg
	cfg2.ModelPaths = map[string]string{"beta": createTempModelFile(t)}

	if err := mgr.RegisterConfig("provider-a", &cfg1, server.URL); err != nil {
		t.Fatalf("RegisterConfig a: %v", err)
	}
	if err := mgr.RegisterConfig("provider-b", &cfg2, server.URL); err != nil {
		t.Fatalf("RegisterConfig b: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// Both providers report the same non-zero PID (shared subprocess).
	s1, _ := mgr.StatusForProvider("provider-a")
	s2, _ := mgr.StatusForProvider("provider-b")
	if s1.PID == 0 || s2.PID == 0 {
		t.Fatalf("expected non-zero PIDs; got a=%d b=%d", s1.PID, s2.PID)
	}
	if s1.PID != s2.PID {
		t.Fatalf("expected shared PID; got a=%d b=%d", s1.PID, s2.PID)
	}
	sharedPID := s1.PID

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := mgr.StopAll(stopCtx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}

	// After StopAll: the shared PID must no longer be alive, and the PID file
	// must be removed (exactly once). If StopAll had iterated per provider,
	// the second stop would attempt to clean up an already-removed PID file.
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("expected PID file removed after StopAll; stat err=%v", err)
	}
	proc, err := os.FindProcess(sharedPID)
	if err != nil {
		t.Logf("FindProcess(%d) err=%v (acceptable after stop)", sharedPID, err)
	} else if err := proc.Signal(syscall.Signal(0)); err == nil {
		t.Errorf("shared subprocess PID %d still alive after StopAll", sharedPID)
	}
}
