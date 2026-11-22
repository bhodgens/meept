package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/models"
)

// 06-planning/01 (F13a approval visibility): the strategic approval gate
// publishes task.pending_approval (internal/agent/strategic.go
// awaitUserApproval) but the TUI dropped it - the App event switch knew
// only the plans.db family. This file owns the task-gate half:
//
//   - handlePendingApprovalEvent: session-scoped extraction of the
//     task.pending_approval payload (task_id, session_id, steps, total)
//     into a models.TaskGateApprovalMsg. An explicitly empty session_id
//     marks a task that belongs to no chat; a MISSING session_id key is
//     treated as belonging to the active transcript (legacy publishers).
//     Both session accessors are checked (App.ActiveSessionID and
//     App.ChatSessionID) - the same dual-binding rule the plan-lifecycle
//     notifications use, because the two can differ briefly during a
//     session switch.
//   - the card itself renders in models.ChatModel (RenderTaskGateCard);
//     approve/reject go through the /plan approve|reject path, which
//     forwards to task.approve / task.reject (C2; reject never executes).

// handlePendingApprovalEvent converts one task.pending_approval bus event
// into a chat-layer approval card. Returns nil when the event is not for
// this transcript (foreign session, empty explicit session_id, or a
// malformed payload) - those must never flood the active chat.
func (a *App) handlePendingApprovalEvent(payload map[string]any) tea.Cmd {
	taskID, _ := payload["task_id"].(string)
	if taskID == "" {
		return nil
	}

	// Session scoping (mirrors the plan-lifecycle handler): an explicit
	// session_id that is empty or foreign drops the event; a missing key
	// is a legacy publisher and renders best-effort into the transcript
	// that is showing.
	if sid, hasSID := payload["session_id"].(string); hasSID {
		if sid == "" || (sid != a.ActiveSessionID() && sid != a.ChatSessionID()) {
			return nil
		}
	}
	sessionID, _ := payload["session_id"].(string)

	total := 0
	if v, ok := payload["total"].(float64); ok {
		total = int(v)
	}

	var summaries []string
	if rawSteps, ok := payload["steps"].([]any); ok {
		for _, raw := range rawSteps {
			step, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			desc, _ := step["description"].(string)
			if desc == "" {
				if seq, ok := step["sequence"].(string); ok {
					desc = fmt.Sprintf("step %s", seq)
				}
			}
			summaries = append(summaries, desc)
		}
	}
	// The publisher counts steps itself; trust total over a partial
	// summaries slice so the step count line stays truthful.
	if len(summaries) > total {
		total = len(summaries)
	}

	card := models.TaskGateApprovalMsg{
		TaskID:    taskID,
		SessionID: sessionID,
		Total:     total,
		Steps:     summaries,
	}
	return a.chat.Update(card)
}

// taskGateCardRows ... (removed: the renderer lives in
// models.ChatModel.renderTaskGateCard to avoid a package cycle.)

// stringField extracts a string payload key ("" when absent or not a
// string) - shared by the task-gate event handlers.
func stringField(payload map[string]any, key string) string {
	v, _ := payload[key].(string)
	return v
}

// intField extracts a numeric payload key as an int (0 when absent or not
// numeric) - shared by the task-gate event handlers.
func intField(payload map[string]any, key string) int {
	v, _ := payload[key].(float64)
	return int(v)
}
