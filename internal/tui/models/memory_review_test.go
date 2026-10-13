package models

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fakeReviewClient implements ReviewRPCClient for testing.
type fakeReviewClient struct {
	connected bool

	queueResult *ReviewQueueResponse
	queueErr    error
	queueCalls  int
	lastQueue   *ReviewQueueParams

	promoteIDs  []string
	rejectIDs   []string
	reasons     map[string]string
	verdictErrs map[string]error
}

func newFakeReviewClient() *fakeReviewClient {
	return &fakeReviewClient{
		connected:   true,
		reasons:     map[string]string{},
		verdictErrs: map[string]error{},
	}
}

func (f *fakeReviewClient) ReviewQueue(p *ReviewQueueParams) (*ReviewQueueResponse, error) {
	f.queueCalls++
	f.lastQueue = p
	if f.queueErr != nil {
		return nil, f.queueErr
	}
	return f.queueResult, nil
}

func (f *fakeReviewClient) PromoteClaim(id, reason string) error {
	f.promoteIDs = append(f.promoteIDs, id)
	f.reasons[id] = reason
	if err := f.verdictErrs[id]; err != nil {
		return err
	}
	return nil
}

func (f *fakeReviewClient) RejectClaim(id, reason string) error {
	f.rejectIDs = append(f.rejectIDs, id)
	f.reasons[id] = reason
	if err := f.verdictErrs[id]; err != nil {
		return err
	}
	return nil
}

func (f *fakeReviewClient) IsConnected() bool { return f.connected }

// seedQueue installs two pending auto claims.
func seedQueue(f *fakeReviewClient) {
	f.queueResult = &ReviewQueueResponse{
		Total: 2,
		AutoClaims: []ReviewClaim{
			{
				ClaimID:    "claim-1",
				Content:    "the deploy pipeline fails on fridays",
				Type:       "claim",
				Category:   "ops",
				Confidence: 0.82,
				CreatedAt:  "2026-10-10T08:00:00Z",
			},
			{
				ClaimID:    "claim-2",
				Content:    "user prefers terse replies",
				Type:       "claim",
				Category:   "preference",
				Confidence: 0.45,
				CreatedAt:  "2026-10-12T08:00:00Z",
			},
		},
	}
}

// keyFor builds the KeyPressMsg for a single rune key.
func reviewKey(k string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func TestReviewModal_FetchPopulatesTable(t *testing.T) {
	fake := newFakeReviewClient()
	seedQueue(fake)
	m := NewMemoryReviewModal(fake)

	m.SetSize(100, 40)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected Init to return a fetch command")
	}
	msg := cmd()
	upd, ok := msg.(ReviewQueueMsg)
	if !ok {
		t.Fatalf("expected ReviewQueueMsg, got %T", msg)
	}
	m.Update(upd)

	if len(m.items) != 2 {
		t.Fatalf("expected 2 claims in model, got %d", len(m.items))
	}
	rows := m.table.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 table rows, got %d", len(rows))
	}
	for _, row := range rows {
		if len(row) != reviewColumnCount {
			t.Fatalf("row has %d cells, want %d (tableutil.SetRows normalization)", len(row), reviewColumnCount)
		}
	}
	if got := rows[0][0]; got != "claim-1" {
		t.Errorf("row 0 id = %q, want claim-1", got)
	}
	if !strings.Contains(rows[0][4], "deploy pipeline") {
		t.Errorf("row 0 preview %q missing content", rows[0][4])
	}
}

// The table must be sized on both axes: a width-0 viewport renders no body
// rows even when rows are set (root AGENTS.md table invariant).
func TestReviewModal_TableSizedAtZeroViewport(t *testing.T) {
	fake := newFakeReviewClient()
	seedQueue(fake)
	m := NewMemoryReviewModal(fake)
	m.SetSize(100, 40)
	m.Update(ReviewQueueMsg{Items: fake.queueResult.AutoClaims, Total: 2})

	view := m.View()
	if !strings.Contains(view, "claim-1") {
		t.Error("width-0 table rendered no rows — tableutil.Size not applied before View")
	}
	if !strings.Contains(view, "claim-2") {
		t.Error("row 2 missing from render")
	}
	if m.table.Width() <= 0 {
		t.Errorf("table width = %d, want > 0", m.table.Width())
	}
}

