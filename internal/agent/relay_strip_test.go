package agent

// 04-clutter TEST-GATE T2 (F11a): relay envelope strip pin.
//
// Frozen defect: F-ORCH-21 (MAJOR, live-verified). task_completed_relay
// ships the raw claims/evidence JSON envelope inside the user reply:
// 'job job-... completed by agent coder: { "status": "completed",
// "accomplished": [...], ... }'. The relay path (formatTaskCompletedMessage
// / bestSummaryResult -> StripClaimsEvidenceOrProse) strips the legacy
// {"claims","evidence"} shape but MISSES the
// {"status","accomplished","not_done","issues","observations"} shape the
// executor actually emits.
//
// Frozen evidence (VERBATIM, no rewording): /tmp/usability-rig/probes/
// p6-positive-control.json - events[52].payload.result.response (the full
// step-result envelope), events[52].payload.result.evidence[0] (the
// job-prefixed, truncated envelope as rendered), turns[0].terminal.payload
// .reply (the defective user-visible reply).
//
// Contract (01-server-reply-hygiene T2): extend StripClaimsEvidenceOrProse
// IN PLACE (never a parallel stripper); prose recovered; no raw JSON keys
// in the reply; legacy envelopes still stripped; raw step results preserved
// in the store (strip is display-only - pinned e2e in output-filters-06).
//
// RED today: the envelope shape passes through the stripper untouched and
// renders raw in both the completion card and the turn.terminal relay.
// GREEN after 04-clutter/01.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// p6StepResultEnvelope is VERBATIM from p6-positive-control.json
// events[52].payload.result.response - the executor's full step result.
const p6StepResultEnvelope = "{\n  \"status\": \"completed\",\n  \"accomplished\": [\n    \"Created file /private/tmp/usability-rig/project/hello.txt with content 'hello world' using file_write with direct:true\",\n    \"File exists and contains 11 bytes\"\n  ],\n  \"not_done\": [],\n  \"issues\": [],\n  \"observations\": [\n    \"The file was successfully written to the project directory as required\"\n  ],\n  \"suggested_next_agent\": \"\",\n  \"user_decision_needed\": false\n}"

// p6JobEvidence is VERBATIM from p6-positive-control.json
// events[52].payload.result.evidence[0] - the job-prefixed envelope as it
// reached the relay (truncated mid-observations by the upstream preview
// cap; the ellipsis is part of the frozen bytes). The stripper must recover
// prose even from this truncated live shape.
const p6JobEvidence = "job job-20261119231406.561752000-0001 completed by agent coder: {\n  \"status\": \"completed\",\n  \"accomplished\": [\n    \"Created file /private/tmp/usability-rig/project/hello.txt with content 'hello world' using file_write with direct:true\",\n    \"File exists and contains 11 bytes\"\n  ],\n  \"not_done\": [],\n  \"issues\": [],\n  \"observations\": [\n    \"The file was successful\u2026"

// p6Turn1Reply is VERBATIM from p6-positive-control.json
// turns[0].terminal.payload.reply - the defective reply the user saw.
const p6Turn1Reply = "## task completed: create a file named hello.\n\n### steps:\n1. [+] create a file named hello.txt in the project directory with the text 'hello world'. use file_write with direct:true. tell me the full path.\n\n**result:**\n\n- job job-20261119231406.561752000-0001 completed by agent coder: {\n  \"status\": \"completed\",\n  \"accomplished\": [\n    \"Created file /private/tmp/usability-rig/project/hello.txt with content 'hello world' using file_write with direct:true\",\n    \"File exists and contains 11 bytes\"\n  ],\n  \"not_done\": [],\n  \"issues\": [],\n  \"observations\": [\n    \"The file was successful\u2026\n\n**summary:** Completed 1/1 steps: Create a file named hello.txt in the ...\n\ncompleted in 12s\n"

// rawEnvelopeKeys are the machine keys that must NEVER render in a
// user-facing reply (F-ORCH-21).
var rawEnvelopeKeys = []string{
	`"status":`,
	`"accomplished":`,
	`"not_done":`,
	`"issues":`,
	`"observations":`,
	`"suggested_next_agent":`,
	`"user_decision_needed":`,
}

func assertNoRawEnvelopeKeys(t *testing.T, label, reply string) {
	t.Helper()
	for _, key := range rawEnvelopeKeys {
		if strings.Contains(reply, key) {
			t.Errorf("%s: raw envelope key %s leaked into the user reply (F-ORCH-21):\n%s", label, key, reply)
		}
	}
}

