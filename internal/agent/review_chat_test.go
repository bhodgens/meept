package agent

// 04-clutter TEST-GATE T3 (F11a): review feedback routing pin.
//
// Frozen defect: F-ORCH-25 (MAJOR, live-verified). '## review: approved'
// messages push internal review feedback into the user chat stream: p2 B
// received chat.response frames titled '## review: approved' carrying
// reviewer feedback prose. Review feedback is internal scaffolding; it
// renders as assistant chat messages - the user sees the review machinery
// arguing with itself.
//
// Frozen evidence (VERBATIM): /tmp/usability-rig/probes/p2-isolation.json
// events[111].payload.reply (approved) and events[56].payload.reply
// (needs more info) - both chat.response frames.
//
// Contract (01-server-reply-hygiene T3): review verdicts land in task
// detail / a dedicated event ONLY; chat.response (and its chat_message WS
// push) never carries "## review:". The task.review_completed bus event
// itself keeps flowing (task-detail consumers read it).
//
// RED today: handleReviewCompleted builds the "## review:" message and
// sendResponse pushes it to chat.response for every linked session (and
// broadcasts when none are linked). GREEN after 04-clutter/01.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// p2ReviewApprovedReply is VERBATIM from p2-isolation.json
// events[111].payload.reply - a chat.response frame the user received.
const p2ReviewApprovedReply = "## review: approved\n\nThe work successfully confirmed creation of alpha_notes.txt and attempted to display its content....\n"

// p2ReviewNeedsInfoReply is VERBATIM from p2-isolation.json
// events[56].payload.reply.
const p2ReviewNeedsInfoReply = "## review: needs more info\n\n**feedback:** I cannot verify the current working directory or check for existing files. Need to explore the environment first.\n"

