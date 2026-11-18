package agent

// Parked-turns-as-queue-jobs gate tests (tree 20261110-parked-turns-as-jobs,
// leaf 00T). TESTS ONLY — these pin the TARGET contracts C-A1/C-A2 and are
// EXPECTED TO FAIL RED (compile: undefined target symbols). Implementation
// leaves 01/02 must land these GREEN WITHOUT editing this file
// ("Gate tests GREEN, untouched"). NEVER confuse with the frozen 20261109
// gate park_owner_gate_test.go — this file is additive; that rig is REUSED:
// quotaGateRecord/throttleGateRecord (park_owner_gate_test.go),
// countingResume (gate_park_test.go), newTestParkStore/
// newParkStoreTestLogger/persistedRowCount (park_store_sqlite_test.go),
// fakeNowFunc/waitUntil (parked_turn_test.go).
//
// Target symbols pinned (RED until leaves 01/02 land):
//   type parkBackend interface — EXACTLY the C-A1 shape:
//       Save(ctx, rec ParkedTurnRecord) error
//       Delete(ctx, kind ParkKind, key string) error
//       LoadOwned(ctx, kind ParkKind, owner string) ([]ParkedTurnRecord, error)
//       Arm(ctx)
//   (*TurnParker).SetBackend(b parkBackend)   // nil-safe: keeps sqlite default
//   newQueueParkBackend(q *queue.Store, kind ParkKind, owner string) parkBackend
//       owner passes through VERBATIM as jobs.claimed_by (C-A2 stamping)
//   config: [parking] queue_backend (cfg.Parking.QueueBackend bool, default false)
//
// Pinned interpretations (leaf 02 must honor these):
//   - SetBackend OVERRIDES the store path: with both a park store and a
//     backend wired, Save/Delete/LoadOwned go to the backend ONLY (store
//     stays empty — the backend replaces storage, not mirrors it).
//   - A queued park writes a jobs row with type == "parked_turn",
//     next_retry_at == rec.ResumeAt, claimed_by == the backend's owner, and
//     the payload JSON round-trips the ParkedTurnRecord (C-A2: kind/key/
//     owner/class/resume_at/attempt/max_attempts/payload blob — field keys
//     accepted as either Go names or snake_case).
//   - Delete(kind, key) removes the parked_turn job whose id == key (and the
//     give-up path must leave NO live job and ZERO dead_letter rows —
//     M-4-park routes give-up to the callback, never moveToDead).

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/queue"
)

// --- rig: recording parkBackend (implements C-A1 exactly) --------------------

// gateSeamBackend is a call-recording parkBackend for the seam test. The
// var assertion below is one of the compile-RED pins: if leaf 01 changes any
// C-A1 signature, this file stops compiling and the seam contract is broken.
type gateSeamBackend struct {
	mu       sync.Mutex
	saves    []ParkedTurnRecord
	delKeys  []string
	loadHits int
	armHits  int
}

func (f *gateSeamBackend) Save(_ context.Context, rec ParkedTurnRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves = append(f.saves, rec)
	return nil
}

func (f *gateSeamBackend) Delete(_ context.Context, _ ParkKind, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delKeys = append(f.delKeys, key)
	return nil
}

func (f *gateSeamBackend) LoadOwned(_ context.Context, _ ParkKind, _ string) ([]ParkedTurnRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadHits++
	return nil, nil
}

func (f *gateSeamBackend) Arm(_ context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.armHits++
}

func (f *gateSeamBackend) counts() (saves, deletes, loads, arms int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.saves), len(f.delKeys), f.loadHits, f.armHits
}

func (f *gateSeamBackend) saved() []ParkedTurnRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ParkedTurnRecord(nil), f.saves...)
}

// C-A1 pin: the fake implements the target interface exactly.
var _ parkBackend = (*gateSeamBackend)(nil)

