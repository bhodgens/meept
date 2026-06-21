// Package models provides the view models for the TUI.
package models

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
)

// TestThreadIndicator_RendersWhenThreadsPresent verifies that the indicator
// shows the active thread's topic label when a session with threads is set.
func TestThreadIndicator_RendersWhenThreadsPresent(t *testing.T) {
	t.Parallel()

	now := time.Now()
	ind := newThreadIndicatorState()
	ind.SetSession(&types.Session{
		ID:             "s1",
		ActiveThreadID: "t1",
		Threads: map[string]types.Thread{
			"t1": {
				ID:             "t1",
				TopicLabel:     "work",
				LastActivityAt: now,
			},
			"t2": {
				ID:             "t2",
				TopicLabel:     "personal",
				LastActivityAt: now.Add(-time.Hour),
			},
		},
	})

	view := ind.View()
	if view == "" {
		t.Fatalf("expected non-empty view when threads are present")
	}
	if !strings.Contains(view, "work") {
		t.Errorf("expected view to contain topic 'work' (active thread), got %q", view)
	}
	if !strings.Contains(view, "thread:") {
		t.Errorf("expected view to contain 'thread:' prefix, got %q", view)
	}
	// Two threads should render the count.
	if !strings.Contains(view, "2 threads") {
		t.Errorf("expected view to contain '(2 threads)' count, got %q", view)
	}
}

// TestThreadIndicator_EmptyWhenNoThreads verifies that the indicator renders
// nothing (empty string) when no threads are present.
func TestThreadIndicator_EmptyWhenNoThreads(t *testing.T) {
	t.Parallel()

	ind := newThreadIndicatorState()
	if view := ind.View(); view != "" {
		t.Errorf("expected empty view when no threads, got %q", view)
	}
	if ind.IsActive() {
		t.Errorf("expected IsActive()=false when no threads")
	}
}

// TestThreadIndicator_NilSessionClears verifies that setting a nil session
// resets the indicator state.
func TestThreadIndicator_NilSessionClears(t *testing.T) {
	t.Parallel()

	now := time.Now()
	ind := newThreadIndicatorState()
	ind.SetSession(&types.Session{
		ID:             "s1",
		ActiveThreadID: "t1",
		Threads: map[string]types.Thread{
			"t1": {ID: "t1", TopicLabel: "work", LastActivityAt: now},
		},
	})
	if !ind.IsActive() {
		t.Fatalf("expected active after setting session with threads")
	}

	ind.SetSession(nil)
	if ind.IsActive() {
		t.Errorf("expected inactive after nil session")
	}
	if view := ind.View(); view != "" {
		t.Errorf("expected empty view after nil session, got %q", view)
	}
}

// TestThreadIndicator_FallsBackToGeneralForBlankLabel verifies that a thread
// with no topic label renders as "general".
func TestThreadIndicator_FallsBackToGeneralForBlankLabel(t *testing.T) {
	t.Parallel()

	now := time.Now()
	ind := newThreadIndicatorState()
	ind.SetSession(&types.Session{
		ID:             "s1",
		ActiveThreadID: "t1",
		Threads: map[string]types.Thread{
			"t1": {ID: "t1", TopicLabel: "", LastActivityAt: now},
		},
	})

	view := ind.View()
	if !strings.Contains(view, "general") {
		t.Errorf("expected 'general' fallback for blank label, got %q", view)
	}
}

// TestThreadIndicator_SelectsActiveThread verifies that the active thread
// (not the first one) is rendered.
func TestThreadIndicator_SelectsActiveThread(t *testing.T) {
	t.Parallel()

	now := time.Now()
	ind := newThreadIndicatorState()
	ind.SetSession(&types.Session{
		ID:             "s1",
		ActiveThreadID: "t2",
		Threads: map[string]types.Thread{
			"t1": {ID: "t1", TopicLabel: "first", LastActivityAt: now},
			"t2": {ID: "t2", TopicLabel: "second-active", LastActivityAt: now},
		},
	})

	view := ind.View()
	if !strings.Contains(view, "second-active") {
		t.Errorf("expected active thread 'second-active' in view, got %q", view)
	}
	if strings.Contains(view, "thread: first") {
		t.Errorf("inactive thread 'first' should not be the rendered label, got %q", view)
	}
}

// TestThreadIndicator_LargeThreadCount verifies that the count renders as "9+"
// when there are more than 9 threads.
func TestThreadIndicator_LargeThreadCount(t *testing.T) {
	t.Parallel()

	now := time.Now()
	threads := make(map[string]types.Thread, 12)
	for i := 0; i < 12; i++ {
		id := "t" + string(rune('a'+i))
		threads[id] = types.Thread{
			ID:             id,
			TopicLabel:     "topic-" + id,
			LastActivityAt: now,
		}
	}
	ind := newThreadIndicatorState()
	ind.SetSession(&types.Session{
		ID:             "s1",
		ActiveThreadID: "ta",
		Threads:        threads,
	})

	view := ind.View()
	if !strings.Contains(view, "9+ threads") {
		t.Errorf("expected '9+ threads' count for 12 threads, got %q", view)
	}
}
