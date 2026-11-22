package agent

// 20261121-interactive-usability leaf 05 (degraded-signals) gate tests.
//
// Pins:
//  1. Degraded classification methods (heuristic_fallback, fallback) append
//     the one-line lowercase honesty note to the terminal reply; healthy
//     lanes (llm, keyword, semantic) keep the reply byte-identical.
//  2. formatTaskCompletedMessage renders the planning-degraded note and the
//     persisted plan_quality marker when present, and is byte-identical to
//     the legacy card when no marker exists.
//  3. planQualityFromMetadata reads the persisted 'plan_quality' key.
//  4. markPlanDegraded persists the marker (read back via
//     readPersistedPlanQuality).
//  5. cooldownForAttempt: first unavailability cooldown is short (15s) and
//     regrows exponentially to the 60s cap.

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

func TestDegradedClassifierNote_AppendedOnFallback(t *testing.T) {
	degraded := []string{"heuristic_fallback", "fallback"}
	for _, method := range degraded {
		reply := "all done, file written"
		intent := &Intent{Method: method}
		got := appendDegradedClassifierNote(reply, intent)
		if got == reply {
			t.Errorf("method %q: note not appended", method)
		}
		if !strings.Contains(got, "[routed by keyword fallback - classification degraded]") {
			t.Errorf("method %q: exact note text missing: %q", method, got)
		}
		if !strings.HasPrefix(got, reply) {
			t.Errorf("method %q: original reply must be preserved as prefix, got %q", method, got)
		}
	}
}

func TestDegradedClassifierNote_ByteIdenticalOnHealthyLanes(t *testing.T) {
	healthy := []string{"llm", "keyword", "semantic", "git_verb_agreement_veto", "short_message_guard", "llm_empty_fallback_chat", ""}
	for _, method := range healthy {
		reply := "healthy reply, untouched"
		intent := &Intent{Method: method}
		if got := appendDegradedClassifierNote(reply, intent); got != reply {
			t.Errorf("method %q: reply must stay byte-identical, got %q", method, got)
		}
	}
	if got := appendDegradedClassifierNote("reply", nil); got != "reply" {
		t.Errorf("nil intent: reply must stay byte-identical, got %q", got)
	}
}

func TestDegradedClassifierNote_Idempotent(t *testing.T) {
	intent := &Intent{Method: "heuristic_fallback"}
	once := appendDegradedClassifierNote("done", intent)
	twice := appendDegradedClassifierNote(once, intent)
	if once != twice {
		t.Errorf("note must not duplicate; once=%q twice=%q", once, twice)
	}
}

func TestPlanQualityMarkerRenderedInTaskCompletedCard(t *testing.T) {
	h := newTestChatHandlerWithBus(t)
	steps := []TaskStepSummary{{
		ID:          "step-1",
		Description: "do the thing",
		State:       "completed",
	}}
	got := h.formatTaskCompletedMessage("do a thing", steps, "12s", "", 1, 1, 0, "degraded: deterministic single-step")
	if !strings.Contains(got, "[planning degraded: single direct step]") {
		t.Errorf("degraded-planning note missing from completion card: %q", got)
	}
	if !strings.Contains(got, "degraded: deterministic single-step") {
		t.Errorf("persisted plan_quality marker not rendered: %q", got)
	}

	// No marker: the card is byte-identical to the legacy shape.
	noMarker := h.formatTaskCompletedMessage("do a thing", steps, "12s", "", 1, 1, 0, "")
	legacy := h.formatTaskCompletedMessage("do a thing", steps, "12s", "", 1, 1, 0, "")
	if noMarker != legacy {
		t.Errorf("empty plan_quality must leave the card byte-identical")
	}
	if strings.Contains(noMarker, "planning degraded") || strings.Contains(noMarker, "plan quality") {
		t.Errorf("no-marker card must not carry degraded copy: %q", noMarker)
	}
}

func TestPlanQualityFromMetadata(t *testing.T) {
	if got := planQualityFromMetadata([]byte(`{"plan_quality":"degraded: deterministic single-step"}`)); got != "degraded: deterministic single-step" {
		t.Errorf("planQualityFromMetadata = %q", got)
	}
	if got := planQualityFromMetadata([]byte(`{"other":"x"}`)); got != "" {
		t.Errorf("absent key must read empty, got %q", got)
	}
	if got := planQualityFromMetadata(nil); got != "" {
		t.Errorf("nil metadata must read empty, got %q", got)
	}
	if got := planQualityFromMetadata([]byte(`not json`)); got != "" {
		t.Errorf("unparsable metadata must read empty, got %q", got)
	}
}

func TestMarkPlanDegradedPersistsMarker(t *testing.T) {
	sp, _ := gateDraftSurfaceWorld(t)
	tk := task.NewTask("create a file", "create a file named hello.txt")
	if err := sp.taskStore.Create(tk); err != nil {
		t.Fatalf("task create: %v", err)
	}
	sp.markPlanDegraded(tk.ID, "degraded: deterministic single-step")
	if got := sp.readPersistedPlanQuality(tk.ID); got != "degraded: deterministic single-step" {
		t.Errorf("readPersistedPlanQuality = %q, want the marker", got)
	}
}

func TestCooldownForAttempt(t *testing.T) {
	first := cooldownForAttempt(0, 15*time.Second, 60*time.Second)
	if first != 15*time.Second {
		t.Errorf("first cooldown = %s, want 15s", first)
	}
	second := cooldownForAttempt(1, 15*time.Second, 60*time.Second)
	if second != 30*time.Second {
		t.Errorf("second cooldown = %s, want 30s", second)
	}
	third := cooldownForAttempt(2, 15*time.Second, 60*time.Second)
	if third != 60*time.Second {
		t.Errorf("third cooldown = %s, want 60s (cap reached)", third)
	}
	capped := cooldownForAttempt(10, 15*time.Second, 60*time.Second)
	if capped != 60*time.Second {
		t.Errorf("capped cooldown = %s, want 60s", capped)
	}
}
