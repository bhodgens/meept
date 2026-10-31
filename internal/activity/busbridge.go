package activity

import (
	"encoding/json"
	"fmt"
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
func WireBusBridge(bus *buspkg.MessageBus, r *Ring) []*buspkg.Subscriber {
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

	var subscribers []*buspkg.Subscriber

	for _, topic := range queueTopics {
		sub := bus.Subscribe("activity-ring", topic)
		subscribers = append(subscribers, sub)
		go func(t string) {
			for msg := range sub.Channel {
				func() {
					defer func() {
						if rec := recover(); rec != nil {
							logger.Error("bus handler panic recovered", "topic", t, "error", rec)
						}
					}()
					rec := recordFromQueuePayload(t, msg)
					if rec.Kind != "" {
						r.Append(rec)
					}
				}()
			}
		}(topic)
	}

	// task.completed and task.failed (bare topics, NOT task.ev.* namespace)
	taskTopics := []string{"task.completed", "task.failed"}
	for _, topic := range taskTopics {
		sub := bus.Subscribe("activity-ring", topic)
		subscribers = append(subscribers, sub)
		go func(t string) {
			for msg := range sub.Channel {
				func() {
					defer func() {
						if rec := recover(); rec != nil {
							logger.Error("bus handler panic recovered", "topic", t, "error", rec)
						}
					}()
					rec := recordFromTaskPayload(t, msg)
					if rec.Kind != "" {
						r.Append(rec)
					}
				}()
			}
		}(topic)
	}

	// agent.progress is a raw string topic
	progSub := bus.Subscribe("activity-ring", "agent.progress")
	subscribers = append(subscribers, progSub)
	go func() {
		for msg := range progSub.Channel {
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Error("bus handler panic recovered", "topic", "agent.progress", "error", rec)
					}
				}()
				rec := recordFromAgentProgressPayload(msg)
				if rec.Kind != "" {
					r.Append(rec)
				}
			}()
		}
	}()

	// turn.terminal is a typed topic
	turnSub := buspkg.SubscribeT(bus, "activity-ring", agent.TopicTurnTerminal, func(ev agent.TurnTerminalEvent) {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("bus handler panic recovered", "topic", "turn.terminal", "error", rec)
				}
			}()
			rec := recordFromTurnTerminalPayload(ev)
			if rec.Kind != "" {
				r.Append(rec)
			}
		}()
	})
	subscribers = append(subscribers, turnSub)

	return subscribers
}

// recordFromQueuePayload converts a queue job bus message to an ActivityRecord.
// Payload keys: job_id, worker_id (queue job IDs are NOT task IDs; worker_id is NOT an agent_id)
func recordFromQueuePayload(topic string, msg *models.BusMessage) ActivityRecord {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return ActivityRecord{}
	}

	kind := "bus_event"
	detail := make(map[string]string)

	// Queue job IDs are in a different identifier space from tasks.id
	// Do NOT populate TaskID; put job_id in Detail instead
	if jobID, ok := payload["job_id"].(string); ok {
		detail["job_id"] = jobID
	}
	// worker_id is not an agent_id; put it in Detail
	if workerID, ok := payload["worker_id"].(string); ok {
		detail["worker_id"] = workerID
	}

	var summary string
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
		Detail:  detail,
		Summary: truncateSummary(summary),
	}
}

// recordFromTaskPayload converts a task event bus message to an ActivityRecord.
// Payload keys: task_id (for task.completed), completed_jobs, failed_jobs (for task.failed)
func recordFromTaskPayload(topic string, msg *models.BusMessage) ActivityRecord {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return ActivityRecord{}
	}

	kind := "bus_event"
	detail := make(map[string]string)
	taskID := ""
	summary := ""

	switch topic {
	case "task.completed":
		summary = "task completed"
		if tid, ok := payload["task_id"].(string); ok {
			taskID = tid
		}
	case "task.failed":
		summary = "task failed"
		// task.failed does NOT carry task_id; put counts in Detail
		// JSON numbers unmarshal as float64, so we handle that
		if v, ok := payload["completed_jobs"].(float64); ok {
			detail["completed_jobs"] = fmt.Sprintf("%d", int(v))
		}
		if v, ok := payload["failed_jobs"].(float64); ok {
			detail["failed_jobs"] = fmt.Sprintf("%d", int(v))
		}
		if v, ok := payload["total_jobs"].(float64); ok {
			detail["total_jobs"] = fmt.Sprintf("%d", int(v))
		}
	}

	return ActivityRecord{
		Kind:    kind,
		At:      msg.Timestamp,
		TaskID:  taskID,
		Detail:  detail,
		Summary: truncateSummary(summary),
	}
}

// recordFromAgentProgressPayload converts an agent.progress bus message to an ActivityRecord.
// Payload keys: conversation_id, stage, detail, agent_id (for ACP), token_count
func recordFromAgentProgressPayload(msg *models.BusMessage) ActivityRecord {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return ActivityRecord{}
	}

	kind := "bus_event"
	detail := make(map[string]string)
	agentID := ""
	summary := ""

	if aid, ok := payload["agent_id"].(string); ok {
		agentID = aid
	}
	if cid, ok := payload["conversation_id"].(string); ok {
		detail["conversation_id"] = cid
	}
	if stage, ok := payload["stage"].(string); ok {
		summary = "agent progress: " + stage
	}
	if detailStr, ok := payload["detail"].(string); ok && detailStr != "" {
		summary = truncateSummary(detailStr)
	}

	return ActivityRecord{
		Kind:    kind,
		At:      msg.Timestamp,
		AgentID: agentID,
		Detail:  detail,
		Summary: truncateSummary(summary),
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
		At:        time.Now().UTC(), // TurnTerminalEvent has no timestamp field; use now
		AgentID:   ev.AgentID,
		SessionID: ev.SessionID,
		TaskID:    ev.TaskID,
		Summary:   truncateSummary(summary),
	}
}
