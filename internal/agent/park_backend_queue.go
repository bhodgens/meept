package agent

// Queue-backed park storage (tree 20261110-parked-turns-as-jobs, leaf 02).
//
// QueueParkBackend implements the C-A1 parkBackend seam (and the optional
// keyedParkBackend capability, so the parker's persistence-key identity
// survives the Save/Load boundary) over the daemon's job queue: a parked
// turn becomes a jobs row with type "parked_turn", payload = the full
// ParkedTurnRecord JSON, wake time = next_retry_at, and the C-K1 owner
// identity rides claimed_by (C-A2).
//
// Design decisions (leaf 02; parent-adjudicated inherited constraints):
//
//   - OWNER STAMPING: the gate pins claimed_by == the backend's owner
//     VERBATIM at insert time. The queue claim flow normally stamps
//     claimed_by only at Claim, so instead of editing queue internals we
//     write the row directly (Store.DB() is public per the gate rig) with
//     state='pending' and claimed_by pre-stamped, and the row is made
//     structurally UNCLAIMABLE by worker pools via a far-future due_at
//     sentinel: every claim path (ClaimNextForAgent /
//     ClaimNextClustered / the PersistentQueue.Claim slow path) filters
//     `due_at IS NULL OR due_at <= now`, so a parked_turn row is never
//     claimed, never reaches FailAttempt/Retry, and can NEVER be
//     movedToDead (the Q-A1 / M-4-park pin: give-up never leaks parked
//     payloads to operator dead-letter surfaces). Owner isolation is
//     enforced by OUR reads: LoadOwned/LoadOwnedKeyed select on
//     claimed_by = normalizeParkOwner(owner). Terminal delete is the
//     parker's existing Delete call on resume/give-up — the backend never
//     routes give-ups (M-4-park: a backend only stores rows).
//
//   - ARM IS A DOCUMENTED NO-OP (like the sqlite wrapper, the leaf doc's
//     simplest-green design): the TurnParker already polls (drainDue) and
//     backend-mode drains on the REAL clock, so a second claim/wake loop
//     would put two owners on one wake decision — the exact C1 bug class
//     this tree kills. Due selection lives in the load + drain path:
//     LoadOwnedKeyed prunes rows whose resume_at <= now on the backend's
//     own clock, and the parker's drainDue filters the in-memory queue on
//     time.Now (never the injected fake clock in backend mode). nowFn is
//     the short-window knob (test seam, same regime as
//     sqliteParkBackend.nowFn; nil = time.Now = production parity, so no
//     extra timer budget beyond the parker's poll interval).
//
//   - ATTEMPT/MaxAttempts round-trip through the payload JSON so the
//     parker-side give-up logic works over the queue unchanged (gate
//     test 3 drives it through the parker and pins zero dead_letter
//     rows).
//
//   - Payload priority/interactive are deliberately NOT inherited:
//     parking is not claim-ordered like user jobs (leaf doc note).

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/queue"
)

const (
	// parkedTurnJobType is the C-A2 job type for queue-backed parks.
	parkedTurnJobType = "parked_turn"
	// parkedTurnSentinelDue keeps parked_turn rows out of EVERY worker
	// claim window (claim SQL requires due_at <= now). Parked turns are
	// woken by the owning parker's poll, not by the claim dispatcher —
	// the claim flow must never touch them (double-execution is exactly
	// the C1 bug class).
	parkedTurnSentinelDue = "2999-01-01T00:00:00Z"
	// parkOwnerPrefix namespaces park owners inside the claimed_by column
	// so a parked row can never collide with a real worker id.
	parkOwnerPrefix = "park:"
)

// parkedTurnJobPayload is the C-A2 payload wire shape: the full
// ParkedTurnRecord with snake_case keys, plus the explicit park_key and
// owner fields (the record itself carries no key; park_key preserves the
// keyedParkBackend row identity across restarts).
type parkedTurnJobPayload struct {
	Kind           string          `json:"kind"`
	ParkKey        string          `json:"park_key"`
	ConversationID string          `json:"conversation_id"`
	SessionID      string          `json:"session_id"`
	AgentID        string          `json:"agent_id"`
	Class          string          `json:"class"`
	ResumeAt       string          `json:"resume_at"`
	Attempt        int             `json:"attempt"`
	MaxAttempts    int             `json:"max_attempts"`
	TurnPayload    json.RawMessage `json:"turn_payload"`
	Autonomous     bool            `json:"autonomous"`
	Owner          string          `json:"owner"`
}

