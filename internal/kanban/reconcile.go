package kanban

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

// reconcile.go implements leaf 03/02: the on-demand full-scan rebuild that
// makes the board tables equal the SOURCE work-item state (master.md
// decision 2: "after rebuild, board == source").
//
// Design posture:
//   - The projector is the LIVE lane (its Stop doc names reconcile as the
//     recovery for events lost under adversarial timing). Reconcile is
//     the AUTHORITATIVE lane: a diff-based rebuild repairing wrong
//     columns, missing cards, orphan cards, duplicates, and drifted
//     title/provenance. Idempotent: a second pass over an unchanged
//     source changes zero rows.
//   - kanban_projections is the mapping AUTHORITY (C3): reconcile reads
//     it and never rewrites it; the rebuild target is kanban_cards. A
//     rule naming a missing column is inert — skipped. Target
//     resolution mirrors projector.handleEvent: first Matches(kind,
//     state) row whose column exists wins; one canonical card per work
//     item, moved (not forked) across boards.
//   - Parked posture: an item whose CURRENT state matches no rule keeps
//     its card untouched. Deletion classes: true orphans (item absent
//     from the live enumeration) and duplicates past the first-by-id;
//     deletes cascade the card's timeline rows (C3 FK). NULL-ref cards
//     are MANUAL and excluded from the scan.
//   - No machine imports: enumeration arrives via the ListFetcher seam
//     (real implementation = 07/01). A nil ListFetcher makes every pass
//     a no-op — never a wipe. A kind whose enumeration ERRORS is skipped
//     wholesale (no creates, no deletes).
//   - Writes land in ONE transaction batch. The Store is
//     single-connection (SetMaxOpenConns(1)), so the reconcile batch and
//     the projector's writes cannot interleave in-process — no writer
//     lock is needed (same note as SetProjectionRule).
//   - Reconcile publishes NOTHING on the bus (C2 keeps kanban.board.changed
//     with the projector + CRUD); 05/01 hooks notifications via SetOnDrift.

// WorkItemRef is one LIVE work item as enumerated from its source
// machine: identity (Kind+ID), the item's placement key, and optional
// source-row provenance.
type WorkItemRef struct {
	Kind string
	ID   string
	// State is expressed in the SAME key the projector consumes: rules
	// map (kind, from_state) -> column (projection_rules.go) and the
	// projector keys on ev.From, so State is the from_state of the
	// transition that last placed the item — the wiring derives it
	// from the machine's state graph. Both lanes then resolve the same
	// column for the same item: that is board == source.
	State string
	// Row optionally carries source-row provenance. A configured
	// RowFetcher takes precedence; both empty => title "kind:id"
	// (same fallback as projector.createCard).
	Row ProvRow
}

// ListFetcher is the enumeration seam between reconcile and the
// work-item machines (parallel to RowFetcher; the real implementation
// closes over the machine stores and lives in daemon wiring, 07/01).
// ListWorkItems returns every LIVE item of one kind; retired/archived
// rows must NOT be returned — their absence is what makes reconcile
// delete the orphaned cards. An error means "source unknown for this
// kind": reconcile skips the kind entirely.
type ListFetcher interface {
	ListWorkItems(ctx context.Context, kind string) ([]WorkItemRef, error)
}

// reconcileKinds is the C2 machine surface (the five state topics). A
// fetcher with no table for a kind returns an empty slice, not an error.
var reconcileKinds = []string{EventKindTask, EventKindJob, EventKindPlan, EventKindStep, EventKindGoal}

// ReconcileStats reports what one ReconcileNow pass changed. Everything
// zero on an idempotent pass.
type ReconcileStats struct {
	Created int // cards materialised for mapped items with no card
	Moved   int // cards re-columned/re-boarded into the rule target
	Fixed   int // title/provenance drift repaired (target unchanged)
	Deleted int // orphan + duplicate cards removed
}

// Total returns the number of rows the pass changed.
func (s ReconcileStats) Total() int { return s.Created + s.Moved + s.Fixed + s.Deleted }

// mappedTarget is the resolved rebuild target for one live item.
type mappedTarget struct {
	boardID, columnID string
	row               ProvRow
}

