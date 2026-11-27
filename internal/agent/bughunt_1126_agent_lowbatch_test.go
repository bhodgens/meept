package agent

// Bughunt 2026-11-25 gaps (issue #189), pins for A2-9 and A2-11.
// A2-7 lives in internal/agent/turnjournal/bughunt_1126_a27_retry_test.go;
// A2-8 lives in plan_critique_loop_test.go's revised fail-open pin.

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// --- A2-9: queue persister (internal/agent/queue_persister.go) ---

// TestA29_EventOnlyForCommittedRows: the flush used to publish
// EventQueuePersisted inside the open transaction, so a rolled-back commit
// announced persists for rows that never landed. The pin proves the
// inverse contract deterministically: whenever a subscriber RECEIVES a
// persisted event, that message's row is already readable — an
// in-transaction publish cannot guarantee this.
func TestA29_EventOnlyForCommittedRows(t *testing.T) {
	b := newTestBus()
	defer b.Close()
	sub := b.Subscribe("a29-pin", bus.EventQueuePersisted)
	defer b.Unsubscribe(sub)

	db := newTestDB(t)
	p, err := NewQueuePersister(db, "conv-a29", slog.Default())
	if err != nil {
		t.Fatalf("NewQueuePersister: %v", err)
	}
	t.Cleanup(p.Stop)
	p.WithBus(b)

	p.EnqueueAsync(QueuedMessage{ID: "a29-m1", Content: "first follow-up", QueueType: QueueTypeFollowUp})
	p.EnqueueAsync(QueuedMessage{ID: "a29-m2", Content: "second follow-up", QueueType: QueueTypeFollowUp})
	p.Flush()

	seen := 0
	deadline := time.After(2 * time.Second)
	for seen < 2 {
		select {
		case msg, ok := <-sub.Channel:
			if !ok {
				t.Fatalf("subscriber channel closed after %d events", seen)
			}
			seen++
			var payload QueueEventPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("event %d: unmarshal payload: %v", seen, err)
			}
			var n int
			// The row must be committed BEFORE the event is observable.
			if err := db.QueryRow(`SELECT count(*) FROM queued_followups WHERE message_id = ?`,
				payload.MessageID).Scan(&n); err != nil {
				t.Fatalf("event %d (%s): row check: %v", seen, payload.MessageID, err)
			}
			if n != 1 {
				t.Errorf("event for %s arrived but the row is not committed (count=%d) — phantom persist path", payload.MessageID, n)
			}
		case <-deadline:
			t.Fatalf("only %d/2 persisted events received", seen)
		}
	}
}

// TestA29_TimerArmedLazily: production creates a persister per turn and
// only ever calls PersistSync. The constructor used to arm a 5s flush
// timer on every one of them; each fired once into an empty flush. The pin
// asserts no timer exists until EnqueueAsync (the only buffering path)
// arms one.
func TestA29_TimerArmedLazily(t *testing.T) {
	db := newTestDB(t)
	p, err := NewQueuePersister(db, "conv-a29-lazy", slog.Default())
	if err != nil {
		t.Fatalf("NewQueuePersister: %v", err)
	}
	t.Cleanup(p.Stop)

	p.mu.Lock()
	timerAtConstruct := p.flushTimer
	p.mu.Unlock()
	if timerAtConstruct != nil {
		t.Error("constructor armed a flush timer; A2-9 requires lazy arming (PersistSync-only turns never need one)")
	}

	p.EnqueueAsync(QueuedMessage{ID: "a29-l1", Content: "buffered", QueueType: QueueTypeFollowUp})
	p.mu.Lock()
	timerAfterEnqueue := p.flushTimer
	p.mu.Unlock()
	if timerAfterEnqueue == nil {
		t.Error("EnqueueAsync did not arm the flush timer; the debounce path is broken")
	}
}

