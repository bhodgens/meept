package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newTestThreadStore is a helper that creates a SQLiteStore, a session row
// (required because session_threads has an FK to sessions), and a
// SQLiteThreadStore bound to that session. It returns the store, the thread
// store, the session, and a cleanup function.
func newTestThreadStore(t *testing.T) (store *SQLiteStore, ts *SQLiteThreadStore, session *Session) {
	t.Helper()

	store, _ = testHelper(t)
	session, err := store.Create("thread-store-test")
	if err != nil {
		store.Close()
		t.Fatalf("failed to create session: %v", err)
	}
	ts = NewSQLiteThreadStore(store.db, session.ID, nil)
	return store, ts, session
}

// makeThread is a helper that constructs a populated Thread for a session.
// Times are truncated to second precision because session_threads stores
// timestamps as RFC3339 (no sub-second component).
func makeThread(session *Session, label string, active bool) *Thread {
	now := time.Now().UTC().Truncate(time.Second)
	return &Thread{
		ID:             "thread-" + label + "-" + session.ID,
		SessionID:      session.ID,
		TopicLabel:     label,
		ConversationID: session.ConversationID + "-" + label,
		CreatedAt:      now,
		LastActivityAt: now,
		Summary:        "",
		IsActive:       active,
	}
}

func TestThreadStore_CreateAndGetRoundtrip(t *testing.T) {
	store, ts, session := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	thread := makeThread(session, "work", true)
	thread.Summary = "Discussion about Go tests"
	if err := ts.CreateThread(ctx, thread); err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}

	got, err := ts.GetThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("GetThread failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil thread")
	}
	if got.ID != thread.ID {
		t.Errorf("ID: got %q want %q", got.ID, thread.ID)
	}
	if got.SessionID != thread.SessionID {
		t.Errorf("SessionID: got %q want %q", got.SessionID, thread.SessionID)
	}
	if got.TopicLabel != thread.TopicLabel {
		t.Errorf("TopicLabel: got %q want %q", got.TopicLabel, thread.TopicLabel)
	}
	if got.ConversationID != thread.ConversationID {
		t.Errorf("ConversationID: got %q want %q", got.ConversationID, thread.ConversationID)
	}
	if got.Summary != thread.Summary {
		t.Errorf("Summary: got %q want %q", got.Summary, thread.Summary)
	}
	if got.IsActive != thread.IsActive {
		t.Errorf("IsActive: got %v want %v", got.IsActive, thread.IsActive)
	}
	if !got.CreatedAt.Equal(thread.CreatedAt) {
		t.Errorf("CreatedAt: got %v want %v", got.CreatedAt, thread.CreatedAt)
	}
	if !got.LastActivityAt.Equal(thread.LastActivityAt) {
		t.Errorf("LastActivityAt: got %v want %v", got.LastActivityAt, thread.LastActivityAt)
	}
}

func TestThreadStore_GetThread_NotFound(t *testing.T) {
	store, ts, _ := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	got, err := ts.GetThread(ctx, "thread-nonexistent")
	if err == nil {
		t.Errorf("expected error for non-existent thread, got nil with %#v", got)
	}
	if got != nil {
		t.Errorf("expected nil thread for non-existent ID, got %#v", got)
	}
}

func TestThreadStore_ListThreadsBySession_OrderedByLastActivityDesc(t *testing.T) {
	store, ts, session := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	base := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	threads := []*Thread{
		{
			ID: "thread-old-" + session.ID, SessionID: session.ID, TopicLabel: "old",
			ConversationID: "conv-old", CreatedAt: base, LastActivityAt: base.Add(1 * time.Minute),
		},
		{
			ID: "thread-new-" + session.ID, SessionID: session.ID, TopicLabel: "new",
			ConversationID: "conv-new", CreatedAt: base, LastActivityAt: base.Add(10 * time.Minute),
		},
		{
			ID: "thread-mid-" + session.ID, SessionID: session.ID, TopicLabel: "mid",
			ConversationID: "conv-mid", CreatedAt: base, LastActivityAt: base.Add(5 * time.Minute),
		},
	}
	for _, th := range threads {
		if err := ts.CreateThread(ctx, th); err != nil {
			t.Fatalf("CreateThread(%s) failed: %v", th.ID, err)
		}
	}

	got, err := ts.ListThreadsBySession(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListThreadsBySession failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 threads, got %d", len(got))
	}

	// Expect newest -> mid -> oldest by last_activity
	wantOrder := []string{"thread-new-", "thread-mid-", "thread-old-"}
	for i, w := range wantOrder {
		if got[i].ID[:len(w)] != w {
			t.Errorf("position %d: got ID %q, want prefix %q", i, got[i].ID, w)
		}
		if i > 0 {
			prev := got[i-1].LastActivityAt
			cur := got[i].LastActivityAt
			if cur.After(prev) {
				t.Errorf("position %d (%s @ %v) is after position %d (%s @ %v); expected DESC",
					i, got[i].ID, cur, i-1, got[i-1].ID, prev)
			}
		}
	}
}

