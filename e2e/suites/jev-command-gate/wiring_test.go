//go:build e2e

// jev-gate-03: score-gate daemon wiring (issue #113).
//
// The previous test constructed jevgate.NewScoreGateHook by hand and
// registered it on a private HookRegistry. That stayed green with zero
// production wiring — the same gap that let issue #87 ship e2e-green.
// This file boots a scratch daemon through the e2e harness and drives a
// real shell tool call, so a deleted registration fails the suite.
//
// Armed ([security.jev_command_gate].enabled=true):
//   - boot log names the live "jev-score-gate" registration,
//   - a benign shell call the shell tool would otherwise run is denied
//     before execution (marker file absent, confirmation shape returned),
//   - the same boot opens the capture store and the deny invokes that
//     store's ConfirmFlow (Escalated log). The scorer-error reason is the
//     branch that also calls Recorder.RecordScore.
//
// Disabled (opt-in default): no registration log, the same call executes.
// That is the wiring-failure property: enforcement rides the live
// registration, not latent package state.
//
// Hermetic: fake LLM, no decision_model slot. ScoreCommand returns
// ErrDecisionUnavailable and the hook fail-closes (the degraded posture
// components.go documents). A sqlite command_gate_records row is NOT the
// deny artifact — RecordScore only arms an in-memory pending; the row
// lands when a human resolves (gatecapture Contract C4). Asserting a row
// here would contradict that contract.
package jevgatecmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

const (
	jevWiringMarkerName = "jev-wiring-marker.txt"
	jevWiringMarkerBody = "jev-wiring-ran"
	jevWiringShellCmd   = "printf " + jevWiringMarkerBody + " > " + jevWiringMarkerName
)

// jevWiringChat is an imperative artifact request so the fake classifier
// pins intent=code (the coder lane holds shell).
const jevWiringChat = "Create a marker file named jev-wiring-marker.txt by running a shell printf command in the project"

func jevWiringLog(t *testing.T, s *harness.Stack) string {
	t.Helper()
	data, err := os.ReadFile(s.Daemon.LogPath())
	if err != nil {
		t.Fatalf("read daemon log: %v", err)
	}
	return string(data)
}

func jevWiringToolResults(f *harness.FakeLLM) string {
	var out []string
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role != "tool" {
				continue
			}
			if c, _ := msg["content"].(string); c != "" {
				out = append(out, c)
			}
		}
	}
	return strings.Join(out, "\n")
}

// jevWiringShellTurn registers the project, binds a session, and drives one
// scripted shell call that writes the marker file unless a before-tool-call
// hook denies it first.
func jevWiringShellTurn(t *testing.T, s *harness.Stack, sessionName string) string {
	t.Helper()
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, sessionName, s.ProjectDir)
	args, err := json.Marshal(map[string]any{
		"command":     jevWiringShellCmd,
		"working_dir": s.ProjectDir,
		"timeout":     15,
	})
	if err != nil {
		t.Fatalf("marshal shell args: %v", err)
	}
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "shell",
		Arguments: string(args),
	})
	s.ChatTurn(t, sessionID, jevWiringChat, 120*time.Second)
	return filepath.Join(s.ProjectDir, jevWiringMarkerName)
}

// TestJevGate03_ArmedBootDeniesShellCall proves the daemon, not a
// hand-built hook, registered jev-score-gate and ran it on a shell call.
func TestJevGate03_ArmedBootDeniesShellCall(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"security.jev_command_gate.enabled": true,
		// Escalated is Debug; INFO would hide the capture-store call.
		"daemon.log_level": "DEBUG",
	}))
	marker := jevWiringShellTurn(t, s, "jev-armed")

	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("armed gate let the shell call execute; marker exists at %s\ntool results:\n%s",
			marker, jevWiringToolResults(s.Fake))
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat marker: %v", err)
	}

	results := jevWiringToolResults(s.Fake)
	for _, want := range []string{
		"requires_confirmation",
		"score gate",
		"confirmation required",
		"scorer error",
	} {
		if !strings.Contains(results, want) {
			t.Fatalf("armed deny missing %q in tool results:\n%s\ndaemon log tail:\n%s",
				want, results, s.Daemon.LogTail())
		}
	}
	if strings.Contains(results, jevWiringMarkerBody) {
		t.Fatalf("denied shell still returned its stdout:\n%s", results)
	}

	log := jevWiringLog(t, s)
	for _, want := range []string{
		"Registered jev score gate BeforeToolCall hook",
		"jev score gate escalates to confirmation",
		"tool call denied by before-tool-call hook",
		"gate capture store open",
		// Capture adapter, not a nil ConfirmFlow: Escalated only logs
		// when the wired gateCaptureStore is the hook's confirm seam.
		"gatecapture: jev score gate escalated",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("armed boot log missing %q\nlog tail:\n%s", want, s.Daemon.LogTail())
		}
	}

	captureDB := filepath.Join(s.MeeptHome, "gatecapture.db")
	if _, err := os.Stat(captureDB); err != nil {
		t.Fatalf("armed wiring did not open the capture store at %s: %v", captureDB, err)
	}
}

// TestJevGate03_DisabledBootExecutesShellCall is the wiring-failure
// property: with the opt-in knob off the daemon does not register the
// hook, and the same shell call executes. Deleting the registration
// block puts production in this posture even when the knob is on.
func TestJevGate03_DisabledBootExecutesShellCall(t *testing.T) {
	s := harness.Start(t)
	marker := jevWiringShellTurn(t, s, "jev-disabled")

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("disabled gate blocked a benign shell call (registration leaked?): %v\ntool results:\n%s\nlog tail:\n%s",
			err, jevWiringToolResults(s.Fake), s.Daemon.LogTail())
	}
	if got := strings.TrimSpace(string(data)); got != jevWiringMarkerBody {
		t.Fatalf("marker = %q, want %q", got, jevWiringMarkerBody)
	}
	if strings.Contains(jevWiringToolResults(s.Fake), "score gate") {
		t.Fatalf("disabled boot still ran the score gate:\n%s", jevWiringToolResults(s.Fake))
	}
	log := jevWiringLog(t, s)
	if strings.Contains(log, "Registered jev score gate BeforeToolCall hook") {
		t.Fatalf("disabled boot registered jev-score-gate; opt-in default broken\nlog tail:\n%s", s.Daemon.LogTail())
	}
	if strings.Contains(log, "jev score gate escalates to confirmation") {
		t.Fatalf("disabled boot still ran the score gate\nlog tail:\n%s", s.Daemon.LogTail())
	}
	// gatecapture.db may exist anyway: the shell permission table opens the
	// same store for chokepoint 2 (components.go). Absence of the
	// registration + escalation lines is the opt-in pin, not the file.
}
