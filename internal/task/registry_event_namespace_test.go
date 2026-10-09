package task

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// TestPublishEventNeverTargetsACommandTopic is the regression pin for the
// 2026-10-08 runaway: 513,000,000 task rows (150 GB tasks.db) and a 145 GB
// meept.log at ~10,000-14,000 rows/second, filling a 927 GB disk to 95%.
//
// ROOT CAUSE: the registry published its own state-change NOTIFICATIONS on the
// SAME topics its request handler subscribes to. NewHandler subscribes
// "task.create" (among others) and routes every message on it through
// handleCreate -> registry.Create. SubscriptionHandler.Start applies no
// message-type filter (internal/bus/handler.go:43-56), so the
// MessageTypeEvent that publishEvent emitted on "task.create" was executed as a
// command: the handler read the notification's own "name" field and created a
// task, which published another "task.create", forever. That is why every row
// was named after an evolver plan title — the title rode along in the payload.
func TestPublishEventNeverTargetsACommandTopic(t *testing.T) {
	// The event namespace must be disjoint from the command namespace.
	if !strings.HasPrefix(taskEventPrefix, "task.ev.") {
		t.Fatalf("taskEventPrefix = %q, want a task.ev. prefix", taskEventPrefix)
	}
	if got := taskEventTopic("task.create"); got != "task.ev.create" {
		t.Fatalf("taskEventTopic(task.create) = %q, want %q", got, "task.ev.create")
	}

	// Every command topic the handler subscribes must map OUTSIDE that set.
	commandTopics := []string{
		"task.create", "task.get", "task.update", "task.cancel", "task.delete",
		"task.list", "task.list_extended", "task.link", "task.unlink", "task.steps",
	}
	for _, ct := range commandTopics {
		ev := taskEventTopic(ct)
		if ev == ct {
			t.Fatalf("command topic %q maps to itself; a notification there is "+
				"executed as a command and self-replicates", ct)
		}
	}
}

// TestCreateDoesNotSelfReplicate is the behavioural half: driving a real bus
// with the real handler must create exactly ONE task per Create call, not an
// unbounded chain. This is the assertion that could not be written before the
// fix, because the cascade never stopped.
func TestCreateDoesNotSelfReplicate(t *testing.T) {
	msgBus := bus.New(nil, slog.Default())
	reg, err := NewRegistry(t.TempDir()+"/tasks.db", msgBus, slog.Default())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	h := NewHandler(reg, msgBus, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.Start(ctx); err != nil {
		t.Fatalf("handler Start: %v", err)
	}
	// Let the subscriptions register before publishing.
	time.Sleep(200 * time.Millisecond)

	if _, err := reg.Create(ctx, "Skill evolution: archive yuanbao", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Before the fix, one Create produced an unbounded cascade on this topic.
	// Wait far longer than a single round trip.
	time.Sleep(2 * time.Second)

	// limit must be positive: List(nil, 0) applies LIMIT 0 and returns nothing.
	tasks, err := reg.store.List(nil, 20)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		names := make([]string, 0, len(tasks))
		for _, tk := range tasks {
			names = append(names, tk.Name)
		}
		t.Fatalf("one Create produced %d tasks (want exactly 1) — the notification "+
			"is being executed as a command and self-replicating: %v", len(tasks), names)
	}
	if tasks[0].Name != "Skill evolution: archive yuanbao" {
		t.Fatalf("task name = %q", tasks[0].Name)
	}
}

// TestPublishEventRefusesCommandTopic proves the guard inside publishEvent
// actually refuses a command topic, rather than only documenting the rule.
func TestPublishEventRefusesCommandTopic(t *testing.T) {
	msgBus := bus.New(nil, slog.Default())
	reg, err := NewRegistry(t.TempDir()+"/tasks.db", msgBus, slog.Default())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	sub := msgBus.Subscribe("test-observer", "task.create")
	time.Sleep(100 * time.Millisecond)

	// Direct violation: publish an event on a command topic.
	reg.publishEvent("task.create", map[string]any{"name": "should not appear"})
	time.Sleep(400 * time.Millisecond)

	if n := len(sub.Channel); n != 0 {
		t.Fatalf("publishEvent accepted a command topic; %d message(s) published", n)
	}
}

// TestEventNotificationsStillPublished proves the fix did not silence
// notifications: they must still be emitted, on the new namespace.
func TestEventNotificationsStillPublished(t *testing.T) {
	msgBus := bus.New(nil, slog.Default())
	reg, err := NewRegistry(t.TempDir()+"/tasks.db", msgBus, slog.Default())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	sub := msgBus.Subscribe("obs", taskEventTopic("task.create"))
	time.Sleep(100 * time.Millisecond)

	if _, err := reg.Create(context.Background(), "a-task", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	time.Sleep(400 * time.Millisecond)

	if len(sub.Channel) == 0 {
		t.Fatal("no notification was published on the event namespace; the fix " +
			"silenced events instead of separating them")
	}
	select {
	case msg := <-sub.Channel:
		if msg.Topic != "task.ev.create" {
			t.Fatalf("unexpected topic %q", msg.Topic)
		}
		if msg.Type != "event" {
			t.Fatalf("notification type = %q, want event", msg.Type)
		}
	default:
		t.Fatal("notification channel empty")
	}
}