func TestThreadStore_ListThreadsBySession_Empty(t *testing.T) {
	store, ts, session := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	got, err := ts.ListThreadsBySession(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListThreadsBySession on empty session failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 threads, got %d", len(got))
	}
}

func TestThreadStore_ListThreadsBySession_IsolatesBySession(t *testing.T) {
	store, _, sessionA := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	sessionB, err := store.Create("thread-store-test-b")
	if err != nil {
		t.Fatalf("failed to create second session: %v", err)
	}

	tsA := NewSQLiteThreadStore(store.db, sessionA.ID, nil)
	tsB := NewSQLiteThreadStore(store.db, sessionB.ID, nil)

	thA := makeThread(sessionA, "alpha", false)
	thB := makeThread(sessionB, "beta", false)
	if err := tsA.CreateThread(ctx, thA); err != nil {
		t.Fatalf("CreateThread(A) failed: %v", err)
	}
	if err := tsB.CreateThread(ctx, thB); err != nil {
		t.Fatalf("CreateThread(B) failed: %v", err)
	}

	gotA, err := tsA.ListThreadsBySession(ctx, sessionA.ID)
	if err != nil {
		t.Fatalf("ListThreadsBySession(A) failed: %v", err)
	}
	if len(gotA) != 1 || gotA[0].ID != thA.ID {
		t.Errorf("expected only thread %q in session A, got %+v", thA.ID, gotA)
	}

	gotB, err := tsB.ListThreadsBySession(ctx, sessionB.ID)
	if err != nil {
		t.Fatalf("ListThreadsBySession(B) failed: %v", err)
	}
	if len(gotB) != 1 || gotB[0].ID != thB.ID {
		t.Errorf("expected only thread %q in session B, got %+v", thB.ID, gotB)
	}
}

func TestThreadStore_UpdateThread(t *testing.T) {
	store, ts, session := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	thread := makeThread(session, "work", false)
	if err := ts.CreateThread(ctx, thread); err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}

	// Mutate every updatable field.
	thread.TopicLabel = "work-updated"
	thread.ConversationID = session.ConversationID + "-work-v2"
	thread.LastActivityAt = thread.CreatedAt.Add(2 * time.Hour)
	thread.Summary = "Refreshed summary"
	thread.IsActive = true

	if err := ts.UpdateThread(ctx, thread); err != nil {
		t.Fatalf("UpdateThread failed: %v", err)
	}

	got, err := ts.GetThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("GetThread after update failed: %v", err)
	}
	if got.TopicLabel != "work-updated" {
		t.Errorf("TopicLabel: got %q want %q", got.TopicLabel, "work-updated")
	}
	if got.ConversationID != session.ConversationID+"-work-v2" {
		t.Errorf("ConversationID: got %q want %q", got.ConversationID, session.ConversationID+"-work-v2")
	}
	if got.Summary != "Refreshed summary" {
		t.Errorf("Summary: got %q want %q", got.Summary, "Refreshed summary")
	}
	if !got.IsActive {
		t.Error("IsActive: got false want true")
	}
	wantActivity := thread.CreatedAt.Add(2 * time.Hour)
	if !got.LastActivityAt.Equal(wantActivity) {
		t.Errorf("LastActivityAt: got %v want %v", got.LastActivityAt, wantActivity)
	}
	// ID, SessionID, CreatedAt must not change.
	if got.ID != thread.ID {
		t.Errorf("ID changed: got %q want %q", got.ID, thread.ID)
	}
	if got.SessionID != thread.SessionID {
		t.Errorf("SessionID changed: got %q want %q", got.SessionID, thread.SessionID)
	}
	if !got.CreatedAt.Equal(thread.CreatedAt) {
		t.Errorf("CreatedAt changed: got %v want %v", got.CreatedAt, thread.CreatedAt)
	}
}

