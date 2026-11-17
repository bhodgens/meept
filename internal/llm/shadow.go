package llm

// Shadow routing (Contract C6, sovereign-router-parity leaf 04, R4; source
// reference: Sovereign Router POST /v1/shadow, src/gateway.py:1121-1180 —
// "decision-only, no upstream call, no ledger entry").
//
// ResolveShadow is the READ-ONLY parallel entry point into the alias
// selection pipeline: it answers "which model would you pick for this
// prompt, and what would it cost" without spending tokens. It mirrors the
// ResolveForAlias decision logic (resolver.go) without ANY of its writes:
//
//	MUST NOT: upstream HTTP, llm_calls / provider_requests /
//	          routing_decisions inserts, Budget.RecordCost / RecordUsage,
//	          AliasHealth mutation (CurrentIndex, CooldownUntil, StickyPins,
//	          ConsecutiveFails, RevertAt), sticky-pin create/release,
//	          endpointBlocks / entryBlocks / credentialBlock mutation,
//	          any counter increment, the tier gate, or depth consumption.
//	MUST:     acquire r.mu.RLock only; return the same model the live path
//	          would pick given identical state; return every candidate with
//	          a skip reason; never error on a legitimately-unroutable
//	          request (a decision with Selected=false and per-candidate
//	          reasons instead).
//
// It is a mirror, not a refactor: ResolveForAlias is untouched (review
// checklist), and the resolver's mutex was widened from sync.Mutex to
// sync.RWMutex so this reader can run concurrently with other readers —
// every existing Lock()/Unlock() call site keeps identical semantics.
//
// NOTE (orchestrator coordination): ShadowDecision.Posture (drift-audit
// ruling #3, the C6 union shape) requires the Posture type, which leaf 05
// owns. Wave 1 dispatches leaf 04 ahead of leaf 05, so the minimal
// vocabulary (type + three frozen constants) is declared AT THE BOTTOM OF
// THIS FILE. When posture.go lands it must NOT redeclare them — keep
// ParsePosture / AllowsSensitive / postureFilter there, and this type
// declaration is the single definition. A duplicate declaration in one
// package does not compile.

import (
	"context"
	"fmt"
	"time"
)

// --------------------------------------------------------------------------
// Contract C6 shapes (union ruling, DRIFT-AUDIT.md #3: master C6 keeps
// Posture Posture on the decision; ShadowCandidate carries leaf 04's richer
// Quality + EstCostUSD fields).
// --------------------------------------------------------------------------

// ShadowRequest names the request to preview. It is a pure input: nothing
// derived from it is ever persisted.
type ShadowRequest struct {
	// Alias is an alias name OR a "provider/model" ref; empty resolves the
	// default model (Reason "shadow_default").
	Alias string `json:"alias,omitempty"`
	// Messages is the prompt to estimate. The codebase's real message type
	// is ChatMessage (the C4/C6 sketch says []Message; same substitution
	// the context-warm seam made and documented in internal/llm/warm.go).
	Messages []ChatMessage `json:"messages,omitempty"`
	// Tools contributes to the input-token estimate (schema overhead).
	// The codebase's tool type is ToolDefinition (sketch: []ToolDef).
	Tools []ToolDefinition `json:"tools,omitempty"`
	// SessionID optionally drives sticky-pin SIMULATION for a
	// balanced_sticky alias; it is never written — no pin is created and
	// no existing pin is released or rewritten.
	SessionID string `json:"session_id,omitempty"`
	// Difficulty is the classified difficulty (0 = unclassified). It feeds
	// the selection-chain filters (quality floor); 0 keeps them inert.
	Difficulty int `json:"difficulty,omitempty"`
}

