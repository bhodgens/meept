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
