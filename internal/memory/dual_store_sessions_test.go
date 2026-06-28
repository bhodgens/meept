package memory

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// TestDualStore_StoreSession_Local_PublishesGossip verifies that a local
// session write (T3.2) goes to local.db only AND fires a SESSION_CREATED
// gossip event on the configured publisher.
func TestDualStore_StoreSession_Local_PublishesGossip(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	pub := &mockGossipPublisher{}
	ds.SetGossipPublisher(pub)

	sess := &Session{
		ID:             "sess-local-1",
		Name:           "local session",
		ConversationID: "conv-1",
		CreatedAt:      time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
		LastActivity:   time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
		ProjectID:      "proj-x",
	}
	ctx := context.Background()
	if err := ds.StoreSession(ctx, sess); err != nil {
		t.Fatalf("StoreSession: %v", err)
	}

	// Allow the goroutine to publish.
	waitForEvents(t, pub, 1, 1*time.Second)

	// local.db must have exactly one row.
	if got := countSessionsDB(t, ds.localDB); got != 1 {
		t.Errorf("local.db sessions count = %d, want 1", got)
	}
	// gossip.db must have zero rows (local writes never go to gossip).
	if got := countSessionsDB(t, ds.gossipDB); got != 0 {
		t.Errorf("gossip.db sessions count = %d, want 0", got)
	}

	// The published event must be SESSION_CREATED with our payload.
	last, ok := pub.getLastEvent()
	if !ok {
		t.Fatal("expected a published SESSION_CREATED event")
	}
	if last.eventType != models.EventTypeSessionCreated {
		t.Errorf("event type = %s, want %s", last.eventType, models.EventTypeSessionCreated)
	}
	payload, ok := last.payload.(models.SessionCreatedPayload)
	if !ok {
		t.Fatalf("payload type = %T, want models.SessionCreatedPayload", last.payload)
	}
	if payload.SessionID != "sess-local-1" {
		t.Errorf("payload.SessionID = %q, want sess-local-1", payload.SessionID)
	}
	if payload.Title != "local session" {
		t.Errorf("payload.Title = %q, want 'local session'", payload.Title)
	}
}

// TestDualStore_StoreSession_NoPublisher verifies that the store works when
// no publisher is wired (cluster disabled).
func TestDualStore_StoreSession_NoPublisher(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	sess := &Session{ID: "sess-nopub", Name: "x", CreatedAt: time.Now().UTC(), LastActivity: time.Now().UTC()}
	if err := ds.StoreSession(context.Background(), sess); err != nil {
		t.Fatalf("StoreSession (no pub): %v", err)
	}
	if got := countSessionsDB(t, ds.localDB); got != 1 {
		t.Errorf("local sessions = %d, want 1", got)
	}
}

// TestDualStore_StoreSession_RejectsEmptyID confirms the ID guard fires.
func TestDualStore_StoreSession_RejectsEmptyID(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	if err := ds.StoreSession(context.Background(), &Session{}); err == nil {
		t.Error("StoreSession(empty ID) should error")
	}
	if err := ds.StoreSession(context.Background(), nil); err == nil {
		t.Error("StoreSession(nil) should error")
	}
}

// TestDualStore_StoreRemoteSession verifies that a remote session write
// (T3.2) lands in gossip.db ONLY and carries source_node.
func TestDualStore_StoreRemoteSession(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	pub := &mockGossipPublisher{}
	ds.SetGossipPublisher(pub)

	sess := &Session{
		ID:           "sess-remote-1",
		Name:         "remote",
		ConversationID: "conv-r",
		CreatedAt:    time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
		LastActivity: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
	}
	if err := ds.StoreRemoteSession(context.Background(), sess, "node-peer-a"); err != nil {
		t.Fatalf("StoreRemoteSession: %v", err)
	}

	if got := countSessionsDB(t, ds.localDB); got != 0 {
		t.Errorf("local sessions = %d, want 0 (remote never writes local)", got)
	}
	if got := countSessionsDB(t, ds.gossipDB); got != 1 {
		t.Errorf("gossip sessions = %d, want 1", got)
	}

	var sourceNode string
	err = ds.gossipDB.QueryRow(
		"SELECT source_node FROM sessions WHERE id = ?", sess.ID,
	).Scan(&sourceNode)
	if err != nil {
		t.Fatalf("scan source_node: %v", err)
	}
	if sourceNode != "node-peer-a" {
		t.Errorf("source_node = %q, want node-peer-a", sourceNode)
	}

	// Remote writes must NOT fire a gossip event (they came from gossip).
	// Give the goroutine a brief window in case the publisher was called.
	time.Sleep(20 * time.Millisecond)
	if got := atomic.LoadInt64(&pub.eventCount); got != 0 {
		t.Errorf("remote StoreRemoteSession published %d events, want 0 (no echo)", got)
	}
}