// Reconciler performs full-scan rebuilds against a Store. Passes are
// serialised: a tick landing during a manual kanban.reconcile.now RPC
// blocks (bounded by its ctx) instead of racing a half-computed diff.
type Reconciler struct {
	store   *Store
	fetcher ListFetcher // nil-safe: passes degrade to a no-op
	proven  RowFetcher  // nil-safe source of truth for re-reads
	logger  *slog.Logger

	// onDrift fires after a pass that changed rows. Guarded by mu;
	// called OUTSIDE mu.
	onDrift func(ctx context.Context)

	mu     sync.Mutex    // guards the tunables above ONLY (no I/O under it)
	passMu chan struct{} // serialises reconcilePass (pass lock, not a data mutex)
}

// NewReconciler wires a reconciler to a store and its source enumerator.
// A nil store is a constructor error (posture of NewProjector); a nil
// fetcher is legal and makes every pass a zero-row no-op — the unwired
// path stays inert by construction.
func NewReconciler(store *Store, fetcher ListFetcher) (*Reconciler, error) {
	return NewReconcilerWithProvenance(store, fetcher, nil)
}

// NewReconcilerWithProvenance also accepts the RowFetcher used to
// re-read source rows during the rebuild — 07/01 passes the projector's
// fetcher so both lanes read identical truth.
func NewReconcilerWithProvenance(store *Store, fetcher ListFetcher, proven RowFetcher) (*Reconciler, error) {
	if store == nil {
		return nil, fmt.Errorf("reconcile: store is required")
	}
	return &Reconciler{
		store:   store,
		fetcher: fetcher,
		proven:  proven,
		logger:  slog.Default(),
		passMu:  make(chan struct{}, 1),
	}, nil
}

// SetProvenanceFetcher installs the RowFetcher used for re-reads. A nil
// f is ignored (nil-guard convention for Set*).
func (r *Reconciler) SetProvenanceFetcher(f RowFetcher) {
	if r == nil || f == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.proven = f
}

// SetSourceFetcher installs the ListFetcher on an already-built
// reconciler (07/01 boot order: ticks before installation are no-ops).
// A nil f is ignored.
func (r *Reconciler) SetSourceFetcher(f ListFetcher) {
	if r == nil || f == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fetcher = f
}

// SetLogger swaps the reconciler's logger; nil is ignored.
func (r *Reconciler) SetLogger(logger *slog.Logger) {
	if r == nil || logger == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logger = logger
}

// SetOnDrift registers a callback fired after a pass that CHANGED rows
// — the seam for the 05/01 RPC wiring to publish kanban.board.changed
// without reconcile owning any bus topic (C2). nil clears it.
func (r *Reconciler) SetOnDrift(fn func(ctx context.Context)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onDrift = fn
}

// ReconcileNow runs one full-scan rebuild (frozen leaf contract:
// ReconcileNow(ctx) error), and is idempotent.
func (r *Reconciler) ReconcileNow(ctx context.Context) error {
	_, err := r.ReconcileNowStats(ctx)
	return err
}

// ReconcileNowStats is ReconcileNow with the per-pass row counts.
func (r *Reconciler) ReconcileNowStats(ctx context.Context) (ReconcileStats, error) {
	var stats ReconcileStats
	if r == nil || r.store == nil {
		return stats, fmt.Errorf("reconcile: reconciler is not configured")
	}
	select {
	case r.passMu <- struct{}{}:
	case <-ctx.Done():
		return stats, fmt.Errorf("reconcile: waiting for prior pass: %w", ctx.Err())
	}
	defer func() { <-r.passMu }()

	stats, err := r.reconcilePass(ctx)
	if err != nil {
		return stats, err
	}
	if stats.Total() > 0 {
		r.mu.Lock()
		hook := r.onDrift
		r.mu.Unlock()
		if hook != nil {
			hook(ctx)
		}
	}
	return stats, nil
}

