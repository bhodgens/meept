package llm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

func createTempModelFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "test-model.gguf")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0o644); err != nil {
		t.Fatalf("failed to create temp model file: %v", err)
	}
	return modelPath
}

func TestValidateAndNormalize_ValidConfig(t *testing.T) {
	modelPath := createTempModelFile(t)
	pidDir := filepath.Join(t.TempDir(), "pid")
	cfg := llm.RuntimeLifecycleConfig{
		Runtime:        "llama-cpp",
		ModelPath:      modelPath,
		AutoStart:      true,
		AutoStopOnExit: true,
		PIDFile:        filepath.Join(pidDir, "llama.pid"),
		SpawnCommand:   []string{"./llama-server", "--model", "$MODEL_PATH", "--port", "8080"},
		SpawnTimeout:   30,
		HealthCheck: llm.HealthCheckConfig{
			Endpoint:           "http://localhost:8080/health",
			IntervalSeconds:    5,
			TimeoutSeconds:     3,
			UnhealthyThreshold: 2,
		},
	}

	result, err := llm.ValidateAndNormalize(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.Type != llm.RuntimeLlamaCpp {
		t.Errorf("expected type %q, got %q", llm.RuntimeLlamaCpp, result.Type)
	}
	if result.ModelPath != modelPath {
		t.Errorf("expected model path %q, got %q", modelPath, result.ModelPath)
	}
	if !result.AutoStart {
		t.Error("expected AutoStart to be true")
	}
	if !result.AutoStop {
		t.Error("expected AutoStop to be true")
	}
	if result.PIDFile != filepath.Join(pidDir, "llama.pid") {
		t.Errorf("unexpected PID file: %s", result.PIDFile)
	}
	if len(result.SpawnCommand) != 5 {
		t.Errorf("expected 5 spawn command parts, got %d", len(result.SpawnCommand))
	}
	if result.SpawnCommand[2] != modelPath {
		t.Errorf("expected MODEL_PATH expansion in spawn command, got %q", result.SpawnCommand[2])
	}
	if result.SpawnTimeout != 30*time.Second {
		t.Errorf("expected spawn timeout %v, got %v", 30*time.Second, result.SpawnTimeout)
	}
	if result.HealthEndpoint != "http://localhost:8080/health" {
		t.Errorf("expected health endpoint %q, got %q", "http://localhost:8080/health", result.HealthEndpoint)
	}
	if result.HealthInterval != 5*time.Second {
		t.Errorf("expected health interval %v, got %v", 5*time.Second, result.HealthInterval)
	}
	if result.HealthTimeout != 3*time.Second {
		t.Errorf("expected health timeout %v, got %v", 3*time.Second, result.HealthTimeout)
	}
	if result.HealthThreshold != 2 {
		t.Errorf("expected health threshold %d, got %d", 2, result.HealthThreshold)
	}
}

func TestValidateAndNormalize_InvalidRuntime(t *testing.T) {
	modelPath := createTempModelFile(t)
	cfg := llm.RuntimeLifecycleConfig{
		Runtime:   "mlx-legacy",
		ModelPath: modelPath,
	}

	_, err := llm.ValidateAndNormalize(cfg)
	if err == nil {
		t.Fatal("expected error for invalid runtime, got nil")
	}
	if want := "unsupported runtime"; !strings.Contains(err.Error(), want) {
		t.Errorf("error should mention unsupported runtime, got: %v", err)
	}
}

func TestValidateAndNormalize_ModelNotFound(t *testing.T) {
	cfg := llm.RuntimeLifecycleConfig{
		Runtime:   "llama-cpp",
		ModelPath: "/nonexistent/path/to/model.gguf",
	}

	_, err := llm.ValidateAndNormalize(cfg)
	if err == nil {
		t.Fatal("expected error for missing model file, got nil")
	}
}

func TestValidateAndNormalize_Defaults(t *testing.T) {
	modelPath := createTempModelFile(t)
	pidDir := filepath.Join(t.TempDir(), "pid")
	cfg := llm.RuntimeLifecycleConfig{
		Runtime: "mlx",
		ModelPath: modelPath,
		PIDFile:   filepath.Join(pidDir, "mlx.pid"),
		SpawnCommand: []string{"./mlx-server", "--model", "$MODEL_PATH"},
		HealthCheck: llm.HealthCheckConfig{
			Endpoint: "http://localhost:8080/health",
		},
	}

	result, err := llm.ValidateAndNormalize(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify defaults
	if result.HealthInterval != 10*time.Second {
		t.Errorf("expected default health interval %v, got %v", 10*time.Second, result.HealthInterval)
	}
	if result.HealthTimeout != 5*time.Second {
		t.Errorf("expected default health timeout %v, got %v", 5*time.Second, result.HealthTimeout)
	}
	if result.HealthThreshold != 3 {
		t.Errorf("expected default health threshold %d, got %d", 3, result.HealthThreshold)
	}
	if result.SpawnTimeout != 60*time.Second {
		t.Errorf("expected default spawn timeout %v, got %v", 60*time.Second, result.SpawnTimeout)
	}
	if result.Type != llm.RuntimeMLX {
		t.Errorf("expected type %q, got %q", llm.RuntimeMLX, result.Type)
	}
}

func TestValidateAndNormalize_OSExpandVars(t *testing.T) {
	modelPath := createTempModelFile(t)
	pidDir := filepath.Join(t.TempDir(), "pid")

	// Set a test env var
	os.Setenv("TEST_MEEPT_PORT", "9999")
	defer os.Unsetenv("TEST_MEEPT_PORT")

	cfg := llm.RuntimeLifecycleConfig{
		Runtime:        "llama-cpp",
		ModelPath:      modelPath,
		PIDFile:        filepath.Join(pidDir, "llama.pid"),
		SpawnCommand:   []string{"./llama-server", "--model", "$MODEL_PATH", "--port", "$TEST_MEEPT_PORT"},
	}

	result, err := llm.ValidateAndNormalize(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.SpawnCommand[2] != modelPath {
		t.Errorf("expected MODEL_PATH expansion, got %q", result.SpawnCommand[2])
	}
	if result.SpawnCommand[4] != "9999" {
		t.Errorf("expected env var expansion to '9999', got %q", result.SpawnCommand[4])
	}
}
