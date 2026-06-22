package agent

import (
	"context"
	"sync"
)

// HookPayload is the data structure passed to HTTP hooks when events fire.
// It captures the event name, originating agent, session, and arbitrary
// event-specific data.
type HookPayload struct {
	Event     string                 `json:"event"`
	AgentID   string                 `json:"agent_id,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// NewHookPayload constructs a HookPayload from the provided fields.
func NewHookPayload(event, agentID, sessionID string, data map[string]interface{}) HookPayload {
	return HookPayload{
		Event:     event,
		AgentID:   agentID,
		SessionID: sessionID,
		Data:      data,
	}
}

// HookBatchExecutor fans out hook payloads to registered HTTP hooks in
// parallel. It is safe for concurrent use.
type HookBatchExecutor struct {
	mu    sync.RWMutex
	hooks []*HTTPHook
}

// NewHookBatchExecutor constructs an empty executor.
func NewHookBatchExecutor() *HookBatchExecutor {
	return &HookBatchExecutor{}
}

// AddHook registers an HTTP hook with the executor.
func (e *HookBatchExecutor) AddHook(h *HTTPHook) {
	if e == nil || h == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hooks = append(e.hooks, h)
}

// ExecuteAll fires the payload at every registered hook in parallel.
func (e *HookBatchExecutor) ExecuteAll(ctx context.Context, payload HookPayload) {
	if e == nil {
		return
	}
	e.mu.RLock()
	hooks := make([]*HTTPHook, len(e.hooks))
	copy(hooks, e.hooks)
	e.mu.RUnlock()

	var wg sync.WaitGroup
	for _, h := range hooks {
		wg.Add(1)
		go func(hook *HTTPHook) {
			defer wg.Done()
			_ = hook.Execute(ctx, payload)
		}(h)
	}
	wg.Wait()
}

// FileWatcherHook is a placeholder for the file-watcher hook. Methods match
// the call sites in loop.go (Start, Stop). The full implementation lives
// in a separate file when the file-watcher feature is wired.
type FileWatcherHook struct{}

// Start begins watching. Accepts a context for future cancellation support.
// Returns nil to keep the loop path no-op safe.
func (fw *FileWatcherHook) Start(_ context.Context) error { return nil }

// Stop ceases watching.
func (fw *FileWatcherHook) Stop() {}
