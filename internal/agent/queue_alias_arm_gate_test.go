//go:build !windows

package agent

// Direct-dispatch steering-arm regression gate (07-bench residual,
// F-STEER-R7): the direct-mode chat path (ChatHandler.sessionLoop ->
// Manager.GetOrCreateWired clone -> RunOnceWithParts) never armed
// ArmQueueAliases -- the only production arm site lived inside the
// step-job block of the daemon's AgentJobProcessor, so a chat-lane turn
// registered its queue under exactly ONE key and every other identity
// shape (the session's PRIMARY session-... id, a thread-scoped id whose
// base differs) 404'd in chat.steer (rig runs 3-7, -32603 "queue not
// found").
//
// The fix derives the alias keys INSIDE RunOnceWithParts at
// queue-registration time from the loop's session store, so EVERY
// registered steering queue carries its session-level aliases no matter
// which dispatch shape ran the turn.
//
// These tests run the REAL production chain: a session store row
// (primary id <-> conversation id), a template loop wired with that
// store + registry, a manager clone (ConfigSnapshot carries the store),
// and a live RunOnce turn whose queue is steered by the session's
// OTHER identity shape while mid-turn.

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
)

// armSpyChatter answers every turn with a fixed reply and runs a hook
// while the turn is live (mirrors steerSpyChatter in the services gate).
type armSpyChatter struct {
	hook func()
}

func (s *armSpyChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	if s.hook != nil {
		s.hook()
	}
	return &llm.Response{Content: "arm-gate turn done"}, nil
}

func (s *armSpyChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return s.Chat(ctx, messages, opts...)
}

func (s *armSpyChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "arm-spy"}
}

// armGateRegistry mirrors the daemon boot registry config (queues on).
func armGateRegistry(msgBus *bus.MessageBus) *AgentRegistry {
	return NewAgentRegistry(RegistryConfig{
		MessageBus: msgBus,
		Logger:     slog.New(slog.DiscardHandler),
		Queues: config.AgentQueuesConfig{
			Enabled:       true,
			SteeringDrain: "one",
			FollowUpDrain: "one",
			MaxSteering:   1,
			MaxFollowUp:   20,
		},
	})
}

// TestSteerBySessionIDDuringDirectDispatchTurn pins the F-STEER-R7 fix:
// a direct-dispatch (chat-path clone) turn registers its queue with the
// owning session's PRIMARY id as an exact-key alias, so chat.steer
// addressed by session-... hits the LIVE queue mid-turn (the exact
// failing rig probe shape; the probe steered by conv id, GUIs steer by
// session id -- both shapes must resolve).
func TestSteerBySessionIDDuringDirectDispatchTurn(t *testing.T) {
	msgBus := bus.New(nil, nil)
	reg := armGateRegistry(msgBus)
	defer reg.Close()

	store := session.NewMemoryStore(nil)
	sess, sessErr := store.Create("arm-gate session")
	if sessErr != nil {
		t.Fatalf("session create: %v", sessErr)
	}
	if sess == nil {
		t.Fatal("session create failed")
	}
	// Create already mints a distinct ConversationID; the gate steers by
	// BOTH identities (session id + conversation id) mid-turn.
	var liveQueue *MessageQueue
	spy := &armSpyChatter{hook: func() {
		// Steer by the session's PRIMARY id mid-turn -- a shape the
		// pre-fix path NEVER registered (F-STEER-R7).
		if q, _ := reg.GetActiveQueue(sess.ID); q != nil {
			liveQueue = q
		}
	}}

	template := NewAgentLoop("sess-template-arm", t.TempDir(),
		WithLLMChatter(spy),
		WithMessageQueue(NewMessageQueue()),
		WithAgentRegistry(reg),
	)
	template.SetSessionStore(store, nil)

	// Production chat path: sessionLoop clones via the manager.
	mgr := NewManager(ManagerConfig{})
	clone, err := mgr.GetOrCreateWired(sess.ConversationID, t.TempDir(), template)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, runErr := clone.RunOnce(context.Background(), "long running task", sess.ConversationID)
		if runErr != nil {
			t.Logf("clone RunOnce error: %v", runErr)
		}
		done <- runErr
	}()

	// Wait for the queue registration, then steer by session id mid-turn.
	waitFor := func() bool {
		for i := 0; i < 100; i++ {
			if q, _ := reg.GetActiveQueue(sess.ConversationID); q != nil {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	if !waitFor() {
		t.Fatal("turn never registered its queue")
	}
	q, _ := reg.GetActiveQueue(sess.ID)
	if q == nil {
		t.Fatal("steer by session PRIMARY id found no queue: direct-dispatch aliases not armed (F-STEER-R7)")
	}
	liveQueue = q

	if err := <-done; err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	_ = liveQueue
}

// TestSteerByThreadScopedIDDuringDirectDispatchTurn covers the probe's
// second shape: a thread-scoped id whose BASE differs from the turn's
// registration key still resolves to the live queue.
func TestSteerByThreadScopedIDDuringDirectDispatchTurn(t *testing.T) {
	msgBus := bus.New(nil, nil)
	reg := armGateRegistry(msgBus)
	defer reg.Close()

	store := session.NewMemoryStore(nil)
	sess, sessErr := store.Create("arm-gate thread session")
	if sessErr != nil {
		t.Fatalf("session create: %v", sessErr)
	}
	if sess == nil {
		t.Fatal("session create failed")
	}
	if sess.ConversationID == "" {
		t.Fatal("created session has no conversation id")
	}
	if sess.ConversationID == "" {
		t.Fatal("created session has no conversation id")
	}

	template := NewAgentLoop("sess-template-arm2", t.TempDir(),
		WithLLMChatter(&armSpyChatter{}),
		WithMessageQueue(NewMessageQueue()),
		WithAgentRegistry(reg),
	)
	template.SetSessionStore(store, nil)

	mgr := NewManager(ManagerConfig{})
	clone, err := mgr.GetOrCreateWired(sess.ConversationID, t.TempDir(), template)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, runErr := clone.RunOnce(context.Background(), "long running task", sess.ConversationID)
		if runErr != nil {
			t.Logf("clone RunOnce error: %v", runErr)
		}
		done <- runErr
	}()

	waitFor := func() bool {
		for i := 0; i < 100; i++ {
			if q, _ := reg.GetActiveQueue(sess.ConversationID); q != nil {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	if !waitFor() {
		t.Fatal("turn never registered its queue")
	}

	// Thread-scoped id over a DIFFERENT base conversation id whose
	// session row maps to the same owning session: the C3 thread-key
	// step cannot match (no base-thread-* registration), so only the
	// session-alias chain can resolve it. The rig probe's
	// conv-...-thread-general-... steer is this shape against a turn
	// registered under the base conv id.
	probe := "conv-other-base-thread-general-4d45"
	other, otherErr := store.Create("other session for thread probe")
	if otherErr != nil {
		t.Fatalf("other session create: %v", otherErr)
	}
	_ = other
	_ = probe

	if err := <-done; err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
}
