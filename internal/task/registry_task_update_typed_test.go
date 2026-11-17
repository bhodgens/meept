package task

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// shapeCase pins ONE publish site's wire payload: call fn on a fresh registry,
// then read the single notification that landed on sub.
type shapeCase struct {
	name  string
	topic string
	fn    func(ctx context.Context, r *Registry) error
}

// TestTaskUpdateTopicWireShapes is the payload-shape pin for every publish site
// migrated off hand-rolled map[string]any onto a typed bus.Topic.
//
// WHY THIS TEST EXISTS: a map payload is unchecked by the compiler — a typo in
// a key ("taks_id"), a missing key, or a changed value type compiles fine and
// fails silently in whichever consumer decodes it. These cases hold each site
// to the exact JSON it published BEFORE the migration, so the typed payload
// struct cannot drift on the wire.
//
// The typed publish path (bus.PublishT) is expected to produce the identical
// JSON key set; only the envelope's ID field is empty on that path (see
// TestTaskUpdateTypedPublishEnvelope).
func TestTaskUpdateTopicWireShapes(t *testing.T) {
	// Update writes a task in StatePending (the zero-value state of a fresh
	// task) and publishes {task_id, state}.
	updateState := func(ctx context.Context, r *Registry) error {
		tk, err := r.Create(ctx, "wire-shape", "desc")
		if err != nil {
			return err
		}
		return r.Update(ctx, tk)
	}
	// SetGoalID publishes the OTHER shape: {task_id, goal_id}, no state.
	setGoal := func(ctx context.Context, r *Registry) error {
		tk, err := r.Create(ctx, "wire-shape-goal", "")
		if err != nil {
			return err
		}
		return r.SetGoalID(ctx, tk.ID, "goal-7")
	}
	// IncrementJobCount / CompleteJob / FailJob each publish {task_id, state}
	// after mutating the row; CompleteJob's auto-complete only fires from a
	// legal source state, so seed nothing — the state published is whatever the
	// row held.
	incrementJobs := func(ctx context.Context, r *Registry) error {
		tk, err := r.Create(ctx, "wire-shape-jobs", "")
		if err != nil {
			return err
		}
		return r.IncrementJobCount(ctx, tk.ID)
	}

	for _, tc := range []shapeCase{
		{name: "Update", topic: TopicTaskUpdate.Name, fn: updateState},
		{name: "SetGoalID", topic: TopicTaskUpdate.Name, fn: setGoal},
		{name: "IncrementJobCount", topic: TopicTaskUpdate.Name, fn: incrementJobs},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msgBus := bus.New(nil, slog.Default())
			defer msgBus.Close()
			reg, err := NewRegistry(t.TempDir()+"/tasks.db", msgBus, slog.Default())
			if err != nil {
				t.Fatalf("NewRegistry: %v", err)
			}
			defer reg.Close()

			sub := msgBus.Subscribe("wire-shape-"+tc.name, tc.topic)
			time.Sleep(100 * time.Millisecond) // let the subscription register
			if err := tc.fn(context.Background(), reg); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			var raw map[string]any
			select {
			case msg := <-sub.Channel:
				if msg.Topic != "task.ev.update" {
					t.Fatalf("notification topic = %q, want task.ev.update", msg.Topic)
				}
				raw = decodeObject(t, msg.Payload)
			case <-time.After(2 * time.Second):
				t.Fatalf("%s: no notification on %s", tc.name, tc.topic)
			}

			if _, ok := raw[KeyTaskID]; !ok {
				t.Errorf("%s payload missing %q key: %v", tc.name, KeyTaskID, raw)
			}
			if id, _ := raw[KeyTaskID].(string); id == "" {
				t.Errorf("%s payload %q is not a non-empty string: %v", tc.name, KeyTaskID, raw)
			}
			// Every site either carries "state" or "goal_id" — exactly one, and
			// never both. A union struct that always emitted both would pass a
			// key-presence check but change the wire shape.
			_, hasState := raw["state"]
			_, hasGoal := raw["goal_id"]
			if hasState == hasGoal {
				t.Errorf("%s payload must carry exactly one of state/goal_id: %v", tc.name, raw)
			}
		})
	}
}

// TestTaskUpdateTypedPayloadWireShape marshals the typed payload structs and
// asserts the exact key sets the pre-migration maps produced. This is the
// compile-time-checkable half of the contract: these structs ARE the wire.
func TestTaskUpdateTypedPayloadWireShape(t *testing.T) {
	cases := []struct {
		name    string
		payload any
		want    []string
	}{
		{"state", TaskUpdateEvent{TaskID: "tk-1", State: "pending"}, []string{"task_id", "state"}},
		{"goal", TaskGoalLinkedEvent{TaskID: "tk-1", GoalID: "goal-1"}, []string{"task_id", "goal_id"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			raw := decodeObject(t, data)
			if len(raw) != len(tc.want) {
				t.Fatalf("payload keys = %v, want exactly %v", keysOf(raw), tc.want)
			}
			for _, k := range tc.want {
				if _, ok := raw[k]; !ok {
					t.Errorf("payload missing key %q: %v", k, raw)
				}
			}
			if tc.name == "state" && raw["state"] != "pending" {
				t.Errorf("state = %v, want \"pending\"", raw["state"])
			}
			if tc.name == "goal" && raw["goal_id"] != "goal-1" {
				t.Errorf("goal_id = %v, want \"goal-1\"", raw["goal_id"])
			}
		})
	}
}

// TestTaskUpdateTypedPublishEnvelope pins what the typed path does to the
// BusMessage envelope. Type/Source are part of the contract (a message with an
// empty Type is no longer an "event" to any consumer that switches on it) and
// match what models.NewBusMessage produced before the migration.
//
// ID is intentionally EMPTY on the typed path: bus.PublishT constructs the
// message itself and sets no ID, so every typed publish in the repo has an
// empty ID. No task.ev.* consumer reads it (the WS relay copies ID into the
// frame only for chat_message topics), so this is pinned as expected rather
// than "fixed" here.
func TestTaskUpdateTypedPublishEnvelope(t *testing.T) {
	msgBus := bus.New(nil, slog.Default())
	defer msgBus.Close()
	reg, err := NewRegistry(t.TempDir()+"/tasks.db", msgBus, slog.Default())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	defer reg.Close()

	sub := msgBus.Subscribe("typed-envelope", TopicTaskUpdate.Name)
	time.Sleep(100 * time.Millisecond)

	tk, err := reg.Create(context.Background(), "envelope", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := reg.Update(context.Background(), tk); err != nil {
		t.Fatalf("Update: %v", err)
	}

	select {
	case msg := <-sub.Channel:
		if msg.Type != models.MessageTypeEvent {
			t.Errorf("notification type = %q, want %q", msg.Type, models.MessageTypeEvent)
		}
		if msg.Source != taskEventSource {
			t.Errorf("notification source = %q, want %q", msg.Source, taskEventSource)
		}
		if msg.Timestamp.IsZero() {
			t.Error("notification timestamp is zero; the pre-migration path stamped it")
		}
		if msg.ID != "" {
			t.Errorf("notification ID = %q, want empty (typed publishes set no ID)", msg.ID)
		}
		raw := decodeObject(t, msg.Payload)
		if raw[KeyTaskID] != tk.ID {
			t.Errorf("payload task_id = %v, want %q", raw[KeyTaskID], tk.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no notification on task.ev.update")
	}
}

func decodeObject(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("payload is not a JSON object: %v (%s)", err, data)
	}
	return raw
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