// ShadowCandidate is one alias member's preview row: every candidate of the
// alias appears exactly once, either Selected or carrying a SkipReason.
type ShadowCandidate struct {
	ModelID    string  `json:"model_id"`
	ProviderID string  `json:"provider_id"`
	Selected   bool    `json:"selected"`
	SkipReason string  `json:"skip_reason,omitempty"`
	Quality    int     `json:"quality"`
	EstCostUSD float64 `json:"est_cost_usd"`
}

// ShadowDecision is the decision-only preview. Reason is one of the frozen
// vocabulary "shadow_alias" | "shadow_direct" | "shadow_default". When
// nothing is routable the identity fields stay empty, Selected is false on
// every candidate, and the error is nil (a legitimately-unroutable request
// is a decision, not a failure).
type ShadowDecision struct {
	ModelID    string            `json:"model_id"`
	ProviderID string            `json:"provider_id"`
	Alias      string            `json:"alias"`
	Reason     string            `json:"reason"`
	EstInput   int               `json:"est_input_tokens"`
	EstOutput  int               `json:"est_output_tokens"`
	EstCostUSD float64           `json:"est_cost_usd"`
	Quality    int               `json:"quality"`
	Posture    Posture           `json:"posture"`
	Candidates []ShadowCandidate `json:"candidates"`
}

// --------------------------------------------------------------------------
// Entry point
// --------------------------------------------------------------------------

// ResolveShadow runs the full selection pipeline (health filters, sticky
// simulation, selection chain, cost estimate) and returns the decision with
// ZERO side effects. The whole computation is map reads + arithmetic under
// r.mu.RLock — no I/O under the lock, and the single Debug log line fires
// after it is released.
func (r *Resolver) ResolveShadow(ctx context.Context, req ShadowRequest) (*ShadowDecision, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("llm: ResolveShadow on a nil resolver")
	}

	estInput := shadowEstimateInputTokens(req.Messages, req.Tools)

	r.mu.RLock()
	dec, err := r.resolveShadowLocked(req, estInput)
	r.mu.RUnlock()

	if err == nil && dec != nil && r.logger != nil {
		r.logger.Debug("shadow route decision (no side effects)",
			"alias", req.Alias,
			"provider", dec.ProviderID,
			"model", dec.ModelID,
			"reason", dec.Reason,
			"est_input", dec.EstInput,
		)
	}
	return dec, err
}

// resolveShadowLocked classifies the request path and builds the decision.
// Callers hold r.mu (read). The ONLY error is a genuinely unknown alias/ref
// name — a caller mistake, not a legitimately-unroutable request; every
// routable-but-blocked state returns a decision with no selection.
func (r *Resolver) resolveShadowLocked(req ShadowRequest, estInput int) (*ShadowDecision, error) {
	now := r.clock()

	if req.Alias != "" {
		if alias, ok := r.aliases[req.Alias]; ok {
			return r.shadowAliasDecision(req, alias, r.health[req.Alias], estInput, now, "shadow_alias"), nil
		}
		if mc := ResolveModelRef(req.Alias, r.config); mc != nil {
			return r.shadowSingleDecision(req, mc, estInput, "shadow_direct", req.Alias, now), nil
		}
		return nil, fmt.Errorf("llm: alias not found: %s", req.Alias)
	}
	// Empty alias: preview the default model (Reason "shadow_default").
	// A daemon with no default model yields an empty decision, not an error.
	return r.shadowSingleDecision(req, r.defaultModel, estInput, "shadow_default", "", now), nil
}

// --------------------------------------------------------------------------
// Alias path: the read-only mirror of ResolveForAlias
// --------------------------------------------------------------------------