// gateQueueDB opens a REAL queue.Store over a temp sqlite file for the
// backend round-trip gates (agent already imports internal/queue — no cycle;
// the driver registers via internal/agent's blank modernc import).
func gateQueueDB(t *testing.T) *queue.Store {
	t.Helper()
	q, err := queue.NewStore(filepath.Join(t.TempDir(), "parkgate-queue.db"), newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("queue.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q
}

// gatePayloadField reads a payload key accepting Go-name or snake_case spellings.
func gatePayloadField(t *testing.T, m map[string]any, keys ...string) any {
	t.Helper()
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	t.Fatalf("parked_turn payload missing field (tried %v): %+v", keys, m)
	return nil
}

// --- 1. TestParkBackendSeamWrapped (C-A1: seam routes, default unchanged) ----

// With SetBackend(fake) wired, the parker's persistence traffic (re-arm
// LoadOwned + Arm at Start, Save at Park, Delete at drain) goes to the
// BACKEND and the sqlite store sees nothing new. Without SetBackend (and
// with SetBackend(nil)) the parker keeps the store path byte-identically.
func TestParkBackendSeamWrapped(t *testing.T) {
	ctx := context.Background()
	store := newTestParkStore(t)
	defer store.Close()

	// Baseline A: no SetBackend call at all — store path unchanged.
	base := NewTurnParker(newParkStoreTestLogger(), func(context.Context, ParkedTurnRecord) {}, 2*time.Hour)
	base.SetParkPersistence(store, ParkKindChat)
	base.SetPersistenceOwner("seam-baseline")
	if !base.Park(quotaGateRecord("s-baseline-a", time.Now().Add(time.Hour))) {
		t.Fatal("baseline park A refused")
	}
	if n := store.persistedRowCount(t, ParkKindChat); n != 1 {
		t.Fatalf("store rows after default park = %d, want 1 (no backend wired)", n)
	}

	// Baseline B: SetBackend(nil) is nil-safe — keeps the sqlite default.
	base.SetBackend(nil)
	if !base.Park(quotaGateRecord("s-baseline-b", time.Now().Add(time.Hour))) {
		t.Fatal("baseline park B (after SetBackend(nil)) refused")
	}
	if n := store.persistedRowCount(t, ParkKindChat); n != 2 {
		t.Fatalf("store rows after SetBackend(nil) park = %d, want 2 (nil must not disable persistence)", n)
	}

	// Seam: parker wired with store (for the park-key plumbing) AND the
	// recording backend — all traffic must route to the backend only.
	before := store.persistedRowCount(t, ParkKindChat)
	be := &gateSeamBackend{}
	var resumed countingResume
	clock := &fakeNowFunc{now: time.Now()}
	p := NewTurnParker(newParkStoreTestLogger(), resumed.add, 2*time.Hour)
	p.SetPollInterval(10 * time.Millisecond)
	p.nowFunc = clock.nowFn
	p.SetParkPersistence(store, ParkKindChat)
	p.SetPersistenceOwner("seam-quota")
	p.SetBackend(be)
	p.Start(ctx)
	defer p.Stop()

	_, _, loads, arms := be.counts()
	if loads < 1 {
		t.Errorf("Start did not re-arm through the backend: LoadOwned hits = %d, want >= 1", loads)
	}
	if arms < 1 {
		t.Errorf("Start did not Arm the backend: Arm hits = %d, want >= 1", arms)
	}

	if !p.Park(quotaGateRecord("s-seam", clock.nowFn().Add(30*time.Millisecond))) {
		t.Fatal("seam park refused")
	}
	waitUntil(t, 3*time.Second, func() bool {
		s, d, _, _ := be.counts()
		return s >= 1 && d >= 1 && resumed.count() >= 1
	})
	clock.advance(50 * time.Millisecond)
	saves, deletes, _, _ := be.counts()
	if saves != 1 {
		t.Errorf("backend Save calls = %d, want 1 (park routed through the seam)", saves)
	}
	if recs := be.saved(); len(recs) == 1 {
		rec := recs[0]
		if rec.SessionID != "s-seam" {
			t.Errorf("backend saw session %q, want s-seam", rec.SessionID)
		}
		if rec.Owner != "seam-quota" {
			t.Errorf("backend saw owner %q, want seam-quota (C-K1 stamp must survive the seam)", rec.Owner)
		}
	}
	if deletes != 1 {
		t.Errorf("backend Delete calls = %d, want 1 (drain routed through the seam)", deletes)
	}
	if n := resumed.count(); n != 1 {
		t.Errorf("resume callbacks = %d, want 1", n)
	}
	if n := store.persistedRowCount(t, ParkKindChat); n != before {
		t.Errorf("store rows changed under the backend seam: %d, want %d (backend REPLACES the store path, not mirrors)", n, before)
	}
}

// --- 2. TestQueueBackendSaveLoadDelete (C-A2: job shape + owner isolation) ---

// newQueueParkBackend over a REAL queue.Store: Save creates a "parked_turn"
// job with next_retry_at == ResumeAt and claimed_by == owner (C-A2 stamping);
// the payload JSON round-trips the record; LoadOwned returns ONLY that
// owner's rows; Delete removes the job.
func TestQueueBackendSaveLoadDelete(t *testing.T) {
	ctx := context.Background()
	q := gateQueueDB(t)

	ownerA := "park:quota-watcher"
	ownerB := "park:throttle"
	bA := newQueueParkBackend(q, ParkKindChat, ownerA)
	bB := newQueueParkBackend(q, ParkKindChat, ownerB)
	bA.Arm(ctx) // must be safe with an empty queue (future-dated jobs are not due)

	// Second-truncated resume times survive the RFC3339 storage round-trip.
	base := time.Now().UTC().Truncate(time.Second).Add(2 * time.Hour)
	recA := quotaGateRecord("s-qb-a", base)
	recA.Owner = ownerA
	if err := bA.Save(ctx, recA); err != nil {
		t.Fatalf("Save A: %v", err)
	}
	recB := throttleGateRecord("s-qb-b", base.Add(time.Minute))
	recB.Owner = ownerB
	if err := bB.Save(ctx, recB); err != nil {
		t.Fatalf("Save B: %v", err)
	}

	// C-A2 row shape for A.
	var (
		jobID       string
		payloadRaw  string
		claimedBy   sql.NullString
		nextRetryAt sql.NullString
		jobType     string
	)
	err := q.DB().QueryRow(
		`SELECT id, type, payload, claimed_by, next_retry_at FROM jobs WHERE claimed_by = ?`, ownerA,
	).Scan(&jobID, &jobType, &payloadRaw, &claimedBy, &nextRetryAt)
	if err != nil {
		t.Fatalf("query parked_turn job for %s: %v", ownerA, err)
	}
	if jobType != "parked_turn" {
		t.Errorf("job type = %q, want parked_turn", jobType)
	}
	if !claimedBy.Valid || claimedBy.String != ownerA {
		t.Errorf("claimed_by = %v, want %q (owner stamping per C-A2)", claimedBy, ownerA)
	}
	if !nextRetryAt.Valid {
		t.Fatal("next_retry_at is NULL — wake time must ride next_retry_at")
	}
	wake, perr := time.Parse(time.RFC3339, nextRetryAt.String)
	if perr != nil {
		t.Fatalf("parse next_retry_at %q: %v", nextRetryAt.String, perr)
	}
	if d := wake.Sub(recA.ResumeAt); d < -time.Second || d > time.Second {
		t.Errorf("next_retry_at = %v, want == rec.ResumeAt %v (±1s storage precision)", wake, recA.ResumeAt)
	}

	// Payload JSON round-trips the ParkedTurnRecord.
	var pm map[string]any
	if err := json.Unmarshal([]byte(payloadRaw), &pm); err != nil {
		t.Fatalf("unmarshal parked_turn payload: %v (raw %s)", err, payloadRaw)
	}
	if s, _ := gatePayloadField(t, pm, "SessionID", "session_id").(string); s != "s-qb-a" {
		t.Errorf("payload session = %q, want s-qb-a", s)
	}
	if c, _ := gatePayloadField(t, pm, "ConversationID", "conversation_id").(string); c != "conv-s-qb-a" {
		t.Errorf("payload conversation = %q, want conv-s-qb-a", c)
	}
	if cl, _ := gatePayloadField(t, pm, "Class", "class").(string); cl != "quota" {
		t.Errorf("payload class = %q, want quota", cl)
	}
	if blob, ok := gatePayloadField(t, pm, "TurnPayload", "turn_payload").(map[string]any); !ok {
		t.Errorf("payload TurnPayload is not a JSON object: %+v", pm)
	} else if msg, _ := blob["message"].(string); msg != "q" {
		t.Errorf("parked turn payload blob message = %q, want q (the blob must round-trip)", msg)
	}

	// LoadOwned: owner isolation — each owner sees exactly its own rows.
	recsA, err := bA.LoadOwned(ctx, ParkKindChat, ownerA)
	if err != nil {
		t.Fatalf("LoadOwned A: %v", err)
	}
	if len(recsA) != 1 || recsA[0].SessionID != "s-qb-a" {
		t.Fatalf("LoadOwned(%s) = %d rows %+v, want exactly the s-qb-a row", ownerA, len(recsA), ownersOf(recsA))
	}
	if recsA[0].Owner != ownerA {
		t.Errorf("round-tripped Owner = %q, want %q", recsA[0].Owner, ownerA)
	}
	if recsA[0].ResumeAt.IsZero() || recsA[0].ResumeAt.Sub(base) < 0 || recsA[0].ResumeAt.Sub(base) > time.Second {
		t.Errorf("round-tripped ResumeAt = %v, want %v", recsA[0].ResumeAt, base)
	}
	recsB, err := bB.LoadOwned(ctx, ParkKindChat, ownerB)
	if err != nil {
		t.Fatalf("LoadOwned B: %v", err)
	}
	if len(recsB) != 1 || recsB[0].SessionID != "s-qb-b" {
		t.Errorf("LoadOwned(%s) = %d rows, want exactly the s-qb-b row (no cross-owner leak)", ownerB, len(recsB))
	}
	none, err := bA.LoadOwned(ctx, ParkKindChat, "park:nobody")
	if err != nil {
		t.Fatalf("LoadOwned nobody: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("LoadOwned(park:nobody) = %d rows, want 0", len(none))
	}

	// Delete removes the job; the other owner's row survives.
	if err := bA.Delete(ctx, ParkKindChat, jobID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var left int
	if err := q.DB().QueryRow(`SELECT COUNT(*) FROM jobs WHERE id = ?`, jobID).Scan(&left); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if left != 0 {
		t.Errorf("job %s survived Delete: %d rows, want 0", jobID, left)
	}
	var bLeft int
	if err := q.DB().QueryRow(`SELECT COUNT(*) FROM jobs WHERE type = 'parked_turn'`).Scan(&bLeft); err != nil {
		t.Fatalf("count parked_turn rows: %v", err)
	}
	if bLeft != 1 {
		t.Errorf("parked_turn rows after one Delete = %d, want 1 (only the deleted job must go)", bLeft)
	}
}

// --- 3. TestQueueBackendGiveUpRoutesToCallback (M-4-park over the queue) -----

// A capped (attempt >= max_attempts) due park on a queue-backed parker gives
// up through the TurnParker give-up callback — NO resume callback, the job
// leaves the claimable set, and ZERO rows land in dead_letter (parked
// payloads must never leak to operator dead-letter surfaces, Q-A1 pin).
func TestQueueBackendGiveUpRoutesToCallback(t *testing.T) {
	ctx := context.Background()
	q := gateQueueDB(t)
	store := newTestParkStore(t) // wired for the park-key plumbing; must stay empty
	defer store.Close()

	b := newQueueParkBackend(q, ParkKindChat, "park:quota-watcher")
	var resumed, gaveUp countingResume
	clock := &fakeNowFunc{now: time.Now()}
	p := NewTurnParker(newParkStoreTestLogger(), resumed.add, 2*time.Hour)
	p.SetPollInterval(10 * time.Millisecond)
	p.nowFunc = clock.nowFn
	p.SetParkPersistence(store, ParkKindChat)
	p.SetPersistenceOwner("quota-watcher")
	p.SetParkKey(func(rec ParkedTurnRecord) string { return "giveup|" + rec.SessionID })
	p.SetGiveUpFunc(gaveUp.add)
	p.SetBackend(b)
	p.Start(ctx)
	defer p.Stop()

	capped := quotaGateRecord("s-giveup", clock.nowFn().Add(30*time.Millisecond))
	capped.Attempt = DefaultQuotaMaxAttempts
	capped.MaxAttempts = DefaultQuotaMaxAttempts
	if !p.Park(capped) {
		t.Fatal("capped park refused")
	}
	waitUntil(t, 3*time.Second, func() bool { return gaveUp.count() >= 1 })
	time.Sleep(200 * time.Millisecond) // let a (wrong) resume or double give-up surface

	if n := gaveUp.count(); n != 1 {
		t.Errorf("give-up callbacks = %d, want exactly 1", n)
	}
	if rec := gaveUp.last(); rec.SessionID != "s-giveup" {
		t.Errorf("give-up fired for %+v, want the s-giveup record", rec)
	}
	if n := resumed.count(); n != 0 {
		t.Errorf("resume callbacks = %d, want 0 (capped record must never replay the turn)", n)
	}

	// The job is terminal: nothing claimable remains for the parked kind.
	var live int
	if err := q.DB().QueryRow(
		`SELECT COUNT(*) FROM jobs WHERE type = 'parked_turn' AND state IN ('pending','claimed','processing')`,
	).Scan(&live); err != nil {
		t.Fatalf("count live parked_turn jobs: %v", err)
	}
	if live != 0 {
		t.Errorf("live parked_turn jobs after give-up = %d, want 0", live)
	}
	// Hard M-4-park pin: give-up routes to the callback, NEVER dead-letter.
	var dead int
	if err := q.DB().QueryRow(`SELECT COUNT(*) FROM dead_letter`).Scan(&dead); err != nil {
		t.Fatalf("count dead_letter rows: %v", err)
	}
	if dead != 0 {
		t.Errorf("dead_letter rows after give-up = %d, want 0 (parked payloads must not leak to dead-letter surfaces)", dead)
	}
	if n := store.persistedRowCount(t, ParkKindChat); n != 0 {
		t.Errorf("sqlite park store rows under the queue backend = %d, want 0 (backend replaces the store path)", n)
	}
}

// --- 4. TestParkingQueueBackendDefaultFalse (opt-in flag, R2) -----------------

// [parking] queue_backend ships OFF (opt-in defaults invariant):
// DefaultConfig has it false, an absent key parses false, and the explicit
// true parses. Kept in internal/agent (this tree's gate file scope) — the
// agent test package already imports internal/config, and DefaultConfig/
// LoadJSON5Bytes are public, so no config-package test file is needed.
func TestParkingQueueBackendDefaultFalse(t *testing.T) {
	c := config.DefaultConfig()
	if c.Parking.QueueBackend {
		t.Fatal("parking.queue_backend default must be false (opt-in; disabled = byte-identical sqlite path)")
	}

	var absent config.Config
	if err := config.LoadJSON5Bytes([]byte(`{ "llm": { "default_provider": "p" } }`), &absent); err != nil {
		t.Fatalf("LoadJSON5Bytes (absent parking key): %v", err)
	}
	if absent.Parking.QueueBackend {
		t.Error("absent parking.queue_backend key must parse false")
	}

	var on config.Config
	if err := config.LoadJSON5Bytes([]byte(`{ "parking": { "queue_backend": true } }`), &on); err != nil {
		t.Fatalf("LoadJSON5Bytes (explicit parking.queue_backend): %v", err)
	}
	if !on.Parking.QueueBackend {
		t.Error("explicit parking.queue_backend=true must parse true")
	}
}
