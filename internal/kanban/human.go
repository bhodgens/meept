package kanban

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/id"
)

// human.go implements plan leaf 03-board-projection/03: the human command
// surface over the projected board (C4 verbs kanban.card.move / takeover /
// assign / comment / timeline run through these methods; the RPC layer
// itself is 05-surfaces/01, the concrete LeaseAuthority is wired by
// 07-daemon-wiring/01).
//
// Why a separate service type instead of *Manager methods: Manager is
// declared in board.go with only a store handle, and board.go is not this
// leaf's file. The human path needs three collaborators the projection and
// CRUD lanes never touch — the bus (C2 publishes), the lease authority
// (C10 rule A), and the wip_mode policy (C9) — so they are held here and
// injected at construction. The machine lane (projector.go) deliberately
// performs NO lease check (see projector.go header); every refusal in this
// file is the human lane enforcing decision 1 / C10.

// WIP mode values (C9 [kanban] wip_mode). config.KanbanConfig.Validate
// accepts exactly "warn", "block", or "" (= warn default); this package
// mirrors that enum without importing config, so a hand-built fixture and
// the daemon's loaded config behave identically.
const (
	WIPModeWarn  = "warn"
	WIPModeBlock = "block"
)

// Card-event kinds written to kanban_card_events (C3). The table backs
// both kanban.card.comment and kanban.card.timeline; comment/takeover/
// assign/move/wip_warning are the human-lane kinds. Machine lanes never
// write this table — projection state rides kanban_cards.
const (
	CardEventComment    = "comment"
	CardEventMove       = "move"
	CardEventAssign     = "assign"
	CardEventTakeover   = "takeover"
	CardEventWIPWarning = "wip_warning"
)

// BoardChanged is the C2 kanban.board.changed payload. The C2 table pins
// the shape to exactly {board_id}; Note is the C10 wip_mode="warn" carrier
// ("warn moves but emits a kanban.board.changed note payload"), kept as an
// omitted field so a plain change still marshals to {"board_id": ...}.
type BoardChanged struct {
	BoardID string `json:"board_id"`
	Note    string `json:"note,omitempty"`
}

// C2 typed topic declarations for the two board-side output topics.
// Placement follows the events.go:70 precedent (internal/agent/topics.go):
// the declaration lives with the package that owns the payload, imports
// bus, never the reverse. events.go is 02/03-owned and frozen, so these
// land here — 03/03 is the first live publisher (subscribers arrive in
// branch 05; the orphan ratchet may flag them until then, by design).
var (
	TopicCardMoved    = bus.NewTopic[CardMoved]("kanban.card.moved")
	TopicBoardChanged = bus.NewTopic[BoardChanged]("kanban.board.changed")
)

// LeaseAuthority is the injected seam between the human command path and
// the work-item machines' claim machinery (master.md decision 1). kanban
// must NOT import the task registry / plan store / employee packages
// directly (leaf constraint: bus/config/auth only), so every lease question
// is asked through this three-method surface. 07-daemon-wiring/01 binds a
// concrete implementation over the queue/task/goal stores.
type LeaseAuthority interface {
	// LookupLease reports whether the work item behind ref currently holds
	// a live claim, naming the holder and the claim's start time so the
	// UI can render holder + age from ErrLeaseHeld (C10).
	LookupLease(ctx context.Context, ref WorkRef) (holder string, since time.Time, held bool, err error)
	// CancelEpisode stops the agent episode driving the work item.
	// Master.md C10 puts this BEFORE ReleaseToken ("cancels the episode,
	// releases the token") so the worker cannot renew a token that was
	// already dropped.
	CancelEpisode(ctx context.Context, ref WorkRef) error
	// ReleaseToken releases the claim token so no machine re-claims under
	// the human's feet.
	ReleaseToken(ctx context.Context, ref WorkRef) error
}

// noopLeaseAuthority is the nil-default: no lease is ever held, release
// and cancel silently succeed. This keeps the board fully usable (manual
// cards, no projection rules) before 07/01 wires the real authority, and
// makes every command path nil-safe by construction.
type noopLeaseAuthority struct{}