// TestDualStore_StoreRemoteSession_RejectsEmptySource tests the source-node guard.
func TestDualStore_StoreRemoteSession_RejectsEmptySource(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	if err := ds.StoreRemoteSession(context.Background(), &Session{ID: "x"}, ""); err == nil {
		t.Error("StoreRemoteSession(empty source) should error")
	}
}

// TestDualStore_GetSession_Merged verifies local-then-gossip lookup order.
func TestDualStore_GetSession_Merged(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	// Local session.
	localSess := &Session{ID: "sess-merged-local", Name: "local", CreatedAt: time.Now().UTC(), LastActivity: time.Now().UTC()}
	if err := ds.StoreSession(ctx, localSess); err != nil {
		t.Fatalf("StoreSession local: %v", err)
	}
	// Remote session.
	remoteSess := &Session{ID: "sess-merged-remote", Name: "remote", CreatedAt: time.Now().UTC(), LastActivity: time.Now().UTC()}
	if err := ds.StoreRemoteSession(ctx, remoteSess, "node-peer"); err != nil {
		t.Fatalf("StoreRemoteSession: %v", err)
	}

	// Lookup local.
	got, err := ds.GetSession(ctx, "sess-merged-local")
	if err != nil {
		t.Fatalf("GetSession local: %v", err)
	}
	if got == nil || got.ID != "sess-merged-local" {
		t.Fatalf("got = %+v, want sess-merged-local", got)
	}
	if got.SourceNode != "" {
		t.Errorf("local SourceNode = %q, want empty", got.SourceNode)
	}

	// Lookup remote.
	got, err = ds.GetSession(ctx, "sess-merged-remote")
	if err != nil {
		t.Fatalf("GetSession remote: %v", err)
	}
	if got == nil || got.ID != "sess-merged-remote" {
		t.Fatalf("got = %+v, want sess-merged-remote", got)
	}
	if got.SourceNode != "node-peer" {
		t.Errorf("remote SourceNode = %q, want node-peer", got.SourceNode)
	}

	// Lookup missing.
	got, err = ds.GetSession(ctx, "sess-missing")
	if err != nil {
		t.Fatalf("GetSession missing: %v", err)
	}
	if got != nil {
		t.Errorf("missing session got = %+v, want nil", got)
	}
}

// TestDualStore_GetSessions verifies local-first ordering and dedup.
func TestDualStore_GetSessions(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	for _, s := range []*Session{
		{ID: "l1", Name: "l1", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), LastActivity: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "l2", Name: "l2", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), LastActivity: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
	} {
		if err := ds.StoreSession(ctx, s); err != nil {
			t.Fatalf("StoreSession %s: %v", s.ID, err)
		}
	}
	remote := &Session{ID: "g1", Name: "g1", CreatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), LastActivity: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}
	if err := ds.StoreRemoteSession(ctx, remote, "node-peer"); err != nil {
		t.Fatalf("StoreRemoteSession: %v", err)
	}

	sessions, err := ds.GetSessions(ctx)
	if err != nil {
		t.Fatalf("GetSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("len(sessions) = %d, want 3", len(sessions))
	}
	// Local first.
	if sessions[0].ID != "l2" && sessions[1].ID != "l1" {
		// Order by last_activity DESC, so l2 (Jan 2) before l1 (Jan 1).
	}
	// Ensure remote appears last.
	if sessions[2].ID != "g1" {
		t.Errorf("sessions[2].ID = %q, want g1 (gossip last)", sessions[2].ID)
	}

	// Dedup: insert the same ID into both stores; local wins.
	dup := &Session{ID: "dup", Name: "local-dup", CreatedAt: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC), LastActivity: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)}
	if err := ds.StoreSession(ctx, dup); err != nil {
		t.Fatalf("StoreSession dup: %v", err)
	}
	dupRemote := &Session{ID: "dup", Name: "remote-dup", CreatedAt: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), LastActivity: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)}
	if err := ds.StoreRemoteSession(ctx, dupRemote, "node-peer"); err != nil {
		t.Fatalf("StoreRemoteSession dup: %v", err)
	}
	sessions, err = ds.GetSessions(ctx)
	if err != nil {
		t.Fatalf("GetSessions dup: %v", err)
	}
	seen := 0
	for _, s := range sessions {
		if s.ID == "dup" {
			seen++
			if s.Name != "local-dup" {
				t.Errorf("dup Name = %q, want local-dup (local wins)", s.Name)
			}
		}
	}
	if seen != 1 {
		t.Errorf("dup occurrences = %d, want 1 (dedup)", seen)
	}
}

