package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/effects"
)

// EffectsRPCHandler provides native RPC methods over the external-effect
// idempotency ledger: effects.list (visibility) and effects.reconcile
// (human reconciliation). It registers DIRECT RegisterHandler closures on
// the rpc server — never a bus proxy (AGENTS.md live-responder rule: a
// proxy with no responder blocks the caller for the full timeout).
type EffectsRPCHandler struct {
	ledger effects.Ledger
}

// NewEffectsRPCHandler creates an effects handler over the ledger. A nil
// ledger yields "effects service not available" errors for reconcile;
// effects.list still answers with an empty list (ParkStore-degradation
// parity: visibility never hard-fails on an unwired ledger).
func NewEffectsRPCHandler(ledger effects.Ledger) *EffectsRPCHandler {
	return &EffectsRPCHandler{ledger: ledger}
}

// RegisterEffectsMethods registers the effects RPC methods on the server.
func (h *EffectsRPCHandler) RegisterEffectsMethods(server *Server) {
	server.RegisterHandler("effects.list", h.handleList)
	server.RegisterHandler("effects.reconcile", h.handleReconcile)
}

func (h *EffectsRPCHandler) handleList(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		State string `json:"state,omitempty"`
		Key   string `json:"key,omitempty"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
	}
	switch req.State {
	case "", string(effects.StateClaimed), string(effects.StateReceipted),
		string(effects.StateCompleted), string(effects.StateAbandoned):
	default:
		return nil, fmt.Errorf("invalid state %q (want claimed, receipted, completed, or abandoned)", req.State)
	}

	// No ledger wired => empty result, NOT an error (parity with the
	// ParkStore degradation posture): CLI visibility stays usable on an
	// unwired daemon.
	if h.ledger == nil {
		return map[string]any{"effects": []effects.EffectRecord{}}, nil
	}

	// Terminal states are not enumerable: the pinned Ledger surface
	// (Contract 1) exposes only ReconcilePending (claimed/receipted) and
	// Get-by-key, so completed/abandoned filters resolve only when a key
	// is supplied; without one the answer is an empty list, not an error.
	terminal := req.State == string(effects.StateCompleted) || req.State == string(effects.StateAbandoned)

	filtered := []effects.EffectRecord{}
	switch {
	case terminal && req.Key != "":
		rec, err := h.ledger.Get(ctx, req.Key)
		if err != nil {
			if errors.Is(err, effects.ErrUnknownKey) {
				break
			}
			return nil, fmt.Errorf("effects: read %s: %w", req.Key, err)
		}
		if string(rec.State) == req.State {
			filtered = append(filtered, *rec)
		}
	case terminal:
		// Not enumerable over the pinned surface: empty list.
	default:
		records, err := h.ledger.ReconcilePending(ctx)
		if err != nil {
			return nil, fmt.Errorf("effects: list pending: %w", err)
		}
		wantPending := req.State == "" || req.State == string(effects.StateClaimed) || req.State == string(effects.StateReceipted)
		for _, rec := range records {
			if req.State != "" && string(rec.State) != req.State {
				continue
			}
			if !wantPending { // unreachable; kept for clarity of the filter table
				continue
			}
			if req.Key != "" && rec.Key != req.Key {
				continue
			}
			filtered = append(filtered, rec)
		}
	}

	// Oldest claimed_at first, per the pinned contract (ReconcilePending
	// already orders this way; the terminal Get path sorts a single
	// record, so this is a no-op there).
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].ClaimedAt.Before(filtered[j].ClaimedAt)
	})
	return map[string]any{
		"effects":   filtered,
		RPCKeyCount: len(filtered),
	}, nil
}

func (h *EffectsRPCHandler) handleReconcile(ctx context.Context, params json.RawMessage) (any, error) {
	if h.ledger == nil {
		return nil, fmt.Errorf("effects service not available")
	}
	var req struct {
		Key     string          `json:"key"`
		Action  string          `json:"action"`
		Receipt json.RawMessage `json:"receipt,omitempty"`
		Reason  string          `json:"reason,omitempty"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
	}
	if req.Key == "" {
		return nil, fmt.Errorf("key is required")
	}
	switch req.Action {
	case "complete":
		return h.reconcileComplete(ctx, req.Key, req.Receipt)
	case "abandon":
		return h.reconcileAbandon(ctx, req.Key, req.Reason)
	default:
		return nil, fmt.Errorf("invalid action")
	}
}

// reconcileComplete marks the effect done: an optional hand-verified
// receipt is recorded first (claimed -> receipted), then Complete fires
// (receipted/claimed -> completed). Complete is idempotent on completed
// priors, so a human can re-confirm an already-completed key. Nothing
// here ever re-executes the effect — this handler has no executor.
//
// Receipt-overwrite decision (leaf-reported): RecordReceipt accepts only
// claimed -> receipted, so a receipt accompanies a re-confirmation of an
// already-receipted or completed record with ErrInvalidTransition. The
// human drops the receipt flag to plain-confirm; the original provider
// receipt stays intact. We surface the ledger's own error rather than
// inventing an overwrite semantics the state machine does not have.
func (h *EffectsRPCHandler) reconcileComplete(ctx context.Context, key string, receipt json.RawMessage) (any, error) {
	if len(receipt) > 0 && string(receipt) != "null" {
		if err := h.ledger.RecordReceipt(ctx, key, receipt); err != nil {
			return nil, fmt.Errorf("effects: record receipt %s: %w", key, err)
		}
	}
	if err := h.ledger.Complete(ctx, key); err != nil {
		return nil, fmt.Errorf("effects: complete %s: %w", key, err)
	}
	rec, err := h.ledger.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("effects: read %s: %w", key, err)
	}
	return map[string]any{"record": rec}, nil
}

func (h *EffectsRPCHandler) reconcileAbandon(ctx context.Context, key, reason string) (any, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("reason required for abandon")
	}
	if err := h.ledger.Abandon(ctx, key, reason); err != nil {
		return nil, fmt.Errorf("effects: abandon %s: %w", key, err)
	}
	rec, err := h.ledger.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("effects: read %s: %w", key, err)
	}
	return map[string]any{"record": rec}, nil
}