// shadowAliasDecision assembles the full candidate list for an alias. The
// winner is chosen by the same ladders ResolveForAlias runs (default
// reversion, alias cooldown, quota blocks, endpoint blocks, sticky pins,
// session-endpoint preference, final re-validation) — every step READ-ONLY,
// so the shared rotation cursor and pin map are never consumed.
func (r *Resolver) shadowAliasDecision(req ShadowRequest, alias *AliasEntry, health *AliasHealth, estInput int, now time.Time, reason string) *ShadowDecision {
	dec := &ShadowDecision{
		Alias:      req.Alias,
		Reason:     reason,
		Posture:    PosturePrivate, // default when the key is absent (Q3); leaf 05 wires the real value
		Candidates: []ShadowCandidate{},
	}
	n := len(alias.Models)
	if n == 0 {
		return dec
	}

	// Winner selection. A balanced-sticky alias with a session simulates
	// resolveStickyCaller (the live early-return path) and always lands on
	// a candidate; the rotation path mirrors the live main path and yields
	// no winner when every candidate is blocked (live: the ErrAll*Blocked
	// sentinels — shadow reports that as no selection, not an error).
	winIdx := -1
	if alias.BalancedStickyRequests && req.SessionID != "" {
		winIdx = r.shadowStickyWinner(alias, health, req.SessionID, now)
	} else if idx, ok := r.shadowRotationWinner(alias, health, req.SessionID, now); ok {
		winIdx = idx
	}

	// FilterCtx mirrors the live chain hook (resolver.go selection-chain
	// hook): capability defaults to the chat class, difficulty from the
	// request (0 = unclassified = the floor never binds).
	fc := &FilterCtx{
		SessionID:  req.SessionID,
		Capability: string(QualityChat),
		EstInput:   estInput,
		Difficulty: req.Difficulty,
		Now:        now,
	}

	for i, m := range alias.Models {
		estOut := shadowEstOutputTokens(m)
		cand := ShadowCandidate{
			ModelID:    m.ModelID,
			ProviderID: m.ProviderID,
			Quality:    m.Quality.QualityFor(QualityChat),
			EstCostUSD: shadowEstCostUSD(m, estInput, estOut),
		}
		if i != winIdx {
			cand.SkipReason = r.shadowSkipReason(req, alias, health, i, now, fc, m, estInput, estOut)
			dec.Candidates = append(dec.Candidates, cand)
			continue
		}
		// Winner: the live chain hook refuses ONLY on VerdictFloor (a Skip
		// verdict still serves — resolver.go hook). Mirror exactly.
		if v, freason := r.runSelectionFilters(&Candidate{
			Model:      m,
			Alias:      req.Alias,
			Capability: fc.Capability,
			EstInput:   estInput,
			EstOutput:  estOut,
		}, fc); v == VerdictFloor {
			// Live would refuse the turn; shadow reports it as an unselected
			// candidate with the binding filter's reason. No error.
			cand.SkipReason = freason
			winIdx = -1
		} else {
			cand.Selected = true
			dec.ModelID = m.ModelID
			dec.ProviderID = m.ProviderID
			dec.EstInput = estInput
			dec.EstOutput = estOut
			dec.EstCostUSD = cand.EstCostUSD
			dec.Quality = cand.Quality
		}
		dec.Candidates = append(dec.Candidates, cand)
	}
	return dec
}