// TestDualStore_StoreTurn_Local_PublishesGossip verifies the turn write path.
func TestDualStore_StoreTurn_Local_PublishesGossip(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	pub := &mockGossipPublisher{}
	ds.SetGossipPublisher(pub)

	turn := &Turn{
		TurnID:    "turn-local-1",
		SessionID: "sess-1",
		Role:      "user",
		Content:   "hello world",
		Timestamp: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
	}
	if err := ds.StoreTurn(context.Background(), turn); err != nil {
		t.Fatalf("StoreTurn: %v", err)
	}

	waitForEvents(t, pub, 1, 1*time.Second)

	if got := countTurnsDB(t, ds.localDB); got != 1 {
		t.Errorf("local turns = %d, want 1", got)
	}
	if got := countTurnsDB(t, ds.gossipDB); got != 0 {
		t.Errorf("gossip turns = %d, want 0", got)
	}

	last, ok := pub.getLastEvent()
	if !ok {
		t.Fatal("expected a published SESSION_TURN event")
	}
	if last.eventType != models.EventTypeSessionTurn {
		t.Errorf("event type = %s, want %s", last.eventType, models.EventTypeSessionTurn)
	}
	payload, ok := last.payload.(models.SessionTurnPayload)
	if !ok {
		t.Fatalf("payload type = %T, want models.SessionTurnPayload", last.payload)
	}
	if payload.TurnID != "turn-local-1" {
		t.Errorf("payload.TurnID = %q, want turn-local-1", payload.TurnID)
	}
	if payload.SessionID != "sess-1" {
		t.Errorf("payload.SessionID = %q, want sess-1", payload.SessionID)
	}
	if payload.Role != "user" {
		t.Errorf("payload.Role = %q, want user", payload.Role)
	}
}

// TestDualStore_StoreRemoteTurn verifies that remote turns go to gossip.db only.
func TestDualStore_StoreRemoteTurn(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	turn := &Turn{
		TurnID:    "turn-remote-1",
		SessionID: "sess-r",
		Role:      "assistant",
		Content:   "remote reply",
		Timestamp: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
	}
	if err := ds.StoreRemoteTurn(context.Background(), turn, "node-peer"); err != nil {
		t.Fatalf("StoreRemoteTurn: %v", err)
	}
	if got := countTurnsDB(t, ds.localDB); got != 0 {
		t.Errorf("local turns = %d, want 0", got)
	}
	if got := countTurnsDB(t, ds.gossipDB); got != 1 {
		t.Errorf("gossip turns = %d, want 1", got)
	}

	var sourceNode string
	err = ds.gossipDB.QueryRow(
		"SELECT source_node FROM turns WHERE turn_id = ?", turn.TurnID,
	).Scan(&sourceNode)
	if err != nil {
		t.Fatalf("scan source_node: %v", err)
	}
	if sourceNode != "node-peer" {
		t.Errorf("source_node = %q, want node-peer", sourceNode)
	}
}

// TestDualStore_StoreRemoteTurn_RejectsBadInputs verifies guards.
func TestDualStore_StoreRemoteTurn_RejectsBadInputs(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	cases := []struct {
		name string
		turn *Turn
		node string
	}{
		{"empty source", &Turn{TurnID: "x"}, ""},
		{"empty turn ID", &Turn{}, "node-x"},
		{"nil turn", nil, "node-x"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			err := ds.StoreRemoteTurn(context.Background(), c.turn, c.node)
			if err == nil {
				t.Errorf("StoreRemoteTurn(%s): expected error, got nil", c.name)
			}
		})
	}
}

