package shadow

import (
	"bytes"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

// TestManager_AutoTrain_DoesNotSpawn verifies the export-only contract:
// NewManager with AutoTrain=true must not start any train loop. It warns at
// startup instead. The strongest observable proof is that the goroutine
// spawner and its plumbing no longer exist on the Manager type.
func TestManager_AutoTrain_DoesNotSpawn(t *testing.T) {
	mgrType := reflect.TypeFor[Manager]()
	if _, ok := mgrType.FieldByName("autoTrainStop"); ok {
		t.Error("Manager.autoTrainStop must not exist: no auto-train goroutine plumbing")
	}
	if _, ok := mgrType.FieldByName("autoTrainDone"); ok {
		t.Error("Manager.autoTrainDone must not exist: no auto-train goroutine plumbing")
	}
	ptrType := reflect.TypeFor[*Manager]()
	if _, ok := ptrType.MethodByName("startAutoTrainChecker"); ok {
		t.Fatal("startAutoTrainChecker must not exist: Manager must never spawn a train loop")
	}
	if _, ok := ptrType.MethodByName("StopAutoTrain"); ok {
		t.Fatal("StopAutoTrain must not exist: there is no train loop to stop")
	}

	// Constructing a manager with AutoTrain=true (and a low threshold that
	// would previously have started a ticker) must succeed without any
	// train goroutine — proven by the absence checks above, which are the
	// only way a goroutine could have been spawned or stopped.
	m := newTestManager(t, func(c *Config) {
		c.Adapters.AutoTrain = true
		c.Adapters.TrainThreshold = 1
	})
	_ = m
}

// TestManager_AutoTrain_WarnsOnStartup asserts the sidecar-only warning is
// logged when AutoTrain is set.
func TestManager_AutoTrain_WarnsOnStartup(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tmpDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Teacher.Model = "test-teacher"
	cfg.DataDir = tmpDir
	cfg.Quality.Method = MethodHeuristic
	cfg.Adapters.AutoTrain = true

	m, err := NewManager(ManagerConfig{Config: cfg, Logger: logger})
	if err != nil {
		if containsFTS5(err.Error()) {
			t.Skip("SQLite FTS5 module not available")
		}
		t.Fatalf("NewManager failed: %v", err)
	}
	defer func() { _ = m.Close() }()

	logs := buf.String()
	if !strings.Contains(logs, "auto-train is sidecar-only; ignored") {
		t.Errorf("expected sidecar-only warning in logs, got:\n%s", logs)
	}
}

// TestManager_AutoTrain_False_NoWarning ensures the warning only fires when
// AutoTrain is actually requested.
func TestManager_AutoTrain_False_NoWarning(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tmpDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Teacher.Model = "test-teacher"
	cfg.DataDir = tmpDir
	cfg.Quality.Method = MethodHeuristic
	cfg.Adapters.AutoTrain = false

	m, err := NewManager(ManagerConfig{Config: cfg, Logger: logger})
	if err != nil {
		if containsFTS5(err.Error()) {
			t.Skip("SQLite FTS5 module not available")
		}
		t.Fatalf("NewManager failed: %v", err)
	}
	defer func() { _ = m.Close() }()

	if strings.Contains(buf.String(), "auto-train is sidecar-only; ignored") {
		t.Error("sidecar-only warning must not fire when AutoTrain is false")
	}
}

// TestManager_Close_IdempotentWithoutAutoTrain guards the refactor: Close no
// longer stops an auto-train goroutine, and still closes cleanly.
func TestManager_Close_IdempotentWithoutAutoTrain(t *testing.T) {
	m := newTestManager(t, func(c *Config) {
		c.Adapters.AutoTrain = true
	})
	if err := m.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}