// shadowStickyWinner simulates resolveStickyCaller WITHOUT any of its
// writes: an existing pin is inspected (never released, never rewritten) and
// the new-pin ladder is computed from the current cursor without storing the
// pin or advancing CurrentIndex. Unlike the rotation path, the live sticky
// path always returns a model (it pins the rotation position even when every
// candidate is blocked), so ok is always true for a non-empty alias.
// Callers hold r.mu (read).
func (r *Resolver) shadowStickyWinner(alias *AliasEntry, health *AliasHealth, sessionID string, now time.Time) int {
	n := len(alias.Models)
	cur := 0
	if health != nil {
		cur = health.CurrentIndex % n
	}

	// Existing pin: the live path serves it while it is healthy — same
	// health gates, evaluated read-only.
	if health != nil && health.StickyPins != nil {
		if pinnedIdx, ok := health.StickyPins[sessionID]; ok && pinnedIdx >= 0 && pinnedIdx < n {
			pm := alias.Models[pinnedIdx]
			pinnedBlocked := r.isEndpointBlocked(health, pm) ||
				(r.quotaEnabled() && r.isQuotaBlocked(health, pm))
			if !pinnedBlocked && (!shadowInCooldown(health, now) || !health.failedModelMatches(alias, pinnedIdx)) {
				return pinnedIdx // live reason: sticky_request
			}
		}
	}

	// New-pin ladder (live: "(Re-)assign a pin"): start at the rotation
	// position, step off the failed model under cooldown, then skip
	// quota-blocked and endpoint-blocked candidates with the same one-lap
	// cap — all blocked lands back on the start position, exactly like the
	// live ladder's wrap-around.
	nextIdx := cur
	if health != nil && shadowInCooldown(health, now) && health.failedModelMatches(alias, nextIdx) {
		nextIdx = (nextIdx + 1) % n
	}
	if r.quotaEnabled() {
		for range n {
			if !r.isQuotaBlocked(health, alias.Models[nextIdx]) {
				break
			}
			nextIdx = (nextIdx + 1) % n
		}
	}
	if r.hasEndpointBlocks() {
		for range n {
			if !r.isEndpointBlocked(health, alias.Models[nextIdx]) {
				break
			}
			nextIdx = (nextIdx + 1) % n
		}
	}
	return nextIdx // live reason: sticky_request_new
}

// shadowRotationWinner mirrors ResolveForAlias's main path (everything
// except the sticky early-return) READ-ONLY: default-model reversion,
// alias-cooldown advance, quota-block rotation, endpoint-block rotation, the
// session-endpoint preference hook, and the final joint re-validation. The
// live path's health writes (cursor advance, RevertAt/CooldownUntil clears,
// routing-log record) are all omitted. ok=false mirrors the live
// ErrAllModelsQuotaBlocked / ErrAllEndpointsBlocked returns.
// Callers hold r.mu (read).
func (r *Resolver) shadowRotationWinner(alias *AliasEntry, health *AliasHealth, sessionID string, now time.Time) (int, bool) {
	n := len(alias.Models)
	cur := 0
	if health != nil {
		cur = health.CurrentIndex % n
	}

	// Default-model reversion (read-only: the live path also clears
	// RevertAt/ConsecutiveFails/CooldownUntil — shadow never does).
	if alias.DefaultModel != "" && health != nil && !health.RevertAt.IsZero() && now.After(health.RevertAt) {
		if di := r.aliasIndexOfRef(alias.DefaultModel, alias); di >= 0 && di != cur {
			cur = di
		}
	}

	// Alias-level cooldown: the live path advances once past the failed
	// model (and releases that model's pins — a write shadow omits).
	if health != nil && shadowInCooldown(health, now) {
		if nxt := (cur + 1) % n; nxt != cur {
			cur = nxt
		}
	}

	// Quota-block pass: rotate to the first unblocked candidate; all
	// blocked mirrors ErrAllModelsQuotaBlocked.
	if r.quotaEnabled() && r.isQuotaBlocked(health, alias.Models[cur]) {
		rotated := false
		j := (cur + 1) % n
		for range n {
			if !r.isQuotaBlocked(health, alias.Models[j]) {
				cur = j
				rotated = true
				break
			}
			j = (j + 1) % n
		}
		if !rotated {
			return 0, false
		}
	}

	// Endpoint-block pass: same shape, distinct all-blocked sentinel.
	if r.isEndpointBlocked(health, alias.Models[cur]) {
		rotated := false
		j := (cur + 1) % n
		for range n {
			if !r.isEndpointBlocked(health, alias.Models[j]) {
				cur = j
				rotated = true
				break
			}
			j = (j + 1) % n
		}
		if !rotated {
			return 0, false
		}
	}

	// Session-endpoint preference hook (read-only): a remembered key
	// belonging to a HEALTHY candidate of THIS alias moves the request-local
	// selection to it; the shared cursor is never repositioned (live
	// semantics — and shadow writes nothing anyway).
	if sessionID != "" {
		if key, ok := preferredEndpointLocked(r, sessionID); ok {
			for idx, cand := range alias.Models {
				if EndpointKey(cand) != key {
					continue
				}
				blocked := r.isEndpointBlocked(health, cand) ||
					(r.quotaEnabled() && r.isQuotaBlocked(health, cand))
				if !blocked {
					cur = idx
				}
				break
			}
		}
	}

	// Final re-validation (live: the sequential quota/endpoint passes can
	// hand the cursor back and forth; re-validate and keep rotating past
	// doubly-blocked models — bughunt round-2 auditor 3, finding 1). The
	// live path's cursor write here is a write shadow omits.
	blockedAt := func(i int) bool {
		m := alias.Models[i]
		return r.isEndpointBlocked(health, m) ||
			(r.quotaEnabled() && r.isQuotaBlocked(health, m))
	}
	if blockedAt(cur) {
		found := -1
		for i := range n {
			idx := (cur + i) % n
			if !blockedAt(idx) {
				found = idx
				break
			}
		}
		if found < 0 {
			return 0, false
		}
		cur = found
	}
	return cur, true
}