// TestRelayEnvelopeStrip pins prose recovery from the
// status/accomplished/not_done/issues/observations envelope on every relay
// render seam. RED today: the shape is not recognized by the stripper.
func TestRelayEnvelopeStrip(t *testing.T) {
	t.Run("best_summary_recovers_prose_from_full_envelope", func(t *testing.T) {
		steps := []TaskStepSummary{{
			ID:          "step-p6-1",
			Description: "create a file named hello.txt in the project directory with the text 'hello world'. use file_write with direct:true. tell me the full path.",
			State:       "completed",
			Result:      p6StepResultEnvelope,
		}}
		got := bestSummaryResult(steps)
		if strings.TrimSpace(got) == "" {
			t.Fatal("bestSummaryResult returned empty for an envelope carrying accomplished prose")
		}
		assertNoRawEnvelopeKeys(t, "bestSummaryResult", got)
		if !strings.Contains(got, "Created file /private/tmp/usability-rig/project/hello.txt") {
			t.Errorf("accomplished prose not recovered: %q", got)
		}
		if !strings.Contains(got, "File exists and contains 11 bytes") {
			t.Errorf("second accomplished item not recovered: %q", got)
		}
	})

	t.Run("completion_card_recovers_prose_from_truncated_job_evidence", func(t *testing.T) {
		// The exact live render path: formatTaskCompletedMessage with the
		// job-prefixed truncated envelope as the step result (p6 shape).
		h := newTestChatHandlerWithBus(t)
		steps := []TaskStepSummary{{
			ID:          "step-p6-1",
			Description: "create a file named hello.txt in the project directory with the text 'hello world'. use file_write with direct:true. tell me the full path.",
			State:       "completed",
			Result:      p6JobEvidence,
		}}
		got := h.formatTaskCompletedMessage("create a file named hello.", steps,
			"12s", "Completed 1/1 steps: Create a file named hello.txt in the ...", 1, 1, 0)
		assertNoRawEnvelopeKeys(t, "formatTaskCompletedMessage", got)
		if !strings.Contains(got, "hello.txt") {
			t.Errorf("card lost the artifact prose entirely: %q", got)
		}
		if got == p6Turn1Reply {
			t.Error("card is byte-identical to the frozen defective p6 reply - the envelope still renders raw")
		}
	})

	t.Run("task_completed_relay_terminal_and_chat_are_clean", func(t *testing.T) {
		// End-to-end through the bus handler: task.completed -> chat.response
		// push AND turn.terminal relay must both carry prose, never keys.
		h := newTestChatHandlerWithBus(t)
		respSub := h.bus.Subscribe("relay-strip-t2-resp", "chat.response")
		defer h.bus.Unsubscribe(respSub)
		termSub := h.bus.Subscribe("relay-strip-t2-term", "turn.terminal")
		defer h.bus.Unsubscribe(termSub)

		payload, err := json.Marshal(map[string]any{
			"task_id":         "task-20261119231402.439929000-0001",
			"name":            "create a file named hello.",
			"completed_jobs":  1,
			"total_jobs":      1,
			"linked_sessions": []string{"sess-relay-strip-t2"},
			"steps": []TaskStepSummary{{
				ID:          "step-p6-1",
				Description: "create a file named hello.txt in the project directory with the text 'hello world'. use file_write with direct:true. tell me the full path.",
				State:       "completed",
				Result:      p6JobEvidence,
			}},
			"execution_time": "12s",
			"result":         "Completed 1/1 steps: Create a file named hello.txt in the ...",
		})
		if err != nil {
			t.Fatalf("marshal task.completed payload: %v", err)
		}
		h.handleTaskCompleted(&models.BusMessage{
			ID:        "evt-relay-strip-t2",
			Type:      models.MessageTypeEvent,
			Topic:     "task.completed",
			Source:    "test",
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		})

		select {
		case msg := <-respSub.Channel:
			var resp ChatResponse
			if err := json.Unmarshal(msg.Payload, &resp); err != nil {
				t.Fatalf("chat.response decode: %v", err)
			}
			assertNoRawEnvelopeKeys(t, "chat.response relay", resp.Reply)
			if !strings.Contains(resp.Reply, "hello.txt") {
				t.Errorf("chat.response relay lost the artifact prose: %q", resp.Reply)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no chat.response push for the linked session")
		}

		select {
		case msg := <-termSub.Channel:
			var ev TurnTerminalEvent
			if err := json.Unmarshal(msg.Payload, &ev); err != nil {
				t.Fatalf("turn.terminal decode: %v", err)
			}
			if ev.HandlerCase != "task_completed_relay" || ev.Status != "completed" {
				t.Fatalf("relay event shape: handler_case=%q status=%q", ev.HandlerCase, ev.Status)
			}
			assertNoRawEnvelopeKeys(t, "turn.terminal relay", ev.Reply)
			if !strings.Contains(ev.Reply, "hello.txt") {
				t.Errorf("turn.terminal relay lost the artifact prose: %q", ev.Reply)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no turn.terminal relay emitted")
		}
	})

	t.Run("legacy_claims_evidence_envelope_still_stripped", func(t *testing.T) {
		// Regression guard (GREEN today, must survive the extension): the
		// legacy {"claims","evidence"} shape keeps its prose recovery.
		legacy := `{"claims":["Created hello.txt at /var/work/project/hello.txt"],` +
			`"evidence":[{"type":"file_exists","path":"/var/work/project/hello.txt"}]}`
		steps := []TaskStepSummary{{State: "completed", Result: legacy}}
		got := bestSummaryResult(steps)
		if strings.Contains(got, `"claims"`) || strings.Contains(got, `"evidence"`) {
			t.Errorf("legacy envelope leaked: %q", got)
		}
		if !strings.Contains(got, "Created hello.txt at /var/work/project/hello.txt") {
			t.Errorf("legacy claims prose lost: %q", got)
		}
	})

	t.Run("plain_prose_passes_byte_identical", func(t *testing.T) {
		// Regression guard (GREEN today): a prose-only result is untouched.
		const prose = "The answer is 4."
		steps := []TaskStepSummary{{State: "completed", Result: prose}}
		if got := bestSummaryResult(steps); got != prose {
			t.Errorf("prose altered: got %q, want %q", got, prose)
		}
	})

	t.Run("stripper_directly_recovers_full_envelope", func(t *testing.T) {
		// The relay renders after the turn ctx may be done; the stripper
		// is pure and must recover prose from the full envelope shape.
		got := StripClaimsEvidenceOrProse(p6StepResultEnvelope)
		assertNoRawEnvelopeKeys(t, "StripClaimsEvidenceOrProse", got)
		if !strings.Contains(got, "Created file /private/tmp/usability-rig/project/hello.txt") {
			t.Errorf("stripper did not recover the accomplished prose: %q", got)
		}
	})
}