// TestA29_PersistSyncStillWorks guards the one live path: a plain sync
// persist lands the row and still announces it.
func TestA29_PersistSyncStillWorks(t *testing.T) {
	b := newTestBus()
	defer b.Close()
	sub := b.Subscribe("a29-sync", bus.EventQueuePersisted)
	defer b.Unsubscribe(sub)

	db := newTestDB(t)
	p, err := NewQueuePersister(db, "conv-a29-sync", slog.Default())
	if err != nil {
		t.Fatalf("NewQueuePersister: %v", err)
	}
	t.Cleanup(p.Stop)
	p.WithBus(b)

	if err := p.PersistSync(QueuedMessage{ID: "a29-s1", Content: "synced", QueueType: QueueTypeFollowUp}); err != nil {
		t.Fatalf("PersistSync: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM queued_followups WHERE message_id = ?`, "a29-s1").Scan(&n); err != nil || n != 1 {
		t.Fatalf("row count = %d (err %v), want 1", n, err)
	}
	select {
	case msg, ok := <-sub.Channel:
		if !ok {
			t.Fatal("subscriber channel closed")
		}
		var payload QueueEventPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.MessageID != "a29-s1" {
			t.Errorf("event message_id = %q, want a29-s1", payload.MessageID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PersistSync published no persisted event")
	}
}

// --- A2-11: ArtifactManager (internal/agent/artifact_integration.go) ---

// writeClaudeMD creates a temp directory holding a minimal parseable
// CLAUDE.md, and returns the directory.
func writeClaudeMD(t *testing.T, marker string) string {
	t.Helper()
	dir := t.TempDir()
	body := "# Project " + marker + "\n\n## Architecture\n\nThe " + marker + " flow handles requests.\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	return dir
}

// TestA211_BuilderPerDirectory: the context builder used to be ONE shared
// field — scanning directory B redirected session A's next BuildContext
// into B. The pin scans B AFTER A and asserts A's build answers from A and
// never mentions B.
func TestA211_BuilderPerDirectory(t *testing.T) {
	dirA := writeClaudeMD(t, "alpha")
	dirB := writeClaudeMD(t, "beta")

	am := NewArtifactManager(slog.Default())
	if _, err := am.ScanDirectory(dirA); err != nil {
		t.Fatalf("scan A: %v", err)
	}
	if _, err := am.ScanDirectory(dirB); err != nil {
		t.Fatalf("scan B: %v", err)
	}

	ctxA, ok := am.BuildContext("explain the flow", dirA)
	if !ok {
		t.Fatal("BuildContext(A) returned no context after B was scanned")
	}
	if !strings.Contains(ctxA, dirA) {
		t.Errorf("BuildContext(A) does not mention directory A; got:\n%s", lastLines(ctxA, 10))
	}
	if strings.Contains(ctxA, dirB) {
		t.Errorf("BuildContext(A) leaks directory B's builder — the shared-builder mix-up (A2-11 iii) is back:\n%s", lastLines(ctxA, 10))
	}

	ctxB, ok := am.BuildContext("explain the flow", dirB)
	if !ok {
		t.Fatal("BuildContext(B) returned no context")
	}
	if !strings.Contains(ctxB, dirB) {
		t.Errorf("BuildContext(B) does not mention directory B")
	}
}

// TestA211_ExpiryIsEnforced: cacheExpiry was reported in stats but never
// applied — entries served scans forever. With expiry set to zero, every
// lookup must miss, so the builder path (and a future scan) rebuilds
// rather than serving the stale entry.
func TestA211_ExpiryIsEnforced(t *testing.T) {
	dir := writeClaudeMD(t, "gamma")
	am := NewArtifactManager(slog.Default())
	if _, err := am.ScanDirectory(dir); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if am.getContextBuilder(dir) == nil {
		t.Fatal("fresh scan produced no context builder")
	}
	am.mu.Lock()
	am.cacheExpiry = 0 // any elapsed time >= expiry → entry is stale
	am.mu.Unlock()
	if cb := am.getContextBuilder(dir); cb != nil {
		t.Error("expired cache entry still served a context builder; cacheExpiry is unenforced (A2-11 i)")
	}
	if _, err := am.ScanDirectory(dir); err != nil {
		t.Fatalf("re-scan after expiry: %v", err)
	}
	if am.GetArtifacts(dir) == nil {
		t.Error("re-scan after expiry did not repopulate the cache")
	}
}

// TestA211_CacheIsBounded: the artifactCache grew one entry per scanned
// directory with no eviction. The pin scans cap+4 distinct directories
// through the production ScanDirectory path and requires the map to stay
// at the cap.
func TestA211_CacheIsBounded(t *testing.T) {
	am := NewArtifactManager(slog.Default())
	dirs := make([]string, 0, artifactCacheCap+4)
	for i := 0; i < artifactCacheCap+4; i++ {
		dirs = append(dirs, writeClaudeMD(t, "cap"+strconv.Itoa(i)))
	}
	for _, dir := range dirs {
		if _, err := am.ScanDirectory(dir); err != nil {
			t.Fatalf("scan %s: %v", dir, err)
		}
	}
	am.mu.RLock()
	n := len(am.artifactCache)
	am.mu.RUnlock()
	if n > artifactCacheCap {
		t.Errorf("artifactCache grew to %d entries, want <= %d (unbounded cache, A2-11 ii)", n, artifactCacheCap)
	}
	if n != artifactCacheCap {
		t.Logf("note: cache settled at %d (cap %d after eviction pressure)", n, artifactCacheCap)
	}
}

// --- 11-24 L13: guard-before-disclosure ordering (handler.go choke point) ---

// TestL13_GuardSeesReplyAsWritten: the truncation disclosure used to be
// appended BEFORE the single reply choke point, and appending it to a pure
// tool-result JSON reply broke the guard's suffix test — the machine dump
// shipped unguarded with the notice glued on. The handler now applies the
// guard first and re-applies the disclosure after. This pin locks the
// composition the handler relies on: (1) a bare JSON reply IS replaced by
// the guard; (2) JSON with the notice appended is NOT (the old order's
// defect, proven mechanically); (3) guard-then-append yields a sanitized
// reply that still ends with the notice.
func TestL13_GuardSeesReplyAsWritten(t *testing.T) {
	jsonReply := `{"tool_result": "stored entry 17", "job_id": "j-9"}`
	// Verbatim text of llm.ContextFirewall.TruncationNotice (gate T4c).
	notice := "note: some background context (for example the skill catalog) exceeded the model's context window and was truncated for this conversation; ask for the full text if you need it."

	// (1) bare JSON: the guard's suffix test matches and the choke point
	// replaces it with the fallback copy.
	if _, ok := toolResultJSONKey(jsonReply); !ok {
		t.Fatal("premise broken: bare tool-result JSON no longer matches the guard")
	}
	guarded := applyReplyGuardWithFallback(jsonReply, slog.Default(), replyGuardContext{}, "")
	if strings.Contains(guarded, "tool_result") {
		t.Errorf("guard failed to replace the pure JSON reply: %q", guarded)
	}

	// (2) the OLD order: notice glued on first → the suffix test fails →
	// the dump would ship verbatim. This is the L13 defect itself.
	noticeFirst := jsonReply + "\n\n" + notice
	if _, ok := toolResultJSONKey(noticeFirst); ok {
		t.Fatal("premise broken: JSON+notice still matches the suffix test; L13 could not have happened")
	}

	// (3) the NEW order: guard replaces, notice rides the sanitized tail.
	final := guarded
	if notice != "" {
		if final != "" {
			final += "\n\n" + notice
		} else {
			final = notice
		}
	}
	if strings.Contains(final, "tool_result") {
		t.Errorf("L13 regression: machine dump shipped in the final reply: %q", final)
	}
	if !strings.HasSuffix(final, notice) {
		t.Errorf("disclosure lost its tail position: %q", final)
	}
}

// lastLines returns the last n lines of s for error messages.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