// reconcilePass is the rebuild body: enumerate, resolve targets, diff
// against the projected cards, apply the diff in one transaction.
// r.mu is never held across I/O here.
func (r *Reconciler) reconcilePass(ctx context.Context) (ReconcileStats, error) {
	var stats ReconcileStats

	r.mu.Lock()
	listF := r.fetcher
	provF := r.proven
	logger := r.logger
	r.mu.Unlock()

	if listF == nil {
		// No source seam = no truth to rebuild against. Never delete
		// on an empty enumeration — that would be a wipe.
		return stats, nil
	}

	rules, err := r.store.LoadProjectionRules(ctx, "")
	if err != nil {
		return stats, fmt.Errorf("reconcile: load projection rules: %w", err)
	}

	// Board -> live column ids (rules are board-scoped; validate the
	// target against the rule's own board).
	colSets := map[string]map[string]bool{}
	columnsOf := func(boardID string) (map[string]bool, error) {
		if set, ok := colSets[boardID]; ok {
			return set, nil
		}
		cols, err := r.store.ListColumns(ctx, boardID)
		if err != nil {
			return nil, fmt.Errorf("reconcile: list columns for board %s: %w", boardID, err)
		}
		set := make(map[string]bool, len(cols))
		for _, c := range cols {
			set[c.ID] = true
		}
		colSets[boardID] = set
		return set, nil
	}

	// Enumerate the source. enumerated[kind] guards orphan deletion
	// (header). workKey is projector.go's (kind, id) identity.
	targets := map[workKey]mappedTarget{}
	parked := map[workKey]bool{}
	enumerated := map[string]bool{}

	for _, kind := range reconcileKinds {
		items, err := listF.ListWorkItems(ctx, kind)
		if err != nil {
			logger.Warn("kanban reconcile: work-item enumeration failed, skipping kind",
				"kind", kind, "error", err)
			continue
		}
		enumerated[kind] = true
		for _, item := range items {
			if item.ID == "" {
				continue
			}
			k := item.Kind
			if k == "" {
				k = kind
			}
			key := workKey{k, item.ID}
			matches := rules.Matches(k, item.State)
			if len(matches) == 0 {
				parked[key] = true
				continue
			}
			// First valid match wins — the projector's rule
			// (handleEvent takes matches[0]).
			found := false
			for _, rule := range matches {
				cols, err := columnsOf(rule.BoardID)
				if err != nil {
					return stats, err
				}
				if !cols[rule.ColumnID] {
					logger.Warn("kanban reconcile: projection rule names a missing column, skipping",
						"board", rule.BoardID, "column", rule.ColumnID,
						"kind", k, "state", item.State)
					continue
				}
				targets[key] = mappedTarget{
					boardID:  rule.BoardID,
					columnID: rule.ColumnID,
					row:      r.resolveProv(provF, k, item.ID, item.Row, logger),
				}
				found = true
				break
			}
			if !found {
				parked[key] = true
			}
		}
	}

	actual, err := r.store.scanProjectedCards(ctx)
	if err != nil {
		return stats, err
	}

	// Deterministic write plan (sorted; map order must never decide
	// which duplicate survives — same lesson as projector's batchOrder).
	keys := make([]workKey, 0, len(targets)+len(parked)+len(actual))
	for key := range targets {
		keys = append(keys, key)
	}
	for key := range parked {
		if _, ok := targets[key]; !ok {
			keys = append(keys, key)
		}
	}
	for key := range actual {
		if _, live := targets[key]; !live {
			if _, isParked := parked[key]; !isParked {
				keys = append(keys, key)
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].kind != keys[j].kind {
			return keys[i].kind < keys[j].kind
		}
		return keys[i].id < keys[j].id
	})

	type repairOp struct {
		card  Card
		moved bool // target column/board differs from the read row
	}
	var creates []Card
	var repairs []repairOp
	var deletes []string
	// orderBase seeds each column's append numbering from its live card
	// count (projector.createCard's count seed).
	orderBase := map[string]int{}

	for _, key := range keys {
		group := actual[key]
		sort.Slice(group, func(i, j int) bool { return group[i].ID < group[j].ID })

		if _, live := targets[key]; !live {
			if parked[key] {
				// Live item, unmapped state: the card parks
				// untouched (dedup only, canonical 1-per-item).
				if len(group) > 1 {
					deletes = append(deletes, cardIDs(group[1:])...)
				}
				continue
			}
			// Orphan candidate. If the kind's enumeration failed,
			// we cannot claim orphanage — leave the card standing.
			if !enumerated[key.kind] {
				continue
			}
			deletes = append(deletes, cardIDs(group)...)
			continue
		}

		target := targets[key]
		if len(group) == 0 {
			// Missing card for a mapped item: create at the target.
			base, err := r.columnOrderBase(ctx, orderBase, target.columnID)
			if err != nil {
				return stats, err
			}
			orderBase[target.columnID] = base + 1
			creates = append(creates, buildCardForTarget(key, target, base))
			continue
		}
		// Keep the first card by id — the same row GetCardByRef's
		// ORDER BY id LIMIT 1 resolves to. The rest are duplicate drift.
		if len(group) > 1 {
			deletes = append(deletes, cardIDs(group[1:])...)
		}
		keep := group[0]
		moved := keep.ColumnID != target.columnID || keep.BoardID != target.boardID
		drifted := !moved && cardRowDrifted(keep, key, target.row)
		if moved || drifted {
			fresh := keep
			fresh.BoardID = target.boardID
			fresh.ColumnID = target.columnID
			applySourceRow(&fresh, key, target.row)
			repairs = append(repairs, repairOp{card: fresh, moved: moved})
		}
	}

	if len(creates) == 0 && len(repairs) == 0 && len(deletes) == 0 {
		return stats, nil // already equal to source — the idempotent fast path
	}

	// One transaction batch: the rebuild is atomic, so a crash or a
	// cancelled ctx mid-pass can never leave board != source.
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return stats, fmt.Errorf("reconcile: begin rebuild tx: %w", err)
	}
	defer func() {
		// Deferred safety net: no-op once Commit succeeded (ErrTxDone);
		// any other rollback failure surfaces at Warn (house pattern,
		// cf. internal/plan/store_check.go).
		if rerr := tx.Rollback(); rerr != nil && !errors.Is(rerr, sql.ErrTxDone) {
			r.logger.Warn("reconcile: rollback of rebuild tx failed", "error", rerr)
		}
	}()

	for _, card := range creates {
		if err := insertCardTx(ctx, tx, card); err != nil {
			return stats, err
		}
		stats.Created++
	}
	for _, op := range repairs {
		if err := updateCardTx(ctx, tx, op.card); err != nil {
			return stats, err
		}
		if op.moved {
			stats.Moved++
		} else {
			stats.Fixed++
		}
	}
	for _, cardID := range deletes {
		if _, err := tx.ExecContext(ctx, "DELETE FROM kanban_cards WHERE id = ?", cardID); err != nil {
			return stats, fmt.Errorf("reconcile: delete orphan card %s: %w", cardID, err)
		}
		stats.Deleted++
	}
	if err := tx.Commit(); err != nil {
		return stats, fmt.Errorf("reconcile: commit rebuild: %w", err)
	}
	return stats, nil
}

