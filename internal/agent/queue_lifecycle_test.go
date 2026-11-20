package agent

// 01-safety TEST GATE T3 (00T-gate.md): steering queue lifecycle (C3).
//
// Three defects pinned:
//   - RunOnce re-registers the SAME l.queue every turn; turn 1's defer
//     unregisters+Close()s it (one-way CAS), so turn 2 of a cached loop
//     steers into a CLOSED queue (F-NEW-11/12). Fix: fresh queue instance
//     per RunOnce registration; the old object stays closed.
//   - ConfigSnapshot carries WithMessageQueue(l.queue): every session
//     clone of a template SHARES one queue pointer; A's turn-end close
//     kills B's steering.
//   - RouteToAgent injects result.Intent.Summary (~100 chars) instead of
//     the FULL original input.
//
// RED today: closed-queue reuse / shared clone pointer / Summary injected.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// queueSpyChatter answers every turn with a fixed reply and runs a hook
// INSIDE the turn, while the loop's queue registration is live.
type queueSpyChatter struct {
	hook func()
}

func (s *queueSpyChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	if s.hook != nil {
		s.hook()
	}
	return &llm.Response{Content: "turn done"}, nil
}

func (s *queueSpyChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return s.Chat(ctx, messages, opts...)
}

func (s *queueSpyChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "queue-spy"}
}

// TestFreshQueuePerTurn pins C3: a cached loop running turn 2 must register
// a FRESH, OPEN queue -- not the closed turn-1 object (no reopen-in-place).
func TestFreshQueuePerTurn(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: slogDiscardLogger()})
	spy := &queueSpyChatter{}
	loop := NewAgentLoop("sess-fresh-q", t.TempDir(),
		WithLLMChatter(spy),
		WithMessageQueue(NewMessageQueue()),
		WithAgentRegistry(reg),
	)

	var turn1Queue, turn2Queue *MessageQueue
	spy.hook = func() {
		q, _ := reg.GetActiveQueue("conv-fresh-q")
		if turn1Queue == nil {
			turn1Queue = q
		} else {
			turn2Queue = q
		}
	}

	ctx := context.Background()
	if _, err := loop.RunOnce(ctx, "first turn", "conv-fresh-q"); err != nil {
		t.Fatalf("turn 1 RunOnce: %v", err)
	}
	if _, err := loop.RunOnce(ctx, "second turn", "conv-fresh-q"); err != nil {
		t.Fatalf("turn 2 RunOnce: %v", err)
	}

	if turn1Queue == nil || turn2Queue == nil {
		t.Fatalf("queue not registered during a turn (turn1=%v turn2=%v)", turn1Queue, turn2Queue)
	}
	if turn2Queue == turn1Queue {
		t.Errorf("CURRENT DEFECT: turn 2 re-registered the SAME queue object as turn 1; C3 requires a fresh instance per RunOnce")
	}
	if turn2Queue.IsClosed() {
		t.Errorf("CURRENT DEFECT: turn 2's registered queue is CLOSED (turn 1's Close is one-way); steering on turn 2 is dead")
	}
	if err := turn2Queue.Steer(ctx, "mid-turn correction", "gate"); err != nil {
		t.Errorf("steer into turn 2's live queue failed: %v (want accepted)", err)
	}
	if !turn1Queue.IsClosed() {
		t.Errorf("turn 1's queue object must STAY closed (fresh instance, never reopen-in-place)")
	}
}

