//go:build e2e

// Package configmodels covers config-models-01: the user's models.json5
// wins (its default model and provider wiring are what the daemon boots
// with), and an empty model slot leaves the general client wired.
package configmodels

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// config-models-01: the user models.json5 wins. The harness seeds
// models.json5 with model "fake/fake-model" routed at the FakeLLM; the
// daemon must boot with THAT default (status.default_model / model) and the
// chat path must actually reach the fake provider — proving the user's
// provider block (not any built-in default) is what got wired.
func TestUserModelsConfigWins(t *testing.T) {
	s := harness.Start(t)

	status := rpcResult(t, s.SocketPath, "status", nil)
	model, _ := status["model"].(string)
	if model != "fake/fake-model" {
		t.Fatalf("status.model = %q, want the user models.json5 default fake/fake-model (full status: %v)", model, status["model"])
	}
	if dm, _ := status["default_model"].(string); dm != "fake/fake-model" {
		t.Fatalf("status.default_model = %q, want fake/fake-model", dm)
	}

	// The wired client is the USER's provider: a code-lane chat turn runs a
	// scripted executor file_write against the fake provider (zero external
	// traffic), proving the general client stayed wired through the user's
	// provider block.
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "config-models", s.ProjectDir)
	artifact := s.ProjectDir + "/models-check.txt"
	s.Fake.EnqueueFileWrite("call-cm1", artifact, "user models win")
	s.Fake.SetPostToolText("wrote the models check file")
	s.ChatTurn(t, sessionID, "Create a file named models-check.txt containing user models win", 120*time.Second)
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("artifact from the user-wired provider missing: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "user models win" {
		t.Fatalf("artifact content = %q, want %q", got, "user models win")
	}
	if s.Fake.RequestCount() == 0 {
		t.Fatal("fake LLM received no requests; the user provider was not the wired client")
	}
}

// config-models-01b: an empty model slot leaves the general client wired.
// models.json5 with an empty "extract_model"/"refusal_model" slot (already
// the harness shape) must not break the daemon: the general completion
// client still serves the default model and turns complete.
func TestEmptySlotLeavesGeneralClientWired(t *testing.T) {
	s := harness.Start(t)

	// The harness-written models.json5 carries "extract_model": "" and
	// "refusal_model": "" — empty slots. The daemon booted healthy with
	// them, and the general client still works.
	if s.Daemon.Pid() == 0 {
		t.Fatal("daemon did not boot with empty model slots")
	}
	health := s.HealthJSON(t)
	if health["status"] != "ok" {
		t.Fatalf("health = %v, want ok with empty model slots", health["status"])
	}

	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "config-models-empty", s.ProjectDir)
	artifact := s.ProjectDir + "/empty-slot.txt"
	s.Fake.EnqueueFileWrite("call-cm2", artifact, "empty slot ok")
	s.Fake.SetPostToolText("wrote the empty slot file")
	s.ChatTurn(t, sessionID, "Create a file named empty-slot.txt containing empty slot ok", 120*time.Second)
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("general client not wired with empty slots; artifact missing: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "empty slot ok" {
		t.Fatalf("artifact content = %q, want %q", got, "empty slot ok")
	}
}
