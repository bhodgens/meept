package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/memory"
)

// EpistemicHandler provides native RPC methods for epistemic memory management.
// It calls Manager directly so that CLI, TUI, and HTTP clients can store and
// manage claims, decisions, predictions, and their lifecycle.
type EpistemicHandler struct {
	manager *memory.Manager
}

// NewEpistemicHandler creates a new handler. If manager is nil the registered
// methods return "memory not initialized" errors.
func NewEpistemicHandler(manager *memory.Manager) *EpistemicHandler {
	return &EpistemicHandler{manager: manager}
}

// managerOrErr returns the manager or an error if it is nil.
func (h *EpistemicHandler) managerOrErr() (*memory.Manager, error) {
	if h.manager == nil || !h.manager.IsInitialized() {
		return nil, fmt.Errorf("memory not initialized")
	}
	return h.manager, nil
}

// RegisterEpistemicHandlers registers epistemic memory RPC methods on the server.
func (h *EpistemicHandler) RegisterEpistemicHandlers(server *Server) {
	server.RegisterHandler("memory.retainClaim", h.handleRetainClaim)
	server.RegisterHandler("memory.retainDecision", h.handleRetainDecision)
	server.RegisterHandler("memory.retainPrediction", h.handleRetainPrediction)
	server.RegisterHandler("memory.markSuperseded", h.handleMarkSuperseded)
	server.RegisterHandler("memory.markResolved", h.handleMarkResolved)
	server.RegisterHandler("memory.recordReview", h.handleRecordReview)
	server.RegisterHandler("memory.promoteClaim", h.handlePromoteClaim)
	server.RegisterHandler("memory.rejectClaim", h.handleRejectClaim)
	server.RegisterHandler("memory.listAutoClaims", h.handleListAutoClaims)
	server.RegisterHandler("memory.listPendingReviews", h.handleListPendingReviews)
	server.RegisterHandler("memory.findCanonical", h.handleFindCanonical)
	server.RegisterHandler("memory.reviewQueue", h.handleReviewQueue)
}

type retainClaimParams struct {
	Text       string   `json:"text"`
	Premises   []string `json:"premises,omitempty"`
	Source     string   `json:"source,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

func (h *EpistemicHandler) handleRetainClaim(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	var p retainClaimParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	id, err := mgr.StoreClaim(ctx, memory.Claim{
		Text:       p.Text,
		Premises:   p.Premises,
		Source:     p.Source,
		Confidence: p.Confidence,
		Tags:       p.Tags,
		Status:     memory.ClaimStatusConfirmed,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id}, nil
}

type retainDecisionParams struct {
	Call            string   `json:"call"`
	Alternatives    []string `json:"alternatives,omitempty"`
	ExpectedOutcome string   `json:"expected_outcome,omitempty"`
	ReviewAt        *time.Time `json:"review_at,omitempty"`
	Premises        []string `json:"premises,omitempty"`
}

func (h *EpistemicHandler) handleRetainDecision(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	var p retainDecisionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	id, err := mgr.StoreDecision(ctx, memory.Decision{
		Call:            p.Call,
		Alternatives:    p.Alternatives,
		ExpectedOutcome: p.ExpectedOutcome,
		ReviewAt:        p.ReviewAt,
		Premises:        p.Premises,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id}, nil
}

type retainPredictionParams struct {
	Forecast        string    `json:"forecast"`
	Horizon         time.Time `json:"horizon"`
	RelatedDecision string    `json:"related_decision,omitempty"`
}

func (h *EpistemicHandler) handleRetainPrediction(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	var p retainPredictionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	id, err := mgr.StorePrediction(ctx, memory.Prediction{
		Forecast:        p.Forecast,
		Horizon:         p.Horizon,
		RelatedDecision: p.RelatedDecision,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id}, nil
}

type markSupersededParams struct {
	OldID string `json:"old_id"`
	NewID string `json:"new_id"`
}

func (h *EpistemicHandler) handleMarkSuperseded(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	var p markSupersededParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	redirected, auditID, err := mgr.MarkSuperseded(ctx, p.OldID, p.NewID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"redirected_edges": redirected,
		"audit_id":         auditID,
	}, nil
}

type markResolvedParams struct {
	PredictionID string `json:"prediction_id"`
	Outcome      string `json:"outcome"`
}

func (h *EpistemicHandler) handleMarkResolved(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	var p markResolvedParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	id, err := mgr.MarkResolved(ctx, p.PredictionID, p.Outcome)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id}, nil
}

type recordReviewParams struct {
	DecisionID     string `json:"decision_id"`
	ActualOutcome  string `json:"actual_outcome"`
}

func (h *EpistemicHandler) handleRecordReview(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	var p recordReviewParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	score, auditID, err := mgr.RecordReview(ctx, p.DecisionID, p.ActualOutcome)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"overlap_score": score,
		"audit_id":      auditID,
	}, nil
}

type claimIDParams struct {
	ID string `json:"id"`
}

func (h *EpistemicHandler) handlePromoteClaim(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	var p claimIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if err := mgr.PromoteClaim(ctx, p.ID); err != nil {
		return nil, err
	}
	return map[string]any{"status": "promoted"}, nil
}

func (h *EpistemicHandler) handleRejectClaim(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	var p claimIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if err := mgr.RejectClaim(ctx, p.ID); err != nil {
		return nil, err
	}
	return map[string]any{"status": "rejected"}, nil
}

type listAutoClaimsParams struct {
	SinceHours int `json:"since_hours,omitempty"`
	Limit      int `json:"limit,omitempty"`
}

func (h *EpistemicHandler) handleListAutoClaims(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	var p listAutoClaimsParams
	_ = json.Unmarshal(params, &p)
	since := time.Now().Add(-time.Duration(p.SinceHours) * time.Hour)
	if p.SinceHours <= 0 {
		since = time.Time{}
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	claims, err := mgr.ListAutoClaims(ctx, since, limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"claims": claims}, nil
}

func (h *EpistemicHandler) handleListPendingReviews(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	before := time.Now()
	decisions, predictions, err := mgr.ListPendingReviews(ctx, before)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"decisions":   decisions,
		"predictions": predictions,
	}, nil
}

type findCanonicalParams struct {
	Topic string `json:"topic"`
}

func (h *EpistemicHandler) handleFindCanonical(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	var p findCanonicalParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	mem, err := mgr.FindCanonicalFor(ctx, p.Topic)
	if err != nil {
		return nil, err
	}
	if mem == nil {
		return map[string]any{"found": false}, nil
	}
	return map[string]any{"found": true, "memory": mem}, nil
}

func (h *EpistemicHandler) handleReviewQueue(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	var p listAutoClaimsParams
	_ = json.Unmarshal(params, &p)
	since := time.Now().Add(-time.Duration(p.SinceHours) * time.Hour)
	if p.SinceHours <= 0 {
		since = time.Time{}
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	autoClaims, err := mgr.ListAutoClaims(ctx, since, limit)
	if err != nil {
		return nil, err
	}
	decisions, predictions, err := mgr.ListPendingReviews(ctx, time.Now())
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"auto_claims":        autoClaims,
		"pending_decisions":  decisions,
		"pending_predictions": predictions,
	}, nil
}
