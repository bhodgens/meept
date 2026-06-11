package tts_test

import (
	"testing"

	"github.com/caimlas/meept/internal/tts"
)

func TestNewSynthesizer_UnknownEngine(t *testing.T) {
	cfg := tts.Config{Engine: "unknown"}
	_, err := tts.NewSynthesizer(cfg)
	if err == nil || err.Error() != `tts: unknown engine "unknown"` {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewSynthesizer_PiperEngine(t *testing.T) {
	cfg := tts.Config{
		Engine: "piper",
		Voice:  "danny-medium",
	}
	// Should fail because piper binary doesn't exist
	_, err := tts.NewSynthesizer(cfg)
	if err == nil {
		t.Error("expected error (piper not installed), got nil")
	}
}

func TestNewSynthesizer_PlatformEngine(t *testing.T) {
	cfg := tts.Config{
		Engine: "platform",
	}
	synth, err := tts.NewSynthesizer(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if synth.Name() != "platform" {
		t.Errorf("expected name 'platform', got %q", synth.Name())
	}
}

func TestDefaultVoicePath(t *testing.T) {
	path, err := tts.DefaultVoicePath("danny-medium")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify path structure (don't assume home dir)
	if path == "" {
		t.Error("expected non-empty path")
	}
	if !contains(path, ".meept/tts/voices/danny-medium.onnx") {
		t.Errorf("unexpected path: %q", path)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
