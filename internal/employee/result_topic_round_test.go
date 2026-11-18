package employee

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/agent"
)

// The execution-result tier is a property of the TRIGGER, not of the
// GoalLoop. A single bus_event trigger must not permanently reclassify
// later goal/manual rounds of the same entity as bot.result (issue-140).
func TestResultTopic_IsPerRoundNotSticky(t *testing.T) {
	loop := NewGoalLoop("emp-topic-test", testTier1Constitution(), nil, nil)
	m := NewManager(nil)

	// Start with a stale bot marker, as a previous bus trigger would leave.
	loop.SetResultTopic(agent.TopicBotResult.Name)

	// A goal round must reset the marker to employee.result.
	loop.mu.Lock()
	before := loop.resultTopic
	loop.mu.Unlock()
	if before != agent.TopicBotResult.Name {
		t.Fatalf("test setup: marker = %q, want bot.result", before)
	}

	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"x","description":"d","prompt":"p"}]}`)
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)
	executor := newStubExecutor()
	executor.succeedWith("done", 10)
	loop.WithReflector(reflector).WithExecutor(executor)

	_, err := m.triggerViaGoalLoop(context.Background(), "emp-topic-test",
		map[string]any{"source": "goal"}, "tier_1_reactive", loop)
	if err != nil {
		t.Fatalf("triggerViaGoalLoop(goal): %v", err)
	}
	loop.mu.Lock()
	got := loop.resultTopic
	loop.mu.Unlock()
	if got != agent.TopicEmployeeResult.Name {
		t.Fatalf("after goal round: resultTopic = %q, want %q", got, agent.TopicEmployeeResult.Name)
	}

	// A bus_event round must set bot.result again.
	reflector2 := newStubReflector()
	reflector2.queueResponse(`{"candidates":[{"title":"x","description":"d","prompt":"p"}]}`)
	reflector2.queueResponse(`{"health":"healthy","reasoning":"ok"}`)
	executor2 := newStubExecutor()
	executor2.succeedWith("done", 10)
	loop.WithReflector(reflector2).WithExecutor(executor2)

	_, err = m.triggerViaGoalLoop(context.Background(), "emp-topic-test",
		map[string]any{"source": "bus_event"}, "tier_1_reactive", loop)
	if err != nil {
		t.Fatalf("triggerViaGoalLoop(bus_event): %v", err)
	}
	loop.mu.Lock()
	got = loop.resultTopic
	loop.mu.Unlock()
	if got != agent.TopicBotResult.Name {
		t.Fatalf("after bus_event round: resultTopic = %q, want %q", got, agent.TopicBotResult.Name)
	}
}
