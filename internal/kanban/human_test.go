package kanban

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// human_test.go pins plan leaf 03-board-projection/03: C10 lease rule A +
// wip_mode policy, the takeover audit-before-effect ORDERING, the C3
// comment/timeline round-trip, and the two C2 publish surfaces
// (kanban.card.moved / kanban.board.changed) on successful human moves
// only.

// fakeAuthority is a recording LeaseAuthority. The cancel/release hooks
// observe the live kanban_card_events table at call time, which is how the
// audit-before-effect ordering is pinned (not by call order alone).
type fakeAuthority struct {
	held       bool
	holder     string
	since      time.Time
	lookupErr  error
	cancelErr  error
	releaseErr error

	calls []string

	// probe: store snapshot read inside CancelEpisode to prove the audit
	// row landed before the authority was consulted.
	probeStore   *Store
	probeCardID  string
	auditRowsAtC int
}

func (f *fakeAuthority) LookupLease(ctx context.Context, ref WorkRef) (string, time.Time, bool, error) {
	f.calls = append(f.calls, "lookup")
	if f.lookupErr != nil {
		return "", time.Time{}, false, f.lookupErr
	}
	if f.held {
		return f.holder, f.since, true, nil
	}
	return "", time.Time{}, false, nil
}

func (f *fakeAuthority) CancelEpisode(ctx context.Context, ref WorkRef) error {
	f.calls = append(f.calls, "cancel")
	if f.probeStore != nil && f.probeCardID != "" {
		events, err := f.probeStore.ListCardEvents(ctx, f.probeCardID)
		if err == nil {
			for _, ev := range events {
				if ev.Kind == CardEventTakeover {
					f.auditRowsAtC++
				}
			}
		}
	}
	return f.cancelErr
}

func (f *fakeAuthority) ReleaseToken(ctx context.Context, ref WorkRef) error {
	f.calls = append(f.calls, "release")
	return f.releaseErr
}

func newHumanTestSvc(t *testing.T, s *Store, opts HumanOptions) *HumanCommands {
	t.Helper()
	h, err := NewHumanCommands(s, opts)
	if err != nil {
		t.Fatalf("NewHumanCommands: %v", err)
	}
	return h
}

// subscribeRaw owns the channel read directly (SubscribeT's reader
// goroutine would race it — the house pattern from events_test.go).
func subscribeRaw(t *testing.T, b *bus.MessageBus, topic string) *bus.Subscriber {
	t.Helper()
	sub := b.Subscribe("raw-sub-"+topic, topic)
	t.Cleanup(func() { b.Unsubscribe(sub) })
	return sub
}

// mustReceive drains one message or fails: used to pin that a publish DID
// happen.
func mustReceive(t *testing.T, sub *bus.Subscriber) *models.BusMessage {
	t.Helper()
	select {
	case m := <-sub.Channel:
		return m
	case <-time.After(2 * time.Second):
		t.Fatalf("no message on topic %q", sub.Topic)
		return nil
	}
}

// assertNoMessage pins refusals publish nothing (the bus is buffered, so
// a short non-blocking window is sufficient — delivery is synchronous into
// the subscriber channel).
func assertNoMessage(t *testing.T, sub *bus.Subscriber) {
	t.Helper()
	select {
	case m := <-sub.Channel:
		t.Fatalf("unexpected message on %q: %s", sub.Topic, string(m.Payload))
	default:
	}
}

func seedCard(t *testing.T, s *Store, boardID, colID, title string, ref WorkRef) string {
	t.Helper()
	ctx := context.Background()
	// Explicit ID: Store.CreateCard takes Card by value, so its
	// pkg/id-generated id would never surface to the test (neighbor
	// tests seed with fixed ids for the same reason).
	cardID := "card-" + title
	card := Card{ID: cardID, BoardID: boardID, ColumnID: colID, Title: title, Ref: ref}
	if err := s.CreateCard(ctx, card); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	return card.ID
}

// ------------------------------------------------------------------
// Constructor / setter nil guards
// ------------------------------------------------------------------

