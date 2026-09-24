//go:build e2e

// Package refusal is the WAVE-B e2e suite for the loop refusal fallback
// (manifest scenarios refusal-01, refusal-02): a provider refusal
// re-dispatches the turn to the fallback model exactly once, and the
// per-agent refusal_model overrides the global slot.
//
// HARNESS GAP (documented, not worked around): a refusal is detected from
// finish_reason "content_filter"/"refusal" (DetectRefusal) or a non-200
// error-body marker (DetectRefusalFromBody). The harness FakeLLM always
// returns finish_reason "stop"/"tool_calls" and HTTP 200 — it has no
// error-injection seam (no 429, no refusal finish reason, no error-body
// scripting), and the task forbids modifying the harness. The refusal-signal
// path itself therefore needs a harness seam (refusal finish-reason /
// status-code injection); what IS testable end to end is covered below.
package refusal

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

func newStack(t *testing.T) *harness.Stack {
	t.Helper()
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	return s
}

// refusalDisclosureMarker is the distinctive suffix the loop appends when a
// refusal-fallback retry was armed and served (loop_refusal.go
// refusalFallbackDisclosure).
const refusalDisclosureMarker = "after refusal]"

// ---------------------------------------------------------------------------
// refusal-01 (M): provider refusal re-dispatches the turn to the fallback model, once
// ---------------------------------------------------------------------------

// TestRefusal01NormalTurnCarriesNoFallbackDisclosure pins the reachable half
// of the one-hop contract: a healthy turn on the default (single fake)
// provider completes WITHOUT the "[answered by <model> after refusal]"
// disclosure — the fallback machinery never arms spuriously — and the
// turn.terminal outcome is honest completed work.
func TestRefusal01NormalTurnCarriesNoFallbackDisclosure(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "ref01", s.ProjectDir)

	artifact := filepath.Join(s.ProjectDir, "ref01.txt")
	s.Fake.SetPostToolText("Created ref01.txt at " + artifact + " without any fallback.")
	s.Fake.EnqueueFileWrite("call-ref01", artifact, "refusal suite")

	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file named ref01.txt containing refusal suite")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("refusal-01: ack missing turn_id: %+v", ack)
	}

	task := waitAnyTask(t, s)
	harness.WaitTaskCompleted(t, s.TasksDBPath(), task.ID, 120*time.Second)

	// The executor's reply reached the user without any fallback disclosure.
	deadline := time.Now().Add(60 * time.Second)
	found := false
	for time.Now().Before(deadline) && !found {
		for _, st := range harness.Steps(t, s.TasksDBPath(), task.ID) {
			if st.Result != "" && strings.Contains(st.Result, "without any fallback") {
				found = true
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !found {
		t.Fatalf("refusal-01: executor reply never reached the step results; steps:\n%s",
			harness.FormatSteps(harness.Steps(t, s.TasksDBPath(), task.ID)))
	}
	// No step result may carry the disclosure marker.
	for _, st := range harness.Steps(t, s.TasksDBPath(), task.ID) {
		if strings.Contains(st.Result, refusalDisclosureMarker) {
			t.Fatalf("refusal-01: step result carries a refusal disclosure on a healthy turn: %q", st.Result)
		}
	}
}

// TestRefusal01FallbackFeatureOffAtWiring pins the feature-off wiring: the
// sandbox models.json5 leaves refusal_model empty (both spec and global), so
// the daemon must NOT log the global-fallback wiring line — the feature is
// off, and handleRefusal would surface any refusal unchanged rather than
// arming a fallback.
func TestRefusal01FallbackFeatureOffAtWiring(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "ref01b", s.ProjectDir)

	artifact := filepath.Join(s.ProjectDir, "ref01b.txt")
	s.Fake.SetPostToolText("Created ref01b.txt at " + artifact + ".")
	s.Fake.EnqueueFileWrite("call-ref01b", artifact, "feature off")

	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file named ref01b.txt containing feature off")
	if turnID, _ := ack["turn_id"].(string); turnID == "" {
		t.Fatalf("refusal-01b: ack missing turn_id: %+v", ack)
	}
	task := waitAnyTask(t, s)
	harness.WaitTaskCompleted(t, s.TasksDBPath(), task.ID, 120*time.Second)

	// The daemon log must not show the fallback wired or armed.
	log := s.Daemon.LogTail()
	if strings.Contains(log, "Global refusal fallback configured") {
		t.Fatalf("refusal-01b: fallback wired despite empty refusal_model; log tail:\n%s", log)
	}
}

// ---------------------------------------------------------------------------
// refusal-02 (S): per-agent refusal_model overrides the global slot
// ---------------------------------------------------------------------------

// TestRefusal02NoFallbackArmingWithoutConfig pins the precedence contract at
// its observable boundary: with BOTH the per-agent spec.RefusalModel and the
// global models.json5 refusal_model unset, no turn — coder lane (async work)
// or analyst lane (deterministic media route) — ever arms the fallback. The
// daemon log must never show fallback arming, and no reply carries the
// disclosure marker. (With a harness refusal seam, this scenario would pin
// the spec-wins-over-global precedence directly; reported as the gap.)
func TestRefusal02NoFallbackArmingWithoutConfig(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "ref02", s.ProjectDir)

	// Coder lane (async task work).
	artifact := filepath.Join(s.ProjectDir, "ref02.txt")
	s.Fake.SetPostToolText("Created ref02.txt at " + artifact + " on the primary model.")
	s.Fake.EnqueueFileWrite("call-ref02", artifact, "precedence")
	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file named ref02.txt containing precedence")
	if turnID, _ := ack["turn_id"].(string); turnID == "" {
		t.Fatalf("refusal-02: ack missing turn_id: %+v", ack)
	}
	task := waitAnyTask(t, s)
	harness.WaitTaskCompleted(t, s.TasksDBPath(), task.ID, 120*time.Second)

	// Analyst lane (deterministic media-URL guard route, inline).
	const analystReply = "here is the video summary"
	s.Fake.SetChatText(analystReply)
	s.Fake.SetPostToolText(analystReply)
	ack2 := s.SubmitChatHTTP(t, sessionID,
		"summarize this video https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	_ = ack2

	// No fallback arming anywhere in the log.
	log := s.Daemon.LogTail()
	for _, marker := range []string{
		"refusal fallback hop armed",
		"Retrying turn on refusal fallback model",
		"refusal fallback: falling back",
	} {
		if strings.Contains(log, marker) {
			t.Fatalf("refusal-02: fallback armed without any refusal_model config (marker %q)", marker)
		}
	}
}
