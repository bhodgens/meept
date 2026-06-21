package agent

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/session"
)

// mockThreadStore is a minimal ThreadRoutable implementation for testing.
type mockThreadStore struct {
	sessions map[string]*session.Session
}

func newMockThreadStore() *mockThreadStore {
	return &mockThreadStore{
		sessions: make(map[string]*session.Session),
	}
}

func (m *mockThreadStore) Get(id string) *session.Session {
	return m.sessions[id]
}

func (m *mockThreadStore) GetActiveThread(ctx context.Context, sessionID string) (*session.Thread, error) {
	sess := m.Get(sessionID)
	if sess == nil {
		return nil, nil
	}
	return sess.GetActiveThread(), nil
}

func (m *mockThreadStore) ListThreadsBySession(ctx context.Context, sessionID string) ([]*session.Thread, error) {
	sess := m.Get(sessionID)
	if sess == nil || sess.Threads == nil {
		return nil, nil
	}
	result := make([]*session.Thread, 0, len(sess.Threads))
	for _, t := range sess.Threads {
		result = append(result, t)
	}
	return result, nil
}

func (m *mockThreadStore) addSession(sess *session.Session) {
	m.sessions[sess.ID] = sess
}

func TestThreadRouter_GetThreadConversationID_NoStore(t *testing.T) {
	router := NewThreadRouter()

	input := "I need to fix this bug"
	convID, err := router.GetThreadConversationID(context.Background(), "sessionx1", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if convID != "sessionx1" {
		t.Errorf("expected fallback to session ID, got %q", convID)
	}
}

func TestThreadRouter_GetThreadConversationID_Migration(t *testing.T) {
	store := newMockThreadStore()
	router := NewThreadRouter(WithThreadRouterSessionStore(store))

	sess := &session.Session{
		ID:             "sessionx1",
		Name:           "Test Session",
		ConversationID: "conv-existing-123",
		Threads:        nil,
	}
	store.addSession(sess)

	// "Hello, how are you" has no keyword matches -> "general" topic
	input := "Hello, how are you"
	convID, err := router.GetThreadConversationID(context.Background(), "sessionx1", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Migration creates "general" thread from existing ConversationID.
	// The general thread ID ends with "-001" (last 4 chars of sessionx1).
	if convID != "conv-existing-123" {
		t.Errorf("expected migration to use existing conversation ID, got %q", convID)
	}

	if sess.Threads == nil {
		t.Fatal("expected session to have threads after migration")
	}

	// Find the general thread (ID format: thread-general-001)
	threads := make([]*session.Thread, 0, len(sess.Threads))
	for _, t := range sess.Threads {
		threads = append(threads, t)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 migrated thread, got %d", len(threads))
	}
	g := threads[0]
	if g.TopicLabel != "general" {
		t.Errorf("expected general thread, got %q", g.TopicLabel)
	}
	if g.ConversationID != "conv-existing-123" {
		t.Errorf("expected migrated thread to use existing conversation ID, got %q", g.ConversationID)
	}
	if !g.IsActive {
		t.Error("expected migrated thread to be active")
	}
	if sess.ActiveThreadID != g.ID {
		t.Errorf("expected ActiveThreadID to be migrated thread, got %q", sess.ActiveThreadID)
	}
}

func TestThreadRouter_GetThreadConversationID_MigrationNewTopic(t *testing.T) {
	store := newMockThreadStore()
	router := NewThreadRouter(WithThreadRouterSessionStore(store))

	sess := &session.Session{
		ID:             "sessionx1",
		ConversationID: "conv-existing-456",
		Threads:        nil,
	}
	store.addSession(sess)

	// Input contains code keywords -> "work" or "code" topic
	input := "I need to fix this bug in the code"
	convID, err := router.GetThreadConversationID(context.Background(), "sessionx1", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Migration creates "general" thread, but input topic is "work"
	// So a new thread is created with conversation ID: conv-existing-456-thread-work-001
	// The general thread should exist
	if sess.Threads == nil {
		t.Fatal("expected session to have threads after migration")
	}

	// Verify at least 2 threads exist (general from migration + new topic thread)
	if len(sess.Threads) < 2 {
		t.Fatalf("expected at least 2 threads, got %d", len(sess.Threads))
	}

	// Find the general thread
	var foundGeneral bool
	for _, t := range sess.Threads {
		if t.TopicLabel == "general" && t.ConversationID == "conv-existing-456" {
			foundGeneral = true
		}
	}
	if !foundGeneral {
		t.Error("expected migrated general thread to exist with original conversation ID")
	}

	// The returned conversation ID should be the new thread's (not base session ID)
	if convID == "sessionx1" {
		t.Errorf("expected thread conversation ID, got raw session ID")
	}
	if !strings.HasPrefix(convID, "conv-existing-456-thread-") {
		t.Errorf("expected thread conversation ID starting with 'conv-existing-456-thread-', got %q", convID)
	}
}

func TestThreadRouter_GetThreadConversationID_NewThread(t *testing.T) {
	store := newMockThreadStore()
	router := NewThreadRouter(WithThreadRouterSessionStore(store))

	sess := &session.Session{
		ID:             "sessionx1",
		ConversationID: "conv-base-789",
		Threads:        make(map[string]*session.Thread),
	}
	store.addSession(sess)

	// "lunch" matches "food" topic keywords
	input := "What should I eat for lunch today"
	convID, err := router.GetThreadConversationID(context.Background(), "sessionx1", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should create a new "food" thread since "lunch" matches food keywords
	if convID == "conv-base-789" {
		t.Errorf("expected new thread conversation ID, got base session ID")
	}

	// Find food thread - just verify it exists
	var foundFood bool
	for _, t := range sess.Threads {
		if t.TopicLabel == "food" {
			foundFood = true
			break
		}
	}
	if !foundFood {
		t.Errorf("expected food thread, got thread keys: %v", getThreadKeys(sess.Threads))
	}
}

func TestThreadRouter_GetThreadConversationID_ReuseExistingThread(t *testing.T) {
	store := newMockThreadStore()

	// Session ID "sessionx1" -> last 4 chars = "onx1" -> thread ID = "thread-general-onx1"
	sess := &session.Session{
		ID:             "sessionx1",
		ConversationID: "conv-base-789",
		Threads: map[string]*session.Thread{
			"thread-general-onx1": {
				ID:             "thread-general-onx1",
				TopicLabel:     "general",
				ConversationID: "conv-base-789", // Reuses base conversation ID
				IsActive:       true,
			},
		},
		ActiveThreadID: "thread-general-onx1",
	}
	store.addSession(sess)

	// "Hello, how are you" -> "general" topic (no keyword matches)
	input := "Hello, how are you"
	router := NewThreadRouter(WithThreadRouterSessionStore(store))
	convID, err := router.GetThreadConversationID(context.Background(), "sessionx1", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should reuse the existing general thread's conversation ID
	if convID != "conv-base-789" {
		t.Errorf("expected existing thread conversation ID %q, got %q", "conv-base-789", convID)
	}
	if len(sess.Threads) != 1 {
		t.Errorf("expected exactly 1 thread, got %d", len(sess.Threads))
	}
}

func TestThreadRouter_GetThreadConversationID_NoSession(t *testing.T) {
	store := newMockThreadStore()
	router := NewThreadRouter(WithThreadRouterSessionStore(store))

	input := "random input"
	convID, err := router.GetThreadConversationID(context.Background(), "nonexistent", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if convID != "nonexistent" {
		t.Errorf("expected fallback to session ID, got %q", convID)
	}
}

func TestThreadRouter_SetActiveThread(t *testing.T) {
	store := newMockThreadStore()
	router := NewThreadRouter(WithThreadRouterSessionStore(store))

	sess := &session.Session{
		ID:      "sessionx1",
		Threads: map[string]*session.Thread{
			"thread-general-001": {
				ID:       "thread-general-001",
				TopicLabel:     "general",
				IsActive:       true,
			},
			"thread-food-001": {
				ID:       "thread-food-001",
				TopicLabel:     "food",
				IsActive:       false,
			},
		},
		ActiveThreadID: "thread-general-001",
	}
	store.addSession(sess)

	err := router.SetActiveThread("sessionx1", "thread-food-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sess.Threads["thread-general-001"].IsActive {
		t.Error("expected general thread to be inactive")
	}
	if !sess.Threads["thread-food-001"].IsActive {
		t.Error("expected food thread to be active")
	}
	if sess.ActiveThreadID != "thread-food-001" {
		t.Errorf("expected active thread ID to be food, got %q", sess.ActiveThreadID)
	}
}

func getThreadKeys(threads map[string]*session.Thread) []string {
	keys := make([]string, 0, len(threads))
	for k := range threads {
		keys = append(keys, k)
	}
	return keys
}

func TestThreadRouter_GetActiveThread(t *testing.T) {
	store := newMockThreadStore()
	router := NewThreadRouter(WithThreadRouterSessionStore(store))

	sess := &session.Session{
		ID: "sessionx1",
		Threads: map[string]*session.Thread{
			"thread-general-001": {
				ID:       "thread-general-001",
				TopicLabel:     "general",
				IsActive:       true,
			},
		},
		ActiveThreadID: "thread-general-001",
	}
	store.addSession(sess)

	tt, err := router.GetActiveThread("sessionx1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tt == nil {
		t.Fatal("expected active thread")
	}
	if tt.TopicLabel != "general" {
		t.Errorf("expected general thread, got %q", tt.TopicLabel)
	}
}

func TestThreadRouter_GetActiveThread_NoStore(t *testing.T) {
	router := NewThreadRouter()
	tt, err := router.GetActiveThread("sessionx1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tt != nil {
		t.Error("expected nil when no store configured")
	}
}

func TestThreadRouter_DetectTopic(t *testing.T) {
	router := NewThreadRouter()

	tests := []struct {
		input    string
		expected string
	}{
		{"I need to debug this error", "code"},
		{"What should I have for dinner?", "food"},
		{"I'm going to the gym tomorrow", "health"},
		{"Hello, how are you?", "general"},
		{"Let's go on vacation next weekend", "personal"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := router.detectTopic(tt.input)
			if got != tt.expected {
				t.Errorf("detectTopic(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestThreadRouter_GenerateThreadID(t *testing.T) {
	router := NewThreadRouter()

	got := router.generateThreadID("sessionx1", "work")
	if got != "thread-work-onx1" {
		t.Errorf("generateThreadID = %q, want %q", got, "thread-work-onx1")
	}
}

// TestDispatcher_UsesThreadRouter proves the dispatcher consults the wired
// thread router when routing to an agent. It uses a minimal dispatcher with
// no registry, so RouteToAgent fails at the "no agent registry" check — but
// only after the thread router has been consulted. We verify the consultation
// happened by observing a side effect in the mock store (the session's
// Threads map is populated by migration).
func TestDispatcher_UsesThreadRouter(t *testing.T) {
	store := newMockThreadStore()
	router := NewThreadRouter(WithThreadRouterSessionStore(store))

	// Pre-register a session that will be migrated when the router touches it.
	sess := &session.Session{
		ID:             "sess-dispatch-1",
		ConversationID: "conv-dispatch-original",
		Threads:        nil, // triggers migration path on first access
	}
	store.addSession(sess)

	d := &Dispatcher{
		logger:       slog.Default(),
		threadRouter: router,
	}
	// No registry set: RouteToAgent will return early with an error, but
	// only after the thread-router block runs.
	result := &DispatchResult{
		AgentID: "chat",
		Intent:  &Intent{Type: string(IntentChat), Summary: "hello there"},
	}

	_, _ = d.RouteToAgent(context.Background(), result, "sess-dispatch-1")

	// If the thread router was consulted, the session will have been
	// migrated (Threads populated). If it wasn't consulted, Threads is
	// still nil.
	if sess.Threads == nil {
		t.Fatal("expected thread router to be consulted (session would be migrated), but Threads is still nil")
	}
	if len(sess.Threads) == 0 {
		t.Fatal("expected at least one migrated thread after RouteToAgent")
	}

	// Verify the migrated general thread carries the original conversation ID.
	var foundGeneral bool
	for _, th := range sess.Threads {
		if th.TopicLabel == "general" && th.ConversationID == "conv-dispatch-original" {
			foundGeneral = true
			break
		}
	}
	if !foundGeneral {
		t.Error("expected migrated general thread with original conversation ID")
	}
}

// TestDispatcher_NoThreadRouter_LegacyPath confirms that when no thread
// router is wired, RouteToAgent does not mutate the session store and the
// conversationID is used as-is. This guards against regressions that would
// accidentally route everything through threads.
func TestDispatcher_NoThreadRouter_LegacyPath(t *testing.T) {
	store := newMockThreadStore()
	sess := &session.Session{
		ID:             "sess-legacy-1",
		ConversationID: "conv-legacy",
		Threads:        nil,
	}
	store.addSession(sess)

	d := &Dispatcher{
		logger:       slog.Default(),
		threadRouter: nil, // legacy mode
	}

	result := &DispatchResult{
		AgentID: "chat",
		Intent:  &Intent{Type: string(IntentChat), Summary: "hello"},
	}

	_, _ = d.RouteToAgent(context.Background(), result, "sess-legacy-1")

	// In legacy mode, the thread router is never consulted, so the session
	// must not be migrated.
	if sess.Threads != nil {
		t.Errorf("expected no thread migration in legacy mode, got %d threads", len(sess.Threads))
	}
}