func (noopLeaseAuthority) LookupLease(context.Context, WorkRef) (string, time.Time, bool, error) {
	return "", time.Time{}, false, nil
}
func (noopLeaseAuthority) CancelEpisode(context.Context, WorkRef) error { return nil }
func (noopLeaseAuthority) ReleaseToken(context.Context, WorkRef) error  { return nil }

// isNilish guards the typed-nil trap (docs/agents/coding-practices.md): a
// (*Concrete)(nil) stored in the interface passes == nil as non-nil and
// panics on the first call whose method derefs the receiver.
func isNilish(a LeaseAuthority) bool {
	if a == nil {
		return true
	}
	v := reflect.ValueOf(a)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	default:
		return false
	}
}

// HumanOptions are the constructor inputs. Zero values are valid: WIPMode
// "" normalizes to "warn" (the C9 default), Bus nil means "publish
// nowhere" (lite/tests), Authority nil means "no leases exist".
type HumanOptions struct {
	WIPMode   string
	Bus       *bus.MessageBus
	Authority LeaseAuthority
	Logger    *slog.Logger
}

// HumanCommands is the human lane of the board. Store I/O is always done
// OUTSIDE mu (mutexio invariant): mu guards only the three collaborator
// fields, which are snapshotted at the top of every command and then used
// lock-free.
type HumanCommands struct {
	store *Store

	mu        sync.RWMutex
	bus       *bus.MessageBus
	authority LeaseAuthority
	wipMode   string
	logger    *slog.Logger
}

