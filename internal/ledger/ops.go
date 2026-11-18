package ledger

// 01-store/03 — query, verify, repair, and tail fan-out, layered on the
// store built by leaves 01-store/01 (schema) and 02 (Append/transitions).
// QueryLog and GetState live in store.go; this file owns QueryOpen,
// SearchLog (FTS5), the fold (VerifyState/RepairState), and the Tail
// live fan-out with its tailer registry.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// =========================================================================
// QueryOpen / SearchLog
// =========================================================================

// QueryOpen returns state rows with Status not in {done, cancelled} for
// the employee, newest-updated first. Ties on updated_at (second-precision
// RFC3339 rows appended in quick succession) break on as_of DESC, which is
// append order, so the freshest write leads.
func (s *Store) QueryOpen(ctx context.Context, employeeID string) ([]State, error) {
	if employeeID == "" {
		return nil, errors.New("ledger: query open: employee_id is required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT employee_id, subject, status, summary, updated_at, as_of, due_at
         FROM ledger_state
         WHERE employee_id = ? AND status IN ('open', 'waiting_on_me', 'waiting_on_them')
         ORDER BY updated_at DESC, as_of DESC`,
		employeeID)
	if err != nil {
		return nil, fmt.Errorf("ledger: query open: %w", err)
	}
	defer rows.Close()

	var out []State
	for rows.Next() {
		st, err := scanState(rows)
		if err != nil {
			return nil, fmt.Errorf("ledger: query open: scan: %w", err)
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: query open: iterate: %w", err)
	}
	return out, nil
}

// SearchLog is the FTS5 text-search companion to QueryLog for the RPC
// leaf: term is matched as a quoted phrase against ledger_fts (subject +
// first 512 bytes of the stored payload, inserted by Append in the same
// tx), and the hit rowids are then filtered by the same LogQuery scoping
// as QueryLog and returned in ascending id order. A blank term degrades
// to QueryLog. Employee scoping is enforced here, not by FTS — the
// contentless index carries no employee_id, so the ledger table remains
// the authority on visibility.
func (s *Store) SearchLog(ctx context.Context, q LogQuery, term string) ([]Record, error) {
	if q.EmployeeID == "" {
		return nil, errors.New("ledger: search log: employee_id is required")
	}
	if strings.TrimSpace(term) == "" {
		return s.QueryLog(ctx, q)
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	var sb strings.Builder
	sb.WriteString(`SELECT id, employee_id, at, kind, subject, payload, source, source_ref, resolves FROM ledger WHERE employee_id = ?`)
	args := []any{q.EmployeeID}
	// Quoted-phrase match neutralises FTS5 operators in user input; the
	// only escape inside an FTS double-quoted string is "" for ".
	sb.WriteString(` AND id IN (SELECT rowid FROM ledger_fts WHERE ledger_fts MATCH ?)`)
	args = append(args, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	if q.Subject != "" {
		sb.WriteString(` AND subject = ?`)
		args = append(args, q.Subject)
	}
	if len(q.Kinds) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(q.Kinds)), ",")
		fmt.Fprintf(&sb, ` AND kind IN (%s)`, placeholders)
		for _, k := range q.Kinds {
			args = append(args, string(k))
		}
	}
	if q.SinceID > 0 {
		sb.WriteString(` AND id > ?`)
		args = append(args, q.SinceID)
	}
	fmt.Fprintf(&sb, ` ORDER BY id ASC LIMIT %d`, limit)

	rows, err := s.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("ledger: search log: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("ledger: search log: scan: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: search log: iterate: %w", err)
	}
	return out, nil
}

// =========================================================================
// VerifyState / RepairState — the fold
// =========================================================================

// VerifyState folds the ledger and diffs ledger_state. The fold's ONLY
// sanctioned derivation (frozen contract): for each stored (employee,
// subject), replay its ledger rows in id order; a row is status-carrying
// iff its payload decodes to the frozen PayloadEnvelope (v == 1 with a
// valid status). The latest such envelope is Want — status/summary from
// the envelope, as_of from that row's id. Log-only rows never change
// Want. A state key with no envelope rows at all has no derivable Want
// and is counted in Checked but never flagged. Have vs Want compares
// status, summary, as_of, and due (never updated_at).
//
// The pool is single-connection (SetMaxOpenConns(1)), so the fold and
// the state scan run as two SEQUENTIAL passes — one full ledger scan
// building the fold map, then a ledger_state scan diffing against it —
// never a nested query under an open cursor.
//
// bughunt D5 (snapshot consistency): the two passes are not one
// transaction, so an Append can commit BETWEEN the fold and the state
// scan — Have is then NEWER than the fold's Want (Have.AsOf > Want.AsOf).
// That is PROGRESS, not divergence: the row is skipped (counted Checked
// only). Without the guard, RepairState's auto-repair path would rewind
// a fresh append with the stale fold. A row whose Have.AsOf <= Want.AsOf
// that still disagrees IS a real divergence (the state was written by a
// fold-era row and does not match the fold).
func (s *Store) VerifyState(ctx context.Context) (*VerifyReport, error) {
	folded, err := s.foldAll(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT employee_id, subject, status, summary, updated_at, as_of, due_at
         FROM ledger_state ORDER BY employee_id ASC, subject ASC`)
	if err != nil {
		return nil, fmt.Errorf("ledger: verify state: %w", err)
	}
	defer rows.Close()

	report := &VerifyReport{}
	for rows.Next() {
		have, err := scanState(rows)
		if err != nil {
			return nil, fmt.Errorf("ledger: verify state: scan state: %w", err)
		}
		report.Checked++
		want, ok := folded[stateKey{have.EmployeeID, have.Subject}]
		if !ok {
			continue
		}
		// Snapshot guard (D5): Have is newer than the fold's snapshot —
		// an Append committed between the two passes landed this state.
		// Progress, never divergence; never repaired.
		if have.AsOf > want.AsOf {
			continue
		}
		if have.Status != want.Status || have.Summary != want.Summary || have.AsOf != want.AsOf ||
			!have.Due.Equal(want.Due) {
			report.Divergent++
			report.Diffs = append(report.Diffs, StateDiff{
				EmployeeID: have.EmployeeID,
				Subject:    have.Subject,
				Have:       have,
				Want:       want,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: verify state: iterate: %w", err)
	}
	return report, nil
}

// stateKey indexes the fold map by the (employee, subject) primary key.
type stateKey struct{ employeeID, subject string }

// foldAll replays every ledger row in id order once, keeping for each
// (employee, subject) the state implied by the LATEST status-carrying
// envelope row. Keys with only log-only rows are absent from the map —
// no derivable Want.
func (s *Store) foldAll(ctx context.Context) (map[stateKey]State, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, employee_id, subject, at, payload FROM ledger ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("ledger: verify state: fold: %w", err)
	}
	defer rows.Close()

	folded := make(map[stateKey]State)
	for rows.Next() {
		var (
			id         int64
			employeeID string
			subject    string
			at         string
			payload    []byte
		)
		if err := rows.Scan(&id, &employeeID, &subject, &at, &payload); err != nil {
			return nil, fmt.Errorf("ledger: verify state: fold: scan: %w", err)
		}
		var env payloadEnvelope
		if err := json.Unmarshal(payload, &env); err != nil {
			continue // not JSON / not an envelope: log-only row
		}
		if env.V != 1 || !validStatus(Status(env.Status)) {
			continue // not a status-carrying envelope
		}
		t, err := parseStoredTime(at)
		if err != nil {
			return nil, fmt.Errorf("ledger: verify state: fold: row %d: %w", id, err)
		}
		due, err := parseDueAt(env.Due)
		if err != nil {
			return nil, fmt.Errorf("ledger: verify state: fold: row %d: bad envelope due %q: %w", id, env.Due, err)
		}
		folded[stateKey{employeeID, subject}] = State{
			EmployeeID: employeeID,
			Subject:    subject,
			Status:     Status(env.Status),
			Summary:    env.Summary,
			UpdatedAt:  t,
			AsOf:       id,
			Due:        due,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: verify state: fold: iterate: %w", err)
	}
	return folded, nil
}

// RepairState rebuilds divergent state rows from the fold. It runs
// VerifyState first and writes ONLY the rows that diverge (healthy rows
// are guarded and left byte-identical; rows NEWER than the fold's
// snapshot — Have.AsOf > Want.AsOf, D5's progress-not-divergence guard —
// are never candidates), in one transaction; returns the repaired count.
// Idempotent: a second run after a clean repair finds no divergence and
// returns 0.
//
// bughunt D5: because repair writes ONLY VerifyState-flagged rows, and
// VerifyState now skips rows newer than the fold's snapshot, a fresh
// append that lands between verify and repair can no longer be rewound —
// the stale fold never reaches it. The one transaction here also bounds
// the write pass so a mid-repair append lands wholly before or after it.
func (s *Store) RepairState(ctx context.Context) (int, error) {
	report, err := s.VerifyState(ctx)
	if err != nil {
		return 0, err
	}
	if report.Divergent == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("ledger: repair state: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	for _, d := range report.Diffs {
		dueAt := ""
		if !d.Want.Due.IsZero() {
			dueAt = d.Want.Due.UTC().Format(time.RFC3339)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ledger_state (employee_id, subject, status, summary, updated_at, as_of, due_at)
             VALUES (?, ?, ?, ?, ?, ?, ?)
             ON CONFLICT(employee_id, subject) DO UPDATE SET
                 status=excluded.status, summary=excluded.summary,
                 updated_at=excluded.updated_at, as_of=excluded.as_of,
                 due_at=excluded.due_at`,
			d.EmployeeID, d.Subject, string(d.Want.Status), d.Want.Summary,
			d.Want.UpdatedAt.UTC().Format(time.RFC3339), d.Want.AsOf, dueAt); err != nil {
			return 0, fmt.Errorf("ledger: repair state: upsert %s/%s: %w", d.EmployeeID, d.Subject, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("ledger: repair state: commit: %w", err)
	}
	return len(report.Diffs), nil
}

// scanState reads one ledger_state row from a rowScanner in the extended
// column order (employee_id, subject, status, summary, updated_at, as_of,
// due_at) — the due_at tail is the 02-daystate/01 additive extension.
func scanState(rows rowScanner) (State, error) {
	var (
		st      State
		status  string
		at      string
		summary string
		dueAt   string
	)
	if err := rows.Scan(&st.EmployeeID, &st.Subject, &status, &summary, &at, &st.AsOf, &dueAt); err != nil {
		return State{}, err
	}
	t, err := parseStoredTime(at)
	if err != nil {
		return State{}, fmt.Errorf("bad updated_at %q: %w", at, err)
	}
	due, err := parseDueAt(dueAt)
	if err != nil {
		return State{}, fmt.Errorf("bad due_at: %w", err)
	}
	st.Status = Status(status)
	st.Summary = summary
	st.UpdatedAt = t
	st.Due = due
	return st, nil
}

// parseDueAt reads a stored due_at TEXT value: "" (or absent) is the zero
// time — "no due" — any other value must parse as RFC3339 (with the same
// fractional-second tolerance as parseStoredTime).
func parseDueAt(dueAt string) (time.Time, error) {
	if dueAt == "" {
		return time.Time{}, nil
	}
	t, err := parseStoredTime(dueAt)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// parseStoredTime reads a stored RFC3339 TEXT timestamp, tolerating the
// fractional-second (RFC3339Nano) form raw writers may produce.
func parseStoredTime(at string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, at)
	if err == nil {
		return t, nil
	}
	t, err = time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad timestamp %q: %w", at, err)
	}
	return t, nil
}

// =========================================================================
// Tail — live fan-out
// =========================================================================

// tailLiveBuffer is how many undelivered live records one Tail
// subscription may hold before Append's fan-out blocks the appending
// caller (backpressure). Catch-up rows are streamed from SQLite and do
// not pass through the buffer.
const tailLiveBuffer = 1024

// tailer is one Tail subscription's fan-out endpoint, registered on the
// Store under s.mu for the lifetime of its delivery goroutine:
//
//   - live:   buffered hand-off from Append (fanoutTailers).
//   - stop:   closed by Store.Close to release the delivery goroutine.
//   - quit:   closed exactly once when the delivery goroutine exits, so
//     a blocked producer unblocks when a dead tailer is torn down.
//   - stopped: guarded by s.mu; makes double-Close safe.
type tailer struct {
	live    chan Record
	stop    chan struct{}
	quit    chan struct{}
	stopped bool
}

// Tail yields records with ID > sinceID: catch-up from SQLite, then live
// from the in-process fan-out registered at Append commit. One channel
// per Tail call; closed on ctx cancel or Store.Close.
//
// No-gap guarantee (frozen): the tailer is registered under the store
// mutex BEFORE the catch-up read runs, so every row committed after the
// Tail call is visible to either the snapshot SELECT, the live push, or
// both. The delivery goroutine dedupes by the monotonic id (skip <=
// lastSeen), so the handoff overlap yields no gaps and at most a
// duplicate a snapshot/live race could have caused — consumers treat
// deliveries idempotently.
func (s *Store) Tail(ctx context.Context, sinceID int64) (<-chan Record, error) {
	out := make(chan Record)
	t := &tailer{
		live: make(chan Record, tailLiveBuffer),
		stop: make(chan struct{}),
		quit: make(chan struct{}),
	}

	// Register first — see the no-gap guarantee above.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("ledger: tail: store is closed")
	}
	s.tailers = append(s.tailers, t)
	s.mu.Unlock()

	go s.runTail(ctx, t, sinceID, out)
	return out, nil
}

// runTail is the per-subscription delivery goroutine: catch-up in id
// order, then live records from t.live, deduped against lastSeen. It
// owns the subscription teardown: deregister, close(out), close(t.quit).
func (s *Store) runTail(ctx context.Context, t *tailer, sinceID int64, out chan<- Record) {
	defer func() {
		s.removeTailer(t)
		close(out)
		close(t.quit)
	}()

	var lastSeen = sinceID
	deliver := func(rec Record) bool {
		if rec.ID <= lastSeen {
			return true // duplicate at the snapshot/live handoff
		}
		select {
		case out <- rec:
			lastSeen = rec.ID
			return true
		case <-ctx.Done():
			return false
		case <-t.stop:
			return false
		}
	}

	// Two-phase catch-up (single-connection pool): drain the snapshot
	// SELECT fully and close its cursor BEFORE delivering or entering
	// the live phase, so the tail goroutine never holds the only DB
	// connection while Append needs it for the next write.
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, employee_id, at, kind, subject, payload, source, source_ref, resolves
         FROM ledger WHERE id > ? ORDER BY id ASC`,
		sinceID)
	if err != nil {
		// Store closed or ctx already cancelled mid-registration: the
		// subscription simply ends (out is closed by the defer).
		return
	}
	var catchup []Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			rows.Close()
			return
		}
		catchup = append(catchup, rec)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return
	}
	rows.Close()

	for _, rec := range catchup {
		if !deliver(rec) {
			return
		}
	}

	// Live phase. Buffered records pushed during catch-up drain here in
	// append order (monotonic ids), after the snapshot, preserving id
	// ordering.
	for {
		select {
		case rec := <-t.live:
			if !deliver(rec) {
				return
			}
		case <-ctx.Done():
			return
		case <-t.stop:
			return
		}
	}
}

// removeTailer deregisters a finished subscription (cancel path). Called
// exactly once per tailer by runTail's defer; no goroutine survives a
// closed out channel.
func (s *Store) removeTailer(t *tailer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, cand := range s.tailers {
		if cand == t {
			s.tailers = append(s.tailers[:i], s.tailers[i+1:]...)
			return
		}
	}
}

// fanoutTailers pushes a just-committed record to every live tailer. It
// snapshots the registry under s.mu but pushes WITHOUT holding the mutex,
// so a slow (buffered) tailer can apply backpressure on the appending
// caller without deadlocking teardown, which also takes s.mu. A tailer
// whose delivery goroutine exited (quit) or whose store closed (stop) is
// skipped instead of blocked.
func (s *Store) fanoutTailers(rec Record) {
	s.mu.Lock()
	if len(s.tailers) == 0 {
		s.mu.Unlock()
		return
	}
	ts := make([]*tailer, len(s.tailers))
	copy(ts, s.tailers)
	s.mu.Unlock()

	for _, t := range ts {
		select {
		case t.live <- rec:
		case <-t.stop:
		case <-t.quit:
		}
	}
}

// wakeTailers closes every registered tailer's stop channel so their
// delivery goroutines exit. Called from Store.Close under s.mu; the
// stopped guard makes it safe under a double Close.
func (s *Store) wakeTailersLocked() {
	for _, t := range s.tailers {
		if !t.stopped {
			t.stopped = true
			close(t.stop)
		}
	}
	s.tailers = nil
}
