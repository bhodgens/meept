package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/caimlas/meept/internal/session"
)

// ThreadRoutable is the minimal interface the ThreadRouter needs from a
// session store to perform migration and thread lookups.
type ThreadRoutable interface {
	Get(id string) *session.Session
	GetActiveThread(ctx context.Context, sessionID string) (*session.Thread, error)
	ListThreadsBySession(ctx context.Context, sessionID string) ([]*session.Thread, error)
}

// ThreadRouter manages thread creation, migration, and routing.
// It is safe for concurrent use.
type ThreadRouter struct {
	mu           sync.RWMutex
	detector     *TopicDetector
	sessionStore ThreadRoutable
	logger       *slog.Logger
}

// ThreadRouterOption configures a ThreadRouter.
type ThreadRouterOption func(*ThreadRouter)

// WithThreadRouterSessionStore sets the session store.
func WithThreadRouterSessionStore(store ThreadRoutable) ThreadRouterOption {
	return func(tr *ThreadRouter) {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		tr.sessionStore = store
	}
}

// WithThreadRouterLogger sets the logger.
func WithThreadRouterLogger(l *slog.Logger) ThreadRouterOption {
	return func(tr *ThreadRouter) {
		if l != nil {
			tr.mu.Lock()
			defer tr.mu.Unlock()
			tr.logger = l
		}
	}
}

// NewThreadRouter creates a thread router with the default topic detector.
func NewThreadRouter(opts ...ThreadRouterOption) *ThreadRouter {
	tr := &ThreadRouter{
		detector: NewTopicDetector(),
	}

	for _, opt := range opts {
		opt(tr)
	}
	return tr
}

// ensureThread ensures the session has a thread matching the given topic.
// If the session has no threads (pre-upgrade), it performs silent migration
// by creating a "general" thread using the existing ConversationID.
// If the requested thread does not exist, it creates one.
func (tr *ThreadRouter) ensureThread(sess *session.Session, threadID, topicLabel string) (*session.Thread, error) {
	// Silent migration: session exists but has no threads yet.
	if sess.Threads == nil && sess.ConversationID != "" {
		generalID := tr.generateThreadID(sess.ID, "general")
		sess.Threads = map[string]*session.Thread{
			generalID: {
				ID:             generalID,
				SessionID:      sess.ID,
				TopicLabel:     "general",
				ConversationID: sess.ConversationID,
				CreatedAt:      sess.CreatedAt,
				IsActive:       true,
			},
		}
		sess.ActiveThreadID = generalID
		if tr.logger != nil {
			tr.logger.Debug("migrated session to general thread",
				"session", sess.ID, "thread", generalID)
		}
	}

	// If session has threads already, check for the requested one.
	if sess.Threads != nil {
		if t, ok := sess.Threads[threadID]; ok {
			t.Touch()
			return t, nil
		}
	}

	// Create a new thread (will be added to the now-ensured map).
	return sess.GetOrCreateThread(threadID, topicLabel), nil
}

func (tr *ThreadRouter) generateThreadID(sessionID, topic string) string {
	return tr.detector.GenerateThreadID(sessionID, topic)
}

// GetThreadConversationID returns the conversation id for the thread that
// best matches the input. It performs silent migration (creating a "general"
// thread from the session's existing conversation id) when needed.
// If no session store is available, it falls back to using the session id
// directly (no thread isolation).
func (tr *ThreadRouter) GetThreadConversationID(ctx context.Context, sessionID, input string) (string, error) {
	tr.mu.RLock()
	store := tr.sessionStore
	logger := tr.logger
	tr.mu.RUnlock()

	if store == nil {
		return sessionID, nil
	}

	sess := store.Get(sessionID)
	if sess == nil {
		return sessionID, nil
	}

	topic := tr.detectTopic(input)
	threadID := tr.generateThreadID(sessionID, topic)

	thread, err := tr.ensureThread(sess, threadID, topic)
	if err != nil {
		if logger != nil {
			logger.Warn("thread routing failed, falling back to session id",
				"session", sessionID, "error", err)
		}
		return sessionID, nil
	}

	return thread.ConversationID, nil
}

// detectTopic returns the topic label for the given input.
func (tr *ThreadRouter) detectTopic(input string) string {
	return tr.detector.Detect(input)
}

// RouteThread determines which thread (if any) should handle the given input
// for a session. It returns the thread ID and the detected topic label.
// If the session's active thread already matches the detected topic, it
// returns the active thread's ID. Otherwise, it creates (or returns) a new
// thread for the detected topic.
func (tr *ThreadRouter) RouteThread(sessionID, input string) (threadID, topic string) {
	topic = tr.detector.Detect(input)
	return tr.generateThreadID(sessionID, topic), topic
}

// SetActiveThread marks the given thread as active for the session.
func (tr *ThreadRouter) SetActiveThread(sessionID, threadID string) error {
	tr.mu.RLock()
	store := tr.sessionStore
	tr.mu.RUnlock()

	if store == nil {
		return fmt.Errorf("session store not configured")
	}

	sess := store.Get(sessionID)
	if sess == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if sess.Threads == nil {
		return fmt.Errorf("session has no threads")
	}

	if _, ok := sess.Threads[threadID]; !ok {
		return fmt.Errorf("thread not found: %s", threadID)
	}

	for _, t := range sess.Threads {
		t.IsActive = false
	}

	sess.Threads[threadID].IsActive = true
	sess.ActiveThreadID = threadID

	return nil
}

// GetActiveThread returns the active thread for the session.
func (tr *ThreadRouter) GetActiveThread(sessionID string) (*session.Thread, error) {
	tr.mu.RLock()
	store := tr.sessionStore
	tr.mu.RUnlock()

	if store == nil {
		return nil, nil
	}

	sess := store.Get(sessionID)
	if sess == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	return sess.GetActiveThread(), nil
}

// CrossThreadContext returns a context string assembled from inactive
// thread summaries so the active thread has continuity with prior topics.
// Returns empty string when there are no threads or no session store.
func (tr *ThreadRouter) CrossThreadContext(sessionID, activeThreadID string) string {
	tr.mu.RLock()
	store := tr.sessionStore
	tr.mu.RUnlock()
	if store == nil {
		return ""
	}
	sess := store.Get(sessionID)
	if sess == nil || len(sess.Threads) == 0 {
		return ""
	}
	threads := make([]*session.Thread, 0, len(sess.Threads))
	for _, t := range sess.Threads {
		threads = append(threads, t)
	}
	return session.AssembleThreadContext(threads, activeThreadID)
}
