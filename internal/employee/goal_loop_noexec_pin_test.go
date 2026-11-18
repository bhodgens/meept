package employee

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
)

// A GoalLoop with no executor cannot execute. It must NOT emit a running
// turn-state edge: without it observers would keep the agent in "running"
// forever (issue-140 observability contract).
func TestGoalLoop_NoExecutorEmitsNoRunningEdge(t *testing.T) {
	b := bus.New(nil, nil)
	defer b.Close()
	sub := b.Subscribe("no-executor-turn", agent.TopicTurnState.Name)
	defer bus.Unsubscribe(b, sub)

	loop := NewGoalLoop("no-exec-emp", testTier1Constitution(), nil, nil)
	loop.SetBus(b)

	if _, err := loop.Execute(context.Background(), PlanRef{ID: "p-no-exec", State: "executing"}); err == nil {
		t.Fatal("Execute without executor must return an error")
	}
	select {
	case msg := <-sub.Channel:
		t.Fatalf("unexecutable round emitted a turn-state edge: %s", msg.Payload)
	default:
	}
}
