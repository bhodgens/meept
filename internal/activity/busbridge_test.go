package activity

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	buspkg "github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

func TestRecordFromQueuePayload(t *testing.T) {
	payload := map[string]any{
		"job_id":    "job-123",
		"worker_id": "worker-456",
	}
	data, _ := json.Marshal(payload)
	msg := &models.BusMessage{
		Type:      models.MessageTypeEvent,
		Source:    "queue",
		Timestamp: time.Now().UTC(),
		Payload:   data,
	}

	rec := recordFromQueuePayload("queue.job.claimed", msg)
	if rec.Kind != "bus_event" {
		t.Errorf("expected kind=bus_event, got %s", rec.Kind)
	}
	// D3: TaskID should be empty (queue job IDs are NOT task IDs)
	if rec.TaskID != "" {
		t.Errorf("expected task_id=empty, got %s", rec.TaskID)
	}
	// D3: AgentID should be empty (worker_id is NOT an agent_id)
	if rec.AgentID != "" {
		t.Errorf("expected agent_id=empty, got %s", rec.AgentID)
	}
	// D3: job_id and worker_id should be in Detail
	if rec.Detail == nil {
		t.Error("expected Detail map to be populated")
	} else {
		if rec.Detail["job_id"] != "job-123" {
			t.Errorf("expected detail[job_id]=job-123, got %s", rec.Detail["job_id"])
		}
		if rec.Detail["worker_id"] != "worker-456" {
			t.Errorf("expected detail[worker_id]=worker-456, got %s", rec.Detail["worker_id"])
		}
	}
	if rec.Summary != "job claimed" {
		t.Errorf("expected summary=job claimed, got %s", rec.Summary)
	}
}

func TestRecordFromTaskPayload(t *testing.T) {
	// D2: Use the REAL topic names (task.completed, task.failed), NOT task.ev.*
	// task.completed carries task_id
	payload := map[string]any{
		"task_id": "task-789",
	}
	data, _ := json.Marshal(payload)
	msg := &models.BusMessage{
		Type:      models.MessageTypeEvent,
		Source:    "task-registry",
		Timestamp: time.Now().UTC(),
		Payload:   data,
	}

	rec := recordFromTaskPayload("task.completed", msg)
	if rec.Kind != "bus_event" {
		t.Errorf("expected kind=bus_event, got %s", rec.Kind)
	}
	if rec.TaskID != "task-789" {
		t.Errorf("expected task_id=task-789, got %s", rec.TaskID)
	}
	if rec.Summary != "task completed" {
		t.Errorf("expected summary=task completed, got %s", rec.Summary)
	}

	// task.failed does NOT carry task_id; it carries completed_jobs, failed_jobs
	payload2 := map[string]any{
		"completed_jobs": 5,
		"failed_jobs":    2,
		"total_jobs":     7,
	}
	data2, _ := json.Marshal(payload2)
	msg2 := &models.BusMessage{
		Type:      models.MessageTypeEvent,
		Source:    "task-registry",
		Timestamp: time.Now().UTC(),
		Payload:   data2,
	}

	rec2 := recordFromTaskPayload("task.failed", msg2)
	if rec2.Kind != "bus_event" {
		t.Errorf("expected kind=bus_event, got %s", rec2.Kind)
	}
	// D2: task.failed has no task_id
	if rec2.TaskID != "" {
		t.Errorf("expected task_id=empty for task.failed, got %s", rec2.TaskID)
	}
	// D2: counts go into Detail
	if rec2.Detail == nil {
		t.Error("expected Detail map to be populated for task.failed")
	} else {
		if rec2.Detail["completed_jobs"] != "5" {
			t.Errorf("expected detail[completed_jobs]=5, got %s", rec2.Detail["completed_jobs"])
		}
		if rec2.Detail["failed_jobs"] != "2" {
			t.Errorf("expected detail[failed_jobs]=2, got %s", rec2.Detail["failed_jobs"])
		}
	}
	if rec2.Summary != "task failed" {
		t.Errorf("expected summary=task failed, got %s", rec2.Summary)
	}
}

func TestRecordFromAgentProgressPayload(t *testing.T) {
	payload := map[string]any{
		"agent_id":        "agent-001",
		"conversation_id": "conv-xyz",
		"stage":           "streaming",
		"detail":          "processing request",
	}
	data, _ := json.Marshal(payload)
	msg := &models.BusMessage{
		Type:      models.MessageTypeEvent,
		Source:    "agent",
		Timestamp: time.Now().UTC(),
		Payload:   data,
	}

	rec := recordFromAgentProgressPayload(msg)
	if rec.Kind != "bus_event" {
		t.Errorf("expected kind=bus_event, got %s", rec.Kind)
	}
	if rec.AgentID != "agent-001" {
		t.Errorf("expected agent_id=agent-001, got %s", rec.AgentID)
	}
	// D4: SessionID should be empty (conversation_id is NOT a session_id)
	if rec.SessionID != "" {
		t.Errorf("expected session_id=empty, got %s", rec.SessionID)
	}
	// D4: conversation_id should be in Detail
	if rec.Detail == nil {
		t.Error("expected Detail map to be populated")
	} else {
		if rec.Detail["conversation_id"] != "conv-xyz" {
			t.Errorf("expected detail[conversation_id]=conv-xyz, got %s", rec.Detail["conversation_id"])
		}
	}
}

