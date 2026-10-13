package tui

import (
	"strings"
	"testing"
	"time"
)

// Leaf T03-L2 end-to-end flow tests: the review modal driven through the
// real RPC client against the stub daemon (headlessProgram + rpcStub).

// newHeadlessAppMemoryReview seeds a stubbed memory.reviewQueue + verdict
// methods so the review modal flow runs end-to-end through the real RPC
// client.
func newHeadlessAppMemoryReview(t *testing.T, w, h int) *headlessProgram {
	t.Helper()
	hp := newHeadlessApp(t, w, h)
	old := time.Now().Add(-40 * 24 * time.Hour).Format(time.RFC3339)
	setJSONStub(t, hp, "memory.reviewQueue", map[string]any{
		"auto_claims": []map[string]any{
			{"claim_id": "claim-1", "content": "the deploy pipeline fails on fridays", "type": "claim", "category": "ops", "confidence": 0.82, "created_at": old},
			{"claim_id": "claim-2", "content": "user prefers terse replies", "type": "claim", "category": "preference", "confidence": 0.45, "created_at": old},
		},
		"total": 2,
	})
	setJSONStub(t, hp, "memory.promoteClaim", map[string]any{"status": "promoted"})
	setJSONStub(t, hp, "memory.rejectClaim", map[string]any{"status": "rejected"})
	return hp
}

// TestHeadlessReviewModalFlow: palette -> v opens the modal over the stubbed
// queue; the table renders both claims and the status bar shows the count.
func TestHeadlessReviewModalFlow(t *testing.T) {
	hp := newHeadlessAppMemoryReview(t, goldenWidth, goldenHeight)
	switchView(t, hp, "v")
	// The queue fetch resolves asynchronously; the settleAsync barrier is
	// what guarantees its msg landed before the final WindowSizeMsg (whose
	// SetSize repopulates the table from the now-populated cache).
	settleAsync()
	hp.settle(goldenWidth, goldenHeight)

	app := hp.finish(goldenWidth, goldenHeight)
	if app.activeModal != ModalMemoryReview {
		t.Fatalf("activeModal = %v, want ModalMemoryReview", app.activeModal)
	}
	view := app.memoryReview.View()
	for _, want := range []string{"claim-1", "claim-2", "deploy"} {
		// The preview column is MaxWidth-truncated at the fitted column
		// width, so assert on text inside the truncation window only.
		if !strings.Contains(view, want) {
			t.Errorf("review view missing %q", want)
		}
	}
	if app.memoryReviewPending != 2 {
		t.Errorf("memoryReviewPending = %d, want 2", app.memoryReviewPending)
	}
}

// TestHeadlessReviewModalPromote: pressing p in the modal promotes the
// selected claim through memory.promoteClaim and the table refresh drops
// the row (stub flips to one claim after the verdict).
func TestHeadlessReviewModalPromote(t *testing.T) {
	hp := newHeadlessAppMemoryReview(t, goldenWidth, goldenHeight)
	switchView(t, hp, "v")
	settleAsync()
	hp.settle(goldenWidth, goldenHeight)

	// After the verdict the daemon-side queue holds only claim-2.
	setJSONStub(t, hp, "memory.reviewQueue", map[string]any{
		"auto_claims": []map[string]any{
			{"claim_id": "claim-2", "content": "user prefers terse replies", "type": "claim", "category": "preference", "confidence": 0.45},
		},
		"total": 1,
	})

	hp.send(keyPress('p', 0))
	settleAsync()
	hp.settle(goldenWidth, goldenHeight)

	app := hp.finish(goldenWidth, goldenHeight)
	view := app.memoryReview.View()
	if strings.Contains(view, "claim-1") {
		t.Error("promoted claim still rendered after refresh")
	}
	if !strings.Contains(view, "claim-2") {
		t.Error("remaining claim missing after refresh")
	}
	if app.memoryReviewPending != 1 {
		t.Errorf("memoryReviewPending = %d, want 1 after verdict", app.memoryReviewPending)
	}
}

// TestHeadlessReviewModalEscClose: esc closes the modal (overlay gone) and
// the app returns to the base view.
func TestHeadlessReviewModalEscClose(t *testing.T) {
	hp := newHeadlessAppMemoryReview(t, goldenWidth, goldenHeight)
	switchView(t, hp, "v")
	hp.settle(goldenWidth, goldenHeight)

	hp.send(keyPress(27, 0))
	hp.settle(goldenWidth, goldenHeight)

	app := hp.finish(goldenWidth, goldenHeight)
	if app.activeModal != ModalNone {
		t.Errorf("activeModal = %v, want ModalNone after esc", app.activeModal)
	}
	if app.memoryReview.IsVisible() {
		t.Error("review modal still visible after esc")
	}
}
