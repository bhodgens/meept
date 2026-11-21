package bus

// 01-safety TEST GATE T5 (00T-gate.md): blocking publishers for critical
// topics (C6).
//
// task.completed / task.failed / task.rejected and plan.completed /
// plan.rejected / plan.failed are per-task-once terminal events: losing
// one to a full subscriber buffer breaks every awaiting client's state
// machine (the serial bus-drop terminal loss, F-NEW-3). C6 requires the
// PublishBlockingT path (5s bounded wait) for these topics, exactly like
// turn.terminal already uses.
//
// This pin exercises the DELIVERY CONTRACT the producers must rely on:
// with a subscriber buffer deliberately FULL, a blocking publish of a
// critical payload still lands once room is made (bounded wait), while
// the legacy non-blocking Publish drops it. The producer-side wiring
// (tactical.go publishEvent / plan manager publishEvent -> blocking for
// the critical topic set) is pinned by the agent/plan gate tests; this
// file pins the bus-level seam they must route through.
//
// RED today: undefined IsCriticalTopic / CriticalTopics (the C6 topic
// set does not exist as a declared contract in the bus package).

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// TestBlockingPublishersCriticalTopicSet pins the C6 topic list as a
// declared, testable contract: every per-task/plan-once terminal topic
// is critical; progress chatter is not.
func TestBlockingPublishersCriticalTopicSet(t *testing.T) {
	critical := []string{
		"turn.terminal",
		"task.completed",
		"task.failed",
		"task.rejected",
		"plan.completed",
		"plan.rejected",
		"plan.failed",
	}
	for _, topic := range critical {
		if !IsCriticalTopic(topic) {
			t.Errorf("CURRENT DEFECT: %q is not declared critical; C6 requires PublishBlockingT + trim exemption for it", topic)
		}
	}
	for _, topic := range []string{"agent.progress", "task.progress", "step.started", "llm.call"} {
		if IsCriticalTopic(topic) {
			t.Errorf("%q must NOT be critical (progress chatter stays drop-oldest)", topic)
		}
	}
}

// TestBlockingPublishersDeliverUnderFullBuffer pins the delivery shape:
// a critical event published through PublishBlockingT survives a full
// subscriber buffer (bounded wait + drain), where non-blocking Publish
// drops it. This is the seam the tactical/plan producers must use.
func TestBlockingPublishersDeliverUnderFullBuffer(t *testing.T) {
	quiet := slog.New(slog.DiscardHandler)
	mb := New(&Config{BufferSize: 1}, quiet)
	defer mb.Close()

	sub := mb.Subscribe("critical-gate", "task.completed")
	defer mb.Unsubscribe(sub)

	// Fill the single-slot buffer with a stale record.
	stale := &models.BusMessage{Type: models.MessageTypeEvent, Source: "gate", Payload: json.RawMessage(`{"n":0}`)}
	if got := mb.Publish("task.completed", stale); got != 1 {
		t.Fatalf("fill publish delivered %d, want 1", got)
	}

	// Non-blocking publish onto the full buffer DROPS (the defect shape
	// the producers exhibit today via plain Publish).
	lost := &models.BusMessage{Type: models.MessageTypeEvent, Source: "gate", Payload: json.RawMessage(`{"n":1}`)}
	if got := mb.Publish("task.completed", lost); got != 0 {
		t.Fatalf("non-blocking publish onto full buffer delivered %d, want 0 (drop)", got)
	}
	if sub.DroppedCount() != 1 {
		t.Fatalf("dropped count = %d, want 1", sub.DroppedCount())
	}

	// The blocking path waits for room: drain the stale record shortly
	// after publishing, and the critical event must still arrive.
	done := make(chan int, 1)
	go func() {
		done <- mb.PublishBlocking("task.completed", &models.BusMessage{
			Type:    models.MessageTypeEvent,
			Source:  "gate",
			Payload: json.RawMessage(`{"task_id":"task-crit"}`),
		})
	}()

	// Give the blocking publish a moment to reach its wait, then drain.
	time.Sleep(50 * time.Millisecond)
	select {
	case <-sub.Channel:
	case <-time.After(2 * time.Second):
		t.Fatal("stale record never drained")
	}

	select {
	case got := <-done:
		if got != 1 {
			t.Fatalf("blocking publish delivered %d, want 1 (bounded wait must deliver the critical event)", got)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("blocking publish never completed")
	}
	select {
	case msg := <-sub.Channel:
		var payload struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.TaskID != "task-crit" {
			t.Fatalf("critical payload lost: %s (%v)", msg.Payload, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("critical event never arrived after the blocking publish")
	}
}
