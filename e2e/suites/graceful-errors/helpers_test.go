//go:build e2e

package gracefulerrors

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// Gate-suite construction helpers, mirroring the in-package
// newEmptyPlanTestPlanner shape (the e2e tier cannot reach the unexported
// agent test fixtures).

func slogDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newGatePlanner builds a StrategicPlanner over real stores + bus with a
// canned planner chatter, via the exported gate seam the suite consumes.
func newGatePlanner(t *testing.T, chatter llm.Chatter) (*agent.StrategicPlanner, *bus.MessageBus, *bus.Subscriber) {
	t.Helper()
	msgBus := bus.New(nil, slogDiscardLogger())
	t.Cleanup(func() { msgBus.Close() })

	taskStore, err := task.NewStore(t.TempDir()+"/tasks.db", slogDiscardLogger())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() { taskStore.Close() })

	sp := NewStrategicPlannerForGate(t, msgBus, taskStore, chatter)
	failedSub := msgBus.Subscribe("gate-observer", "task.failed")
	return sp, msgBus, failedSub
}

// lastFailedReason reports the latest task.failed payload error observed
// on the subscriber ("" when none arrived).
func lastFailedReason(t *testing.T, sub *bus.Subscriber) string {
	t.Helper()
	select {
	case msg := <-sub.Channel:
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Logf("task.failed payload unparsable: %v", err)
		}
		return payload.Error
	default:
		return ""
	}
}
