package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

// fakeAgentHealth is a scripted AgentHealthSource for tests.
type fakeAgentHealth struct {
	parked map[string]bool
}

func (f *fakeAgentHealth) AgentParkedOrCooling(agentID string) bool {
	return f.parked[agentID]
}

// Compile-time assertion: the fake satisfies the quota-aware health contract.
var _ AgentHealthSource = (*fakeAgentHealth)(nil)

// drainWarning waits for one routing.warning event on sub, returning its
// payload; ok=false on timeout (no event arrived).
func drainWarning(sub *bus.Subscriber, wait time.Duration) (map[string]any, bool) {
	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			return nil, false
		}
		return payload, true
	case <-time.After(wait):
		return nil, false
	}
}

// TestAgentQuotaAware_Setter verifies the setter stores a non-nil source,
// survives a nil call without panicking (house nil-guard rule: nil is
// ignored, never clears), and that the stored source is consultable.
func TestAgentQuotaAware_Setter(t *testing.T) {
	ts := NewTacticalScheduler(TacticalSchedulerConfig{})

	if ts.health != nil {
		t.Fatal("health source should default to nil (legacy behavior)")
	}

	fake := &fakeAgentHealth{parked: map[string]bool{"coder": true}}
	ts.SetAgentHealthSource(fake)

	if ts.health == nil {
		t.Fatal("SetAgentHealthSource did not store the source")
	}
	if !ts.health.AgentParkedOrCooling("coder") {
		t.Error("stored source should report coder parked")
	}
	if ts.health.AgentParkedOrCooling("analyst") {
		t.Error("stored source should report analyst healthy")
	}

	// nil is ignored: no panic, previously-wired source stays active.
	ts.SetAgentHealthSource(nil)
	if ts.health == nil {
		t.Error("SetAgentHealthSource(nil) must not clear a wired source")
	}
}

// TestAgentQuotaAware_SelectAgentWarning covers the selectAgent warning
// behavior: the hint table's route is NEVER changed — a parked agent's
// endpoint only publishes a routing.warning event alongside the unchanged
// route. Nil health source = byte-identical legacy behavior.
func TestAgentQuotaAware_SelectAgentWarning(t *testing.T) {
	tests := []struct {
		name         string
		health       *fakeAgentHealth // nil = leave ts.health unwired
		hint         string
		wantAgent    string
		wantWarning  bool
		wantParkedID string // when wantWarning, the agent_id in the payload
	}{
		{
			name:         "parked agent publishes warning, route unchanged",
			health:       &fakeAgentHealth{parked: map[string]bool{"coder": true}},
			hint:         "code",
			wantAgent:    "coder",
			wantWarning:  true,
			wantParkedID: "coder",
		},
		{
			name:        "healthy agent publishes no warning",
			health:      &fakeAgentHealth{parked: map[string]bool{}},
			hint:        "code",
			wantAgent:   "coder",
			wantWarning: false,
		},
		{
			name:        "nil health source is legacy: no event, no panic",
			health:      nil,
			hint:        "code",
			wantAgent:   "coder",
			wantWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskStore, err := task.NewStore(t.TempDir()+"/tasks.db", nil)
			if err != nil {
				t.Fatalf("failed to create task store: %v", err)
			}
			defer taskStore.Close()

			msgBus := bus.New(nil, slogDiscardLogger())
			ts := NewTacticalScheduler(TacticalSchedulerConfig{
				StepStore: taskStore.StepStore(),
				TaskStore: taskStore,
				Bus:       msgBus,
				Logger:    slogDiscardLogger(),
			})
			if tt.health != nil {
				ts.SetAgentHealthSource(tt.health)
			}

			sub := msgBus.Subscribe("test-quota-aware", "routing.warning")
			defer msgBus.Unsubscribe(sub)

			step := &task.TaskStep{ID: "step-quota-aware-1", ToolHint: tt.hint}
			gotAgent := ts.selectAgent(step)

			if gotAgent != tt.wantAgent {
				t.Errorf("selectAgent route = %q, want %q (route must never change)", gotAgent, tt.wantAgent)
			}

			payload, got := drainWarning(sub, 500*time.Millisecond)
			if tt.wantWarning {
				if !got {
					t.Fatal("expected a routing.warning event, got none")
				}
				if payload["agent_id"] != tt.wantParkedID {
					t.Errorf("warning agent_id = %v, want %q", payload["agent_id"], tt.wantParkedID)
				}
				if payload["step_id"] != step.ID {
					t.Errorf("warning step_id = %v, want %q", payload["step_id"], step.ID)
				}
				if payload["reason"] != "endpoint_parked_or_cooling" {
					t.Errorf("warning reason = %v, want %q", payload["reason"], "endpoint_parked_or_cooling")
				}
			} else if got {
				t.Fatalf("unexpected routing.warning event: %v", payload)
			}
		})
	}
}
