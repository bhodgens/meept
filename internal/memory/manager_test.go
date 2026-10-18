package memory

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func TestManager_BackendDefaultCase(t *testing.T) {
	// Test that an unknown backend value triggers a warning and falls back to local store
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	cfg := ManagerConfig{
		Config: config.MemoryConfig{
			Backend:    "sqlie", // typo, unknown value
			Episodic:   config.EpisodicConfig{Enabled: true},
			Task:       config.TaskMemoryConfig{Enabled: true},
		},
		Logger: logger,
	}

	m := NewManager(cfg)
	err := m.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Check that a warning was logged with the unknown backend value
	logOutput := buf.String()
	if !bytes.Contains([]byte(logOutput), []byte("unknown memory backend value")) {
		t.Errorf("Expected warning about unknown backend, got: %s", logOutput)
	}
	if !bytes.Contains([]byte(logOutput), []byte("sqlie")) {
		t.Errorf("Expected backend value 'sqlie' in warning, got: %s", logOutput)
	}

	// Verify the fallback store was chosen (wantSQLite should be true)
	// We can check this by verifying the manager initialized with SQLite backends
	// The initSQLiteBackends was called because wantSQLite = true in default case
	if m.episodic == nil && m.task == nil {
		t.Error("Expected SQLite backends to be initialized as fallback")
	}
}