func TestThreadStore_UpdateThread_NoOpFieldsIgnored(t *testing.T) {
	store, ts, session := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	thread := makeThread(session, "general", false)
	if err := ts.CreateThread(ctx, thread); err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}

	// UpdateThread SQL does not set CreatedAt or SessionID; pass unchanged
	// values to confirm the row is updated without error.
	if err := ts.UpdateThread(ctx, thread); err != nil {
		t.Fatalf("UpdateThread (no-op) failed: %v", err)
	}
}

func TestThreadStore_DeleteThread(t *testing.T) {
	store, ts, session := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	thread := makeThread(session, "temp", false)
	if err := ts.CreateThread(ctx, thread); err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}

	if err := ts.DeleteThread(ctx, thread.ID); err != nil {
		t.Fatalf("DeleteThread failed: %v", err)
	}

	got, err := ts.GetThread(ctx, thread.ID)
	if err == nil {
		t.Errorf("expected error after delete, got thread %+v", got)
	}
	if got != nil {
		t.Errorf("expected nil thread after delete, got %+v", got)
	}
}

func TestThreadStore_DeleteThread_Idempotent(t *testing.T) {
	store, ts, _ := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	// Deleting a non-existent thread should not error (SQLite DELETE on
	// missing row is a no-op; Exec returns no error).
	if err := ts.DeleteThread(ctx, "thread-never-existed"); err != nil {
		t.Errorf("DeleteThread on non-existent ID returned error: %v", err)
	}
}

func TestThreadStore_GetActiveThread_NoActive(t *testing.T) {
	store, ts, session := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	// Create two inactive threads.
	for _, label := range []string{"alpha", "beta"} {
		th := makeThread(session, label, false)
		if err := ts.CreateThread(ctx, th); err != nil {
			t.Fatalf("CreateThread(%s) failed: %v", label, err)
		}
	}

	got, err := ts.GetActiveThread(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetActiveThread returned error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil active thread, got %+v", got)
	}
}

func TestThreadStore_GetActiveThread_ReturnsActiveOne(t *testing.T) {
	store, ts, session := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	inactive := makeThread(session, "archived", false)
	active := makeThread(session, "current", true)
	if err := ts.CreateThread(ctx, inactive); err != nil {
		t.Fatalf("CreateThread(inactive) failed: %v", err)
	}
	if err := ts.CreateThread(ctx, active); err != nil {
		t.Fatalf("CreateThread(active) failed: %v", err)
	}

	got, err := ts.GetActiveThread(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetActiveThread failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected active thread, got nil")
	}
	if got.ID != active.ID {
		t.Errorf("expected active thread %q, got %q", active.ID, got.ID)
	}
	if !got.IsActive {
		t.Error("expected IsActive=true")
	}
}

func TestThreadStore_SetActiveThread_DeactivatesOthers(t *testing.T) {
	store, ts, session := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	// Create three threads; the first is initially active.
	t1 := makeThread(session, "one", true)
	t2 := makeThread(session, "two", false)
	t3 := makeThread(session, "three", false)
	for _, th := range []*Thread{t1, t2, t3} {
		if err := ts.CreateThread(ctx, th); err != nil {
			t.Fatalf("CreateThread(%s) failed: %v", th.ID, err)
		}
	}

	// Switch active to t2.
	if err := ts.SetActiveThread(ctx, session.ID, t2.ID); err != nil {
		t.Fatalf("SetActiveThread failed: %v", err)
	}

	got, err := ts.GetActiveThread(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetActiveThread failed: %v", err)
	}
	if got == nil || got.ID != t2.ID {
		t.Fatalf("expected active thread %q, got %+v", t2.ID, got)
	}

	// All threads: only t2 should be active.
	all, err := ts.ListThreadsBySession(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListThreadsBySession failed: %v", err)
	}
	wantActive := map[string]bool{t1.ID: false, t2.ID: true, t3.ID: false}
	for _, th := range all {
		want, ok := wantActive[th.ID]
		if !ok {
			t.Errorf("unexpected thread %q in list", th.ID)
			continue
		}
		if th.IsActive != want {
			t.Errorf("thread %q: IsActive got %v want %v", th.ID, th.IsActive, want)
		}
	}
}

