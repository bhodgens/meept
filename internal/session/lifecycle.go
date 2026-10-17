// lifecycle.go provides the session teardown notification surface
// (session-affinity plan Contract 5): a direct Go callback hook fired by
// both store implementations on every archive/delete path, plus the idle
// reaper that fires SessionEventIdle once per idle episode.
//
// This is deliberately NOT a bus topic: the WS surface must not change and
// `make graphs` must stay green. Leaf 06 subscribes a TeardownCoordinator
// (internal/llm) as the hook to release upstream llama-server KV slots.
package session

import (
	"context"
	"log/slog"
	"reflect"
	"sync"
	"time"
)

// SessionEvent enumerates teardown-relevant lifecycle moments.
type SessionEvent string

const (
	// SessionEventArchive fires when a session is archived (user action).
	SessionEventArchive SessionEvent = "archive"
	// SessionEventDelete fires when a session is removed.
	SessionEventDelete SessionEvent = "delete"
	// SessionEventIdle fires after a session has had no LLM-relevant
	// activity for the configured IdleRelease quiet period.
	SessionEventIdle SessionEvent = "idle"
)

// SessionLifecycleHook receives session teardown notifications. It is a
// direct Go callback, not a bus topic. The hook receives ONLY the id and
// the event — loading the session for the hook is the subscriber's job.
// Implementations must be fast and non-blocking: the store invokes the
// hook synchronously (outside its own lock) on teardown paths. A panicking
// or erroring hook never fails the Archive/Delete caller — teardown
// notification is best-effort by contract.
type SessionLifecycleHook interface {
	OnSessionEvent(ctx context.Context, sessionID string, event SessionEvent)
}

// invokeLifecycleHook fires hook for one event, best-effort: nil and
// typed-nil hooks are no-ops (the typed-nil interface trap: a
// (*T)(nil) stored in the interface is non-nil but must not be invoked),
// a panicking hook is recovered and logged at Warn, and nothing is ever
// propagated to the Archive/Delete caller.
func invokeLifecycleHook(hook SessionLifecycleHook, ctx context.Context, sessionID string, event SessionEvent) {
	if hook == nil {
		return
	}
	if v := reflect.ValueOf(hook); v.Kind() == reflect.Pointer && v.IsNil() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("session lifecycle hook panicked",
				"session_id", sessionID,
				"event", string(event),
				"panic", r)
		}
	}()
	hook.OnSessionEvent(ctx, sessionID, event)
}

// --- Store-side hook plumbing ---

// lifecycleFire is a pending hook invocation collected under the store
// lock and fired after the lock is released (never hold a mutex across
// the hook call — repo invariant).
type lifecycleFire struct {
	hook      SessionLifecycleHook
	sessionID string
	event     SessionEvent
}

// fireLifecycle runs the pending invocations outside any store lock.
// Each event is independent: one panicking hook does not skip the rest.
func fireLifecycle(logger *slog.Logger, fires ...lifecycleFire) {
	for _, f := range fires {
		if f.hook == nil {
			continue
		}
		if logger != nil {
			logger.Debug("session lifecycle event",
				"session_id", f.sessionID,
				"event", string(f.event))
		}
		invokeLifecycleHook(f.hook, context.Background(), f.sessionID, f.event)
	}
}

// storeLifecycleMixin is embedded by both MemoryStore and SQLiteStore.
// The hook field is guarded by the embedding store's own mutex — the
// mixin's methods are called only while that lock is held for the field
// access, exactly like every other store field.
type storeLifecycleMixin struct {
	lifecycleHook SessionLifecycleHook
}

// SetLifecycleHook wires (or replaces) the teardown notification hook.
// Typed-nil safe: a plain nil and a typed-nil (*T)(nil) are both ignored
// (checked via reflection — the plain `hook != nil` test misses the
// typed-nil trap), so callers can pass maybe-nil hooks unguarded.
// To detach a hook, call SetLifecycleHook again with a real hook; there is
// deliberately no un-set (a store without a hook is the normal state).
func (m *storeLifecycleMixin) SetLifecycleHook(hook SessionLifecycleHook) {
	if hook == nil {
		return
	}
	if v := reflect.ValueOf(hook); v.Kind() == reflect.Pointer && v.IsNil() {
		return
	}
	m.lifecycleHook = hook
}

// collectLocked snapshots pending fires under the CALLER'S lock. Call it
// only while the embedding store's mutex is held.
func (m *storeLifecycleMixin) collectLocked(logger *slog.Logger, event SessionEvent, sessionIDs ...string) []lifecycleFire {
	if m.lifecycleHook == nil || len(sessionIDs) == 0 {
		return nil
	}
	fires := make([]lifecycleFire, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		fires = append(fires, lifecycleFire{hook: m.lifecycleHook, sessionID: id, event: event})
	}
	return fires
}

// --- IdleReaper ---