// resolveProv picks the source row for an enumerated item: the
// RowFetcher first (the projector's authority); on absence or error,
// the WorkItemRef.Row the fetcher carried.
func (r *Reconciler) resolveProv(provF RowFetcher, kind, itemID string, carried ProvRow, logger *slog.Logger) ProvRow {
	if provF == nil {
		return carried
	}
	var (
		prov ProvRow
		err  error
	)
	switch kind {
	case EventKindTask:
		prov, err = provF.TaskRow(itemID)
	case EventKindJob:
		prov, err = provF.JobRow(itemID)
	case EventKindPlan:
		prov, err = provF.PlanRow(itemID)
	case EventKindStep:
		prov, err = provF.StepRow(itemID)
	case EventKindGoal:
		prov, err = provF.GoalRow(itemID)
	default:
		return carried
	}
	if err != nil {
		logger.Warn("kanban reconcile: provenance re-read failed, using enumerated row",
			"kind", kind, "id", itemID, "error", err)
		return carried
	}
	return prov
}

// buildCardForTarget materialises the canonical card for one mapped
// live item, mirroring projector.createCard field-for-field (title
// fallback "kind:id") so both lanes produce equal rows for the same
// source — the kill/restart equality test depends on it.
func buildCardForTarget(key workKey, target mappedTarget, order int) Card {
	title := target.row.Title
	if title == "" {
		title = fmt.Sprintf("%s:%s", key.kind, key.id)
	}
	return Card{
		ID:         id.Generate("card-"),
		BoardID:    target.boardID,
		ColumnID:   target.columnID,
		Title:      title,
		Ref:        WorkRef{Kind: key.kind, ID: key.id},
		GoalID:     target.row.GoalID,
		EmployeeID: target.row.EmployeeID,
		PlanID:     target.row.PlanID,
		SessionID:  target.row.SessionID,
		TriggerRef: target.row.TriggerRef,
		Order:      order,
	}
}