// TestDualStore_GetTurnsForSession verifies merged turn reads.
func TestDualStore_GetTurnsForSession(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	// Two local turns.
	if err := ds.StoreTurn(ctx, &Turn{TurnID: "lt1", SessionID: "s1", Role: "user", Content: "l1", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("StoreTurn lt1: %v", err)
	}
	if err := ds.StoreTurn(ctx, &Turn{TurnID: "lt2", SessionID: "s1", Role: "assistant", Content: "l2", Timestamp: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("StoreTurn lt2: %v", err)
	}
	// One remote turn.
	if err := ds.StoreRemoteTurn(ctx, &Turn{TurnID: "rt1", SessionID: "s1", Role: "user", Content: "r1", Timestamp: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}, "node-peer"); err != nil {
		t.Fatalf("StoreRemoteTurn rt1: %v", err)
	}
	// Different session (should be excluded).
	if err := ds.StoreTurn(ctx, &Turn{TurnID: "ot", SessionID: "other", Role: "user", Content: "other", Timestamp: time.Now().UTC()}); err != nil {
		t.Fatalf("StoreTurn other: %v", err)
	}

	turns, err := ds.GetTurnsForSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetTurnsForSession: %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("len(turns) = %d, want 3", len(turns))
	}
	// Order: local first (by ts ASC), then gossip.
	if turns[0].TurnID != "lt1" {
		t.Errorf("turns[0] = %q, want lt1", turns[0].TurnID)
	}
	if turns[1].TurnID != "lt2" {
		t.Errorf("turns[1] = %q, want lt2", turns[1].TurnID)
	}
	if turns[2].TurnID != "rt1" {
		t.Errorf("turns[2] = %q, want rt1", turns[2].TurnID)
	}
	if turns[2].SourceNode != "node-peer" {
		t.Errorf("turns[2].SourceNode = %q, want node-peer", turns[2].SourceNode)
	}

	// Empty session ID returns nil, nil.
	turns, err = ds.GetTurnsForSession(ctx, "")
	if err != nil {
		t.Fatalf("GetTurnsForSession(empty): %v", err)
	}
	if turns != nil {
		t.Errorf("turns for empty session = %v, want nil", turns)
	}
}

// TestDualStore_PublishTurn_Adapter verifies that DualStore satisfies the
// session package's TurnGossipPublisher interface by calling PublishTurn.
func TestDualStore_PublishTurn_Adapter(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	pub := &mockGossipPublisher{}
	ds.SetGossipPublisher(pub)

	ts := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	if err := ds.PublishTurn("sess-via-adapter", "turn-adapter-1", "user", "via adapter", ts); err != nil {
		t.Fatalf("PublishTurn: %v", err)
	}

	waitForEvents(t, pub, 1, 1*time.Second)

	// Verify the row landed in local.db.
	if got := countTurnsDB(t, ds.localDB); got != 1 {
		t.Errorf("local turns = %d, want 1", got)
	}
	var role, content string
	if err := ds.localDB.QueryRow(
		"SELECT role, content FROM turns WHERE turn_id = ?", "turn-adapter-1",
	).Scan(&role, &content); err != nil {
		t.Fatalf("scan adapter turn: %v", err)
	}
	if role != "user" || content != "via adapter" {
		t.Errorf("role/content = %q/%q, want user/via adapter", role, content)
	}
}

// TestDualStore_StoreTurn_OwnNode writes a turn whose Metadata carries
// source_node=localNode and verifies it lands in local.db (not gossip).
func TestDualStore_StoreTurn_OwnNode(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-self", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	if err := ds.StoreTurn(context.Background(), &Turn{
		TurnID:    "self-turn",
		SessionID: "s-self",
		Role:      "user",
		Content:   "self",
		Timestamp: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("StoreTurn: %v", err)
	}

	if got := countTurnsDB(t, ds.localDB); got != 1 {
		t.Errorf("local turns = %d, want 1", got)
	}
	if got := countTurnsDB(t, ds.gossipDB); got != 0 {
		t.Errorf("gossip turns = %d, want 0", got)
	}
}

// TestDualStore_GetSessionTurnCountByOwner verifies the diagnostic counter.
func TestDualStore_GetSessionTurnCountByOwner(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-local", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	// 2 local turns.
	if err := ds.StoreTurn(ctx, &Turn{TurnID: "c1", SessionID: "s", Role: "user", Content: "1", Timestamp: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := ds.StoreTurn(ctx, &Turn{TurnID: "c2", SessionID: "s", Role: "asst", Content: "2", Timestamp: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	// 1 remote turn.
	if err := ds.StoreRemoteTurn(ctx, &Turn{TurnID: "c3", SessionID: "s", Role: "user", Content: "3", Timestamp: time.Now().UTC()}, "node-x"); err != nil {
		t.Fatal(err)
	}

	local, gossip, err := ds.GetSessionTurnCountByOwner(ctx)
	if err != nil {
		t.Fatalf("GetSessionTurnCountByOwner: %v", err)
	}
	if local != 2 {
		t.Errorf("local = %d, want 2", local)
	}
	if gossip != 1 {
		t.Errorf("gossip = %d, want 1", gossip)
	}
}

// ---------- helpers ----------

// waitForEvents polls the mock publisher until at least n events are
// observed or the deadline elapses. Publication is asynchronous (goroutine)
// so tests must wait for the event to land.
func waitForEvents(t *testing.T, pub *mockGossipPublisher, n int, max time.Duration) {
	t.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&pub.eventCount) >= int64(n) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d gossip events (got %d)", n, atomic.LoadInt64(&pub.eventCount))
}

func countSessionsDB(t *testing.T, db *sql.DB) int {
	t.Helper()
	if db == nil {
		return 0
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil {
		// Table may not exist yet.
		if errors.Is(err, sql.ErrNoRows) {
			return 0
		}
		t.Fatalf("countSessions: %v", err)
	}
	return count
}

func countTurnsDB(t *testing.T, db *sql.DB) int {
	t.Helper()
	if db == nil {
		return 0
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM turns").Scan(&count); err != nil {
		t.Fatalf("countTurns: %v", err)
	}
	return count
}