func TestReviewModal_PromoteKey(t *testing.T) {
	fake := newFakeReviewClient()
	seedQueue(fake)
	m := NewMemoryReviewModal(fake)
	m.SetSize(100, 40)
	m.Update(ReviewQueueMsg{Items: fake.queueResult.AutoClaims, Total: 2})

	cmd := m.Update(reviewKey("p"))
	if cmd == nil {
		t.Fatal("p produced no command")
	}
	// The verdict cmd runs the RPC, then the model refreshes; the refreshed
	// queue no longer holds claim-1 (daemon-side final state).
	fake.queueResult.AutoClaims = fake.queueResult.AutoClaims[1:]
	fake.queueResult.Total = 1
	m.Update(cmd())
	if fetch := m.Update(ReviewQueueMsg{Items: fake.queueResult.AutoClaims, Total: 1}); fetch != nil {
		m.Update(fetch())
	}

	if len(fake.promoteIDs) != 1 || fake.promoteIDs[0] != "claim-1" {
		t.Fatalf("promote called with %v, want [claim-1]", fake.promoteIDs)
	}
	// Refresh drops the row and keeps the cursor stable.
	if len(m.items) != 1 {
		t.Fatalf("items after promote = %d, want 1", len(m.items))
	}
	if m.table.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0 (stable)", m.table.Cursor())
	}
	if m.table.Rows()[0][0] != "claim-2" {
		t.Errorf("remaining row = %q, want claim-2", m.table.Rows()[0][0])
	}
}

func TestReviewModal_RejectKey(t *testing.T) {
	fake := newFakeReviewClient()
	seedQueue(fake)
	m := NewMemoryReviewModal(fake)
	m.SetSize(100, 40)
	m.Update(ReviewQueueMsg{Items: fake.queueResult.AutoClaims, Total: 2})

	// Move cursor to the second row, then reject it.
	m.Update(reviewKey("j"))
	cmd := m.Update(reviewKey("r"))
	if cmd == nil {
		t.Fatal("r produced no command")
	}
	// The daemon drops claim-2 after the verdict.
	fake.queueResult.AutoClaims = fake.queueResult.AutoClaims[:1]
	fake.queueResult.Total = 1
	m.Update(cmd())
	if fetch := m.Update(ReviewQueueMsg{Items: fake.queueResult.AutoClaims, Total: 1}); fetch != nil {
		m.Update(fetch())
	}

	if len(fake.rejectIDs) != 1 || fake.rejectIDs[0] != "claim-2" {
		t.Fatalf("reject called with %v, want [claim-2]", fake.rejectIDs)
	}
	if len(m.items) != 1 {
		t.Fatalf("items after reject = %d, want 1", len(m.items))
	}
	// Cursor stayed on row 0 but selection clamps to the remaining claim.
	if m.table.Cursor() > 0 {
		t.Errorf("cursor = %d, want 0 after drop", m.table.Cursor())
	}
	if got := m.items[0].ClaimID; got != "claim-1" {
		t.Errorf("remaining item = %q, want claim-1", got)
	}
}