// reviewEventMsg builds one task.review_completed bus message.
func reviewEventMsg(t *testing.T, taskID, stepID, status, feedback string, revisionCount int) *models.BusMessage {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"task_id":        taskID,
		"step_id":        stepID,
		"status":         status,
		"feedback":       feedback,
		"confidence":     0.9,
		"reviewer":       "reviewer",
		"revision_count": revisionCount,
	})
	if err != nil {
		t.Fatalf("marshal review payload: %v", err)
	}
	return &models.BusMessage{
		ID:        "evt-review-" + stepID,
		Type:      models.MessageTypeEvent,
		Topic:     "task.review_completed",
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

// TestReviewFeedbackNotChat pins that review verdicts never reach the chat
// stream. RED today: every verdict is pushed as a "## review:" chat reply.
func TestReviewFeedbackNotChat(t *testing.T) {
	verdicts := []struct {
		name          string
		status        string
		feedback      string
		revisionCount int
	}{
		{"approved", "approved", "The work successfully confirmed creation of alpha_notes.txt and attempted to display its content.", 0},
		{"rejected", "rejected", "The step result does not match the requested deliverable.", 1},
		{"needs_info", "needs_info", "I cannot verify the current working directory or check for existing files. Need to explore the environment first.", 0},
	}

	for _, v := range verdicts {
		t.Run(v.name+"_never_pushed_to_chat", func(t *testing.T) {
			h := newTestChatHandlerWithBus(t)
			respSub := h.bus.Subscribe("review-t3-resp-"+v.name, "chat.response")
			defer h.bus.Unsubscribe(respSub)
			chatSub := h.bus.Subscribe("review-t3-chat-"+v.name, "chat_message")
			defer h.bus.Unsubscribe(chatSub)

			h.handleReviewCompleted(reviewEventMsg(t, "task-review-t3", "step-review-t3", v.status, v.feedback, v.revisionCount))

			// Drain window: the push is synchronous today, so any frame
			// arrives immediately; a short bounded wait covers async wiring.
			deadline := time.Now().Add(300 * time.Millisecond)
			for time.Now().Before(deadline) {
				select {
				case msg := <-respSub.Channel:
					var resp ChatResponse
					if err := json.Unmarshal(msg.Payload, &resp); err != nil {
						t.Fatalf("chat.response decode: %v", err)
					}
					if strings.Contains(resp.Reply, "## review:") {
						t.Fatalf("F-ORCH-25: review verdict %q pushed to chat.response as %q - "+
							"review feedback belongs in task detail/events only", v.status, resp.Reply)
					}
				case msg := <-chatSub.Channel:
					var frame map[string]any
					if err := json.Unmarshal(msg.Payload, &frame); err != nil {
						t.Fatalf("chat_message decode: %v", err)
					}
					content, _ := frame["content"].(string)
					if strings.Contains(content, "## review:") {
						t.Fatalf("F-ORCH-25: review verdict %q pushed to the chat_message WS stream: %q", v.status, content)
					}
				default:
					time.Sleep(10 * time.Millisecond)
				}
			}
		})
	}

	t.Run("linked_sessions_receive_no_review_chat", func(t *testing.T) {
		// The linked-session fan-out is the live p2 shape: every linked
		// session got its own "## review:" chat.response frame.
		taskStore, _ := newTestTaskAndStepStore(t)
		tk := task.NewTask("task-review-t3-linked", "review gate linked-session task")
		if err := taskStore.Create(tk); err != nil {
			t.Fatalf("create task: %v", err)
		}
		if err := taskStore.LinkSession(tk.ID, "sess-review-t3-a"); err != nil {
			t.Fatalf("link session a: %v", err)
		}
		if err := taskStore.LinkSession(tk.ID, "sess-review-t3-b"); err != nil {
			t.Fatalf("link session b: %v", err)
		}

		h := newTestChatHandlerWithBus(t)
		h.SetTaskStore(taskStore)
		respSub := h.bus.Subscribe("review-t3-linked-resp", "chat.response")
		defer h.bus.Unsubscribe(respSub)

		h.handleReviewCompleted(reviewEventMsg(t, tk.ID, "step-review-t3-linked", "approved", "looks good", 0))

		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
			select {
			case msg := <-respSub.Channel:
				var resp ChatResponse
				if err := json.Unmarshal(msg.Payload, &resp); err != nil {
					t.Fatalf("chat.response decode: %v", err)
				}
				if strings.Contains(resp.Reply, "## review:") {
					t.Fatalf("F-ORCH-25: linked session %q received review chat: %q", resp.ConversationID, resp.Reply)
				}
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}
	})

	t.Run("review_event_still_flows_for_task_detail", func(t *testing.T) {
		// Regression guard (GREEN today, must survive the fix): the
		// task.review_completed event itself is the task-detail feed and
		// must keep carrying the verdict + feedback. The fix reroutes the
		// CHAT push; it must not silence the event.
		h := newTestChatHandlerWithBus(t)
		reviewSub := h.bus.Subscribe("review-t3-detail", "task.review_completed")
		defer h.bus.Unsubscribe(reviewSub)

		msg := reviewEventMsg(t, "task-review-t3-detail", "step-detail", "approved", "verdict prose", 0)
		h.bus.Publish("task.review_completed", msg)

		select {
		case got := <-reviewSub.Channel:
			var payload struct {
				Status   string `json:"status"`
				Feedback string `json:"feedback"`
				TaskID   string `json:"task_id"`
			}
			if err := json.Unmarshal(got.Payload, &payload); err != nil {
				t.Fatalf("review event decode: %v", err)
			}
			if payload.Status != "approved" || payload.Feedback != "verdict prose" || payload.TaskID != "task-review-t3-detail" {
				t.Errorf("task-detail review feed lost verdict data: %+v", payload)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("task.review_completed event did not flow - the task-detail feed must survive the chat reroute")
		}
	})

	t.Run("frozen_p2_replies_are_review_chatter", func(t *testing.T) {
		// Fixture self-check (GREEN today): the frozen p2 frames ARE the
		// defect shape - both start with the review header. Pins the
		// fixtures against accidental rewording.
		for _, frozen := range []string{p2ReviewApprovedReply, p2ReviewNeedsInfoReply} {
			if !strings.HasPrefix(frozen, "## review:") {
				t.Errorf("frozen p2 fixture lost its review header: %q", frozen)
			}
		}
	})
}