func TestHumanCommands_ConstructorGuards(t *testing.T) {
	if _, err := NewHumanCommands(nil, HumanOptions{}); err == nil {
		t.Fatal("NewHumanCommands(nil store): want error")
	}
	s := newTestStore(t)
	if _, err := NewHumanCommands(s, HumanOptions{WIPMode: "vibes"}); err == nil {
		t.Fatal("NewHumanCommands(wip_mode=vibes): want error (C9 enum)")
	}
	// Empty WIPMode normalizes to the C9 default "warn".
	h := newHumanTestSvc(t, s, HumanOptions{})
	if _, _, mode := h.deps(); mode != WIPModeWarn {
		t.Errorf("default wip_mode = %q, want %q", mode, WIPModeWarn)
	}
	// Nil authority downgrades to the noop default, not a nil deref: a
	// takeover on a referenced card must succeed through the noop path.
	h2 := newHumanTestSvc(t, s, HumanOptions{Authority: nil})
	boardID, colA, _ := seedBoard(t, s)
	cid := seedCard(t, s, boardID, colA, "c", WorkRef{Kind: "task", ID: "t-nil"})
	if err := h2.Takeover(context.Background(), cid, "ops"); err != nil {
		t.Fatalf("Takeover with nil authority: %v", err)
	}
}

func TestHumanCommands_SettersNilSafe(t *testing.T) {
	s := newTestStore(t)
	h := newHumanTestSvc(t, s, HumanOptions{})
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)

	// Typed-nil interface trap: a (*fakeAuthority)(nil) is non-nil under
	// == nil and would panic on first method call if stored raw. All
	// setters must accept it and keep the service functional.
	var typedNil *fakeAuthority
	h.SetLeaseAuthority(typedNil)
	h.SetBus(nil)
	if err := h.SetWIPMode(""); err != nil {
		t.Fatalf("SetWIPMode(\"\"): %v", err)
	}
	if err := h.SetWIPMode("block"); err != nil {
		t.Fatalf("SetWIPMode(block): %v", err)
	}
	if err := h.SetWIPMode("nope"); err == nil {
		t.Fatal("SetWIPMode(nope): want error, old mode must stand")
	}
	if _, _, mode := h.deps(); mode != WIPModeBlock {
		t.Errorf("mode after rejected set = %q, want block", mode)
	}

	// And the command path must still work with everything nil-shaped.
	cid := seedCard(t, s, boardID, colA, "setter card", WorkRef{Kind: "task", ID: "t-set"})
	if err := h.MoveCard(ctx, cid, "", colB, "ops"); err != nil {
		t.Fatalf("MoveCard after nil setters: %v", err)
	}
}

// ------------------------------------------------------------------
// C10 lease rule A
// ------------------------------------------------------------------

func TestHumanMove_LeaseRefusal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	b := bus.New(nil, nil)
	movedSub := subscribeRaw(t, b, TopicCardMoved.Name)
	changedSub := subscribeRaw(t, b, TopicBoardChanged.Name)

	since := time.Now().UTC().Add(-90 * time.Second)
	auth := &fakeAuthority{held: true, holder: "emp-7", since: since}
	h := newHumanTestSvc(t, s, HumanOptions{Bus: b, Authority: auth})

	// colB is an ACTIVE-lane target: a projection rule maps the task kind
	// into it ("a column whose mapping targets an active state").
	if err := s.SetProjectionRule(ctx, ProjectionRule{BoardID: boardID, Kind: "task", FromState: "pending", ColumnID: colB}); err != nil {
		t.Fatalf("SetProjectionRule: %v", err)
	}
	cid := seedCard(t, s, boardID, colA, "leased card", WorkRef{Kind: "task", ID: "task-1"})

	err := h.MoveCard(ctx, cid, colA, colB, "ops")
	var lh ErrLeaseHeld
	if !errors.As(err, &lh) {
		t.Fatalf("MoveCard into claimed lane: got %v, want ErrLeaseHeld", err)
	}
	if lh.Holder != "emp-7" {
		t.Errorf("ErrLeaseHeld.Holder = %q, want emp-7", lh.Holder)
	}
	if age, ok := LeaseAge(err); !ok || age < 60*time.Second || age > 5*time.Minute {
		t.Errorf("LeaseAge = %v (ok=%v), want ~90s surfaced from the error", age, ok)
	}

	// The refusal must not move the card and must not publish.
	if card, err := s.GetCard(ctx, cid); err != nil || card.ColumnID != colA {
		t.Fatalf("card moved despite lease refusal: %+v err=%v", card, err)
	}
	assertNoMessage(t, movedSub)
	assertNoMessage(t, changedSub)
	// Lease lane consulted, effect never reached the store.
	if len(auth.calls) != 1 || auth.calls[0] != "lookup" {
		t.Errorf("authority calls = %v, want [lookup]", auth.calls)
	}
}

