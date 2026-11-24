package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// Identity canonicalization (20261121-interactive-usability leaf 01, fix 1):
// a queue registered under an EPHEMERAL step-task id with a session-level
// alias must be found by the EXACT alias key, and the alias must die with
// the primary registration (same lifecycle, no orphaned alias keys).
func TestRegisterActiveQueueAliasExactKeyHit(t *testing.T) {
	r := NewAgentRegistry(RegistryConfig{
		Queues: config.AgentQueuesConfig{Enabled: true},
	})
	defer r.Close()

	q := NewMessageQueue(WithQueueAgentID("test"))
	gen := r.RegisterActiveQueue("step-task-1-step-1", q, nil, "conv-session")

	if gen == 0 {
		t.Fatal("generation must be >= 1")
	}
	got, generation := r.GetActiveQueue("conv-session")
	if got != q {
		t.Fatalf("alias exact-key lookup: got %v, want the registered queue", got)
	}
	if generation != gen {
		t.Fatalf("alias generation = %d, want primary generation %d", generation, gen)
	}
}

func TestUnregisterActiveQueueRemovesAliases(t *testing.T) {
	r := NewAgentRegistry(RegistryConfig{
		Queues: config.AgentQueuesConfig{Enabled: true},
	})
	defer r.Close()

	q := NewMessageQueue(WithQueueAgentID("test"))
	r.RegisterActiveQueue("step-task-2-step-1", q, nil, "conv-live", "conv-secondary")
	r.UnregisterActiveQueue("step-task-2-step-1")

	if got, _ := r.GetActiveQueue("conv-live"); got != nil {
		t.Fatalf("alias conv-live survived the primary unregister: %v", got)
	}
	if got, _ := r.GetActiveQueue("conv-secondary"); got != nil {
		t.Fatalf("alias conv-secondary survived the primary unregister: %v", got)
	}
	if got, _ := r.GetActiveQueue("step-task-2-step-1"); got != nil {
		t.Fatalf("primary key survived its own unregister: %v", got)
	}
}

// A subsequent registration REPLACES the alias with a newer entry; the old
// registration's unregister must not delete the newer alias key.
func TestAliasLifecycleNewerRegistrationWins(t *testing.T) {
	r := NewAgentRegistry(RegistryConfig{
		Queues: config.AgentQueuesConfig{Enabled: true},
	})
	defer r.Close()

	q1 := NewMessageQueue(WithQueueAgentID("test"))
	q2 := NewMessageQueue(WithQueueAgentID("test"))
	r.RegisterActiveQueue("step-task-old", q1, nil, "conv-turn")
	r.RegisterActiveQueue("step-task-new", q2, nil, "conv-turn")
	r.UnregisterActiveQueue("step-task-old")

	got, _ := r.GetActiveQueue("conv-turn")
	if got != q2 {
		t.Fatalf("newer registration's alias clobbered by older unregister: got %v, want %v", got, q2)
	}
	r.UnregisterActiveQueue("step-task-new")
	if got, _ := r.GetActiveQueue("conv-turn"); got != nil {
		t.Fatalf("alias survived BOTH unregisters: %v", got)
	}
}

// Bughunt (2026-11-23): an alias key that equals another LIVE
// registration's PRIMARY key must NOT shadow it. Shape: an interactive
// chat turn registers primary K; a step-job turn then registers with
// alias K. Before the shadow guard the alias overwrote K's entry, so the
// step job's unregister deleted K and closed the LIVE chat queue
// mid-turn (steer/followup 404 + injection into a closed queue).
func TestRegisterActiveQueueAliasCannotShadowLivePrimary(t *testing.T) {
	r := NewAgentRegistry(RegistryConfig{
		Queues: config.AgentQueuesConfig{Enabled: true},
	})
	defer r.Close()

	// A: interactive chat turn, primary "conv-K".
	qA := NewMessageQueue(WithQueueAgentID("chat"))
	r.RegisterActiveQueue("conv-K", qA, nil)

	// B: step-job turn under an ephemeral id aliasing "conv-K".
	qB := NewMessageQueue(WithQueueAgentID("step"))
	r.RegisterActiveQueue("step-task-shadow-step-1", qB, nil, "conv-K")

	// The live primary still owns its key.
	got, _ := r.GetActiveQueue("conv-K")
	if got != qA {
		t.Fatalf("alias shadowed a live primary: conv-K resolves to %v, want the primary queue", got)
	}

	// B's unregister must leave A's registration (and queue) intact.
	r.UnregisterActiveQueue("step-task-shadow-step-1")
	got, _ = r.GetActiveQueue("conv-K")
	if got != qA {
		t.Fatalf("conv-K after alias unregister: got %v, want the primary queue", got)
	}
	if qA.IsClosed() {
		t.Fatal("alias owner's unregister closed the LIVE primary queue")
	}
	if !qB.IsClosed() {
		t.Fatal("unregistering B must close B's own queue")
	}

	// A's own unregister removes the key and closes its queue.
	r.UnregisterActiveQueue("conv-K")
	if got, _ := r.GetActiveQueue("conv-K"); got != nil {
		t.Fatalf("primary key survived its own unregister: %v", got)
	}
	if !qA.IsClosed() {
		t.Fatal("primary queue not closed after its own unregister")
	}
}

// Empty and duplicate alias ids are skipped, and a queue with NO aliases
// behaves byte-identically (the interactive-turn shape).
func TestRegisterActiveQueueAliasSkipsEmptyAndDupes(t *testing.T) {
	r := NewAgentRegistry(RegistryConfig{
		Queues: config.AgentQueuesConfig{Enabled: true},
	})
	defer r.Close()

	q := NewMessageQueue(WithQueueAgentID("test"))
	r.RegisterActiveQueue("conv-primary", q, nil, "", "conv-primary")
	if got, _ := r.GetActiveQueue("conv-primary"); got != q {
		t.Fatalf("primary lookup after alias dedupe: got %v", got)
	}
}
