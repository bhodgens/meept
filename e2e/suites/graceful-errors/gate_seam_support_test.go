//go:build e2e

package gracefulerrors

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// Gate seam for the e2e graceful-errors suite (leaf 06): the e2e tier
// cannot reach the unexported test fixtures in internal/agent, so the
// suite constructs a minimal real planner through exported constructors
// only. Production callers never touch this file.

// NewStrategicPlannerForGate builds a StrategicPlanner over a real task
// store + bus with a single planner loop served by the given chatter --
// the smallest wiring that exercises Plan end to end hermetically.
//
// The planner loop is built DIRECTLY (not via registry.createLoop):
// createLoop wires a chatter only when the registry carries a shared LLM
// client or manager, neither of which exists in this hermetic rig. With
// the registry's PlannerChatter empty, the loop would answer "no LLM
// client configured" and the empty-plan lane would never run.
// WithLLMChatter(plannerChatter) is the exact opt the daemon's
// planner_model slot uses, so this is the production wiring shape with
// the fake in the slot. The registry (spec-only) is still needed: Plan
// resolves the planner through registry.Get.
func NewStrategicPlannerForGate(t *testing.T, msgBus *bus.MessageBus, taskStore *task.Store, plannerChatter llm.Chatter) *agent.StrategicPlanner {
	t.Helper()
	reg := agent.NewAgentRegistry(agent.RegistryConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		// Registry-built loops (GetForTask lazy-create) answer through
		// the fake: SharedChatter is the createLoop branch for a rig
		// with no shared *llm.Client (createLoop falls to
		// WithLLMChatter(r.sharedChatter) when PlannerChatter is empty).
		SharedChatter: plannerChatter,
	})
	if err := reg.RegisterSpec(&agent.AgentSpec{
		ID:      config.AgentIDPlanner,
		Name:    "Planner",
		Role:    agent.RoleExecutor,
		Enabled: true,
	}); err != nil {
		t.Fatalf("RegisterSpec: %v", err)
	}

	planner := agent.NewStrategicPlanner(agent.StrategicPlannerConfig{
		Registry:       reg,
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	gateStores[planner] = gateAccess{taskStore: taskStore, stepStore: taskStore.StepStore()}
	return planner
}

// gateAccess carries the stores for a gate-built planner. The planner's
// own fields are unexported; the suite asserts through the SAME store
// pointers it handed in (they are shared, not copies).
type gateAccess struct {
	taskStore *task.Store
	stepStore *task.StepStore
}

var gateStores = map[*agent.StrategicPlanner]gateAccess{}

func gateTaskStore(sp *agent.StrategicPlanner) *task.Store {
	return gateStores[sp].taskStore
}

func gateStepStore(sp *agent.StrategicPlanner) *task.StepStore {
	return gateStores[sp].stepStore
}

func gatePlanQuality(sp *agent.StrategicPlanner, taskID string) string {
	// The degraded marker is persisted on the task metadata; read it
	// through the store the suite handed the planner.
	tsk, err := gateTaskStore(sp).GetByID(taskID)
	if err != nil || tsk == nil {
		return ""
	}
	var m map[string]any
	if len(tsk.Metadata) > 0 {
		if err := json.Unmarshal(tsk.Metadata, &m); err != nil {
			return ""
		}
	}
	if v, ok := m["plan_quality"].(string); ok {
		return v
	}
	return ""
}
