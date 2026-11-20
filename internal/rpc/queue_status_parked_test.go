package rpc

// 05-navigation TEST-GATE T6 (leaf 00T-gate.md, matrix F12c; leaf
// 03-restore-queue-visibility T2; audit B5).
//
// Pins queue_status parked/planning visibility: chat.queue_status must
// report the session's non-terminal TASK states (task_states
// []{task_id,state,name}), not just the running-loop queue depths.
// Today a parked/planning/awaiting_approval task with no active loop
// answers all zeros - the work is invisible (B5).
//
// RED today: handleStatus returns only steering_depth/followup_depth/
// is_active/generation; no task_states key exists. The lookup must be
// session-scoped (never cross-session) and work with NO active queue.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/task"
)

// gateParkedStore opens a real task store in the test's temp dir and
// seeds one planning + one awaiting_approval task linked to sess-parked,
// plus one executing task linked to a DIFFERENT session (the
// cross-session leak probe).
func gateParkedStore(t *testing.T) *task.Store {
	t.Helper()
	store, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), nil)
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	now := time.Now().UTC()
	seed := []struct {
		id, name, session string
		state             task.TaskState
	}{
		{"task-plan-1", "quartz planning work", "sess-parked", task.StatePlanning},
		{"task-approval-1", "quartz approval wait", "sess-parked", task.StateAwaitingApproval},
		{"task-other-session", "foreign executing work", "sess-other", task.StateExecuting},
	}
	for _, s := range seed {
		tk := &task.Task{
			ID:        s.id,
			Name:      s.name,
			State:     s.state,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := store.Create(tk); err != nil {
			t.Fatalf("create task %s: %v", s.id, err)
		}
		if err := store.LinkSession(s.id, s.session); err != nil {
			t.Fatalf("link task %s: %v", s.id, err)
		}
	}
	return store
}

// gateQueueStatus calls chat.queue_status through the registered handler
// (the same dispatch the TUI/GUI queue views use) and decodes the result.
func gateQueueStatus(t *testing.T, h *QueueHandler, convID string) map[string]any {
	t.Helper()
	params, err := json.Marshal(map[string]any{"conversation_id": convID})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	result, err := h.handleStatus(context.Background(), params)
	if err != nil {
		t.Fatalf("handleStatus: %v", err)
	}
	resp, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("handleStatus result is %T, want map", result)
	}
	return resp
}

// TestQueueStatusParked pins the parked/planning visibility contract:
// with NO active loop queue, queue_status still lists the session's
// non-terminal tasks with their states.
func TestQueueStatusParked(t *testing.T) {
	store := gateParkedStore(t)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{TaskStore: store})
	t.Cleanup(func() { reg.Close() })
	h := NewQueueHandler(reg)

	resp := gateQueueStatus(t, h, "sess-parked")

	raw, present := resp["task_states"]
	if !present {
		t.Fatalf("queue_status has no task_states key - parked/planning work is invisible (B5; zeros-only payload): %v", resp)
	}
	rows, ok := raw.([]map[string]any)
	if !ok {
		// Accept the JSON-decoded shape too (handler may build []any).
		list, listOK := raw.([]any)
		if !listOK {
			t.Fatalf("task_states is %T, want a row list", raw)
		}
		rows = make([]map[string]any, 0, len(list))
		for _, r := range list {
			rm, rmOK := r.(map[string]any)
			if !rmOK {
				t.Fatalf("task_states row is %T, want object", r)
			}
			rows = append(rows, rm)
		}
	}

	byID := map[string]map[string]any{}
	for _, row := range rows {
		id, _ := row["task_id"].(string)
		if id == "" {
			t.Errorf("task_states row missing task_id: %v", row)
			continue
		}
		for _, key := range []string{"state", "name"} {
			if v, _ := row[key].(string); v == "" {
				t.Errorf("task_states row %s missing %s (contract: {task_id,state,name}): %v", id, key, row)
			}
		}
		byID[id] = row
	}

	plan, ok := byID["task-plan-1"]
	if !ok {
		t.Errorf("planning task task-plan-1 not listed in task_states (parked/planning invisible; B5): %v", rows)
	} else if plan["state"] != "planning" {
		t.Errorf("task-plan-1 state = %v, want planning", plan["state"])
	}
	approval, ok := byID["task-approval-1"]
	if !ok {
		t.Errorf("awaiting_approval task task-approval-1 not listed in task_states: %v", rows)
	} else if approval["state"] != "awaiting_approval" {
		t.Errorf("task-approval-1 state = %v, want awaiting_approval", approval["state"])
	}

	// Session scoping: the other session's task must never leak.
	if _, leaked := byID["task-other-session"]; leaked {
		t.Errorf("task_states leaked a task from another session (must be session-scoped): %v", rows)
	}
}

// TestQueueStatusParkedWithActiveQueue pins the combined arm: an active
// steering queue AND parked tasks both surface (depths keep working,
// task_states is additive - C3 resolution unchanged).
func TestQueueStatusParkedWithActiveQueue(t *testing.T) {
	store := gateParkedStore(t)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{TaskStore: store})
	t.Cleanup(func() { reg.Close() })

	q := agent.NewMessageQueue(agent.WithQueueAgentID("gate"))
	reg.RegisterActiveQueue("sess-parked", q, nil)
	if err := q.Steer(context.Background(), "change approach", "gate"); err != nil {
		t.Fatalf("steer: %v", err)
	}

	h := NewQueueHandler(reg)
	resp := gateQueueStatus(t, h, "sess-parked")

	if depth, _ := resp["steering_depth"].(int); depth != 1 {
		t.Errorf("steering_depth = %v, want 1 (existing depth contract must survive)", resp["steering_depth"])
	}
	if _, present := resp["task_states"]; !present {
		t.Errorf("queue_status with an active queue still omits task_states (parked work invisible beside the loop; B5): %v", resp)
	}
}