func TestHumanMove_LeaseLookupErrorFailsClosed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	if err := s.SetProjectionRule(ctx, ProjectionRule{BoardID: boardID, Kind: "task", FromState: "pending", ColumnID: colB}); err != nil {
		t.Fatalf("SetProjectionRule: %v", err)
	}
	auth := &fakeAuthority{lookupErr: errors.New("registry down")}
	h := newHumanTestSvc(t, s, HumanOptions{Authority: auth})
	cid := seedCard(t, s, boardID, colA, "card", WorkRef{Kind: "task", ID: "task-2"})

	err := h.MoveCard(ctx, cid, colA, colB, "ops")
	if err == nil {
		t.Fatal("lease lookup error must refuse the move (fail closed)")
	}
	if !errors.Is(err, auth.lookupErr) {
		t.Errorf("lookup error not wrapped: %v", err)
	}
}

func TestHumanMove_LeaseNotEngagedOutsideProjectedLane(t *testing.T) {
	// C10 rule A refuses only moves into a column the mapping targets.
	// Manual reorder into a column no rule maps to is not "entering the
	// machine's active lane" — a held lease must not block it.
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	// No projection rules at all.
	auth := &fakeAuthority{held: true, holder: "emp-7", since: time.Now().UTC()}
	h := newHumanTestSvc(t, s, HumanOptions{Authority: auth})
	cid := seedCard(t, s, boardID, colA, "card", WorkRef{Kind: "task", ID: "task-3"})

	if err := h.MoveCard(ctx, cid, colA, colB, "ops"); err != nil {
		t.Fatalf("MoveCard into unmapped column: %v", err)
	}
	if len(auth.calls) != 0 {
		t.Errorf("lease consulted on unmapped target: %v", auth.calls)
	}
}

func TestHumanMove_NoLeaseHappyPathAndPublishes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	if err := s.SetProjectionRule(ctx, ProjectionRule{BoardID: boardID, Kind: "task", FromState: "pending", ColumnID: colB}); err != nil {
		t.Fatalf("SetProjectionRule: %v", err)
	}
	b := bus.New(nil, nil)
	movedSub := subscribeRaw(t, b, TopicCardMoved.Name)
	changedSub := subscribeRaw(t, b, TopicBoardChanged.Name)
	auth := &fakeAuthority{}
	h := newHumanTestSvc(t, s, HumanOptions{Bus: b, Authority: auth})
	cid := seedCard(t, s, boardID, colA, "free card", WorkRef{Kind: "task", ID: "task-4"})

	if err := h.MoveCard(ctx, cid, colA, colB, "ops"); err != nil {
		t.Fatalf("MoveCard happy path: %v", err)
	}
	var cm CardMoved
	if m := mustReceive(t, movedSub); json.Unmarshal(m.Payload, &cm) != nil {
		t.Fatal("card.moved payload undecodable")
	}
	if cm.CardID != cid || cm.FromColumnID != colA || cm.ToColumnID != colB || cm.Actor != "ops" || cm.LeaseBlocked {
		t.Errorf("CardMoved payload = %+v, want {%s %s %s ops false}", cm, cid, colA, colB)
	}
	var bc BoardChanged
	if m := mustReceive(t, changedSub); json.Unmarshal(m.Payload, &bc) != nil {
		t.Fatal("board.changed payload undecodable")
	}
	if bc.BoardID != boardID || bc.Note != "" {
		t.Errorf("BoardChanged = %+v, want {boardID, no note}", bc)
	}
	// A move leaves a timeline row (C3).
	events, err := h.Timeline(ctx, cid)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(events) != 1 || events[0].Kind != CardEventMove {
		t.Errorf("timeline after move = %+v, want one move row", events)
	}
}