// applySourceRow writes the source-derived fields onto a card being
// repaired. A zero row (source unresolvable) is a no-op — moving a
// card must never blank provenance it legitimately carries.
func applySourceRow(card *Card, key workKey, row ProvRow) {
	if row == (ProvRow{}) {
		return
	}
	title := row.Title
	if title == "" {
		title = fmt.Sprintf("%s:%s", key.kind, key.id)
	}
	card.Title = title
	card.GoalID = row.GoalID
	card.EmployeeID = row.EmployeeID
	card.PlanID = row.PlanID
	card.SessionID = row.SessionID
	card.TriggerRef = row.TriggerRef
}

// cardRowDrifted compares an in-place card's source-derived fields
// against the enumerated source row (title + provenance). Zero-row
// guard: an unresolvable row reports "no drift" rather than erasing
// provenance a live card legitimately carries.
func cardRowDrifted(c Card, key workKey, row ProvRow) bool {
	if row == (ProvRow{}) {
		return false
	}
	title := row.Title
	if title == "" {
		title = fmt.Sprintf("%s:%s", key.kind, key.id)
	}
	return title != c.Title || row.GoalID != c.GoalID || row.EmployeeID != c.EmployeeID ||
		row.PlanID != c.PlanID || row.SessionID != c.SessionID || row.TriggerRef != c.TriggerRef
}

// columnOrderBase returns the next append order for a column (count
// queried once per pass, cached across creates).
func (r *Reconciler) columnOrderBase(ctx context.Context, cache map[string]int, columnID string) (int, error) {
	if base, ok := cache[columnID]; ok {
		return base, nil
	}
	n, err := r.store.countCards(ctx, columnID)
	if err != nil {
		return 0, fmt.Errorf("reconcile: size column %s: %w", columnID, err)
	}
	return n, nil
}

// cardIDs projects a sorted card group to its ids.
func cardIDs(cs []Card) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

// scanProjectedCards reads every card that carries a work ref (the
// projected surface), grouped by (ref kind, ref id). NULL-ref MANUAL
// cards are excluded so a rebuild can never delete them. store.go is
// read-only for this leaf, so the accessor lives here, mirroring
// store.go's scan style.
func (s *Store) scanProjectedCards(ctx context.Context) (map[workKey][]Card, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, board_id, column_id, title, ref_kind, ref_id, labels, goal_id, employee_id, plan_id, session_id, trigger_ref, order_val
		FROM kanban_cards WHERE COALESCE(ref_kind, '') <> '' AND COALESCE(ref_id, '') <> '' ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed to scan projected cards: %w", err)
	}
	defer rows.Close()

	out := map[workKey][]Card{}
	var refKind, refID sql.NullString
	var labelsJSON string
	for rows.Next() {
		var c Card
		if err := rows.Scan(&c.ID, &c.BoardID, &c.ColumnID, &c.Title, &refKind, &refID, &labelsJSON,
			&c.GoalID, &c.EmployeeID, &c.PlanID, &c.SessionID, &c.TriggerRef, &c.Order); err != nil {
			return nil, fmt.Errorf("failed to scan projected card: %w", err)
		}
		c.Ref = WorkRef{Kind: refKind.String, ID: refID.String}
		if err := unmarshalLabels(labelsJSON, &c.Labels); err != nil {
			return nil, fmt.Errorf("failed to decode card %s labels: %w", c.ID, err)
		}
		key := workKey{c.Ref.Kind, c.Ref.ID}
		out[key] = append(out[key], c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate projected cards: %w", err)
	}
	return out, nil
}