// shadowSkipReason names why a NON-winner candidate would not serve. Health
// facts outrank filter verdicts (the binding constraint the routing log
// names), then the selection chain, then plain rotation position.
// Callers hold r.mu (read).
func (r *Resolver) shadowSkipReason(req ShadowRequest, alias *AliasEntry, health *AliasHealth, idx int, now time.Time, fc *FilterCtx, m *ModelConfig, estInput, estOutput int) string {
	if r.quotaEnabled() && r.isQuotaBlocked(health, m) {
		return "quota_blocked"
	}
	if r.isEndpointBlocked(health, m) {
		return "endpoint_blocked"
	}
	if health != nil && shadowInCooldown(health, now) && health.failedModelMatches(alias, idx) {
		return "alias_cooldown"
	}
	if v, freason := r.runSelectionFilters(&Candidate{
		Model:      m,
		Alias:      req.Alias,
		Capability: fc.Capability,
		EstInput:   estInput,
		EstOutput:  estOutput,
	}, fc); v != VerdictAllow {
		return freason
	}
	return "not_rotation_head"
}

// shadowInCooldown is the nil-safe read of AliasHealth.inCooldown (that
// method derefs the receiver; a shadow resolve on an alias with no health
// state yet must read it as "not cooling down", never create the state).
func shadowInCooldown(health *AliasHealth, now time.Time) bool {
	return health != nil && !health.CooldownUntil.IsZero() && now.Before(health.CooldownUntil)
}

// --------------------------------------------------------------------------
// Single-candidate paths (direct ref / default model)
// --------------------------------------------------------------------------

// shadowSingleDecision previews one model (a "provider/model" ref or the
// default model). There is no rotation or health state to mirror for a
// single ref; the selection chain still runs so a Floor verdict (the same
// veto the alias path enforces) surfaces as an unselected candidate with the
// filter's reason rather than a bogus selection.
func (r *Resolver) shadowSingleDecision(req ShadowRequest, mc *ModelConfig, estInput int, reason, aliasLabel string, now time.Time) *ShadowDecision {
	dec := &ShadowDecision{
		Alias:      aliasLabel,
		Reason:     reason,
		Posture:    PosturePrivate, // default when the key is absent (Q3); leaf 05 wires the real value
		Candidates: []ShadowCandidate{},
	}
	if mc == nil {
		return dec
	}
	estOut := shadowEstOutputTokens(mc)
	cand := ShadowCandidate{
		ModelID:    mc.ModelID,
		ProviderID: mc.ProviderID,
		Quality:    mc.Quality.QualityFor(QualityChat),
		EstCostUSD: shadowEstCostUSD(mc, estInput, estOut),
	}
	if v, freason := r.runSelectionFilters(&Candidate{
		Model:      mc,
		Alias:      aliasLabel,
		Capability: string(QualityChat),
		EstInput:   estInput,
		EstOutput:  estOut,
	}, &FilterCtx{
		SessionID:  req.SessionID,
		Capability: string(QualityChat),
		EstInput:   estInput,
		Difficulty: req.Difficulty,
		Now:        now,
	}); v == VerdictFloor {
		cand.SkipReason = freason
	} else {
		cand.Selected = true
		dec.ModelID = mc.ModelID
		dec.ProviderID = mc.ProviderID
		dec.EstInput = estInput
		dec.EstOutput = estOut
		dec.EstCostUSD = cand.EstCostUSD
		dec.Quality = cand.Quality
	}
	dec.Candidates = append(dec.Candidates, cand)
	return dec
}

