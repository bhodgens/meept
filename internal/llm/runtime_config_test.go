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
		Runtime:      "mlx",
		ModelPath:    modelPath,
		PIDFile:      filepath.Join(pidDir, "mlx.pid"),
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
		Runtime:      "llama-cpp",
		ModelPath:    modelPath,
		PIDFile:      filepath.Join(pidDir, "llama.pid"),
		SpawnCommand: []string{"./llama-server", "--model", "$MODEL_PATH", "--port", "$TEST_MEEPT_PORT"},
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

func TestIsLoopbackBaseURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"http://localhost:8080/v1", true},
		{"http://127.0.0.1:8080/v1", true},
		{"http://[::1]:8080/v1", true},
		{"http://::1:8080/v1", true},
		{"http://0:0:0:0:0:0:0:1:8080/v1", true},
		{"https://LOCALHOST:443", true},
		{"http://0.0.0.0:8080", false},
		{"http://192.168.1.5:8080", false},
		{"https://api.example.com/v1", false},
		{"http://8.8.8.8", false},
		{"", false},
		{"not a url", false},
		{"http://fe80::1", false},
		{"http://10.0.0.1", false},
		{"http://172.16.0.1", false},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			got := llm.IsLoopbackBaseURL(tc.url)
			if got != tc.want {
				t.Errorf("IsLoopbackBaseURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestValidateAndNormalize_ModelPaths(t *testing.T) {
	// Create two model files.
	dir := t.TempDir()
	pathA := filepath.Join(dir, "model-a.gguf")
	pathB := filepath.Join(dir, "model-b.gguf")
	for _, p := range []string{pathA, pathB} {
		if err := os.WriteFile(p, []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pidDir := filepath.Join(t.TempDir(), "pid")
	cfg := llm.RuntimeLifecycleConfig{
		Runtime:    "mlx",
		ModelPaths: map[string]string{"alpha": pathA, "beta": pathB},
		PIDFile:    filepath.Join(pidDir, "mlx.pid"),
		SpawnCommand: []string{
			"./mlx-server",
			"--first", "${MODEL_PATH}",
			"--all", "${MODEL_PATHS}",
			"--json", "${MODEL_PATHS_JSON}",
			"--alpha", "${MODEL_PATH:alpha}",
		},
	}

	result, err := llm.ValidateAndNormalize(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(result.ModelPaths) != 2 {
		t.Errorf("expected 2 model paths, got %d: %v", len(result.ModelPaths), result.ModelPaths)
	}
	if result.ModelPaths["alpha"] != pathA {
		t.Errorf("alpha path mismatch: got %q", result.ModelPaths["alpha"])
	}
	if result.ModelPaths["beta"] != pathB {
		t.Errorf("beta path mismatch: got %q", result.ModelPaths["beta"])
	}

	// MODEL_PATH is the first path (sorted by key => "alpha" => pathA).
	if result.ModelPath != pathA {
		t.Errorf("expected ModelPath to be %q, got %q", pathA, result.ModelPath)
	}

	// Verify spawn-command expansion.
	// Sorted keys: [alpha, beta]; paths: [pathA, pathB]
	// [0] = "./mlx-server"
	// [1] = "--first"
	// [2] = ${MODEL_PATH} => pathA
	// [3] = "--all"
	// [4] = ${MODEL_PATHS} => "pathA pathB"
	// [5] = "--json"
	// [6] = ${MODEL_PATHS_JSON} => `["pathA","pathB"]`
	// [7] = "--alpha"
	// [8] = ${MODEL_PATH:alpha} => pathA
	if result.SpawnCommand[2] != pathA {
		t.Errorf("MODEL_PATH expansion: got %q", result.SpawnCommand[2])
	}
	if result.SpawnCommand[4] != pathA+" "+pathB {
		t.Errorf("MODEL_PATHS expansion: got %q", result.SpawnCommand[4])
	}
	wantJSON := `["` + pathA + `","` + pathB + `"]`
	if result.SpawnCommand[6] != wantJSON {
		t.Errorf("MODEL_PATHS_JSON expansion: got %q, want %q", result.SpawnCommand[6], wantJSON)
	}
	if result.SpawnCommand[8] != pathA {
		t.Errorf("MODEL_PATH:alpha expansion: got %q", result.SpawnCommand[8])
	}
}

func TestValidateAndNormalize_LegacyModelPathMirroredToDefault(t *testing.T) {
	modelPath := createTempModelFile(t)
	pidDir := filepath.Join(t.TempDir(), "pid")
	cfg := llm.RuntimeLifecycleConfig{
		Runtime:   "llama-cpp",
		ModelPath: modelPath,
		PIDFile:   filepath.Join(pidDir, "llama.pid"),
	}

	result, err := llm.ValidateAndNormalize(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(result.ModelPaths) != 1 {
		t.Fatalf("expected 1 model path, got %d", len(result.ModelPaths))
	}
	if result.ModelPaths["default"] != modelPath {
		t.Errorf("expected 'default' key to mirror model_path, got %q", result.ModelPaths["default"])
	}
}

func TestValidateAndNormalize_NoModelPaths(t *testing.T) {
	pidDir := filepath.Join(t.TempDir(), "pid")
	cfg := llm.RuntimeLifecycleConfig{
		Runtime: "llama-cpp",
		PIDFile: filepath.Join(pidDir, "llama.pid"),
	}
	_, err := llm.ValidateAndNormalize(cfg)
	if err == nil {
		t.Fatal("expected error when no model_path or model_paths configured")
	}
}

func TestValidateAndNormalize_MissingModelInPaths(t *testing.T) {
	pidDir := filepath.Join(t.TempDir(), "pid")
	cfg := llm.RuntimeLifecycleConfig{
		Runtime:    "llama-cpp",
		ModelPaths: map[string]string{"key": "/nonexistent/model.gguf"},
		PIDFile:    filepath.Join(pidDir, "llama.pid"),
	}
	_, err := llm.ValidateAndNormalize(cfg)
	if err == nil {
		t.Fatal("expected error for missing model in model_paths")
	}
}

func TestComputeEndpointKey(t *testing.T) {
	cases := []struct {
		runtime, baseURL, want string
	}{
		{"llama-cpp", "http://127.0.0.1:8080/v1", "llama-cpp:127.0.0.1:8080"},
		{"mlx", "http://localhost:9090", "mlx:localhost:9090"},
		{"llama-cpp", "http://localhost", "llama-cpp:localhost:8080"},
		{"mlx", "", "mlx:127.0.0.1:8080"},
		{"llama-cpp", "not a url", "llama-cpp:127.0.0.1:8080"},
	}
	for _, tc := range cases {
		got := llm.ComputeEndpointKey(tc.runtime, tc.baseURL)
		if got != tc.want {
			t.Errorf("ComputeEndpointKey(%q, %q) = %q, want %q", tc.runtime, tc.baseURL, got, tc.want)
		}
	}
}