func TestHumanMove_SameColumnAndBadTargets(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	b := bus.New(nil, nil)
	movedSub := subscribeRaw(t, b, TopicCardMoved.Name)
	h := newHumanTestSvc(t, s, HumanOptions{Bus: b})
	cid := seedCard(t, s, boardID, colA, "card", WorkRef{})

	// Same-column gesture: success, no publish, no timeline row.
	if err := h.MoveCard(ctx, cid, colA, colA, "ops"); err != nil {
		t.Fatalf("same-column move: %v", err)
	}
	assertNoMessage(t, movedSub)

	// Unknown target column -> ErrUnknownColumn (not an FK blowup).
	if err := h.MoveCard(ctx, cid, colA, "col-zzz", "ops"); err == nil {
		t.Fatal("unknown toCol: want ErrUnknownColumn")
	} else {
		var uc ErrUnknownColumn
		if !errors.As(err, &uc) || uc.ID != "col-zzz" {
			t.Fatalf("unknown toCol: got %v, want ErrUnknownColumn{col-zzz}", err)
		}
	}
	// Stale fromCol -> ErrStaleCard (caller must re-read, per store.go).
	if err := h.MoveCard(ctx, cid, "col-old", colB, "ops"); !errors.Is(err, ErrStaleCard) {
		t.Fatalf("stale fromCol: got %v, want ErrStaleCard", err)
	}
	// Unknown card -> ErrUnknownCard.
	if err := h.MoveCard(ctx, "card-nope", "", colB, "ops"); !errors.As(err, new(ErrUnknownCard)) {
		t.Fatalf("unknown card: got %v, want ErrUnknownCard", err)
	}
	assertNoMessage(t, movedSub)
}

// ------------------------------------------------------------------
// C10 WIP policy (block vs warn)
// ------------------------------------------------------------------

func fillColumn(t *testing.T, s *Store, boardID, colID, prefix string, n int) {
	t.Helper()
	ctx := context.Background()
	for range n {
		if err := s.CreateCard(ctx, Card{BoardID: boardID, ColumnID: colID, Title: prefix}); err != nil {
			t.Fatalf("fill card: %v", err)
		}
	}
}

func TestHumanMove_WIPBlock(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// seedBoard: colB has WIP 2.
	boardID, colA, colB := seedBoard(t, s)
	fillColumn(t, s, boardID, colB, "filler", 2)
	b := bus.New(nil, nil)
	movedSub := subscribeRaw(t, b, TopicCardMoved.Name)
	h := newHumanTestSvc(t, s, HumanOptions{Bus: b, WIPMode: WIPModeBlock})
	cid := seedCard(t, s, boardID, colA, "the movee", WorkRef{})

	err := h.MoveCard(ctx, cid, colA, colB, "ops")
	var wf ErrWIPFull
	if !errors.As(err, &wf) {
		t.Fatalf("block move at cap: got %v, want ErrWIPFull", err)
	}
	if wf.ColumnID != colB || wf.Limit != 2 {
		t.Errorf("ErrWIPFull = %+v, want {colB 2}", wf)
	}
	// BEFORE the move: card unmoved, nothing published, no timeline row.
	if card, err := s.GetCard(ctx, cid); err != nil || card.ColumnID != colA {
		t.Fatalf("card moved despite WIP block: %+v err=%v", card, err)
	}
	assertNoMessage(t, movedSub)
	if events, err := h.Timeline(ctx, cid); err != nil || len(events) != 0 {
		t.Errorf("timeline after blocked move = %+v err=%v, want empty", events, err)
	}
}