// TestCloneQueueIsolation pins C3: two session clones of one template must
// never share a queue instance; closing A's queue must not close B's.
func TestCloneQueueIsolation(t *testing.T) {
	template := NewAgentLoop("sess-template", t.TempDir(),
		WithLLMChatter(&queueSpyChatter{}),
		WithMessageQueue(NewMessageQueue()),
	)
	mgr := NewManager(ManagerConfig{})
	dir := t.TempDir()
	cloneA, err := mgr.GetOrCreateWired("sess-a", dir, template)
	if err != nil {
		t.Fatalf("clone A: %v", err)
	}
	cloneB, err := mgr.GetOrCreateWired("sess-b", dir, template)
	if err != nil {
		t.Fatalf("clone B: %v", err)
	}

	// Construction-time sharing is the ConfigSnapshot defect: a non-nil
	// queue pointer identical across clones (and the template).
	if cloneA.queue != nil && cloneA.queue == cloneB.queue {
		t.Errorf("CURRENT DEFECT: session clones share ONE queue pointer via ConfigSnapshot WithMessageQueue(l.queue)")
	}

	// Turn-level proof: run one turn per clone against a shared registry
	// and capture the queues actually registered. They must be distinct,
	// and A's turn-end close must leave B's queue usable.
	reg := NewAgentRegistry(RegistryConfig{Logger: slogDiscardLogger()})
	spy := &queueSpyChatter{}
	var queueA, queueB *MessageQueue
	spy.hook = func() {
		if q, _ := reg.GetActiveQueue("conv-clone-a"); q != nil {
			queueA = q
		}
		if q, _ := reg.GetActiveQueue("conv-clone-b"); q != nil {
			queueB = q
		}
	}
	loopA := NewAgentLoop("sess-a", dir, WithLLMChatter(spy),
		WithMessageQueue(NewMessageQueue()), WithAgentRegistry(reg))
	loopB := NewAgentLoop("sess-b", dir, WithLLMChatter(spy),
		WithMessageQueue(NewMessageQueue()), WithAgentRegistry(reg))
	// Mirror the clone wiring: the manager-built clones carry the shared
	// snapshot queue; drive the SAME shape through the registry.
	if cloneA.queue != nil {
		loopA.queue = cloneA.queue
	}
	if cloneB.queue != nil {
		loopB.queue = cloneB.queue
	}

	ctx := context.Background()
	if _, rerr := loopA.RunOnce(ctx, "work A", "conv-clone-a"); rerr != nil {
		t.Fatalf("clone A turn: %v", rerr)
	}
	if _, rerr := loopB.RunOnce(ctx, "work B", "conv-clone-b"); rerr != nil {
		t.Fatalf("clone B turn: %v", rerr)
	}
	if queueA == nil || queueB == nil {
		t.Fatalf("clone turns registered no queues (A=%v B=%v)", queueA, queueB)
	}
	if queueA == queueB {
		t.Errorf("CURRENT DEFECT: both session clones registered the SAME queue instance; A's turn-end Close kills B's steering")
	}
	if queueB.IsClosed() {
		t.Errorf("CURRENT DEFECT: clone B's queue is closed after clone A's turn ended (shared pointer)")
	}
}

// TestSteerFullText pins C3: the dispatcher's queue injection must carry
// the FULL original user input, never the ~100-char Intent.Summary.
func TestSteerFullText(t *testing.T) {
	d, reg := newTestDispatcherWithRegistry(t)
	q := NewMessageQueue()
	reg.RegisterActiveQueue("conv-steer-full", q, nil)

	fullInput := "Please stop the current refactor and instead focus on the billing export: " +
		strings.Repeat("the export must include every invoice line item with tax breakdown and currency normalization, ", 3) +
		"and write the result to billing-export.csv before anything else."
	if len(fullInput) <= 200 {
		t.Fatalf("fixture bug: full input must exceed the summary truncation window, got %d chars", len(fullInput))
	}
	summary := fullInput[:100]

	result := &DispatchResult{
		Intent: &Intent{
			Type:    string(IntentCode),
			Summary: summary,
		},
		AgentID:           config.AgentIDCoder,
		OriginalInput:     fullInput,
		ExplicitSteerMode: true,
	}

	reply, err := d.RouteToAgent(context.Background(), result, "conv-steer-full")
	if err != nil {
		t.Fatalf("RouteToAgent steer: %v", err)
	}
	if !strings.Contains(reply, "queued") {
		t.Fatalf("steer was not queued (reply=%q); the active queue lane must inject", reply)
	}
	drained := q.DrainSteering()
	if len(drained) != 1 {
		t.Fatalf("drained %d steering messages, want 1", len(drained))
	}
	if drained[0].Content != fullInput {
		t.Errorf("CURRENT DEFECT: injected steering text is %d chars (Intent.Summary truncation); want the FULL %d-char original input.\ninjected: %q",
			len(drained[0].Content), len(fullInput), drained[0].Content)
	}
}