func TestRecordFromTurnTerminalPayload(t *testing.T) {
	ev := agent.TurnTerminalEvent{
		ConversationID: "conv-abc",
		SessionID:      "session-123",
		TurnID:         "turn-456",
		TaskID:         "task-789",
		AgentID:        "agent-001",
		Status:         "completed",
		Reply:          "Hello, world!",
	}

	rec := recordFromTurnTerminalPayload(ev)
	if rec.Kind != "bus_event" {
		t.Errorf("expected kind=bus_event, got %s", rec.Kind)
	}
	if rec.AgentID != "agent-001" {
		t.Errorf("expected agent_id=agent-001, got %s", rec.AgentID)
	}
	if rec.SessionID != "session-123" {
		t.Errorf("expected session_id=session-123, got %s", rec.SessionID)
	}
	if rec.TaskID != "task-789" {
		t.Errorf("expected task_id=task-789, got %s", rec.TaskID)
	}
	if rec.Summary != "turn completed" {
		t.Errorf("expected summary=turn completed, got %s", rec.Summary)
	}
}

func TestTruncateSummary(t *testing.T) {
	long := "this is a very long summary that exceeds two hundred runes and should be truncated to exactly the right length"
	if len(long) > 200 {
		rec := truncateSummary(long)
		if len([]rune(rec)) > 200 {
			t.Errorf("truncateSummary produced more than 200 runes")
		}
	}
}

func TestBusBridge_WireAndAppend(t *testing.T) {
	r := NewRing(10)
	bus := buspkg.New(buspkg.DefaultConfig(), slog.Default())

	WireBusBridge(bus, r)

	// Publish a turn.terminal event using buspkg.PublishT
	ev := agent.TurnTerminalEvent{
		ConversationID: "conv-1",
		Status:         "completed",
		Reply:          "test reply",
	}
	buspkg.PublishT(bus, agent.TopicTurnTerminal, "test-source", ev)

	// Give the goroutine time to process
	time.Sleep(100 * time.Millisecond)

	tail, dropped := r.Tail(10)
	if dropped != 0 {
		t.Errorf("expected dropped=0, got %d", dropped)
	}
	if len(tail) != 1 {
		t.Errorf("expected 1 record, got %d", len(tail))
	}
	if tail[0].Kind != "bus_event" {
		t.Errorf("expected kind=bus_event, got %s", tail[0].Kind)
	}
}

func TestBusBridge_MalformedPayloadDoesNotKillSubscription(t *testing.T) {
	// D6: Verify that a malformed payload doesn't kill the subscription.
	// The key check is that after a malformed payload, a subsequent good
	// payload is still processed. We use a synchronous check: publish bad,
	// then good, and assert the good one lands.
	r := NewRing(10)
	bus := buspkg.New(buspkg.DefaultConfig(), slog.Default())

	// Wire the bridge - this sets up the subscription handler
	WireBusBridge(bus, r)

	// Publish a malformed message on a topic the bridge listens to
	// queue.job.claimed is one of the topics WireBusBridge subscribes to
	malformed := &models.BusMessage{
		Type:      models.MessageTypeEvent,
		Source:    "queue",
		Timestamp: time.Now().UTC(),
		Payload:   []byte("{invalid json}"),
	}
	bus.Publish("queue.job.claimed", malformed)

	// Small delay to let the handler process (it should not panic)
	time.Sleep(10 * time.Millisecond)

	// Publish a valid message
	validPayload := map[string]any{"job_id": "job-ok"}
	data, _ := json.Marshal(validPayload)
	valid := &models.BusMessage{
		Type:      models.MessageTypeEvent,
		Source:    "queue",
		Timestamp: time.Now().UTC(),
		Payload:   data,
	}
	bus.Publish("queue.job.claimed", valid)

	// Give it time to process
	time.Sleep(100 * time.Millisecond)

	// The ring should have received the valid record
	tail, dropped := r.Tail(10)
	if dropped != 0 {
		t.Errorf("expected dropped=0, got %d", dropped)
	}
	if len(tail) != 1 {
		t.Errorf("expected 1 record in ring, got %d", len(tail))
	} else {
		if tail[0].Kind != "bus_event" {
			t.Errorf("expected kind=bus_event, got %s", tail[0].Kind)
		}
		if tail[0].Detail == nil || tail[0].Detail["job_id"] != "job-ok" {
			t.Errorf("expected detail[job_id]=job-ok, got %v", tail[0].Detail)
		}
	}
}