// insertCardTx / updateCardTx mirror Store.CreateCard / UpdateCard
// inside the reconcile transaction batch (the exported methods
// autocommit per statement; the rebuild must be one atomic unit).
func insertCardTx(ctx context.Context, tx *sql.Tx, c Card) error {
	labelsJSON, err := json.Marshal(c.Labels)
	if err != nil {
		return fmt.Errorf("reconcile: encode labels: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO kanban_cards (id, board_id, column_id, title, ref_kind, ref_id, labels, goal_id, employee_id, plan_id, session_id, trigger_ref, order_val) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.BoardID, c.ColumnID, c.Title, c.Ref.Kind, c.Ref.ID, string(labelsJSON),
		c.GoalID, c.EmployeeID, c.PlanID, c.SessionID, c.TriggerRef, c.Order)
	if err != nil {
		return fmt.Errorf("reconcile: create card for %s/%s: %w", c.Ref.Kind, c.Ref.ID, err)
	}
	return nil
}

func updateCardTx(ctx context.Context, tx *sql.Tx, c Card) error {
	labelsJSON, err := json.Marshal(c.Labels)
	if err != nil {
		return fmt.Errorf("reconcile: encode labels for card %s: %w", c.ID, err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE kanban_cards SET board_id=?, column_id=?, title=?, ref_kind=?, ref_id=?, labels=?, goal_id=?, employee_id=?, plan_id=?, session_id=?, trigger_ref=?, order_val=? WHERE id=?`,
		c.BoardID, c.ColumnID, c.Title, c.Ref.Kind, c.Ref.ID, string(labelsJSON),
		c.GoalID, c.EmployeeID, c.PlanID, c.SessionID, c.TriggerRef, c.Order, c.ID)
	if err != nil {
		return fmt.Errorf("reconcile: repair card %s: %w", c.ID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reconcile: read rows affected: %w", err)
	}
	if affected == 0 {
		return ErrUnknownCard{ID: c.ID}
	}
	return nil
}

// defaultReconcilers binds each live Store to its wired Reconciler so
// the 3-arg RunPeriodicReconcile signature can drive a fetcher the
// helper cannot know about (no machine imports here). The daemon owns
// exactly one store; tests use UnregisterDefaultReconciler.
var defaultReconcilers sync.Map // *Store -> *Reconciler

// RegisterDefaultReconciler binds a fully-wired reconciler (ListFetcher
// + RowFetcher in hand) to its store. 07/01 calls it right after
// NewReconcilerWithProvenance. Nil arguments are ignored.
func RegisterDefaultReconciler(store *Store, rec *Reconciler) {
	if store == nil || rec == nil {
		return
	}
	defaultReconcilers.Store(store, rec)
}

// UnregisterDefaultReconciler drops the binding (test teardown).
func UnregisterDefaultReconciler(store *Store) {
	if store == nil {
		return
	}
	defaultReconcilers.Delete(store)
}

// RunPeriodicReconcile drives the registered reconciler's ReconcileNow
// on a ticker until ctx is cancelled, then returns ctx.Err() with no
// goroutine left behind (07/01 calls it in one on the daemon stop
// context). interval <= 0 means DISABLED (C9 default "0s"): returns nil
// immediately. Ticks before registration are no-ops. A nil store is an
// error return, never a panic.
func RunPeriodicReconcile(ctx context.Context, store *Store, interval time.Duration) error {
	if store == nil {
		return fmt.Errorf("reconcile: store is required")
	}
	if interval <= 0 {
		return nil // C9: "0s" = disabled
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			v, ok := defaultReconcilers.Load(store)
			if !ok {
				continue
			}
			rec, valid := v.(*Reconciler)
			if !valid || rec == nil {
				continue
			}
			if err := rec.ReconcileNow(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return ctx.Err() // shutdown path, not a finding
				}
				store.logger.Warn("kanban reconcile: periodic pass failed", "error", err)
			}
		}
	}
}
