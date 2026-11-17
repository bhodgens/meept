package rpc

// shadow_route.go exposes the read-only llm.route.shadow RPC (Contract C6,
// sovereign-router-parity leaf 04, R4): "which model would you pick for this
// prompt, and what would it cost" — answered with NO upstream call, no
// llm_calls row, no budget consumption, no alias-health or sticky-pin
// mutation. Pattern: intake_status.go (a frozen-JSON view struct whose field
// names ARE the cross-surface contract, a snapshot-fn handler the daemon
// wires, nil-safe registration that never answers "method not found").
//
// Read-only posture: the daemon passes a resolver snapshot closure; the
// handler holds no resolver reference of its own and cannot mutate anything.
// Owner-trusted transport (Unix socket, no token auth) per the repo's
// Unix-RPC posture — identical to intake.status.

import (
	"context"
	"encoding/json"

	"github.com/caimlas/meept/internal/llm"
)

// ShadowRouteRequest is the llm.route.shadow wire request. Empty alias
// previews the default model; ModelRef (a "provider/model" ref) takes
// precedence over Alias when both are set.
type ShadowRouteRequest struct {
	Alias    string `json:"alias,omitempty"`
	Prompt   string `json:"prompt"`
	ModelRef string `json:"model_ref,omitempty"`
}

// ShadowRouteView is the llm.route.shadow response. Field names are FROZEN
// (TUI/GUI/CLI parity, mirroring the intake.status precedent — the e2e gate
// asserts model_id / provider_id / candidates[].selected /
// candidates[].skip_reason verbatim). Cost and token figures are the LOCAL
// estimate; they are returned here and never persisted (the
// routing_decisions table gains NO columns — the estimate lives in the
// response only).
type ShadowRouteView struct {
	ModelID    string                `json:"model_id"`
	ProviderID string                `json:"provider_id"`
	Alias      string                `json:"alias"`
	Reason     string                `json:"reason"`
	EstInput   int                   `json:"est_input_tokens"`
	EstOutput  int                   `json:"est_output_tokens"`
	EstCostUSD float64               `json:"est_cost_usd"`
	Quality    int                   `json:"quality"`
	Candidates []ShadowCandidateView `json:"candidates"`
}

// ShadowCandidateView is one candidate row of the frozen wire contract.
type ShadowCandidateView struct {
	ModelID    string  `json:"model_id"`
	ProviderID string  `json:"provider_id"`
	Quality    int     `json:"quality"`
	EstCostUSD float64 `json:"est_cost_usd"`
	Selected   bool    `json:"selected"`
	SkipReason string  `json:"skip_reason,omitempty"`
}

// ShadowRouteSeam is the daemon-supplied snapshot closure. It receives the
// decoded request and returns the view; the handler itself never touches the
// resolver. Nil seam = every call answers the disabled envelope (never
// "method not found" — a daemon that never wired a resolver still answers).
type ShadowRouteSeam func(ctx context.Context, req ShadowRouteRequest) (*ShadowRouteView, error)

// ShadowRouteHandler serves llm.route.shadow.
type ShadowRouteHandler struct {
	seam ShadowRouteSeam
}

// NewShadowRouteHandler builds the handler. A nil seam is allowed: every
// call renders the disabled envelope.
func NewShadowRouteHandler(seam ShadowRouteSeam) *ShadowRouteHandler {
	return &ShadowRouteHandler{seam: seam}
}

// SetSeam re-wires the resolver snapshot source. Nil-guarded per the repo
// setter convention.
func (h *ShadowRouteHandler) SetSeam(seam ShadowRouteSeam) {
	if h != nil && seam != nil {
		h.seam = seam
	}
}

// RegisterShadowRouteMethods registers llm.route.shadow on server. Safe to
// call with a nil handler (registers the disabled envelope).
func (h *ShadowRouteHandler) RegisterShadowRouteMethods(server *Server) {
	server.RegisterHandler("llm.route.shadow", h.handleShadowRoute)
}

// handleShadowRoute answers llm.route.shadow. Read-only: it decodes, hands
// the request to the seam, and maps the result onto the frozen view. A nil
// handler or seam answers the empty envelope — never an error, never
// "method not found".
func (h *ShadowRouteHandler) handleShadowRoute(ctx context.Context, params json.RawMessage) (any, error) {
	var req ShadowRouteRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
	}
	if h == nil || h.seam == nil {
		return ShadowRouteView{
			Alias:      req.Alias,
			Reason:     "shadow_unavailable",
			Candidates: []ShadowCandidateView{},
		}, nil
	}
	view, err := h.seam(ctx, req)
	if err != nil {
		return nil, err
	}
	if view == nil {
		return ShadowRouteView{
			Alias:      req.Alias,
			Reason:     "shadow_unavailable",
			Candidates: []ShadowCandidateView{},
		}, nil
	}
	if view.Candidates == nil {
		view.Candidates = []ShadowCandidateView{}
	}
	return *view, nil
}

// ShadowRouteViewFromDecision maps the resolver's typed decision onto the
// frozen wire view. Field-by-field; the JSON tags do the freezing.
func ShadowRouteViewFromDecision(dec *llm.ShadowDecision) *ShadowRouteView {
	if dec == nil {
		return &ShadowRouteView{Candidates: []ShadowCandidateView{}}
	}
	out := &ShadowRouteView{
		ModelID:    dec.ModelID,
		ProviderID: dec.ProviderID,
		Alias:      dec.Alias,
		Reason:     dec.Reason,
		EstInput:   dec.EstInput,
		EstOutput:  dec.EstOutput,
		EstCostUSD: dec.EstCostUSD,
		Quality:    dec.Quality,
		Candidates: make([]ShadowCandidateView, 0, len(dec.Candidates)),
	}
	for _, c := range dec.Candidates {
		out.Candidates = append(out.Candidates, ShadowCandidateView{
			ModelID:    c.ModelID,
			ProviderID: c.ProviderID,
			Quality:    c.Quality,
			EstCostUSD: c.EstCostUSD,
			Selected:   c.Selected,
			SkipReason: c.SkipReason,
		})
	}
	return out
}
