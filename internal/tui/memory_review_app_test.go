package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/models"
)

// TestPaletteMemoryReviewEntry: the command palette declares the
// `memory review` entry (leaf T03-L2) with the configurable key.
func TestPaletteMemoryReviewEntry(t *testing.T) {
	items := CommandPaletteModal(DefaultStyles(), DefaultClientConfig())
	found := false
	for _, it := range items.items {
		if it.Label == "memory review" {
			found = true
			if it.Key != "v" {
				t.Errorf("memory review palette key = %q, want v (default)", it.Key)
			}
			if strings.Contains(it.Description, "Review") || strings.ToUpper(it.Description) == it.Description {
				if strings.ToLower(it.Description) != it.Description {
					t.Errorf("palette description not lowercase: %q", it.Description)
				}
			}
		}
	}
	if !found {
		t.Fatal("command palette has no `memory review` entry")
	}
}

// TestPaletteOpensMemoryReviewModal: pressing the palette key opens the
// review modal and issues a queue fetch (the dead-socket client makes the
// fetch fail asynchronously; the modal must still be visible).
func TestPaletteOpensMemoryReviewModal(t *testing.T) {
	app := createTestApp()

	app.activeModal = ModalCommandPalette
	app.commandPalette.Show()

	msg := tea.KeyPressMsg{Code: 'v', Text: "v"}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.activeModal != ModalMemoryReview {
		t.Fatalf("expected ModalMemoryReview, got %v", newApp.activeModal)
	}
	if newApp.memoryReview == nil {
		t.Fatal("memory review modal not wired")
	}
	if !newApp.memoryReview.IsVisible() {
		t.Error("memory review modal not visible after palette selection")
	}
}

// TestMemoryReviewModalEscCloses: esc closes the modal and the app returns
// to modal-none (no lingering overlay).
func TestMemoryReviewModalEscCloses(t *testing.T) {
	app := createTestApp()
	app.memoryReview = models.NewMemoryReviewModal(app.rpc)
	app.activeModal = ModalMemoryReview
	app.memoryReview.Show()

	esc := tea.KeyPressMsg{Code: 27, Text: "esc"}
	_, _ = app.handleModalKey(esc)

	if app.memoryReview.IsVisible() {
		t.Error("esc did not hide the review modal")
	}
	if app.activeModal != ModalNone {
		t.Errorf("activeModal = %v, want ModalNone", app.activeModal)
	}
}

// TestStatusBarShowsPendingClaims: a ReviewQueueMsg with items flips the
// status-bar indicator; an empty queue hides it.
func TestStatusBarShowsPendingClaims(t *testing.T) {
	app := createTestApp()

	newModel, _ := app.Update(models.ReviewQueueMsg{
		Items: []models.ReviewClaim{{ClaimID: "c1", Content: "x", Confidence: 0.5}},
		Total: 1,
	})
	newApp := newModel.(*App)
	if newApp.memoryReviewPending != 1 {
		t.Fatalf("memoryReviewPending = %d, want 1", newApp.memoryReviewPending)
	}
	bar := newApp.renderStatusBar()
	if !strings.Contains(bar, "claims to review") {
		t.Errorf("status bar missing pending-claims indicator: %q", bar)
	}

	var model tea.Model
	model, _ = app.Update(models.ReviewQueueMsg{Items: nil, Total: 0})
	newApp = model.(*App)
	if newApp.memoryReviewPending != 0 {
		t.Fatalf("memoryReviewPending = %d, want 0 after empty queue", newApp.memoryReviewPending)
	}
	bar = newApp.renderStatusBar()
	if strings.Contains(bar, "claims to review") {
		t.Error("status bar shows indicator with an empty queue")
	}
}

// TestReviewVerdictStatusMessage: a successful verdict sets a lowercase
// status message (parity with the pending-changes flow).
func TestReviewVerdictStatusMessage(t *testing.T) {
	app := createTestApp()

	newModel, _ := app.Update(models.ReviewVerdictMsg{ClaimID: "claim-9", Verdict: "promote"})
	newApp := newModel.(*App)
	if newApp.statusMessage == "" {
		t.Fatal("no status message after verdict")
	}
	if strings.ToLower(newApp.statusMessage) != newApp.statusMessage {
		t.Errorf("status message not lowercase: %q", newApp.statusMessage)
	}
	if !strings.Contains(newApp.statusMessage, "promoted") {
		t.Errorf("status message %q missing verdict", newApp.statusMessage)
	}
}

// TestReviewRPCClientSatisfiesInterface: the concrete RPCClient implements
// models.ReviewRPCClient (compile-time guard for the modal wiring).
func TestReviewRPCClientSatisfiesInterface(t *testing.T) {
	var _ models.ReviewRPCClient = (*RPCClient)(nil)
}