// QueueParkBackend is the C-A2 parkBackend over internal/queue.Store.
// Exported so internal/daemon can construct it in the wiring; consumers
// pass it to (*TurnParker).SetBackend (the C-A1 interface itself stays
// unexported per its frozen shape).
type QueueParkBackend struct {
	q     *queue.Store
	kind  ParkKind
	owner string // normalized (park: naming space); the claimed_by value
	// nowFn overrides time.Now for prune/wake decisions (test seam; nil
	// means the real clock — production parity).
	nowFn func() time.Time
}

var (
	_ parkBackend      = (*QueueParkBackend)(nil)
	_ keyedParkBackend = (*QueueParkBackend)(nil)
)

// newQueueParkBackend is the constructor signature pinned by
// park_queue_gate_test.go: owner passes through VERBATIM as
// jobs.claimed_by (C-A2 stamping), after park: naming-space
// normalization (an owner already carrying the prefix — the gate's
// "park:quota-watcher" — is kept byte-identical; the bare ParkOwner*
// consts map into the space).
func newQueueParkBackend(q *queue.Store, kind ParkKind, owner string) parkBackend {
	return NewQueueParkBackend(q, kind, owner)
}

// NewQueueParkBackend constructs the exported form for daemon wiring.
func NewQueueParkBackend(q *queue.Store, kind ParkKind, owner string) *QueueParkBackend {
	return &QueueParkBackend{q: q, kind: kind, owner: normalizeParkOwner(owner)}
}

// normalizeParkOwner maps a parker identity (ParkOwner* consts: bare
// "quota-watcher", "throttle", ...) into the claimed_by naming space
// ("park:quota-watcher"). An empty owner stays empty (= no filter, the
// parker's C-K1 legacy sticky-filter convention); an owner already
// carrying the prefix passes through verbatim so the gate's pinned value
// is stored exactly as asserted.
func normalizeParkOwner(owner string) string {
	if owner == "" || strings.HasPrefix(owner, parkOwnerPrefix) {
		return owner
	}
	return parkOwnerPrefix + owner
}

func (b *QueueParkBackend) now() time.Time {
	if b.nowFn != nil {
		return b.nowFn()
	}
	return time.Now()
}

func (b *QueueParkBackend) db() *sql.DB {
	if b == nil || b.q == nil {
		return nil
	}
	return b.q.DB()
}

// Save is the plain C-A1 entry (no key argument): like the sqlite
// wrapper's Save it derives a fresh row identity — here a crypto-random
// park job id (two parked turns are two rows, never deduped). The parker
// never reaches this method while the store-derived park key is non-empty
// (it prefers SaveKeyed).
func (b *QueueParkBackend) Save(ctx context.Context, rec ParkedTurnRecord) error {
	if b == nil {
		return nil
	}
	return b.insert(ctx, "", rec)
}

// SaveKeyed mirrors ParkPersistence.Save: the parker-derived persistence
// key becomes the job row id, kind-scoped — so the store's
// UNIQUE(kind, park_key) dedup semantics survive: a re-parked episode or
// gate overwrites its own row (ON CONFLICT upsert) instead of
// accumulating duplicates across restarts.
func (b *QueueParkBackend) SaveKeyed(ctx context.Context, kind ParkKind, key string, rec ParkedTurnRecord) error {
	if b == nil {
		return nil
	}
	return b.insert(ctx, string(kind)+"|"+key, rec)
}