func TestReviewModal_VerdictErrorKeepsRow(t *testing.T) {
	fake := newFakeReviewClient()
	seedQueue(fake)
	fake.verdictErrs["claim-1"] = errors.New("claim claim-1 is not auto (status=\"promoted\")")
	m := NewMemoryReviewModal(fake)
	m.SetSize(100, 40)
	m.Update(ReviewQueueMsg{Items: fake.queueResult.AutoClaims, Total: 2})

	cmd := m.Update(reviewKey("p"))
	m.Update(cmd())

	// Failed verdict: the row stays and the error is visible.
	if len(m.items) != 2 {
		t.Fatalf("items after failed verdict = %d, want 2 (row kept)", len(m.items))
	}
	if m.err == nil || !strings.Contains(m.err.Error(), "not auto") {
		t.Errorf("err = %v, want the wrapped rpc error", m.err)
	}
	if !strings.Contains(m.View(), "not auto") {
		t.Error("error not surfaced in the view")
	}
}

func TestReviewModal_DetailToggle(t *testing.T) {
	fake := newFakeReviewClient()
	seedQueue(fake)
	m := NewMemoryReviewModal(fake)
	m.SetSize(100, 40)
	m.Update(ReviewQueueMsg{Items: fake.queueResult.AutoClaims, Total: 2})

	if m.selected != nil {
		t.Fatal("no detail expected before enter")
	}
	m.Update(reviewKeyFor(KeyEnter))
	if m.selected == nil || m.selected.ClaimID != "claim-1" {
		t.Fatalf("enter did not select claim-1: %+v", m.selected)
	}
	if !strings.Contains(m.View(), "user wrote") && !strings.Contains(m.View(), "the deploy pipeline fails on fridays") {
		t.Error("detail view missing full content")
	}
	m.Update(reviewKeyFor(KeyEsc))
	if m.selected != nil {
		t.Error("esc did not close the detail panel")
	}
	// Second esc closes the modal itself.
	m.Update(reviewKeyFor(KeyEsc))
	if m.visible {
		t.Error("esc did not close the modal")
	}
}

func TestReviewModal_EmptyQueue(t *testing.T) {
	fake := newFakeReviewClient()
	fake.queueResult = &ReviewQueueResponse{AutoClaims: []ReviewClaim{}, Total: 0}
	m := NewMemoryReviewModal(fake)
	m.SetSize(100, 40)
	m.Update(ReviewQueueMsg{Items: nil, Total: 0})

	view := m.View()
	if !strings.Contains(view, "no claims pending") {
		t.Errorf("empty state text missing: %q", view)
	}
	// Verdict keys on an empty queue are no-ops (no command, no panic).
	if cmd := m.Update(reviewKey("p")); cmd != nil {
		t.Error("p on empty queue returned a command")
	}
	if cmd := m.Update(reviewKey("r")); cmd != nil {
		t.Error("r on empty queue returned a command")
	}
}

func TestReviewModal_QueueErrorSurfaces(t *testing.T) {
	fake := newFakeReviewClient()
	fake.queueErr = errors.New("memory not initialized")
	m := NewMemoryReviewModal(fake)
	m.SetSize(100, 40)
	m.Update(ReviewQueueMsg{Err: fake.queueErr})

	if !strings.Contains(m.View(), "memory not initialized") {
		t.Error("queue fetch error not rendered")
	}
}

// reviewKeyFor builds a KeyPressMsg for named keys (enter/esc).
func reviewKeyFor(k string) tea.KeyPressMsg {
	codes := map[string]rune{KeyEnter: 13, KeyEsc: 27}
	return tea.KeyPressMsg{Code: codes[k], Text: k}
}

func TestReviewModal_LowercaseView(t *testing.T) {
	fake := newFakeReviewClient()
	seedQueue(fake)
	m := NewMemoryReviewModal(fake)
	m.SetSize(100, 40)
	m.Update(ReviewQueueMsg{Items: fake.queueResult.AutoClaims, Total: 2})

	view := m.View()
	// Strip the seeded content lines (claims may legitimately contain upper
	// case); the chrome must be lowercase.
	for _, banned := range []string{"Pending", "Review Queue", "Promote", "Reject", "Detail", "Close"} {
		if strings.Contains(view, banned) {
			t.Errorf("view contains non-lowercase chrome %q", banned)
		}
	}
}