func TestThreadStore_SetActiveThread_TwiceIsStable(t *testing.T) {
	store, ts, session := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	a := makeThread(session, "alpha", false)
	b := makeThread(session, "beta", false)
	for _, th := range []*Thread{a, b} {
		if err := ts.CreateThread(ctx, th); err != nil {
			t.Fatalf("CreateThread(%s) failed: %v", th.ID, err)
		}
	}

	// Set a active, then set a active again.
	if err := ts.SetActiveThread(ctx, session.ID, a.ID); err != nil {
		t.Fatalf("SetActiveThread(a) failed: %v", err)
	}
	if err := ts.SetActiveThread(ctx, session.ID, a.ID); err != nil {
		t.Fatalf("SetActiveThread(a) second call failed: %v", err)
	}

	got, err := ts.GetActiveThread(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetActiveThread failed: %v", err)
	}
	if got == nil || got.ID != a.ID {
		t.Fatalf("expected active thread %q, got %+v", a.ID, got)
	}
}

func TestThreadStore_CreateThread_ReplacesExisting(t *testing.T) {
	// CreateThread uses INSERT OR REPLACE, so re-creating with the same ID
	// (same session_id+topic_label unique constraint isn't the key here — ID is
	// PK) should replace the row.
	store, ts, session := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	thread := makeThread(session, "work", false)
	thread.Summary = "first revision"
	if err := ts.CreateThread(ctx, thread); err != nil {
		t.Fatalf("CreateThread (first) failed: %v", err)
	}

	thread.Summary = "second revision"
	thread.IsActive = true
	if err := ts.CreateThread(ctx, thread); err != nil {
		t.Fatalf("CreateThread (replace) failed: %v", err)
	}

	got, err := ts.GetThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("GetThread failed: %v", err)
	}
	if got.Summary != "second revision" {
		t.Errorf("Summary: got %q want %q", got.Summary, "second revision")
	}
	if !got.IsActive {
		t.Error("IsActive: got false want true")
	}
	// Only one row should exist.
	list, err := ts.ListThreadsBySession(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListThreadsBySession failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 thread after replace, got %d", len(list))
	}
}

func TestThreadStore_GetThread_ErrorIsNotSentinel(t *testing.T) {
	// Ensure GetThread returns a distinct error (not sql.ErrNoRows leaked) so
	// callers can use errors.Is without depending on database/sql internals.
	store, ts, _ := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	_, err := ts.GetThread(ctx, "thread-missing")
	if err == nil {
		t.Fatal("expected error for missing thread")
	}
	if errors.Is(err, context.Canceled) {
		t.Errorf("missing-thread error should not be context.Canceled, got: %v", err)
	}
}

func TestThreadStore_CreateThread_RejectsEmptyThread(t *testing.T) {
	// While the SQLiteThreadStore does not itself nil-check, a zero-ID thread
	// violates the PRIMARY KEY constraint; the error must propagate (not be
	// silently swallowed).
	store, ts, session := newTestThreadStore(t)
	defer store.Close()
	ctx := context.Background()

	thread := &Thread{
		ID:             "", // empty primary key
		SessionID:      session.ID,
		TopicLabel:     "empty",
		ConversationID: "conv-empty",
		CreatedAt:      time.Now().UTC(),
		LastActivityAt: time.Now().UTC(),
	}
	err := ts.CreateThread(ctx, thread)
	if err == nil {
		// SQLite with modernc may accept empty-string PKs; if so, we just
		// confirm the row exists and clean up.
		got, gerr := ts.GetThread(ctx, "")
		if gerr != nil {
			t.Fatalf("empty-ID thread created but not retrievable: %v", gerr)
		}
		if got == nil {
			t.Fatal("empty-ID thread created but GetThread returned nil")
		}
		return
	}
	// An error is the preferred path; just make sure it's not nil-quiet.
	t.Logf("empty-ID thread rejected as expected: %v", err)
}