// IdleReaper fires SessionEventIdle for sessions whose last activity is
// older than the configured idle-release duration. It REUSES the existing
// ActivityTracker's per-session last-activity data (no second tracker —
// plan constraint) and runs on its own goroutine with a ticker; Start/Stop
// mirror the EmbeddingWorker lifecycle (startOnce/stopOnce, Stop waits for
// the goroutine to exit — no leaked ticker on shutdown).
type IdleReaper struct {
	// tracker is the shared ActivityTracker (activity_tracker.go). The
	// reaper consumes its per-session last-activity data directly; the
	// RecordActivity feed becomes the production activity source this
	// daemon wiring (leaf 06) provides.
	tracker *ActivityTracker
	hook    SessionLifecycleHook
	logger  *slog.Logger

	// idleCheckInterval is the ticker period (leaf contract: interval is a
	// field so tests can shorten it). Test-only writes happen before Start;
	// Start/Stop synchronize via startOnce/stopOnce.
	idleCheckInterval time.Duration

	// idleRelease is the quiet period after which a session is idle.
	// Zero disables reaping entirely (the config-absent default — leaf 02
	// wires daemon config via SetIdleRelease).
	idleRelease time.Duration

	mu sync.Mutex
	// idleFired records sessions in a currently-fired idle episode: while
	// present, the session is idle AND already notified (fires once per
	// episode). Re-armed (removed) by new activity.
	idleFired map[string]struct{}

	startOnce sync.Once
	stopOnce  sync.Once
	stopChan  chan struct{}
	stopped   chan struct{}
	started   bool
}

// defaultIdleCheckInterval is the reaper's ticker period (leaf contract:
// default 1m).
const defaultIdleCheckInterval = 1 * time.Minute

// NewIdleReaper constructs a reaper over the store's activity tracker.
// Reaping is DISABLED until SetIdleRelease is called with a positive
// duration. hook may be nil (disabled reaper); it is re-read per tick.
func NewIdleReaper(tracker *ActivityTracker, hook SessionLifecycleHook, logger *slog.Logger) *IdleReaper {
	if logger == nil {
		logger = slog.Default()
	}
	return &IdleReaper{
		tracker:           tracker,
		hook:              hook,
		logger:            logger,
		idleCheckInterval: defaultIdleCheckInterval,
		idleFired:         make(map[string]struct{}),
		stopChan:          make(chan struct{}),
		stopped:           make(chan struct{}),
	}
}

// SetIdleRelease configures the idle quiet period. Zero (or negative)
// disables reaping — the zero value is the config-absent default, so the
// reaper never fires until daemon wiring opts in.
func (r *IdleReaper) SetIdleRelease(d time.Duration) {
	if d < 0 {
		d = 0
	}
	r.mu.Lock()
	r.idleRelease = d
	r.mu.Unlock()
}

// Start launches the reaper goroutine. Safe to call multiple times; only
// the first call launches the goroutine.
func (r *IdleReaper) Start() {
	r.startOnce.Do(func() {
		r.started = true
		go r.run()
	})
}

// Stop signals the reaper to stop and waits for its goroutine to exit.
// Safe to call multiple times and without Start.
func (r *IdleReaper) Stop() {
	r.stopOnce.Do(func() {
		close(r.stopChan)
	})
	if r.started {
		<-r.stopped
	}
}

func (r *IdleReaper) run() {
	defer close(r.stopped)
	ticker := time.NewTicker(r.idleCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			r.reaperTick(context.Background())
		}
	}
}

// reaperTick compares now-lastActivity (from the shared ActivityTracker)
// against the idle-release window. A session crossing into idle fires
// SessionEventIdle EXACTLY ONCE and is recorded in idleFired; a session
// with fresh activity is removed from idleFired (re-arm). Disabled
// (idleRelease <= 0) ticks do nothing and do not disturb episode state.
func (r *IdleReaper) reaperTick(ctx context.Context) {
	r.mu.Lock()
	release := r.idleRelease
	if release <= 0 {
		r.mu.Unlock()
		return
	}
	hook := r.hook

	// Collect under lock: sessions newly idle (fire) and sessions that
	// re-armed (unmark). Snapshot the tracker's activity map under ITS OWN
	// RLock inside our critical section — lock ordering is always
	// reaper.mu -> tracker.mu (and no other path takes tracker.mu while
	// holding a store lock), so no inversion is possible.
	var fires []lifecycleFire
	if hook != nil {
		type activitySnapshot struct {
			id           string
			lastActivity time.Time
		}
		r.tracker.mu.RLock()
		snapshots := make([]activitySnapshot, 0, len(r.tracker.activity))
		for id, state := range r.tracker.activity {
			snapshots = append(snapshots, activitySnapshot{id: id, lastActivity: state.LastActivity})
		}
		r.tracker.mu.RUnlock()

		now := time.Now()
		for _, snap := range snapshots {
			if now.Sub(snap.lastActivity) >= release {
				if _, already := r.idleFired[snap.id]; !already {
					r.idleFired[snap.id] = struct{}{}
					fires = append(fires, lifecycleFire{hook: hook, sessionID: snap.id, event: SessionEventIdle})
				}
			} else {
				delete(r.idleFired, snap.id) // new activity re-arms the episode
			}
		}
	} else {
		r.idleFired = make(map[string]struct{})
	}
	r.mu.Unlock()

	fireLifecycle(r.logger, fires...)
}