func TestHumanMove_WIPWarn(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	fillColumn(t, s, boardID, colB, "filler", 2)
	b := bus.New(nil, nil)
	movedSub := subscribeRaw(t, b, TopicCardMoved.Name)
	changedSub := subscribeRaw(t, b, TopicBoardChanged.Name)
	h := newHumanTestSvc(t, s, HumanOptions{Bus: b, WIPMode: WIPModeWarn})
	cid := seedCard(t, s, boardID, colA, "the movee", WorkRef{})

	if err := h.MoveCard(ctx, cid, colA, colB, "ops"); err != nil {
		t.Fatalf("warn move at cap must succeed: %v", err)
	}
	// Publish order in move(): card.moved first, board.changed second.
	var cm CardMoved
	if m := mustReceive(t, movedSub); json.Unmarshal(m.Payload, &cm) != nil {
		t.Fatal("no/wrong card.moved on warn move")
	}
	if cm.CardID != cid || cm.LeaseBlocked {
		t.Errorf("CardMoved = %+v, want moved card, not lease-blocked", cm)
	}
	// warn => note ON the board.changed payload (C10/C2).
	var bc BoardChanged
	m := mustReceive(t, changedSub)
	if err := json.Unmarshal(m.Payload, &bc); err != nil {
		t.Fatalf("board.changed decode: %v", err)
	}
	if bc.BoardID != boardID || bc.Note == "" {
		t.Errorf("BoardChanged = %+v, want boardID + note", bc)
	}
	// The note marshals away when absent: {"board_id"} exactly (C2).
	plain, _ := json.Marshal(BoardChanged{BoardID: "b"})
	if string(plain) != `{"board_id":"b"}` {
		t.Errorf("BoardChanged JSON = %s, want exactly {board_id}", plain)
	}
	// warn attaches a note to the timeline (C3 row).
	events, err := h.Timeline(ctx, cid)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	kinds := map[string]bool{}
	for _, ev := range events {
		kinds[ev.Kind] = true
	}
	if !kinds[CardEventMove] || !kinds[CardEventWIPWarning] {
		t.Errorf("timeline kinds after warn move = %+v, want move + wip_warning rows", events)
	}
}

func mustReceiveLenient(sub *bus.Subscriber) *models.BusMessage {
	select {
	case m := <-sub.Channel:
		return m
	case <-time.After(2 * time.Second):
		return nil
	}
}

// ------------------------------------------------------------------
// Takeover: audit-before-effect ORDERING + failure semantics
// ------------------------------------------------------------------

func TestHumanTakeover_AuditBeforeEffectOrdering(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, _ := seedBoard(t, s)
	cid := seedCard(t, s, boardID, colA, "card", WorkRef{Kind: "task", ID: "task-9"})

	auth := &fakeAuthority{probeStore: s, probeCardID: cid}
	h := newHumanTestSvc(t, s, HumanOptions{Authority: auth})

	if err := h.Takeover(ctx, cid, "ops"); err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	// ORDER PIN 1: the takeover audit row existed in kanban_card_events
	// at the moment the authority was first called (audit-before-effect).
	if auth.auditRowsAtC != 1 {
		t.Errorf("takeover audit rows visible to CancelEpisode = %d, want 1 (audit must land FIRST)", auth.auditRowsAtC)
	}
	// ORDER PIN 2: master.md C10 verbatim — "cancels the episode,
	// releases the token": episode cancel strictly before token release.
	if len(auth.calls) != 2 || auth.calls[0] != "cancel" || auth.calls[1] != "release" {
		t.Errorf("authority calls = %v, want [cancel release]", auth.calls)
	}
	// Takeover itself does not move the card; the move is the follow-on
	// gesture (TakeoverMove).
	if card, err := s.GetCard(ctx, cid); err != nil || card.ColumnID != colA {
		t.Errorf("Takeover must not move the card: %+v err=%v", card, err)
	}
}

func TestHumanTakeover_UnleasedCardIsLegalNoOp(t *testing.T) {
	// Manual card (no work ref): release/cancel are inert, the audit row
	// still lands (the human wants the trail before the move).
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, _ := seedBoard(t, s)
	auth := &fakeAuthority{}
	h := newHumanTestSvc(t, s, HumanOptions{Authority: auth})
	cid := seedCard(t, s, boardID, colA, "manual card", WorkRef{})

	if err := h.Takeover(ctx, cid, "ops"); err != nil {
		t.Fatalf("Takeover on unleased/manual card: %v", err)
	}
	if len(auth.calls) != 0 {
		t.Errorf("authority consulted for a ref-less card: %v", auth.calls)
	}
	events, err := h.Timeline(ctx, cid)
	if err != nil || len(events) != 1 || events[0].Kind != CardEventTakeover {
		t.Errorf("timeline = %+v err=%v, want exactly one takeover row", events, err)
	}
}