// NewHumanCommands builds the service. Only a missing store is fatal; a
// bad WIPMode is rejected with the same enum rule as
// config.KanbanConfig.Validate so wiring mistakes fail at boot, not at
// first move.
func NewHumanCommands(store *Store, opts HumanOptions) (*HumanCommands, error) {
	if store == nil {
		return nil, fmt.Errorf("kanban: human commands require a store")
	}
	mode := opts.WIPMode
	if mode == "" {
		mode = WIPModeWarn
	}
	if mode != WIPModeWarn && mode != WIPModeBlock {
		return nil, fmt.Errorf("kanban: wip_mode %q invalid, want %q or %q", mode, WIPModeWarn, WIPModeBlock)
	}
	authority := opts.Authority
	if isNilish(authority) {
		authority = noopLeaseAuthority{}
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &HumanCommands{
		store:     store,
		bus:       opts.Bus,
		authority: authority,
		wipMode:   mode,
		logger:    logger,
	}, nil
}

// SetBus swaps the publish bus. Nil is a legal value ("publish nowhere");
// the rule that every Set* is nil-safe is honored by accepting nil without
// a panic and downgrading publishes to no-ops.
func (h *HumanCommands) SetBus(b *bus.MessageBus) {
	h.mu.Lock()
	h.bus = b
	h.mu.Unlock()
}

// SetLeaseAuthority binds the concrete claim authority (07/01 wiring).
// Nil (typed or plain) restores the noop default — never a nil deref.
func (h *HumanCommands) SetLeaseAuthority(a LeaseAuthority) {
	if isNilish(a) {
		a = noopLeaseAuthority{}
	}
	h.mu.Lock()
	h.authority = a
	h.mu.Unlock()
}

// SetWIPMode swaps the C9 policy. Invalid values are rejected and the old
// mode stands, so a config reload carrying garbage cannot silently
// weaken a "block" posture to "warn".
func (h *HumanCommands) SetWIPMode(mode string) error {
	if mode == "" {
		mode = WIPModeWarn
	}
	if mode != WIPModeWarn && mode != WIPModeBlock {
		return fmt.Errorf("kanban: wip_mode %q invalid, want %q or %q", mode, WIPModeWarn, WIPModeBlock)
	}
	h.mu.Lock()
	h.wipMode = mode
	h.mu.Unlock()
	return nil
}

// deps snapshots the collaborator fields; all I/O happens after this
// returns, with mu released.
func (h *HumanCommands) deps() (*bus.MessageBus, LeaseAuthority, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.bus, h.authority, h.wipMode
}

// MoveCard executes a human card move enforcing C10: lease rule A first
// (a move onto a column the card's work item projects into while a live
// claim is held is refused with ErrLeaseHeld{Holder, Since}), then the
// wip_mode policy BEFORE the write ("block" refuses with ErrWIPFull,
// "warn" moves and notes). Successful moves ONLY publish CardMoved on
// kanban.card.moved and BoardChanged on kanban.board.changed (C2), and
// append a "move" timeline row (C3).
//
// fromCol "" defaults to the card's current column; a non-empty mismatch
// is ErrStaleCard (the same linearisation the store CAS gives cross-
// process movers). A same-column move is a no-op success that publishes
// nothing — re-drops onto the origin column are ordering gestures, not
// state changes.
func (h *HumanCommands) MoveCard(ctx context.Context, cardID, fromCol, toCol, actor string) error {
	return h.move(ctx, cardID, fromCol, toCol, actor, true, false)
}

// Takeover is the only lease-rule bypass (master.md decision 1; C4
// kanban.card.takeover, grant board.takeover enforced at the RPC layer
// 05/01, not here — this package has no principal context).
//
// ORDER, pinned by the frozen contract text: audit row FIRST
// (kanban_card_events kind=takeover — "it emits an audit record before
// acting (audit-before-effect precedent, voice dial)"), then cancel the
// episode, then release the token ("cancels the episode, releases the
// token"), per master.md C10 verbatim. A failure of either authority step
// aborts before any card mutation; the audit row stays, because it records
// that the takeover was ATTEMPTED. Takeover on a card with no work ref or
// no live lease is a legal no-op (audit lands, release/cancel are inert) —
// the human may want the audit trail before a plain MoveCard regardless of
// whether a lease is actually held.
func (h *HumanCommands) Takeover(ctx context.Context, cardID, actor string) error {
	if cardID == "" || actor == "" {
		return fmt.Errorf("kanban: takeover requires cardID and actor")
	}
	card, err := h.store.GetCard(ctx, cardID)
	if err != nil {
		return fmt.Errorf("kanban: takeover: %w", err)
	}
	_, authority, _ := h.deps()

	// 1. audit-before-effect.
	if err := h.appendEvent(ctx, cardID, actor, CardEventTakeover,
		fmt.Sprintf("takeover claimed by %s (card ref %s/%s)", actor, card.Ref.Kind, card.Ref.ID)); err != nil {
		return fmt.Errorf("kanban: takeover audit: %w", err)
	}

	// 2/3. cancel episode, release token. Manual cards (empty ref) skip
	// both: there is no machine claim to break.
	if card.Ref.Kind != "" && card.Ref.ID != "" {
		if err := authority.CancelEpisode(ctx, card.Ref); err != nil {
			return fmt.Errorf("kanban: takeover cancel episode: %w", err)
		}
		if err := authority.ReleaseToken(ctx, card.Ref); err != nil {
			return fmt.Errorf("kanban: takeover release token: %w", err)
		}
	}
	return nil
}

// TakeoverMove composes the full takeover-then-move gesture C10 describes
// ("takeover ... then moves") against the injected authority. The move
// that follows a successful takeover SKIPS the lease re-check — the claim
// was just broken by this very call, and re-consulting a store that
// commits the release asynchronously could race the bypass back into a
// refusal. The CardMoved it publishes carries LeaseBlocked=true so
// surfaces can tell a forced move from an ordinary one. WIP policy still
// applies: takeover bypasses the lease, never a column limit.
func (h *HumanCommands) TakeoverMove(ctx context.Context, cardID, fromCol, toCol, actor string) error {
	if err := h.Takeover(ctx, cardID, actor); err != nil {
		return err
	}
	return h.move(ctx, cardID, fromCol, toCol, actor, false, true)
}

// Assign records the owning employee on the card (C4 kanban.card.assign).
// employeeID "" un-assigns. It writes the card row, appends an "assign"
// timeline row, and publishes board.changed — but never card.moved: an
// assignment is not a column transition.
func (h *HumanCommands) Assign(ctx context.Context, cardID, employeeID string) error {
	if cardID == "" {
		return fmt.Errorf("kanban: assign requires cardID")
	}
	card, err := h.store.GetCard(ctx, cardID)
	if err != nil {
		return fmt.Errorf("kanban: assign: %w", err)
	}
	card.EmployeeID = employeeID
	if err := h.store.UpdateCard(ctx, card); err != nil {
		return fmt.Errorf("kanban: assign update card: %w", err)
	}
	note := fmt.Sprintf("assigned to %s", employeeID)
	if employeeID == "" {
		note = "unassigned"
	}
	if err := h.appendEvent(ctx, cardID, employeeID, CardEventAssign, note); err != nil {
		return fmt.Errorf("kanban: assign audit: %w", err)
	}
	h.publishBoardChanged(BoardChanged{BoardID: card.BoardID})
	return nil
}

// Comment appends one kind="comment" row to kanban_card_events (C3 — the
// same append-only table that backs Timeline; this is the data source of
// C4 kanban.card.comment). The actor parameter exists because C3 pins an
// actor column on the row; the RPC layer (05/01) passes the authenticated
// principal. It returns the stored event (id + created_at assigned by the
// store) so callers can echo it.
func (h *HumanCommands) Comment(ctx context.Context, cardID, actor, text string) (CardEvent, error) {
	if cardID == "" || text == "" {
		return CardEvent{}, fmt.Errorf("kanban: comment requires cardID and text")
	}
	// The FK would reject an orphan row anyway, but the pre-check returns
	// the contract error (ErrUnknownCard) instead of a raw constraint
	// failure.
	if _, err := h.store.GetCard(ctx, cardID); err != nil {
		return CardEvent{}, fmt.Errorf("kanban: comment: %w", err)
	}
	// Stamp ID + CreatedAt here rather than relying on the store's
	// zero-filling: AppendCardEvent takes the struct by value, so its
	// generated id would be invisible to the caller. Pre-stamping makes
	// the echoed event exact, not best-effort.
	ev := CardEvent{
		ID:        id.Generate("evt-"),
		CardID:    cardID,
		Kind:      CardEventComment,
		Actor:     actor,
		Body:      text,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.store.AppendCardEvent(ctx, ev); err != nil {
		return CardEvent{}, fmt.Errorf("kanban: comment append: %w", err)
	}
	return ev, nil
}

// Timeline reads the card's kanban_card_events rows oldest-first (C3/C4
// kanban.card.timeline — the comment method's table, so the two are
// round-trip consistent by construction). An unknown card returns an
// empty timeline, matching the store's append-only read posture.
func (h *HumanCommands) Timeline(ctx context.Context, cardID string) ([]CardEvent, error) {
	if cardID == "" {
		return nil, fmt.Errorf("kanban: timeline requires cardID")
	}
	events, err := h.store.ListCardEvents(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("kanban: timeline: %w", err)
	}
	return events, nil
}

// LeaseAge extracts the live-claim age from an ErrLeaseHeld for surface
// rendering ("The UI must display holder + age from the error", C10).
// ok=false for any other error.
func LeaseAge(err error) (time.Duration, bool) {
	if lh, ok := errors.AsType[ErrLeaseHeld](err); ok {
		return time.Since(lh.Since), true
	}
	return 0, false
}

// move is the shared body of MoveCard / TakeoverMove. enforceLease=false
// marks the takeover bypass; leaseBlocked stamps the published CardMoved.
func (h *HumanCommands) move(ctx context.Context, cardID, fromCol, toCol, actor string, enforceLease, leaseBlocked bool) error {
	if cardID == "" || toCol == "" || actor == "" {
		return fmt.Errorf("kanban: move requires cardID, toCol and actor")
	}
	b, authority, wipMode := h.deps()

	card, err := h.store.GetCard(ctx, cardID)
	if err != nil {
		return fmt.Errorf("kanban: move: %w", err)
	}
	if fromCol == "" {
		fromCol = card.ColumnID
	}
	if fromCol != card.ColumnID {
		return ErrStaleCard
	}
	if toCol == fromCol {
		return nil // same-column ordering gesture: no state, no publish
	}

	// The target column must exist on the SAME board — an unknown toCol is
	// a miss, not an FK constraint blowup, and a cross-board move would
	// decouple card.BoardID from its column's board.
	columns, err := h.store.ListColumns(ctx, card.BoardID)
	if err != nil {
		return fmt.Errorf("kanban: move list columns: %w", err)
	}
	var target Column
	found := false
	for _, c := range columns {
		if c.ID == toCol {
			target = c
			found = true
			break
		}
	}
	if !found {
		return ErrUnknownColumn{ID: toCol}
	}

	// C10 rule A — refusal condition, verbatim: "card.move into a column
	// whose mapping targets an active state of a card whose work ref holds
	// a live claim". "whose mapping targets" is the kanban_projections
	// lane (a column no rule maps the card's kind into is a purely manual
	// column: moving there is not entering the machine's active lane, so
	// the lease rule is not engaged). The live-claim question itself is
	// the authority's job; a lookup error FAILS CLOSED — a board that
	// cannot answer "is this claimed?" must not move claimed work.
	if enforceLease && card.Ref.Kind != "" && card.Ref.ID != "" {
		mapped, err := h.columnIsProjectedTarget(ctx, card, toCol)
		if err != nil {
			return err
		}
		if mapped {
			holder, since, held, err := authority.LookupLease(ctx, card.Ref)
			if err != nil {
				return fmt.Errorf("kanban: move lease lookup: %w", err)
			}
			if held {
				return ErrLeaseHeld{Holder: holder, Since: since}
			}
		}
	}

	// C10 WIP rule: target count vs Column.WIP (0 = unlimited) BEFORE the
	// move. block refuses; warn moves and notes (C2 board.changed note +
	// a wip_warning timeline row).
	var note string
	if target.WIP > 0 {
		count, err := h.store.countCards(ctx, toCol)
		if err != nil {
			return fmt.Errorf("kanban: move wip count: %w", err)
		}
		if count >= target.WIP {
			if wipMode == WIPModeBlock {
				return ErrWIPFull{ColumnID: toCol, Limit: target.WIP}
			}
			note = fmt.Sprintf("moved onto over-WIP column %s: %d/%d", toCol, count, target.WIP)
		}
	}

	// Effect. The store CAS re-checks the source column, so a racer that
	// moved the card between our read and write loses linearly here.
	if err := h.store.MoveCard(ctx, cardID, fromCol, toCol); err != nil {
		return fmt.Errorf("kanban: move: %w", err)
	}

	// Timeline rows (C3), then the two C2 publishes — successful human
	// moves ONLY; refusals above published nothing.
	if err := h.appendEvent(ctx, cardID, actor, CardEventMove,
		fmt.Sprintf("moved %s -> %s by %s", fromCol, toCol, actor)); err != nil {
		return fmt.Errorf("kanban: move audit: %w", err)
	}
	if note != "" {
		if err := h.appendEvent(ctx, cardID, actor, CardEventWIPWarning, note); err != nil {
			return fmt.Errorf("kanban: move wip note: %w", err)
		}
	}
	if b != nil {
		bus.PublishT(b, TopicCardMoved, "kanban-human", CardMoved{
			CardID:       cardID,
			FromColumnID: fromCol,
			ToColumnID:   toCol,
			Actor:        actor,
			LeaseBlocked: leaseBlocked,
		})
		bus.PublishT(b, TopicBoardChanged, "kanban-human", BoardChanged{BoardID: card.BoardID, Note: note})
	}
	return nil
}

// columnIsProjectedTarget reports whether any projection rule on the
// card's board maps the card's work-item kind into columnID — the
// "column whose mapping targets an active state" predicate of C10 rule A.
func (h *HumanCommands) columnIsProjectedTarget(ctx context.Context, card Card, columnID string) (bool, error) {
	rules, err := h.store.LoadProjectionRules(ctx, card.BoardID)
	if err != nil {
		return false, fmt.Errorf("kanban: move rule lookup: %w", err)
	}
	for _, r := range rules.Rows() {
		if r.Kind == card.Ref.Kind && r.ColumnID == columnID {
			return true, nil
		}
	}
	return false, nil
}

// appendEvent writes one kanban_card_events row (C3). The store fills ID
// (pkg/id) and CreatedAt when zero.
func (h *HumanCommands) appendEvent(ctx context.Context, cardID, actor, kind, body string) error {
	return h.store.AppendCardEvent(ctx, CardEvent{
		CardID: cardID,
		Kind:   kind,
		Actor:  actor,
		Body:   body,
	})
}

// publishBoardChanged fires kanban.board.changed with a nil-bus no-op
// guard (PublishT would deref the bus).
func (h *HumanCommands) publishBoardChanged(payload BoardChanged) {
	b, _, _ := h.deps()
	if b == nil {
		return
	}
	bus.PublishT(b, TopicBoardChanged, "kanban-human", payload)
}
