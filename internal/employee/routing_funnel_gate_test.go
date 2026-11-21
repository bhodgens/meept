package employee

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
)

// TestPlanRefTopicSurvivesLaterBusEvent pins C1: the result topic is a
// property of the round, stamped when the round starts. A later bus event
// that retargets the shared GoalLoop flag must not retarget an already
// stamped round.
//
// PlanRef.ResultTopic is the stamp. A later SetResultTopic on the loop
// must not change the topic Execute publishes. The test fails today
// because Execute still reads the live loop flag.
func TestPlanRefTopicSurvivesLaterBusEvent(t *testing.T) {
	loop := NewGoalLoop("emp-stamp", &Constitution{Purpose: "pin the stamp"}, nil, slog.New(slog.DiscardHandler))
	loop.WithExecutor(&emitGoalExecutor{output: "done", tokens: 1})
	loop.WithRetryPolicy(RetryPolicy{}) // no backoff if the executor fails

	msgBus := bus.New(nil, nil)
	t.Cleanup(func() { msgBus.Close() })
	loop.SetBus(msgBus)

	botSub := subscribeGateRaw(t, msgBus, "c1-bot", agent.TopicBotResult.Name)
	empSub := subscribeGateRaw(t, msgBus, "c1-employee", agent.TopicEmployeeResult.Name)
	t.Cleanup(func() {
		msgBus.Unsubscribe(botSub)
		msgBus.Unsubscribe(empSub)
	})

	// Stamp the round on the plan, then let a later bus event move the live flag.
	loop.SetResultTopic(agent.TopicEmployeeResult.Name)
	desired := agent.TopicBotResult.Name

	_, err := loop.Execute(context.Background(), PlanRef{
		ID:          "plan-stamp",
		State:       "approved",
		Prompt:      "execute the stamped round",
		ApproverID:  "system",
		ResultTopic: desired,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	select {
	case msg := <-botSub.Channel:
		if msg.Topic != desired {
			t.Fatalf("published topic %q, want %q", msg.Topic, desired)
		}
	case msg := <-empSub.Channel:
		t.Fatalf("published topic %q, want %q (Execute read the live GoalLoop flag after SetResultTopic(%q))",
			msg.Topic, desired, agent.TopicEmployeeResult.Name)
	case <-time.After(2 * time.Second):
		t.Fatalf("no execution result published; want topic %q", desired)
	}
}
