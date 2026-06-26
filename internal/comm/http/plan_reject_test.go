package http

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/services"
)

// testPlanSvc holds a PlanService along with the underlying manager and store
// so tests can call manager-only methods (e.g. SubmitPlan) and inspect
// signoffs directly.
type testPlanSvc struct {
	*services.PlanService
	mgr   *plan.PlanManager
	store plan.PlanStore
}

// newTestPlanService creates a PlanService backed by a temporary SQLite store
// for use in HTTP handler tests.
func newTestPlanService(t *testing.T) *testPlanSvc {
	t.Helper()
	logger := slog.Default()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := plan.NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := config.PlansConfig{
		Mode: "threshold",
		Threshold: config.PlansThresholdConfig{
			MinSteps:           3,
			ComplexityKeywords: []string{"refactor", "migrate", "implement"},
			AlwaysPlanIntents:  []string{"plan", "implement", "build"},
		},
		Storage: config.PlansStorageConfig{
			DefaultPath:      dir,
			FilenameTemplate: "{{slug}}.md",
		},
		Approval: config.PlansApprovalConfig{
			RequireApproval: true,
			AllowRevision:   true,
			MaxRevisions:    3,
		},
		Confirmation: config.PlansConfirmationConfig{
			RequireSignoff: true,
		},
	}

	mgr := plan.NewPlanManager(store, nil, cfg, nil, logger)
	return &testPlanSvc{
		PlanService: services.NewPlanService(mgr, store),
		mgr:         mgr,
		store:       store,
	}
}

// createPendingPlan creates a plan and transitions it to pending_approval so
// it can be rejected.
func createPendingPlan(t *testing.T, tps *testPlanSvc) *plan.Plan {
	t.Helper()
	ctx := t.Context()
	p, err := tps.Create(ctx, services.CreatePlanRequest{
		Title:    "test plan",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if err := tps.mgr.SubmitPlan(ctx, p.ID); err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	return p
}

// TestPlanRejectApproverIDFallback verifies that the reject handler accepts
// the approver_id field and falls back to the by field when approver_id is
// empty (mirroring the approve handler).
func TestPlanRejectApproverIDFallback(t *testing.T) {
	tps := newTestPlanService(t)
	srv := NewServer(ServerConfig{}, nil, nil, nil,
		&services.ServiceRegistry{Plan: tps.PlanService}, nil)

	p := createPendingPlan(t, tps)

	// Send reject with approver_id (no by field).
	body, _ := json.Marshal(map[string]string{
		"approver_id": "agent-007",
		"reason":      "needs more work",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plans/"+p.ID+"/reject", bytes.NewReader(body))
	req.SetPathValue("id", p.ID)
	w := httptest.NewRecorder()

	srv.handlePlanReject(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusOK, w.Body.String())
	}

	// Verify the plan was rejected (state = cancelled).
	ctx := t.Context()
	updated, err := tps.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if updated.State != plan.StateCancelled {
		t.Errorf("plan state = %s, want %s", updated.State, plan.StateCancelled)
	}

	// Verify the signoff recorded the approver_id value.
	signoffs, err := tps.store.GetSignoffs(ctx, p.ID)
	if err != nil {
		t.Fatalf("get signoffs: %v", err)
	}
	if len(signoffs) != 1 {
		t.Fatalf("expected 1 signoff, got %d", len(signoffs))
	}
	if signoffs[0].By != "agent-007" {
		t.Errorf("signoff by = %q, want %q", signoffs[0].By, "agent-007")
	}
}

// TestPlanRejectByFallback verifies that the by field still works when
// approver_id is not provided (backward compatibility).
func TestPlanRejectByFallback(t *testing.T) {
	tps := newTestPlanService(t)
	srv := NewServer(ServerConfig{}, nil, nil, nil,
		&services.ServiceRegistry{Plan: tps.PlanService}, nil)

	p := createPendingPlan(t, tps)

	body, _ := json.Marshal(map[string]string{
		"by":     "operator-1",
		"reason": "out of scope",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plans/"+p.ID+"/reject", bytes.NewReader(body))
	req.SetPathValue("id", p.ID)
	w := httptest.NewRecorder()

	srv.handlePlanReject(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusOK, w.Body.String())
	}

	ctx := t.Context()
	signoffs, err := tps.store.GetSignoffs(ctx, p.ID)
	if err != nil {
		t.Fatalf("get signoffs: %v", err)
	}
	if len(signoffs) != 1 {
		t.Fatalf("expected 1 signoff, got %d", len(signoffs))
	}
	if signoffs[0].By != "operator-1" {
		t.Errorf("signoff by = %q, want %q", signoffs[0].By, "operator-1")
	}
}