// --------------------------------------------------------------------------
// Cost + token estimators (new, leaf 04; never touches the tier gate and
// consumes no depth — pure arithmetic over the model's declared prices).
// --------------------------------------------------------------------------

// shadowEstOutputTokens is the estimated output for a previewed turn: the
// model's configured max output; 0 when unconfigured (the cost term then
// carries input only — never fabricate a bound).
func shadowEstOutputTokens(m *ModelConfig) int {
	if m == nil || m.MaxTokens < 0 {
		return 0
	}
	return m.MaxTokens
}

// shadowEstCostUSD is the frozen arithmetic (leaf doc: models.go TotalCost
// shape, computed locally): (EstInput/1e6)*CostPerMillionInput +
// (EstOutput/1e6)*CostPerMillionOutput.
func shadowEstCostUSD(m *ModelConfig, estInput, estOutput int) float64 {
	if m == nil {
		return 0
	}
	return float64(estInput)/1e6*m.CostPerMillionInput +
		float64(estOutput)/1e6*m.CostPerMillionOutput
}

// shadowEstimateInputTokens estimates the prompt's input tokens with the
// package's existing heuristic tokenizer (3 chars/token, tokenizer.go) over
// message content plus per-tool schema overhead (ToolDefinition.CountTokens
// already carries the structural allowance). Deterministic, dependency-free,
// and deliberately simple: shadow is a preview, and the estimate is reported
// IN the decision (est_input_tokens) so consumers can see the basis.
func shadowEstimateInputTokens(messages []ChatMessage, tools []ToolDefinition) int {
	tok := &HeuristicTokenizer{}
	total := 0
	for i := range messages {
		total += tok.CountTokens(messages[i].Content)
	}
	for i := range tools {
		total += tools[i].CountTokens(tok)
	}
	return total
}

// --------------------------------------------------------------------------
// Posture vocabulary (minimal declaration — SEE THE HEADER NOTE).
//
// Owned by leaf 05 (Contract C4, posture.go): the full surface
// (ParsePosture, AllowsSensitive, postureFilter, the ModelDef.DataPosture
// mapping) lands there. Leaf 04 declares ONLY the type and the three frozen
// constants because the C6 union ShadowDecision (drift-audit ruling #3)
// carries a Posture value and wave 1 dispatches leaf 04 first. posture.go
// must NOT redeclare these — move, don't duplicate.
// --------------------------------------------------------------------------

// Posture is the per-model data-posture label. The zero value "" is NOT a
// valid posture: consumers treat absent/unknown as PosturePrivate
// (backward-compatible default, master Q3) — leaf 05's ParsePosture owns
// that mapping.
type Posture string

const (
	// PosturePrivate: no-training, cleared for sensitive turns.
	PosturePrivate Posture = "private"
	// PosturePublicOnly: provider may train -> sensitive turns barred.
	PosturePublicOnly Posture = "public_only"
	// PostureNonSensitive: aggregator, posture unclear -> sensitive barred.
	PostureNonSensitive Posture = "non_sensitive"
)