// insert writes (or upserts, when rowID is non-empty) the parked_turn
// jobs row: claimed_by = the backend owner (C-A2 verbatim pin),
// next_retry_at = rec.ResumeAt (the wake time), due_at = the unclaimable
// sentinel, payload = the record JSON. The row never enters the
// claim/Fail/dead-letter lifecycle from here.
func (b *QueueParkBackend) insert(ctx context.Context, rowID string, rec ParkedTurnRecord) error {
	db := b.db()
	if db == nil {
		return errors.New("queue park backend: no queue store wired")
	}
	if rowID == "" {
		var buf [12]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return fmt.Errorf("queue park backend: derive row id: %w", err)
		}
		rowID = "park-" + hex.EncodeToString(buf[:])
	}
	kind := b.kind
	if kind == "" {
		kind = ParkKindChat
	}
	owner := rec.Owner
	if owner == "" {
		owner = b.owner
	}
	turnPayload := rec.TurnPayload
	if len(turnPayload) == 0 {
		// A nil RawMessage marshals as JSON `null` and would make the
		// payload undecodable-by-meaning on load; "null" is an explicit,
		// decodable empty blob (the sqlite store persists "" the same
		// conceptual way).
		turnPayload = json.RawMessage("null")
	}
	payload := parkedTurnJobPayload{
		Kind:           string(kind),
		ParkKey:        rowID,
		ConversationID: rec.ConversationID,
		SessionID:      rec.SessionID,
		AgentID:        rec.AgentID,
		Class:          parkClassString(rec.Class),
		ResumeAt:       formatParkTime(rec.ResumeAt),
		Attempt:        rec.Attempt,
		MaxAttempts:    rec.MaxAttempts,
		TurnPayload:    turnPayload,
		Autonomous:     rec.Autonomous,
		Owner:          owner,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("queue park backend: encode payload: %w", err)
	}
	resumeAt := formatParkTime(rec.ResumeAt)
	if resumeAt == "" {
		// A zero resume time is "due now" — an empty next_retry_at means
		// "no backoff" in the queue schema, so encode the epoch
		// explicitly to keep the wake predicate meaningful.
		resumeAt = time.Unix(0, 0).UTC().Format(time.RFC3339)
	}
	now := b.now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `
		INSERT INTO jobs (id, type, priority, state, payload, required_caps,
		                  max_retries, retry_count, interactive, claimed_by,
		                  created_at, updated_at, due_at, next_retry_at)
		VALUES (?, ?, 1, 'pending', ?, '[]', 0, 0, 0, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			payload = excluded.payload,
			claimed_by = excluded.claimed_by,
			next_retry_at = excluded.next_retry_at,
			updated_at = excluded.updated_at`,
		rowID, parkedTurnJobType, string(raw), b.owner, now, now, parkedTurnSentinelDue, resumeAt)
	if err != nil {
		return fmt.Errorf("queue park backend: insert parked_turn job: %w", err)
	}
	return nil
}

// Delete removes the parked_turn row whose id == key. SaveKeyed stored
// the row under kind+"|"+key (the parker's fresh-park identity) while
// LoadOwnedKeyed returns bare row ids (the re-arm identity), so both
// spellings resolve: the raw kind-prefixed form and the key as-is (a
// plain-Save park-<hex> id, or an already-prefixed re-armed key).
// Absent rows are a no-op (idempotent, same contract as
// ParkPersistence.Delete). Terminal management: this is the ONLY way a
// parked_turn job dies — resume and give-up both flow through the
// parker's Delete, never through queue failure accounting.
func (b *QueueParkBackend) Delete(ctx context.Context, kind ParkKind, key string) error {
	db := b.db()
	if db == nil || key == "" {
		return nil
	}
	k := kind
	if k == "" {
		k = b.kind
	}
	_, err := db.ExecContext(ctx,
		`DELETE FROM jobs WHERE type = ? AND (id = ? OR id = ?)`,
		parkedTurnJobType, string(k)+"|"+key, key)
	if err != nil {
		return fmt.Errorf("queue park backend: delete parked_turn job: %w", err)
	}
	return nil
}

// LoadOwned is the plain C-A1 entry (no now param): prune on the
// backend's own clock.
func (b *QueueParkBackend) LoadOwned(ctx context.Context, kind ParkKind, owner string) ([]ParkedTurnRecord, error) {
	records, _, err := b.LoadOwnedKeyed(ctx, kind, owner, b.now())
	return records, err
}

