package activity

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/agent"
	buspkg "github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// WireBusBridge subscribes to live bus topics and maps payloads to ActivityRecord.
// Each handler converts its payload to an ActivityRecord (Kind "bus_event") and
// Append()s it to the ring in a goroutine-safe, non-blocking way. Panics in
// handlers are recovered and logged.
//
// We take the concrete *bus.MessageBus because SubscribeT is a generic function
// and cannot be expressed in a local interface. The payload->record mapping is
// factored into separate functions so they can be tested without a bus.
func WireBusBridge(bus *buspkg.MessageBus, r *Ring) {
	logger := slog.Default()

	// queue.job.* topics
	queueTopics := []string{
		"queue.job.claimed",
		"queue.job.completed",
		"queue.job.failed",
		"queue.job.retry",
		"queue.job.requeue",
		"queue.job.recovered",
	}

	for _, topic := range queueTopics {
		sub := bus.Subscribe("activity-ring", topic)
		go func(t string) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("bus handler panic recovered", "topic", t, "error", rec)
				}
			}()
			for msg := range sub.Channel {
				rec := recordFromQueuePayload(t, msg)
				if rec.Kind != "" {
					r.Append(rec)
				}
			}
		}(topic)
	}

	// task.completed and task.failed (via task.ev.* namespace)
	taskTopics := []string{"task.ev.completed", "task.ev.failed"}
	for _, topic := range taskTopics {
		sub := bus.Subscribe("activity-ring", topic)
		go func(t string) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("bus handler panic recovered", "topic", t, "error", rec)
				}
			}()
			for msg := range sub.Channel {
				rec := recordFromTaskPayload(t, msg)
				if rec.Kind != "" {
					r.Append(rec)
				}
			}
		}(topic)
	}

	// agent.progress is a raw string topic
	progSub := bus.Subscribe("activity-ring", "agent.progress")
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("bus handler panic recovered", "topic", "agent.progress", "error", rec)
			}
		}()
		for msg := range progSub.Channel {
			rec := recordFromAgentProgressPayload(msg)
			if rec.Kind != "" {
				r.Append(rec)
			}
		}
	}()

	// turn.terminal is a typed topic
	turnSub := buspkg.SubscribeT(bus, "activity-ring", agent.TopicTurnTerminal, func(ev agent.TurnTerminalEvent) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("bus handler panic recovered", "topic", "turn.terminal", "error", rec)
			}
		}()
		rec := recordFromTurnTerminalPayload(ev)
		if rec.Kind != "" {
			r.Append(rec)
		}
	})
	_ = turnSub // subscription kept alive; unsubscribe would be needed in long-running daemon
}

// recordFromQueuePayload converts a queue job bus message to an ActivityRecord.
// Payload keys: job_id, worker_id, claim_token (for completed/failed)
func recordFromQueuePayload(topic string, msg *models.BusMessage) ActivityRecord {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return ActivityRecord{}
	}

	kind := "bus_event"
	agentID := ""
	taskID := ""
	summary := ""

	if jobID, ok := payload["job_id"].(string); ok {
		taskID = jobID
	}
	if workerID, ok := payload["worker_id"].(string); ok {
		agentID = workerID
	}

	switch topic {
	case "queue.job.claimed":
		summary = "job claimed"
	case "queue.job.completed":
		summary = "job completed"
	case "queue.job.failed":
		summary = "job failed"
	case "queue.job.retry":
		summary = "job retry"
	case "queue.job.requeue":
		summary = "job requeued"
	case "queue.job.recovered":
		summary = "job recovered"
	}

	return ActivityRecord{
		Kind:    kind,
		At:      msg.Timestamp,
		AgentID: agentID,
		TaskID:  taskID,
		Summary: truncateSummary(summary),
	}
}

// recordFromTaskPayload converts a task event bus message to an ActivityRecord.
// Payload keys: task_id, status, result
func recordFromTaskPayload(topic string, msg *models.BusMessage) ActivityRecord {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return ActivityRecord{}
	}

	kind := "bus_event"
	taskID := ""
	summary := ""

	if tid, ok := payload["task_id"].(string); ok {
		taskID = tid
	}

	switch topic {
	case "task.ev.completed":
		summary = "task completed"
	case "task.ev.failed":
		summary = "task failed"
	}

	return ActivityRecord{
		Kind:    kind,
		At:      msg.Timestamp,
		TaskID:  taskID,
		Summary: truncateSummary(summary),
	}
}

// recordFromAgentProgressPayload converts an agent.progress bus message to an ActivityRecord.
// Payload keys: agent_id, conversation_id, stage, detail, token_count
func recordFromAgentProgressPayload(msg *models.BusMessage) ActivityRecord {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return ActivityRecord{}
	}

	kind := "bus_event"
	agentID := ""
	conversationID := ""
	summary := ""

	if aid, ok := payload["agent_id"].(string); ok {
		agentID = aid
	}
	if cid, ok := payload["conversation_id"].(string); ok {
		conversationID = cid
	}
	if stage, ok := payload["stage"].(string); ok {
		summary = "agent progress: " + stage
	}
	if detail, ok := payload["detail"].(string); ok && detail != "" {
		summary = truncateSummary(detail)
	}

	return ActivityRecord{
		Kind:      kind,
		At:        msg.Timestamp,
		AgentID:   agentID,
		SessionID: conversationID,
		Summary:   truncateSummary(summary),
	}
}

// recordFromTurnTerminalPayload converts a TurnTerminalEvent to an ActivityRecord.
func recordFromTurnTerminalPayload(ev agent.TurnTerminalEvent) ActivityRecord {
	kind := "bus_event"
	summary := ""
	if ev.Status != "" {
		summary = "turn " + ev.Status
	} else if ev.Reply != "" {
		summary = truncateSummary(ev.Reply)
	}

	return ActivityRecord{
		Kind:      kind,
		At:        time.Now().UTC(),
		AgentID:   ev.AgentID,
		SessionID: ev.SessionID,
		TaskID:    ev.TaskID,
		Summary:   truncateSummary(summary),
	}
}