func TestHumanTakeoverMove_AuthorityFailureLeavesCardUnmoved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	b := bus.New(nil, nil)
	movedSub := subscribeRaw(t, b, TopicCardMoved.Name)
	auth := &fakeAuthority{held: true, holder: "emp-7", since: time.Now().UTC(), cancelErr: errors.New("episode kill failed")}
	h := newHumanTestSvc(t, s, HumanOptions{Bus: b, Authority: auth})
	cid := seedCard(t, s, boardID, colA, "card", WorkRef{Kind: "task", ID: "task-10"})

	err := h.TakeoverMove(ctx, cid, colA, colB, "ops")
	if err == nil {
		t.Fatal("TakeoverMove with failing authority: want error")
	}
	if !errors.Is(err, auth.cancelErr) {
		t.Errorf("authority error not wrapped: %v", err)
	}
	// Failure of the authority step aborts BEFORE the card mutation.
	if card, err := s.GetCard(ctx, cid); err != nil || card.ColumnID != colA {
		t.Fatalf("card moved despite authority failure: %+v err=%v", card, err)
	}
	assertNoMessage(t, movedSub)
	// Audit-before-effect still holds on the failure path.
	events, _ := h.Timeline(ctx, cid)
	if len(events) != 1 || events[0].Kind != CardEventTakeover {
		t.Errorf("failed takeover timeline = %+v, want the takeover audit row", events)
	}
}

func TestHumanTakeoverMove_BypassesLeaseAndStampsBlocked(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	if err := s.SetProjectionRule(ctx, ProjectionRule{BoardID: boardID, Kind: "task", FromState: "pending", ColumnID: colB}); err != nil {
		t.Fatalf("SetProjectionRule: %v", err)
	}
	b := bus.New(nil, nil)
	movedSub := subscribeRaw(t, b, TopicCardMoved.Name)
	auth := &fakeAuthority{} // lease NOT held — takeover still legal
	h := newHumanTestSvc(t, s, HumanOptions{Bus: b, Authority: auth})
	cid := seedCard(t, s, boardID, colA, "card", WorkRef{Kind: "task", ID: "task-11"})

	if err := h.TakeoverMove(ctx, cid, colA, colB, "ops"); err != nil {
		t.Fatalf("TakeoverMove: %v", err)
	}
	if card, err := s.GetCard(ctx, cid); err != nil || card.ColumnID != colB {
		t.Fatalf("card not moved by takeover: %+v err=%v", card, err)
	}
	var cm CardMoved
	m := mustReceiveLenient(movedSub)
	if m == nil {
		t.Fatal("no card.moved published")
	}
	if err := json.Unmarshal(m.Payload, &cm); err != nil {
		t.Fatalf("card.moved decode: %v", err)
	}
	if !cm.LeaseBlocked {
		t.Errorf("takeover move must stamp LeaseBlocked=true, got %+v", cm)
	}
	// A plain MoveCard under a live lease refuses the SAME move — pins
	// the takeover bypass against the refusal path.
	if err := h.MoveCard(ctx, cid, colB, colA, "ops"); err != nil {
		t.Errorf("manual return move blocked unexpectedly: %v", err)
	}
}

// ------------------------------------------------------------------
// C3 comment / timeline round-trip
// ------------------------------------------------------------------

func TestHumanCommentTimelineRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, _ := seedBoard(t, s)
	h := newHumanTestSvc(t, s, HumanOptions{})
	cid := seedCard(t, s, boardID, colA, "card", WorkRef{})

	if _, err := h.Comment(ctx, cid, "ops", "first note"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	echo, err := h.Comment(ctx, cid, "ops", "second note")
	if err != nil {
		t.Fatalf("Comment 2: %v", err)
	}
	if echo.ID == "" || echo.CreatedAt.IsZero() {
		t.Errorf("Comment echo = %+v, want filled ID + CreatedAt", echo)
	}
	// Comment on unknown card -> ErrUnknownCard, not a raw FK error.
	if _, err := h.Comment(ctx, "card-nope", "ops", "x"); !errors.As(err, new(ErrUnknownCard)) {
		t.Errorf("comment on unknown card: got %v, want ErrUnknownCard", err)
	}

	events, err := h.Timeline(ctx, cid)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	bodies := map[string]CardEvent{}
	for _, ev := range events {
		if ev.Kind != CardEventComment {
			t.Errorf("timeline row kind = %q, want comment", ev.Kind)
		}
		bodies[ev.Body] = ev
	}
	if len(bodies) != 2 {
		t.Fatalf("timeline = %+v, want both comments", events)
	}
	for _, body := range []string{"first note", "second note"} {
		ev, ok := bodies[body]
		if !ok {
			t.Fatalf("timeline missing %q", body)
		}
		if ev.CardID != cid || ev.Actor != "ops" {
			t.Errorf("timeline row for %q = %+v, want card+actor", body, ev)
		}
	}
	// Round-trip symmetry: the echoed second comment equals the stored
	// row (id + body).
	if got := bodies["second note"]; got.ID != echo.ID {
		t.Errorf("echoed event ID %q != stored %q", echo.ID, got.ID)
	}
	// Timeline requires a card id.
	if _, err := h.Timeline(ctx, ""); err == nil {
		t.Error("Timeline(\" \"): want error")
	}
}

// ------------------------------------------------------------------
// C4 assign
// ------------------------------------------------------------------

func TestHumanAssign(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, _ := seedBoard(t, s)
	b := bus.New(nil, nil)
	movedSub := subscribeRaw(t, b, TopicCardMoved.Name)
	changedSub := subscribeRaw(t, b, TopicBoardChanged.Name)
	h := newHumanTestSvc(t, s, HumanOptions{Bus: b})
	cid := seedCard(t, s, boardID, colA, "card", WorkRef{})

	if err := h.Assign(ctx, cid, "emp-42"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	card, err := s.GetCard(ctx, cid)
	if err != nil || card.EmployeeID != "emp-42" {
		t.Fatalf("card after assign = %+v err=%v", card, err)
	}
	events, err := h.Timeline(ctx, cid)
	if err != nil || len(events) != 1 || events[0].Kind != CardEventAssign {
		t.Errorf("timeline after assign = %+v err=%v", events, err)
	}
	// Assignment is not a column transition: board.changed yes,
	// card.moved never.
	var bc BoardChanged
	m := mustReceive(t, changedSub)
	if err := json.Unmarshal(m.Payload, &bc); err != nil || bc.BoardID != boardID {
		t.Errorf("board.changed after assign = %s err=%v", m.Payload, err)
	}
	assertNoMessage(t, movedSub)

	// Un-assign with empty id.
	if err := h.Assign(ctx, cid, ""); err != nil {
		t.Fatalf("Assign(unassign): %v", err)
	}
	if card, err := s.GetCard(ctx, cid); err != nil || card.EmployeeID != "" {
		t.Errorf("card after un-assign = %+v err=%v", card, err)
	}
	// Unknown card.
	if err := h.Assign(ctx, "card-nope", "emp"); !errors.As(err, new(ErrUnknownCard)) {
		t.Errorf("assign unknown card: got %v, want ErrUnknownCard", err)
	}
}

// ------------------------------------------------------------------
// C2 topic surface pins
// ------------------------------------------------------------------

func TestHumanTopics_C2Names(t *testing.T) {
	// C2 pins the exact strings — a rename silently orphans branch 05.
	if TopicCardMoved.Name != "kanban.card.moved" {
		t.Errorf("TopicCardMoved.Name = %q", TopicCardMoved.Name)
	}
	if TopicBoardChanged.Name != "kanban.board.changed" {
		t.Errorf("TopicBoardChanged.Name = %q", TopicBoardChanged.Name)
	}
	// CardMoved (C1) has no tags: JSON keys are the field names.
	raw, err := json.Marshal(CardMoved{CardID: "c", FromColumnID: "a", ToColumnID: "b", Actor: "ops", LeaseBlocked: true})
	if err != nil {
		t.Fatalf("marshal CardMoved: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"CardID", "FromColumnID", "ToColumnID", "Actor", "LeaseBlocked"} {
		if _, ok := got[key]; !ok {
			t.Errorf("CardMoved JSON missing %q in %s", key, raw)
		}
	}
	if len(got) != 5 {
		t.Errorf("CardMoved JSON has %d keys, want 5: %s", len(got), raw)
	}
}