// LoadOwnedKeyed is the owner-scoped load, one transaction (the sqlite
// store's load contract ported): SELECT jobs rows of type parked_turn
// with claimed_by = normalizeParkOwner(owner) (empty owner = no filter,
// the parker's C-K1 convention), kind-filtered on the payload
// discriminator; rows whose wake has passed (resume_at <= now) are
// pruned by DELETE INSIDE the same transaction (at-most-once
// resurrection — a concurrent re-arm can never see a row both prune and
// return); survivors come back oldest-wake-first WITH their row ids as
// park keys. An undecodable row is left in place: its owner/kind cannot
// be attributed safely, and Delete-by-id (the parker's terminal path)
// still resolves it — pruning blind could destroy another owner's row.
func (b *QueueParkBackend) LoadOwnedKeyed(ctx context.Context, kind ParkKind, owner string, now time.Time) ([]ParkedTurnRecord, []string, error) {
	db := b.db()
	if b == nil || db == nil {
		return nil, nil, nil
	}
	o := normalizeParkOwner(owner)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("queue park backend: load begin: %w", err)
	}
	defer func() {
		// Rollback after Commit is a documented no-op; the error carries
		// no information worth surfacing, matching the sqlite store.
		if rerr := tx.Rollback(); rerr != nil && !errors.Is(rerr, sql.ErrTxDone) {
			_ = rerr
		}
	}()

	query := `SELECT id, payload FROM jobs WHERE type = ?`
	args := []any{parkedTurnJobType}
	if o != "" {
		query += ` AND claimed_by = ?`
		args = append(args, o)
	}
	query += ` ORDER BY next_retry_at ASC, id ASC`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("queue park backend: load query: %w", err)
	}

	var (
		records []ParkedTurnRecord
		keys    []string
		expired []string
	)
	for rows.Next() {
		var id, payloadRaw string
		if err := rows.Scan(&id, &payloadRaw); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("queue park backend: load scan: %w", err)
		}
		var p parkedTurnJobPayload
		if err := json.Unmarshal([]byte(payloadRaw), &p); err != nil {
			// Undecodable payload: leave the row (see the doc comment) —
			// never re-execute a turn we cannot decode.
			continue
		}
		if p.Kind != string(kind) {
			// Another parker's kind sharing this queue: invisible — the
			// kind partition the owner filter already implies.
			continue
		}
		class, ok := parseParkClass(p.Class)
		if !ok {
			// Unknown class on OUR owner's row: drop it (never map a
			// foreign class onto quota — the C-K1 defense in depth).
			expired = append(expired, id)
			continue
		}
		resumeAt := parseParkTime(p.ResumeAt)
		if resumeAt.IsZero() || !resumeAt.After(now) {
			// Expired bucket: a row whose wait already lapsed (including
			// one that expired while the daemon was down) is DELETED, not
			// resumed — catch-up resume is not implemented (see the
			// TurnParker.reArm doc in parked_turn.go).
			expired = append(expired, id)
			continue
		}
		turnPayload := p.TurnPayload
		if string(turnPayload) == "null" {
			turnPayload = nil
		}
		rec := ParkedTurnRecord{
			ConversationID: p.ConversationID,
			SessionID:      p.SessionID,
			AgentID:        p.AgentID,
			Class:          class,
			ResumeAt:       resumeAt,
			Attempt:        p.Attempt,
			MaxAttempts:    p.MaxAttempts,
			TurnPayload:    turnPayload,
			Autonomous:     p.Autonomous,
			Owner:          p.Owner,
		}
		records = append(records, rec)
		// Return the BARE park key (SaveKeyed stored the row under
		// kind+"|"+key) so the parker's drain-side Delete(key) resolves
		// the exact row it re-armed, byte-identically with the sqlite
		// store's key contract. Plain-Save rows (random "park-" ids,
		// no kind prefix) pass through unchanged.
		keys = append(keys, strings.TrimPrefix(id, string(kind)+"|"))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, fmt.Errorf("queue park backend: load iterate: %w", err)
	}
	rows.Close()

	// Prune inside the same transaction (owner-scoped predicate when an
	// owner filter is active, exactly the sqlite store's posture: one
	// parker's Load can never delete another owner's row).
	deleteStmt := `DELETE FROM jobs WHERE id = ? AND type = ?`
	if o != "" {
		deleteStmt += ` AND claimed_by = ?`
	}
	for _, id := range expired {
		var execArgs []any
		if o != "" {
			execArgs = []any{id, parkedTurnJobType, o}
		} else {
			execArgs = []any{id, parkedTurnJobType}
		}
		if _, err := tx.ExecContext(ctx, deleteStmt, execArgs...); err != nil {
			return nil, nil, fmt.Errorf("queue park backend: prune expired row %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("queue park backend: load commit: %w", err)
	}
	return records, keys, nil
}

// Arm is a documented no-op (see the file header): the TurnParker's
// existing poller owns the drain and the backend-mode drain is pinned to
// the REAL clock, so launching a second claim/wake loop here would put
// two owners on one wake decision — the C1 bug class.
func (b *QueueParkBackend) Arm(context.Context) {}
