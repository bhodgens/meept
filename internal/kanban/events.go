package kanban

import (
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// StateEvent.Kind values — the machine names pinned by plan 02/03 (C1/C2).
// The event carries identity only (C1 provenance rule): the projector
// resolves goal/employee/plan/session provenance from the source store
// keyed by StateEvent.ID, never from the event.
const (
	EventKindTask = "task"
	EventKindJob  = "job"
	// Plan/step/goal kinds are the machines wired by plan leaf 02/04.
	EventKindPlan = "plan"
	EventKindStep = "step"
	EventKindGoal = "goal"
)

// StateEvent.Reason labels — short trigger names for the transition, as
// pinned by plan 02/03. Every publisher in internal/task and internal/queue
// uses one of these so consumers can switch on reason without parsing prose.
const (
	EventReasonClaim       = "claim"
	EventReasonProcessing  = "processing"
	EventReasonComplete    = "complete"
	EventReasonFail        = "fail"
	EventReasonRetry       = "retry"
	EventReasonRequeue     = "requeue"
	EventReasonCancel      = "cancel"
	EventReasonUpdateState = "update-state"
)

// StateEvent.Reason labels added by plan 02/04 for the plan, step, and goal
// machines. Each publisher uses one of these so consumers can switch on
// reason without parsing prose, same contract as the 02/03 set above.
const (
	// Plan machine.
	EventReasonCreate       = "create"        // row lands in draft (CreatePlan tail write)
	EventReasonSubmit       = "submit"        // draft -> pending_approval
	EventReasonApprove      = "approve"       // pending_approval -> approved
	EventReasonReject       = "reject"        // pending_approval -> cancelled (operator reject)
	EventReasonRevise       = "revise"        // -> planning (rework loop)
	EventReasonRevisionDone = "revision-done" // planning -> draft (revision rework completes)
	EventReasonSynthesize   = "synthesize"    // draft|approved -> synthesizing (lease claim)
	EventReasonExecuting    = "executing"     // synthesizing -> executing (expansion succeeded)
	EventReasonRelease      = "release"       // synthesizing -> draft|approved (failed expansion)
	EventReasonPark         = "park"          // synthesizing -> failed (attempts exhausted) / goal -> paused
	EventReasonReclaim      = "reclaim"       // synthesizing -> approved (expired lease reclaimed)
	EventReasonConfirm      = "confirm"       // completed|failed|cancelled -> confirmed
	EventReasonPhase        = "phase"         // plan phase state change (reserved for the phase machine; no plan-level publisher fires it)
	// Step machine (internal/task step.go): SetState writes carry the
	// generic reason the transition log already stores ("state_change");
	// SetStateWithReason carries its caller-supplied reason verbatim.
	EventReasonStateChange = "state_change"
	// Goal machine.
	EventReasonResume = "resume" // paused|blocked -> active
	EventReasonBlock  = "block"  // active|paused -> blocked (round cap / self-reported block)
	EventReasonRetire = "retire" // any -> retired
	EventReasonSteer  = "steer"  // same-state field update (assessment/steering write)
)

// C2 typed topic declarations. Publishers MUST use these vars via
// bus.PublishT (through PublishTaskState/PublishJobState below); subscribers
// MUST use bus.SubscribeT with the same var so the compiler enforces the
// frozen StateEvent payload (C1).
//
// Placement follows the internal/agent/topics.go:14 precedent: the
// declaration lives in the package that owns the payload type (kanban owns
// StateEvent), and the owning package imports bus — never the reverse.
var (
	TopicTaskState = bus.NewTopic[StateEvent]("kanban.event.task.state")
	TopicJobState  = bus.NewTopic[StateEvent]("kanban.event.job.state")
	// Plan/step/goal topics are the machines wired by plan leaf 02/04;
	// the C2 table pins the same five-topic surface.
	TopicPlanState = bus.NewTopic[StateEvent]("kanban.event.plan.state")
	TopicStepState = bus.NewTopic[StateEvent]("kanban.event.step.state")
	TopicGoalState = bus.NewTopic[StateEvent]("kanban.event.goal.state")
)

// PublishTaskState publishes a task-machine StateEvent on the
// kanban.event.task.state topic (C2). It fills the identity defaults the
// contract pins — Kind = "task" when unset, TS = time.Now().UTC() when zero —
// so every wire publisher stays a one-liner. A nil bus is a silent no-op,
// matching the nil-bus tolerance of every legacy publisher in internal/task
// and internal/queue.
func PublishTaskState(b *bus.MessageBus, ev StateEvent) {
	if b == nil {
		return
	}
	if ev.Kind == "" {
		ev.Kind = EventKindTask
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	bus.PublishT(b, TopicTaskState, "task-registry", ev)
}

// PublishJobState publishes a job-machine StateEvent on the
// kanban.event.job.state topic (C2). Same default-filling and nil-bus
// contract as PublishTaskState.
func PublishJobState(b *bus.MessageBus, ev StateEvent) {
	if b == nil {
		return
	}
	if ev.Kind == "" {
		ev.Kind = EventKindJob
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	bus.PublishT(b, TopicJobState, "queue", ev)
}

// PublishPlanState publishes a plan-machine StateEvent on the
// kanban.event.plan.state topic (C2, plan leaf 02/04). Same default-filling
// and nil-bus contract as PublishTaskState. The source string matches the
// plan manager's legacy publishEvent source ("plan-manager").
func PublishPlanState(b *bus.MessageBus, ev StateEvent) {
	if b == nil {
		return
	}
	if ev.Kind == "" {
		ev.Kind = EventKindPlan
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	bus.PublishT(b, TopicPlanState, "plan-manager", ev)
}

// PublishStepState publishes a step-machine StateEvent on the
// kanban.event.step.state topic (C2, plan leaf 02/04). Same default-filling
// and nil-bus contract as PublishTaskState. The source string names the
// step store, the only publisher wired for this machine.
func PublishStepState(b *bus.MessageBus, ev StateEvent) {
	if b == nil {
		return
	}
	if ev.Kind == "" {
		ev.Kind = EventKindStep
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	bus.PublishT(b, TopicStepState, "step-store", ev)
}

// PublishGoalState publishes a goal-machine StateEvent on the
// kanban.event.goal.state topic (C2, plan leaf 02/04). Same default-filling
// and nil-bus contract as PublishTaskState. The source string names the
// goal store, the machine's authority (the C-P3 transition fence lives here).
func PublishGoalState(b *bus.MessageBus, ev StateEvent) {
	if b == nil {
		return
	}
	if ev.Kind == "" {
		ev.Kind = EventKindGoal
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	bus.PublishT(b, TopicGoalState, "goal-store", ev)
}
