package services

// Steering-fix gate (07-bench residual, p5): chat.steer / chat.followup
// returned ErrQueueNotFound for EVERY conversation in the production rig
// because the daemon singleton template loop was built before
// NewAgentRegistry and never received it -- every direct-mode chat turn
// (ChatHandler.sessionLoop -> Manager.GetOrCreateWired clone) then had
// agentRegistry == nil && queue == nil, so RunOnceWithParts' per-turn queue
// registration guard never fired and the services lookup always failed.
//
// These tests exercise the PRODUCTION wiring path, not a hand-registered
// queue: template loop -> SetAgentRegistry (the new daemon boot seam) ->
// manager clone via ConfigSnapshot -> ChatService.Steer/FollowUp into the
// LIVE queue of a turn that is actively running.

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// steerSpyChatter answers every turn with a fixed reply and runs a hook
// while the turn's queue registration is live (mirrors queueSpyChatter in
// internal/agent/queue_lifecycle_test.go).
type steerSpyChatter struct {
	hook func()
}

func (s *steerSpyChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	if s.hook != nil {
		s.hook()
	}
	return &llm.Response{Content: "steer-gate turn done"}, nil
}

func (s *steerSpyChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return s.Chat(ctx, messages, opts...)
}

func (s *steerSpyChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "steer-spy"}
}

// steerGateRegistry builds the registry the daemon boot constructs (message
// bus + queues enabled, mirroring the config defaults in
// internal/config/schema.go DefaultConfig Queues block).
func steerGateRegistry(msgBus *bus.MessageBus) *agent.AgentRegistry {
	return agent.NewAgentRegistry(agent.RegistryConfig{
		MessageBus: msgBus,
		Logger:     slog.New(slog.DiscardHandler),
		Queues: config.AgentQueuesConfig{
			Enabled:         true,
			SteeringDrain:   "one",
			FollowUpDrain:   "one",
			MaxSteering:     1,
			MaxFollowUp:     20,
			PersistFollowUp: false,
		},
	})
}

// steerGateChatService builds the ChatService the daemon wires
// (services.NewRegistry passes c.AgentRegistry -- the SAME instance the
// template loop must now hold).
func steerGateChatService(msgBus *bus.MessageBus, reg *agent.AgentRegistry) *ChatService {
	var logger *slog.Logger
	return NewChatService(msgBus, reg, logger)
}

// TestSteerDuringActiveTurnThroughProductionWiring pins the p5 fix end to
// end: an UNWIRED template (exactly the pre-fix daemon shape), late-wired
// via SetAgentRegistry, cloned through the manager the way
// ChatHandler.sessionLoop does, with a REAL turn running -- chat.steer must
// land in the live queue while the turn is mid-flight.
func TestSteerDuringActiveTurnThroughProductionWiring(t *testing.T) {
	msgBus := bus.New(nil, nil)
	reg := steerGateRegistry(msgBus)
	defer reg.Close()

	// Pre-fix daemon shape: template WITHOUT registry and WITHOUT queue
	// (components.go constructed it before NewAgentRegistry existed).
	template := agent.NewAgentLoop("sess-template-steer", t.TempDir(),
		agent.WithLLMChatter(&steerSpyChatter{}),
	)
	if template.SteeringQueue() != nil {
		t.Fatal("precondition: template must start queue-less (the daemon wiring gap this fix closes)")
	}

	// The NEW boot seam: late-attach after registry construction.
	template.SetAgentRegistry(reg)
	if template.SteeringQueue() == nil {
		t.Fatal("SetAgentRegistry must mint the template's steering queue")
	}

	// Production chat path: sessionLoop clones via the manager.
	mgr := agent.NewManager(agent.ManagerConfig{})
	clone, err := mgr.GetOrCreateWired("conv-steer-gate", t.TempDir(), template)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}
	if clone.SteeringRegistry() == nil {
		t.Fatal("clone must inherit the registry via ConfigSnapshot")
	}
	if clone.SteeringQueue() == nil {
		t.Fatal("clone must inherit a queue via ConfigSnapshot")
	}
	if clone.SteeringQueue() == template.SteeringQueue() {
		t.Fatal("clone must NOT share the template's queue instance (C3 isolation)")
	}

	svc := steerGateChatService(msgBus, reg)

	steered := make(chan error, 2)
	var liveQueue *agent.MessageQueue
	spy := &steerSpyChatter{hook: func() {
		// Mid-turn: the production Steer call (services -> registry ->
		// live queue). This is the exact RPC surface the p5 probe drove.
		steered <- svc.Steer(context.Background(), SteerRequest{
			ConversationID: "conv-steer-gate",
			Message:        "mid-turn correction: skip step4",
			Source:         "probe",
		})
		if q, _ := reg.GetActiveQueue("conv-steer-gate"); q != nil {
			liveQueue = q
		}
	}}

	// Wire the hook-bearing chatter into the CLONE (the clone copies the
	// template's chatter; replace it so the hook runs inside THIS turn).
	clone.SetChatter(spy)

	done := make(chan error, 1)
	go func() {
		_, err := clone.RunOnce(context.Background(), "long running task", "conv-steer-gate")
		done <- err
	}()

	var steerErr error
	select {
	case steerErr = <-steered:
	case <-time.After(10 * time.Second):
		t.Fatal("steer hook never ran (turn did not reach the spy chatter)")
	}
	if steerErr != nil {
		t.Fatalf("chat.steer into a LIVE turn failed through production wiring: %v", steerErr)
	}

	if err := <-done; err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// The steered message must have been IN the live queue (Steer accepted
	// mid-turn, drain site consumed it before turn end). The turn-end
	// unregister legitimately closes the queue, so the post-turn assertion
	// runs against the queue captured inside the turn.
	if liveQueue == nil {
		t.Fatal("no queue was registered during the turn (registration guard never fired)")
	}
	drained := liveQueue.DrainSteering()
	if len(drained) != 1 || drained[0].Content != "mid-turn correction: skip step4" {
		t.Errorf("drained steering = %+v, want the mid-turn correction exactly once", drained)
	}
}

