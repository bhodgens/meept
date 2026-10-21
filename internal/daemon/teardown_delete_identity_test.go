package daemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
)

// recordingTeardownAdapter records every Teardown dispatch.
type recordingTeardownAdapter struct {
	mu       sync.Mutex
	dispatch []string // "sessionID|endpointKey"
}

func (a *recordingTeardownAdapter) Handles(string) bool { return true }
func (a *recordingTeardownAdapter) Teardown(_ context.Context, sessionID, endpointKey string) error {
	a.mu.Lock()
	a.dispatch = append(a.dispatch, sessionID+"|"+endpointKey)
	a.mu.Unlock()
	return nil
}
func (a *recordingTeardownAdapter) SweepTeardown(_ context.Context, _, _ string) error { return nil }
func (a *recordingTeardownAdapter) CloudTeardown(_ context.Context, _, _ string) error { return nil }

// TestTeardownStoreHook_DeletePathReachesAccountingIdentity pins the
// delete-path teardown fix end-to-end at the daemon seam: the store
// DELETEs the row before firing the hook, so the post-delete identity
// resolver (a store Get) structurally cannot resolve the conversation id.
// The store now hands the pre-delete-captured alternate identity to
// teardownStoreHook (SessionLifecycleHookWithAlternate), which dispatches
// under BOTH identities — the snapshot keys on the accounting id, so that
// dispatch must find the endpoint and erase.
func TestTeardownStoreHook_DeletePathReachesAccountingIdentity(t *testing.T) {
	store := session.NewMemoryStore(nil)
	sess, err := store.Create("delete-identity-daemon")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The snapshot is keyed on THIS session's generated conversation id
	// (the accounting identity), matching production keying.
	convID := sess.ConversationID
	if convID == "" || convID == sess.ID {
		t.Fatalf("fixture invalid: ConversationID=%q ID=%q must be distinct", convID, sess.ID)
	}

	// Snapshot keyed ONLY on the accounting (conversation) id — the
	// production shape that made delete-time teardown unreachable.
	adapter := &recordingTeardownAdapter{}
	coord := llm.NewTeardownCoordinator(
		[]llm.TeardownAdapter{adapter},
		func() map[string]string { return map[string]string{convID: "llama-cpp:127.0.0.1:8080"} },
		time.Second, nil,
	)
	// The daemon's identity resolver: a post-delete store Get — sees
	// nothing after the row is gone (the defect this test pins closed).
	coord.SetIdentityResolver(func(id string) (string, bool) {
		s := store.Get(id)
		if s == nil || s.ConversationID == "" || s.ConversationID == id {
			return "", false
		}
		return s.ConversationID, true
	})
	store.SetLifecycleHook(teardownStoreHook{coordinator: coord})

	if !store.Delete(sess.ID) {
		t.Fatal("Delete returned false")
	}
	// dispatchAsync runs on a goroutine; ShutdownSweep is the synchronous
	// wait-for-inflight seam.
	coord.ShutdownSweep(context.Background())

	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	found := false
	for _, d := range adapter.dispatch {
		if d == convID+"|llama-cpp:127.0.0.1:8080" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no teardown dispatch under the accounting identity %q; dispatches = %v", convID, adapter.dispatch)
	}
}