// TestFollowUpDuringActiveTurnThroughProductionWiring covers chat.followup
// on the same production path (the p5 probe's sibling RPC).
func TestFollowUpDuringActiveTurnThroughProductionWiring(t *testing.T) {
	msgBus := bus.New(nil, nil)
	reg := steerGateRegistry(msgBus)
	defer reg.Close()

	template := agent.NewAgentLoop("sess-template-follow", t.TempDir(),
		agent.WithLLMChatter(&steerSpyChatter{}),
	)
	template.SetAgentRegistry(reg)

	mgr := agent.NewManager(agent.ManagerConfig{})
	clone, err := mgr.GetOrCreateWired("conv-follow-gate", t.TempDir(), template)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}

	svc := steerGateChatService(msgBus, reg)

	var once int32
	followed := make(chan error, 2)
	spy := &steerSpyChatter{hook: func() {
		// The follow-up drain continues the reasoning cycle, so the chatter
		// is called again after the injection; inject on the FIRST call
		// only or the probe loop feeds itself forever.
		if !atomic.CompareAndSwapInt32(&once, 0, 1) {
			return
		}
		followed <- svc.FollowUp(context.Background(), FollowUpRequest{
			ConversationID: "conv-follow-gate",
			Message:        "follow-up: also summarize",
			Source:         "probe",
		})
	}}
	clone.SetChatter(spy)

	done := make(chan error, 1)
	go func() {
		_, err := clone.RunOnce(context.Background(), "long running task", "conv-follow-gate")
		done <- err
	}()

	var fuErr error
	select {
	case fuErr = <-followed:
	case <-time.After(10 * time.Second):
		t.Fatal("followup hook never ran")
	}
	if fuErr != nil {
		t.Fatalf("chat.followup into a LIVE turn failed through production wiring: %v", fuErr)
	}

	if err := <-done; err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
}

// TestSetAgentRegistryNilSafeAndQueuePreserving guards the seam's contract:
// nil registry is a no-op and an existing queue is never replaced.
func TestSetAgentRegistryNilSafeAndQueuePreserving(t *testing.T) {
	loop := agent.NewAgentLoop("sess-nil-reg", t.TempDir(),
		agent.WithLLMChatter(&steerSpyChatter{}),
	)
	loop.SetAgentRegistry(nil) // must not panic
	if loop.SteeringRegistry() != nil {
		t.Error("nil SetAgentRegistry must not attach anything")
	}

	msgBus := bus.New(nil, nil)
	reg := steerGateRegistry(msgBus)
	defer reg.Close()

	loop.SetAgentRegistry(reg)
	q := loop.SteeringQueue()
	if q == nil {
		t.Fatal("queue must be minted on first attach")
	}

	// Second attach must keep the SAME queue (never clobber a live queue).
	loop.SetAgentRegistry(reg)
	if loop.SteeringQueue() != q {
		t.Error("second SetAgentRegistry must preserve the existing queue instance")
	}
}
