package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// CapabilityError is returned when no model satisfies a skill's requirements.
type CapabilityError struct {
	SkillName string
	Requires  []string
}

func (e *CapabilityError) Error() string {
	return fmt.Sprintf("no model satisfies capability requirements %v for skill %q", e.Requires, e.SkillName)
}

// ErrAllModelsQuotaBlocked is returned by ResolveForAlias and
// RotateToNextModel when every candidate model in the alias is currently
// quota-blocked. Callers should treat it as a retry-later condition (the
// alias unblocks when the earliest quota reset elapses), not a config error.
var ErrAllModelsQuotaBlocked = errors.New("all models in alias are quota-blocked")

// ErrAllEndpointsBlocked is returned by ResolveForAlias when every
// candidate model in the alias is blocked by an endpoint-level timeout
// cooldown or by an armed alias-level explicit-timeout block (tree 02 leaf
// 04, DECISIONS.md D10). It is DISTINCT from ErrAllModelsQuotaBlocked (the
// two failure classes have different horizons and different clearing
// rules) — check with errors.Is, never string matching.
var ErrAllEndpointsBlocked = errors.New("all models in alias are endpoint-blocked")

// SkillRequirements defines the capability requirements for a skill.
type SkillRequirements struct {
	Name     string
	Requires []string
}

// Resolver resolves model selection based on capability matching.
type Resolver struct {
	config        *ProvidersConfig
	defaultModel  *ModelConfig
	smallModel    *ModelConfig
	decisionModel *ModelConfig
	allModels     []*ModelConfig
	aliases       map[string]*AliasEntry
	health        map[string]*AliasHealth
	pricingSyncer *PricingSyncer
	routingLogger *RoutingLogger
	mu            sync.Mutex
	logger        *slog.Logger
	// quotaCfg holds quota-aware retry settings. Nil disables quota blocking.
	quotaCfg *QuotaWaitConfig
	// endpointBlocks maps EndpointKey(cfg) -> block deadline for
	// endpoint-level timeout cooldowns (tree 02 leaf 04, DECISIONS.md
	// D10). It deliberately lives on the RESOLVER, not on AliasHealth
	// (audit B3): AliasHealth is per-alias, but D10 shared fate is
	// cross-alias — openai/model-1 on the medium alias and openai/model-2
	// on the thinkhard alias are different aliases over one endpoint, and
	// a timeout on either must skip both. Guarded by the single Resolver
	// .mu regime like every other field; zero deadline or one in the past
	// means unblocked.
	endpointBlocks map[string]time.Time
	// now is the injected clock (SHARED-CONVENTIONS §5) used by the
	// endpoint/alias-block paths for lazy expiry checks. Nil = time.Now.
	now func() time.Time

	// sessionEndpoints maps sessionID -> EndpointKey(cfg) of the endpoint
	// that last served that session successfully (session-affinity tree
	// leaf 04, master Contract 4). It deliberately lives ON THE RESOLVER
	// next to endpointBlocks: the same mutex guards both, and the
	// resolve-time healthy-candidate gate needs isQuotaBlocked /
	// isEndpointBlocked, which are r.mu-locked helpers.
	//
	// In-memory only, never persisted; LRU-bounded at
	// stickySessionEndpointCap sessions. A success overwrites on every
	// call (Contract 4); quota blocks, endpoint cooldowns, and refusal
	// policy take precedence over the preference at resolve time.
	//
	// Consumers: RememberSessionEndpoint (writer, one reporting seam —
	// ProviderManager.reportSessionEndpoint, provider_manager.go),
	// PreferredEndpoint
	// (resolve-time reader), SessionEndpoints (leaf-06 teardown
	// coordinator's point-in-time snapshot).
	sessionEndpoints map[string]string
	// sessionEndpointOrder maps sessionID -> index into sessionEndpointLRU
	// (-1 = evicted, slice compaction pending). The order slice is the LRU
	// list: front = least recently used (evicted first at cap).
	sessionEndpointOrder map[string]int
	// sessionEndpointLRU holds session IDs in use recency order; see
	// sessionEndpointOrder. Guarded by mu (all three fields together).
	sessionEndpointLRU []string
}

// stickySessionEndpointCap bounds the per-session endpoint preference map
// (master Contract 4: cap 1024 sessions). Exposed as a var, not a const,
// ONLY for the eviction test to exercise the LRU path without allocating
// 1024 sessions; production code must never reassign it.
var stickySessionEndpointCap = 1024

// NewResolver creates a new model resolver.
func NewResolver(cfg *ProvidersConfig, logger *slog.Logger) *Resolver {
	if logger == nil {
		logger = slog.Default()
	}

	r := &Resolver{
		config:               cfg,
		allModels:            GetAllModels(cfg),
		aliases:              make(map[string]*AliasEntry),
		health:               make(map[string]*AliasHealth),
		endpointBlocks:       make(map[string]time.Time),
		sessionEndpoints:     make(map[string]string),
		sessionEndpointOrder: make(map[string]int),
		logger:               logger,
	}

	// Resolve default and small models
	if cfg.Model != "" {
		r.defaultModel = ResolveModelRef(cfg.Model, cfg)
	}
	if cfg.SmallModel != "" {
		r.smallModel = ResolveModelRef(cfg.SmallModel, cfg)
	}
	// Decision model slot (decision-model tree leaf 01): resolved only when
	// explicitly configured. Empty slot = nil = every decision-model feature
	// stays disabled (never falls back to small_model or the default model).
	if cfg.DecisionModel != "" {
		r.decisionModel = ResolveModelRef(cfg.DecisionModel, cfg)
		if r.decisionModel == nil {
			r.logger.Warn("decision_model ref does not resolve to a configured model; decision-model features disabled",
				"ref", cfg.DecisionModel)
		}
	}

	// Load model aliases
	for aliasName, aliasEntry := range cfg.ModelAliases {
		models := make([]*ModelConfig, 0, len(aliasEntry.Models))
		for _, modelRef := range aliasEntry.Models {
			mc := ResolveModelRef(modelRef, cfg)
			if mc != nil {
				models = append(models, mc)
			}
		}
		if len(models) > 0 {
			timeout := time.Duration(aliasEntry.Timeout) * time.Second
			// explicitTimeout distinguishes a config-declared timeout:
			// from the substituted default. Alias-level blocks (D10) arm
			// ONLY when the config declared one.
			explicitTimeout := aliasEntry.Timeout > 0
			if timeout == 0 {
				timeout = 30 * time.Second // Default timeout
			}
			maxFails := aliasEntry.MaxFails
			if maxFails == 0 {
				maxFails = 3 // Default max fails
			}
			entry := &AliasEntry{
				Models:                 models,
				Timeout:                timeout,
				MaxFails:               maxFails,
				DefaultModel:           aliasEntry.DefaultModel,
				BalancedStickyRequests: aliasEntry.BalancedStickyRequests,
			}
			// Stamp explicit-config-ness onto every member model so the
			// failure path can consult the identity-bearing config itself.
			for _, m := range entry.Models {
				m.ConfiguredTimeout = explicitTimeout
			}
			if explicitTimeout {
				// Arming is done per-alias-health in getOrCreateHealth;
				// record it here on the alias entry's members and rely on
				// the health-level flag below.
				entry.timeoutArmed = true
			}
			r.aliases[aliasName] = entry
		}
	}

	return r
}

// SetRoutingLogger attaches a routing decision logger. Pass nil to disable
// (the setter is a no-op on nil to honor the CLAUDE.md typed-nil setter rule).
func (r *Resolver) SetRoutingLogger(rl *RoutingLogger) {
	if rl != nil {
		r.routingLogger = rl
	}
}

// SetQuotaConfig attaches quota-aware retry settings. Pass nil to disable
// quota blocking; a zero-value QuotaWaitConfig also means DISABLED (callers
// that want enabled-with-defaults must populate the fields — see
// ConfigFromSchema). No-op on a nil receiver to honor the typed-nil setter
// rule (AGENTS.md).
func (r *Resolver) SetQuotaConfig(cfg *QuotaWaitConfig) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cfg == nil {
		r.quotaCfg = nil
		return
	}
	c := *cfg // copy value: caller's struct is not aliased
	r.quotaCfg = &c
}

// quotaEnabled reports whether quota blocking is active. Callers must hold
// Resolver.mu.
func (r *Resolver) quotaEnabled() bool {
	return r.quotaCfg != nil && r.quotaCfg.Enabled
}

// DefaultModel returns the default model configuration.
func (r *Resolver) DefaultModel() *ModelConfig {
	return r.defaultModel
}

// SmallModel returns the small/fast model configuration.
func (r *Resolver) SmallModel() *ModelConfig {
	return r.smallModel
}

// DecisionModel returns the dedicated decision-model configuration, or nil
// when no decision_model slot is configured (features disabled).
func (r *Resolver) DecisionModel() *ModelConfig {
	if r == nil || r.decisionModel == nil {
		return nil
	}
	return r.decisionModel
}

// DecisionModelName returns "provider/model-id" for the configured decision
// model, or "" when unset/nil (decision-model features disabled).
func (r *Resolver) DecisionModelName() string {
	if r == nil || r.decisionModel == nil {
		return ""
	}
	return r.decisionModel.ProviderID + "/" + r.decisionModel.ModelID
}

// ImageModel returns the configured image-generation model, if any.
func (r *Resolver) ImageModel() *ModelConfig {
	if r == nil || r.config == nil || r.config.ImageModel == "" {
		return nil
	}
	return r.resolveGenerationRef(r.config.ImageModel, CapImageGen)
}

// VideoModel returns the configured video-generation model, if any.
func (r *Resolver) VideoModel() *ModelConfig {
	if r == nil || r.config == nil || r.config.VideoModel == "" {
		return nil
	}
	return r.resolveGenerationRef(r.config.VideoModel, CapVideoGen)
}

// ResolveGeneration resolves a model ref or alias for image/video generation.
// Empty ref uses the image_model / video_model slot, then the cheapest model
// with the matching capability.
func (r *Resolver) ResolveGeneration(ref, kind string) (*ModelConfig, error) {
	if r == nil {
		return nil, fmt.Errorf("model resolver is not configured")
	}
	capName := CapImageGen
	if kind == "video" {
		capName = CapVideoGen
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if kind == "video" {
			if mc := r.VideoModel(); mc != nil {
				return mc, nil
			}
		} else if mc := r.ImageModel(); mc != nil {
			return mc, nil
		}
		if cheapest := r.FindCheapest([]string{capName}); cheapest != nil {
			return cheapest, nil
		}
		return nil, fmt.Errorf("no model with capability %q; set %s_model in models.json5", capName, kind)
	}
	mc := r.resolveGenerationRef(ref, capName)
	if mc == nil {
		return nil, fmt.Errorf("unknown generation model %q (want provider/id or alias with capability %s)", ref, capName)
	}
	return mc, nil
}

func (r *Resolver) resolveGenerationRef(ref, capName string) *ModelConfig {
	if models, ok := r.GetAllModelsForAlias(ref); ok {
		for _, mc := range models {
			if mc != nil && mc.HasCapability(capName) {
				return mc
			}
		}
	}
	if mc := ResolveModelRef(ref, r.config); mc != nil && mc.HasCapability(capName) {
		return mc
	}
	return nil
}

// AllModels returns all available model configurations.
func (r *Resolver) AllModels() []*ModelConfig {
	return r.allModels
}

// ResolveForSkill selects the appropriate model for a skill.
// If skill is nil or has no requirements, returns the current or default model.
// Otherwise, finds the cheapest model that satisfies the requirements.
func (r *Resolver) ResolveForSkill(skill *SkillRequirements, currentModel *ModelConfig) (*ModelConfig, error) {
	effectiveCurrent := currentModel
	if effectiveCurrent == nil {
		effectiveCurrent = r.defaultModel
	}

	// No skill or no requirements -> use current model
	if skill == nil || len(skill.Requires) == 0 {
		if effectiveCurrent != nil {
			return effectiveCurrent, nil
		}
		// Fallback: return first available model
		if len(r.allModels) > 0 {
			return r.allModels[0], nil
		}
		return nil, &CapabilityError{SkillName: "(none)", Requires: nil}
	}

	required := skill.Requires

	// Check if current model satisfies requirements
	if effectiveCurrent != nil && effectiveCurrent.HasCapabilities(required) {
		r.logger.Debug("Current model satisfies requirements",
			"model", effectiveCurrent.ModelID,
			"requires", required,
		)
		return effectiveCurrent, nil
	}

	// Find cheapest model that satisfies requirements
	var candidates []*ModelConfig
	for _, m := range r.allModels {
		if m.HasCapabilities(required) {
			candidates = append(candidates, m)
		}
	}

	if len(candidates) == 0 {
		return nil, &CapabilityError{
			SkillName: skill.Name,
			Requires:  required,
		}
	}

	// Sort by total cost, cheapest first
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].TotalCost() < candidates[j].TotalCost()
	})

	selected := candidates[0]
	r.logger.Info("Escalated to model for skill",
		"model", selected.ModelID,
		"provider", selected.ProviderID,
		"skill", skill.Name,
		"requires", required,
	)

	// Persist the routing decision for later mining (Phase 4 student-learns-routing).
	// Errors are best-effort: routing data is observability/training fodder, not
	// a correctness signal, so we swallow rather than fail the request.
	if r.routingLogger != nil {
		decision := RoutingDecision{
			ChosenModelID:    selected.ModelID,
			ChosenProviderID: selected.ProviderID,
			Reason:           "capability_escalation",
			Skill:            skill.Name,
			CandidatesJSON:   r.candidatesJSON(candidates),
		}
		_ = r.routingLogger.Record(context.Background(), decision)
	}

	return selected, nil
}

// ResolveRef resolves a "provider/model-id" reference.
func (r *Resolver) ResolveRef(ref string) *ModelConfig {
	mc := ResolveModelRef(ref, r.config)
	if mc == nil {
		return nil
	}
	// Persist the routing decision for later mining (G9 wiring: explicit
	// resolution path). Best-effort: swallow errors so routing observability
	// never breaks serving.
	if r.routingLogger != nil {
		_ = r.routingLogger.Record(context.Background(), RoutingDecision{
			ChosenModelID:    mc.ModelID,
			ChosenProviderID: mc.ProviderID,
			Reason:           "explicit",
		})
	}
	return mc
}

// ResolveEscalationRef resolves an escalation target — an alias name or a
// "provider/model" ref — to a model ref string usable as a loop model ref.
// Implements agent.ModelResolver. Named ResolveEscalationRef, not ResolveRef,
// because *Resolver already defines ResolveRef(ref string) *ModelConfig
// above (audit B2: a same-name/different-signature method would not compile).
// Alias resolution INHERITS alias rotation, cooldowns, and quota blocks: a
// fully quota-blocked escalation alias surfaces ErrAllModelsQuotaBlocked and
// the loop's existing quota handling takes over (SHARED-CONVENTIONS §2 — no
// second handling path).
func (r *Resolver) ResolveEscalationRef(ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("empty escalation ref")
	}

	// Alias branch: delegate to the existing alias lookup (rotation,
	// cooldown, quota semantics all inherited). callerKey is "" — escalation
	// is a fresh resolution, not a sticky-caller request. ResolveForAlias
	// takes r.mu internally; no lock is held across the call.
	if r.HasAlias(ref) {
		mc, err := r.ResolveForAlias(ref, "")
		if err != nil {
			return "", err
		}
		if mc == nil {
			return "", fmt.Errorf("alias %q resolved to no model", ref)
		}
		// Record the escalation decision for later mining (leaf 04 Task 3),
		// same best-effort shape as the direct branch below. Alias is
		// carried so escalations are queryable uniformly regardless of
		// target form (D3: alias targets are the primary style).
		if r.routingLogger != nil {
			// Log-and-continue, never swallow silently (SHARED-CONVENTIONS
			// §2 forbids `_ = f()`); observability must not fail serving.
			if recErr := r.routingLogger.Record(context.Background(), RoutingDecision{
				ChosenModelID:    mc.ModelID,
				ChosenProviderID: mc.ProviderID,
				Alias:            ref,
				Reason:           "escalation",
			}); recErr != nil {
				slog.Warn("escalation routing-log record failed", "alias", ref, "error", recErr)
			}
		}
		return mc.ProviderID + "/" + mc.ModelID, nil
	}

	// Direct-ref branch: mirror ResolveRef's lookup via ResolveModelRef, but
	// surface unknown refs as errors instead of nil (escalation must fail
	// loudly so DecideEscalation degrades to the legacy path).
	mc := ResolveModelRef(ref, r.config)
	if mc == nil {
		return "", fmt.Errorf("escalation ref not found: %s", ref)
	}

	// Persist the routing decision for later mining. Best-effort: swallow
	// errors so routing observability never breaks serving. Mirrors the
	// capability_escalation recording shape above (resolver.go:269).
	if r.routingLogger != nil {
		// Log-and-continue, never swallow silently (SHARED-CONVENTIONS
		// §2 forbids `_ = f()`); observability must not fail serving.
		if recErr := r.routingLogger.Record(context.Background(), RoutingDecision{
			ChosenModelID:    mc.ModelID,
			ChosenProviderID: mc.ProviderID,
			Reason:           "escalation",
		}); recErr != nil {
			slog.Warn("escalation routing-log record failed", "ref", ref, "error", recErr)
		}
	}

	return mc.ProviderID + "/" + mc.ModelID, nil
}

// FindByCapabilities finds all models with the specified capabilities.
func (r *Resolver) FindByCapabilities(caps []string) []*ModelConfig {
	var results []*ModelConfig
	for _, m := range r.allModels {
		if m.HasCapabilities(caps) {
			results = append(results, m)
		}
	}
	return results
}

// FindCheapest finds the cheapest model with the specified capabilities.
func (r *Resolver) FindCheapest(caps []string) *ModelConfig {
	models := r.FindByCapabilities(caps)
	if len(models) == 0 {
		return nil
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].TotalCost() < models[j].TotalCost()
	})

	return models[0]
}

// FindByProvider returns all models from a specific provider.
func (r *Resolver) FindByProvider(providerID string) []*ModelConfig {
	var results []*ModelConfig
	for _, m := range r.allModels {
		if m.ProviderID == providerID {
			results = append(results, m)
		}
	}
	return results
}

// ResolveForAlias resolves an alias to a specific model, handling rotation.
// It returns the currently active model for the given alias.
// When callerKey is non-empty and BalancedStickyRequests is true, the caller
// is pinned to a single model within the alias.
func (r *Resolver) ResolveForAlias(aliasName string, sessionID string) (*ModelConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	alias, ok := r.aliases[aliasName]
	if !ok {
		return nil, fmt.Errorf("alias not found: %s", aliasName)
	}

	health := r.getOrCreateHealth(aliasName)
	now := time.Now()

	// Sticky requests: resolve before alias-level rotation so a caller pinned
	// to a failed model releases its pin and re-pins to a healthy model,
	// while callers pinned to unaffected models keep their pins.
	if alias.BalancedStickyRequests && sessionID != "" {
		return r.resolveStickyCaller(alias, aliasName, health, sessionID, now), nil
	}

	reverted := false

	// Default-model reversion: armed by RecordAliasFailure when default_model
	// is configured, the rotation snaps back to the default model once the
	// reversion deadline passes instead of continuing round-robin from the
	// fallback position. RevertAt survives the cooldown reset below (which
	// zeroes CooldownUntil when advancing), so it cannot be skipped.
	if alias.DefaultModel != "" && !health.RevertAt.IsZero() && now.After(health.RevertAt) {
		if defaultIdx := r.aliasIndexOfRef(alias.DefaultModel, alias); defaultIdx >= 0 && health.CurrentIndex != defaultIdx {
			health.CurrentIndex = defaultIdx
			reverted = true
		}
		health.RevertAt = time.Time{}
		health.ConsecutiveFails = 0
		health.CooldownUntil = time.Time{}
	}

	// Alias-level cooldown: the current model failed recently, so advance the
	// rotation once to the next candidate and reset the failure state for it.
	// A single AliasHealth tracks one cooldown for the alias as a whole, so
	// the candidate inherits a cleared cooldown when it becomes current.
	if health.inCooldown(now) {
		nextIdx := (health.CurrentIndex + 1) % len(alias.Models)
		if nextIdx != health.CurrentIndex {
			// The model being left is the failed one; release any sticky
			// pins on it so those callers re-pin on their next resolve.
			if left := alias.Models[health.CurrentIndex]; health.failedModelMatches(alias, health.CurrentIndex) {
				health.releasePinsForModel(alias, left.ProviderID, left.ModelID)
			}
			health.CurrentIndex = nextIdx
			health.ConsecutiveFails = 0
			health.CooldownUntil = time.Time{}
		}
	}

	// Check quota blocks on the current model. A blocked current model
	// rotates to the next unblocked candidate; when every candidate is
	// blocked, fail with ErrAllModelsQuotaBlocked instead of returning a
	// blocked model (Bug A read health.entryBlocks directly and missed
	// credential-level blocks; Bug B parked on a blocked model).
	if r.quotaEnabled() {
		if r.isQuotaBlocked(health, alias.Models[health.CurrentIndex]) {
			nextIdx := (health.CurrentIndex + 1) % len(alias.Models)
			rotated := false
			for range len(alias.Models) {
				candidate := alias.Models[nextIdx]
				if !r.isQuotaBlocked(health, candidate) {
					health.CurrentIndex = nextIdx
					rotated = true
					break
				}
				nextIdx = (nextIdx + 1) % len(alias.Models)
			}
			if !rotated {
				return nil, fmt.Errorf("%w: alias %q (all %d model(s) quota-blocked)",
					ErrAllModelsQuotaBlocked, aliasName, len(alias.Models))
			}
			r.logger.Info("Rotated away from quota-blocked current model",
				"alias", aliasName,
				"new_index", health.CurrentIndex,
			)
		}
	}

	// Endpoint-level timeout cooldowns (tree 02 leaf 04, DECISIONS.md
	// D10): a throttled/timed-out model's whole base endpoint is blocked —
	// including across aliases, because endpointBlocks lives on the
	// Resolver (audit B3) — and an armed alias-level explicit-timeout
	// block excludes every member. Endpoint-blocked candidates are skipped
	// exactly like quota-blocked ones; when nothing unblocked remains the
	// DISTINCT ErrAllEndpointsBlocked is returned. NOTE precedence: quota
	// blocks outrank endpoint blocks, so a candidate that is BOTH is
	// reported as quota-blocked above.
	if r.isEndpointBlocked(health, alias.Models[health.CurrentIndex]) {
		nextIdx := (health.CurrentIndex + 1) % len(alias.Models)
		rotated := false
		for range len(alias.Models) {
			candidate := alias.Models[nextIdx]
			if !r.isEndpointBlocked(health, candidate) {
				health.CurrentIndex = nextIdx
				rotated = true
				break
			}
			nextIdx = (nextIdx + 1) % len(alias.Models)
		}
		if !rotated {
			return nil, fmt.Errorf("%w: alias %q (all %d model(s) endpoint-blocked)",
				ErrAllEndpointsBlocked, aliasName, len(alias.Models))
		}
		r.logger.Info("Rotated away from endpoint-blocked current model",
			"alias", aliasName,
			"new_index", health.CurrentIndex,
		)
	}

	// Alias resolution order: default reversion / alias cooldown / quota
	// blocks / endpoint blocks run above (block precedence UNCHANGED).
	// BELOW here, after every filter, the winner is re-validated and only
	// then is the session's sticky endpoint preference consulted — a
	// remembered key belonging to a HEALTHY candidate of THIS alias moves
	// to the front by OVERWRITING health.CurrentIndex with that
	// candidate's index; an unknown or BLOCKED key (quota-blocked,
	// endpoint-cooldown-blocked, or caught by the armed alias-level
	// timeout block) degrades to the winner chosen above. A preference can
	// never rescue a blocked winner: the health filters already ran on the
	// chosen index, so an all-blocked alias errors out before the hook
	// runs, and the hook's chosen index is re-validated against BOTH
	// filters. This is a READ-side preference: no rotation cursor, no
	// sticky pin, no shared-state mutation — resolution remains
	// request-scoped (WithResolvedModel), the next resolve for any other
	// session (or with no scope) is byte-identical to pre-leaf-04.
	//
	// Block precedence preserved exactly (Contract 4 / review checklist):
	// quota blocks > endpoint blocks > alias blocks > stickiness.
	if sessionID != "" {
		// NOTE: r.mu is already held here — read the map through the
		// locked helper, never through PreferredEndpoint (self-deadlock;
		// the selflock analyzer class).
		if preferredKey, ok := preferredEndpointLocked(r, sessionID); ok {
			for idx, candidate := range alias.Models {
				if EndpointKey(candidate) != preferredKey {
					continue
				}
				// The remembered endpoint belongs to a candidate of THIS
				// alias. Serve it FIRST only when it is healthy under the
				// SAME filters that gated the rotation winner above (not
				// quota-blocked, not endpoint-cooldown-blocked, not caught
				// by the armed alias-level timeout block — all three via
				// the same isQuotaBlocked / isEndpointBlocked predicates).
				blocked := (r.quotaEnabled() && r.isQuotaBlocked(health, candidate)) ||
					r.isEndpointBlocked(health, candidate)
				if !blocked {
					if idx != health.CurrentIndex {
						r.logger.Debug("Sticky session endpoint preference moving candidate to front",
							"alias", aliasName,
							"session_id", sessionID,
							"endpoint_key", preferredKey,
							"provider", candidate.ProviderID,
							"model", candidate.ModelID,
							"prev_index", health.CurrentIndex,
							"new_index", idx,
						)
						health.CurrentIndex = idx
					}
				}
				// Either way the scan ends at the FIRST alias candidate
				// carrying the key: models sharing an endpoint are
				// interchangeable from the stickiness contract's point of
				// view (EndpointKey is the identity), so the first match
				// is deterministic and preserves rotation-order stability.
				break
			}
			// No alias candidate carries the key: preference stale for
			// this alias (key belongs to a different alias only, or the
			// config changed) — the winner chosen above stands unchanged.
		}
	}

	// Return the active model — but only if it survived BOTH filters. The
	// quota pass and endpoint pass each rotate using their own predicate,
	// so sequential passes can hand the cursor back and forth: with
	// models=[A,B], A quota-blocked-only and B endpoint-blocked-only, the
	// quota pass rotates A→B and the endpoint pass rotates B→A, returning
	// the quota-blocked A (bughunt round-2 auditor 3, finding 1). Re-
	// validate the winner and keep rotating past doubly-blocked models.
	if (r.quotaEnabled() && r.isQuotaBlocked(health, alias.Models[health.CurrentIndex])) ||
		r.isEndpointBlocked(health, alias.Models[health.CurrentIndex]) {
		n := len(alias.Models)
		finalIdx := -1
		for i := range n {
			idx := (health.CurrentIndex + i) % n
			blocked := r.isEndpointBlocked(health, alias.Models[idx]) ||
				(r.quotaEnabled() && r.isQuotaBlocked(health, alias.Models[idx]))
			if !blocked {
				finalIdx = idx
				break
			}
		}
		if finalIdx < 0 {
			// Every candidate carries some block: report by precedence —
			// quota wins (documented ordering: quota > endpoint > alias).
			return nil, fmt.Errorf("%w: alias %q (all %d model(s) blocked)",
				ErrAllModelsQuotaBlocked, aliasName, n)
		}
		if finalIdx != health.CurrentIndex {
			r.logger.Info("Rotated to jointly-unblocked candidate",
				"alias", aliasName,
				"new_index", finalIdx,
			)
			health.CurrentIndex = finalIdx
		}
	}

	// Return the active model
	if health.CurrentIndex < len(alias.Models) {
		chosen := alias.Models[health.CurrentIndex]
		// Persist the routing decision for later mining (Phase 4 student-learns-routing).
		// Best-effort: swallow errors so routing observability never breaks serving.
		if r.routingLogger != nil {
			reason := "round_robin"
			if reverted {
				reason = "default_reversion"
			}
			decision := RoutingDecision{
				ChosenModelID:    chosen.ModelID,
				ChosenProviderID: chosen.ProviderID,
				Alias:            aliasName,
				Reason:           reason,
			}
			_ = r.routingLogger.Record(context.Background(), decision)
		}
		return chosen, nil
	}

	return nil, fmt.Errorf("all models in alias %q exhausted", aliasName)
}

// RememberSessionEndpoint records the endpoint (host + credential
// fingerprint, EndpointKey) that just served sessionID successfully.
// CONTRACT (master.md Contract 4, leaf 04): overwrites on EVERY success;
// no-op when sessionID == "". The single reporting seam lives in the agent
// loop's chatWithFailoverRaw success path (internal/agent/loop.go — search
// "leaf 04 reporting seam" there): it is the one choke point that sees both
// the session scope (WithTaskScope sessionID) and the serving EndpointKey
// (it holds the resolved *ModelConfig). Never call it from client-level
// success paths — one seam, not two.
//
// Storage: in-memory LRU map capped at stickySessionEndpointCap (1024)
// sessions, guarded by the Resolver mutex, never persisted; the
// least-recently-used session is evicted at cap. The endpoint key is only
// kept when it currently belongs to a HEALTHY candidate of SOME alias (not
// quota-blocked, not endpoint-blocked, not caught by an armed alias-level
// block) — an unknown or currently-blocked endpoint key is dropped rather
// than remembered as a preference (Contract 4's precedence rule).
func (r *Resolver) RememberSessionEndpoint(sessionID, endpointKey string) {
	if r == nil || sessionID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// Healthy-candidate gate: scan the aliases this key belongs to. The
	// SAME EndpointKey can serve multiple aliases (medium / thinkhard share
	// a host+credential); if ANY of them currently serves it healthy, the
	// preference is legitimate — the resolve-time gate re-checks against
	// the alias being resolved, which is the authoritative one.
	for aliasName, alias := range r.aliases {
		health := r.getOrCreateHealth(aliasName)
		if r.rememberSessionEndpointIfHealthy(alias, health, endpointKey) {
			r.rememberSessionEndpointLocked(sessionID, endpointKey)
			r.logger.Debug("Remembered session endpoint preference",
				"session_id", sessionID,
				"endpoint_key", endpointKey,
			)
			return
		}
	}
	// Unknown or fully-blocked endpoint key: drop rather than remember —
	// stickiness must never resurrect a preference for an endpoint that
	// cannot currently serve any candidate.
	r.logger.Debug("Session endpoint preference not remembered (no healthy candidate carries the key)",
		"session_id", sessionID,
		"endpoint_key", endpointKey,
	)
}

// PreferredEndpoint returns the remembered endpoint key for sessionID, if
// any. Consulted BEFORE candidate ordering during alias resolution; the
// resolve-time gate (stickyEndpointHook) re-validates health against the
// alias actually being resolved, so a stale preference degrades to normal
// ordering rather than pinning to a blocked endpoint.
func (r *Resolver) PreferredEndpoint(sessionID string) (string, bool) {
	if r == nil || sessionID == "" {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return preferredEndpointLocked(r, sessionID)
}

// preferredEndpointLocked is the map read shared by PreferredEndpoint and
// the in-resolve hook (which already holds r.mu and must not re-lock).
func preferredEndpointLocked(r *Resolver, sessionID string) (string, bool) {
	keyA, ok := r.sessionEndpoints[sessionID]
	return keyA, ok
}

// rememberSessionEndpointIfHealthy reports whether keyA belongs to a
// candidate of alias that would be served by normal ordering right now —
// i.e. the candidate exists AND is neither quota-blocked, endpoint-blocked,
// nor caught by an armed alias-level block (leaf 06 teardown consumers must
// never receive keys stickiness itself would refuse to serve). Used by the
// RememberSessionEndpoint LRU path; the resolve-time hook performs the same
// checks independently. Callers must hold r.mu.
func (r *Resolver) rememberSessionEndpointIfHealthy(alias *AliasEntry, health *AliasHealth, keyA string) bool {
	for _, m := range alias.Models {
		if EndpointKey(m) != keyA {
			continue
		}
		if r.quotaEnabled() && r.isQuotaBlocked(health, m) {
			return false
		}
		if r.isEndpointBlocked(health, m) {
			return false
		}
		return true
	}
	return false
}

// rememberSessionEndpointLocked records a successful chat completion:
// sessionID mapped to the serving endpoint key, LRU order refreshed,
// evicting the least-recently-used session when the map is at capacity
// (stickySessionEndpointCap). Overwrites refresh recency (Contract 4).
// sessionID=="" must never reach this path (guarded by RememberSessionEndpoint;
// a defensive no-op here as well). endpointKey=="" clears any stale entry —
// an unknown serving endpoint must never masquerade as a preference.
// Callers must hold r.mu.
func (r *Resolver) rememberSessionEndpointLocked(sessionID, endpointKey string) {
	if r.sessionEndpoints == nil {
		r.sessionEndpoints = make(map[string]string, stickySessionEndpointCap)
	}
	// Remove any existing LRU slot for this session. order[s]==-1 marks a
	// tombstoned (already-evicted) session; a fresh session has no entry at
	// all — the two-value map read distinguishes them (AGENTS.md two-value
	// rule; a plain lookup returns 0 for missing keys, which would slice
	// LRU[0:0] and corrupt the front of the list). The idx bounds check
	// self-heals any residual inconsistency (a session whose order entry
	// points past a list that shrank underneath it, e.g. across a cap
	// change): a stale slot is dropped and the session is re-appended
	// fresh below, so the map and the list converge instead of panicking.
	if idx, present := r.sessionEndpointOrder[sessionID]; present && idx != -1 && idx >= 0 && idx < len(r.sessionEndpointLRU) {
		if r.sessionEndpointLRU[idx] == sessionID {
			r.sessionEndpointLRU = append(r.sessionEndpointLRU[:idx], r.sessionEndpointLRU[idx+1:]...)
		}
	}
	delete(r.sessionEndpointOrder, sessionID)
	if endpointKey == "" {
		delete(r.sessionEndpoints, sessionID)
		return
	}
	r.sessionEndpoints[sessionID] = endpointKey
	r.sessionEndpointOrder[sessionID] = len(r.sessionEndpointLRU)
	r.sessionEndpointLRU = append(r.sessionEndpointLRU, sessionID)
	if len(r.sessionEndpointLRU) > stickySessionEndpointCap {
		oldest := r.sessionEndpointLRU[0]
		r.sessionEndpointLRU = r.sessionEndpointLRU[1:]
		delete(r.sessionEndpoints, oldest)
		delete(r.sessionEndpointOrder, oldest)
		r.logger.Debug("Evicted session endpoint preference (LRU cap)",
			"sessions_stored", len(r.sessionEndpoints),
		)
	}
}

// SessionEndpoints returns a mutex-safe SNAPSHOT of the session → endpoint
// map for the leaf-06 teardown coordinator (master Contract 6: "add a narrow
// resolver accessor if needed, e.g. SessionEndpoints() map[string]string
// snapshot, mutex-safe copy"). Callers get a copy; mutating it never touches
// resolver state. The map is a point-in-time view — teardown reference
// counting must tolerate concurrent successes adding entries. A nil receiver
// returns nil (typed-nil setter rule, AGENTS.md).
func (r *Resolver) SessionEndpoints() map[string]string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.sessionEndpoints))
	for sessionID, endpointKey := range r.sessionEndpoints {
		out[sessionID] = endpointKey
	}
	return out
}

// ForgetSessionEndpoint drops the sticky preference for one session
// (session-affinity integration fix: the PM failure path calls this when
// the remembered endpoint actually FAILED, so the next resolve cannot
// re-pull the dead endpoint to the front and ping-pong on it — rotation
// must make progress first; the memory re-arms on the next success via
// RememberSessionEndpoint, which also heals teardown refcounting). No-op
// on a nil receiver or empty session; an unknown session is already
// forgotten. Calls the LOCKED helper directly: the public path's
// healthy-candidate gate would reject an empty key (no candidate carries
// it) and fall through to "not remembered" without ever clearing. The
// helper's empty-key branch performs the tombstone-correct LRU slot
// removal (two-value order read + bounds self-heal) — a second
// hand-rolled removal here would diverge from that logic.
func (r *Resolver) ForgetSessionEndpoint(sessionID string) {
	if r == nil || sessionID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rememberSessionEndpointLocked(sessionID, "")
}

// inCooldown reports whether the alias-level cooldown is active at now.
// Callers must hold Resolver.mu.
func (h *AliasHealth) inCooldown(now time.Time) bool {
	return !h.CooldownUntil.IsZero() && now.Before(h.CooldownUntil)
}

// failedModelMatches reports whether the model at alias.Models[idx] is the
// one identified by the health's failed-model identity. Callers must hold
// Resolver.mu.
func (h *AliasHealth) failedModelMatches(alias *AliasEntry, idx int) bool {
	if h.FailedProviderID == "" && h.FailedModelID == "" {
		return false
	}
	if idx >= len(alias.Models) {
		return false
	}
	m := alias.Models[idx]
	return m.ProviderID == h.FailedProviderID && m.ModelID == h.FailedModelID
}

// releasePinsForModel drops every sticky pin whose model matches the given
// provider/model identity. Called when that model fails or rotates out so
// pinned callers re-pin to a healthy model on their next resolve. Callers
// must hold Resolver.mu.
func (h *AliasHealth) releasePinsForModel(alias *AliasEntry, providerID, modelID string) {
	if providerID == "" && modelID == "" {
		return
	}
	for caller, pinned := range h.StickyPins {
		// Pins store rotation indexes; resolve identity via the alias at
		// release time. Entries pointing past the list are stale and dropped.
		if pinned >= 0 && pinned < len(alias.Models) {
			m := alias.Models[pinned]
			if m.ProviderID == providerID && m.ModelID == modelID {
				delete(h.StickyPins, caller)
			}
		} else {
			delete(h.StickyPins, caller)
		}
	}
}

// resolveStickyCaller serves one ResolveForAlias call for a sticky alias.
// It must be called with r.mu held. The caller either keeps an existing pin
// (when the pinned model is healthy), or is (re-)pinned to the next healthy
// rotation position. Always returns a model.
func (r *Resolver) resolveStickyCaller(alias *AliasEntry, aliasName string, health *AliasHealth, callerKey string, now time.Time) *ModelConfig {
	if health.StickyPins == nil {
		health.StickyPins = make(map[string]int)
	}

	// Quota-aware pin selection: a pinned model whose credential is
	// quota-blocked must not be served (same semantics as the rotation
	// path below). Release the pin so the caller lands on an unblocked
	// candidate; when every candidate is blocked, the least-expired one
	// is returned so the caller's request surfaces the quota error
	// instead of deadlocking the resolve.
	r.releaseQuotaBlockedPin(alias, health)

	if pinnedIdx, ok := health.StickyPins[callerKey]; ok {
		switch {
		case pinnedIdx >= len(alias.Models):
			// Stale pin beyond the configured model list (config changed).
			delete(health.StickyPins, callerKey)
		case r.isEndpointBlocked(health, alias.Models[pinnedIdx]):
			// D10 shared fate: the pinned model's endpoint sits in a
			// timeout cooldown — release the pin so the caller re-pins
			// to a candidate on a live endpoint (bughunt round-1,
			// auditor 2 finding 4; sticky aliases previously bypassed
			// endpoint blocks entirely).
			delete(health.StickyPins, callerKey)
		case !health.inCooldown(now) || !health.failedModelMatches(alias, pinnedIdx):
			// The pinned model is still usable: either the alias is healthy
			// or the active cooldown belongs to a different model.
			return r.recordStickyDecision(alias, aliasName, pinnedIdx, "sticky_request")
		default:
			// Pinned model failed; release the pin so the caller re-pins to
			// a healthy model below.
			delete(health.StickyPins, callerKey)
		}
	}

	// (Re-)assign a pin. Start at the rotation position; if that position is
	// the model currently failing, advance once so the new pin lands on a
	// healthy candidate.
	nextIdx := health.CurrentIndex % len(alias.Models)
	if health.inCooldown(now) && health.failedModelMatches(alias, nextIdx) {
		nextIdx = (nextIdx + 1) % len(alias.Models)
	}
	// Skip quota-blocked candidates, mirroring the rotation path. If every
	// candidate is blocked, nextIdx cycles back to its start value and we
	// pin there anyway (resolve cannot return an error here).
	if r.quotaEnabled() {
		startIdx := nextIdx
		for range len(alias.Models) {
			if !r.isQuotaBlocked(health, alias.Models[nextIdx]) {
				break
			}
			nextIdx = (nextIdx + 1) % len(alias.Models)
		}
		if nextIdx == startIdx && r.isQuotaBlocked(health, alias.Models[nextIdx]) {
			r.logger.Warn("All alias models quota-blocked; serving rotation-head candidate anyway",
				"alias", aliasName,
				"model", alias.Models[nextIdx].ModelID,
			)
		}
	}
	// Skip endpoint-blocked candidates with the same one-full-lap cap
	// (bughunt round-1 sticky-caller gap): a blocked candidate never
	// wins a NEW pin while an unblocked member exists.
	if r.hasEndpointBlocks() {
		startIdx := nextIdx
		for range len(alias.Models) {
			if !r.isEndpointBlocked(health, alias.Models[nextIdx]) {
				break
			}
			nextIdx = (nextIdx + 1) % len(alias.Models)
		}
		if nextIdx == startIdx && r.isEndpointBlocked(health, alias.Models[nextIdx]) {
			r.logger.Warn("All alias models endpoint-blocked; serving rotation-head candidate anyway",
				"alias", aliasName,
				"model", alias.Models[nextIdx].ModelID,
			)
		}
	}
	health.StickyPins[callerKey] = nextIdx
	health.CurrentIndex = (nextIdx + 1) % len(alias.Models)
	return r.recordStickyDecision(alias, aliasName, nextIdx, "sticky_request_new")
}

// hasEndpointBlocks reports whether the resolver currently holds any live
// endpoint-level timeout block. Callers must hold Resolver.mu. Cheap gate so
// the sticky re-pin path skips the per-candidate scan on resolvers that
// never armed a block.
func (r *Resolver) hasEndpointBlocks() bool {
	now := r.clock()
	for _, until := range r.endpointBlocks {
		if now.Before(until) {
			return true
		}
	}
	return false
}

// releaseQuotaBlockedPin drops every sticky pin whose pinned model is
// currently quota-blocked so the pinned callers re-pin to an unblocked
// candidate. Callers must hold Resolver.mu.
func (r *Resolver) releaseQuotaBlockedPin(alias *AliasEntry, health *AliasHealth) {
	if !r.quotaEnabled() || len(alias.Models) == 0 {
		return
	}
	for caller, pinned := range health.StickyPins {
		if pinned >= 0 && pinned < len(alias.Models) && r.isQuotaBlocked(health, alias.Models[pinned]) {
			delete(health.StickyPins, caller)
		}
	}
}

// recordStickyDecision returns alias.Models[idx] and logs the routing
// decision. Callers must hold Resolver.mu.
func (r *Resolver) recordStickyDecision(alias *AliasEntry, aliasName string, idx int, reason string) *ModelConfig {
	chosen := alias.Models[idx]
	if r.routingLogger != nil {
		_ = r.routingLogger.Record(context.Background(), RoutingDecision{
			ChosenModelID:    chosen.ModelID,
			ChosenProviderID: chosen.ProviderID,
			Alias:            aliasName,
			Reason:           reason,
		})
	}
	return chosen
}

// aliasIndexOfRef returns the index of the model identified by ref within
// alias.Models, or -1 when the ref is unknown or not part of the alias.
func (r *Resolver) aliasIndexOfRef(ref string, alias *AliasEntry) int {
	mc := ResolveModelRef(ref, r.config)
	if mc == nil {
		return -1
	}
	for i, m := range alias.Models {
		if m.ProviderID == mc.ProviderID && m.ModelID == mc.ModelID {
			return i
		}
	}
	return -1
}

// candidatesJSON serializes the candidate model list as a JSON array of
// "provider/model-id" strings, for the routing decision log. Errors are
// swallowed because json.Marshal on a []string cannot fail in practice.
func (r *Resolver) candidatesJSON(candidates []*ModelConfig) string {
	ids := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.ProviderID + "/" + c.ModelID
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

// VerdictForFailure maps a failure into the leaf-01 policy vocabulary,
// adding the transport seam audit M4 requires: status-bearing errors go
// through Classify; transport timeouts (context deadline, net.Error
// timeout) reach this path WITHOUT an HTTP status/body and would otherwise
// never fire endpoint cooldowns — inverting D10's purpose. A wrapped
// FailureQuota verdict (QuotaResetError) unwraps through so the quota
// class is preserved; anything else is FailureNone (alias cooldown still
// applies, but no endpoint block).
//
// Sticky-rotation correction (session-affinity leaf 07, GAP A): the
// unwrapped status now comes from ANY *APIError in the wrap chain, not
// just a top-level one. The streaming client's short-retry exhaustion
// wraps the 503 APIError in a ClientError ("streaming failed after N
// attempts"); with the top-level-only check that failure classified
// FailureNone, armed NO endpoint block, and the resolve-time sticky hook
// re-pulled the dead endpoint to the front on every resolve — the alias
// cursor rotated to the healthy member but the next attempt went to the
// corpse again (observed end-to-end by the session-affinity rotation
// proof: A and B alternating new_index 1/0 while B served zero requests).
// Status-bearing 5xx is FailureServerError under Classify, which the
// endpoint-cooldown arm below treats like the throttle class for block
// purposes — a dead endpoint's whole base URL must lose stickiness until
// the block expires (cleared lazily by expiry + a later success, the
// same single pattern as every other block map).
func VerdictForFailure(err error) PolicyVerdict {
	if err == nil {
		return PolicyVerdict{Class: FailureNone}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return PolicyVerdict{Class: FailureThrottle, Reason: "transport_timeout"}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return PolicyVerdict{Class: FailureThrottle, Reason: "transport_timeout"}
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return Classify(apiErr.StatusCode, nil, []byte(apiErr.Detail), time.Now())
	}
	return PolicyVerdict{Class: FailureNone}
}

// RecordAliasFailure records a failure for cooldown tracking. failedModel
// identifies the model that failed; pass nil when unknown (the failure is
// then attributed to no specific model and no sticky pins are released by
// identity). Callers should pass the ModelConfig the failed request was
// served by.
//
// Failure-class routing (tree 02 leaf 04, DECISIONS.md D10): a
// FailureThrottle verdict (bare 429, transport timeout) ALSO blocks the
// failed model's whole base ENDPOINT — cross-alias, keyed by host +
// credential (EndpointKey) — for the alias timeout base (or the 30s
// default). FailureQuota never reaches this function (quota blocks are
// recorded by the agent loop); other classes only advance the alias
// cooldown as before.
//
// Quota guard (quota-reset-resilience master contract 4, bughunt
// 2026-09-04): a *QuotaResetError is NEVER an alias failure. The loop's
// quota branch is guarded, but analyzer/classifier call sites passed raw
// Chat errors through — and a QuotaResetError unwraps to a 429 APIError,
// whose Classify verdict is FailureThrottle, so quota advanced the alias
// cooldown AND armed endpoint blocks. Enforced here so every current and
// future call site is safe.
func (r *Resolver) RecordAliasFailure(aliasName string, err error, failedModel *ModelConfig) {
	if IsQuotaResetError(err) {
		r.logger.Warn("Quota error not recorded as alias failure (contract 4)",
			"alias", aliasName,
			"provider", func() string {
				if failedModel != nil {
					return failedModel.ProviderID
				}
				return ""
			}(),
		)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	health := r.getOrCreateHealth(aliasName)
	health.ConsecutiveFails++
	health.LastFailure = time.Now()

	// Record WHICH model failed. Sticky pins are released by model identity
	// at resolve time, so interleaved resolves for other models between the
	// failure and this call cannot misattribute it (issue #30).
	// Calculate cooldown with exponential backoff: timeout * 2^(fails-1)
	// Cap at 2^10 = 1024x to avoid integer overflow and astronomically large backoffs.
	alias := r.aliases[aliasName]
	if alias == nil {
		return
	}
	// Consistent-member tracking for the alias-level explicit timeout
	// (leaf Task 3): the streak only grows while the SAME member fails;
	// a different member resets it (identity check mirroring issue #30).
	// The comparison uses the PREVIOUS failure's identity, so it must run
	// before failedModel overwrites it below. An all-empty previous
	// identity means no known failure yet — the first failure can never
	// be "consistent" with itself.
	sameMember := failedModel != nil &&
		(health.FailedProviderID != "" || health.FailedModelID != "") &&
		failedModel.ProviderID == health.FailedProviderID &&
		failedModel.ModelID == health.FailedModelID
	if failedModel != nil {
		if sameMember {
			health.TimeoutStreak++
		} else {
			health.TimeoutStreak = 1
		}
		health.FailedProviderID = failedModel.ProviderID
		health.FailedModelID = failedModel.ModelID
		health.releasePinsForModel(alias, failedModel.ProviderID, failedModel.ModelID)
	} else {
		health.TimeoutStreak = 0
	}
	shift := min(health.ConsecutiveFails-1, 10)
	backoffFactor := 1 << uint(shift)
	cooldownDuration := alias.Timeout * time.Duration(backoffFactor)
	health.CooldownUntil = time.Now().Add(cooldownDuration)
	if health.TimeoutArmed && failedModel != nil && health.TimeoutStreak >= alias.MaxFails {
		// A consistent failure burst arms a block; each subsequent armed
		// block DOUBLES the duration, capped at 4x the alias's own base
		// (documented — there is no provider Options.Timeout to use as
		// the ceiling; leaf Contract 3).
		health.TimeoutBlocks++
		// Cap the shift OPERAND (not just the multiplier): at
		// TimeoutBlocks==64, 1<<63 wraps negative, min picks it, and the
		// negative blockDuration puts TimeoutBlockUntil in the past —
		// permanently disabling the block for the rest of the streak
		// (bughunt round-2 auditor 3, finding 3). Same pattern as the
		// cooldown backoff shift cap above.
		shift := min(health.TimeoutBlocks-1, 2) // 1<<2 = 4x = the multiplier cap
		multiplier := min(1<<uint(shift), 4)
		blockDuration := min(alias.Timeout*time.Duration(multiplier), 4*alias.Timeout)
		health.TimeoutBlockUntil = r.clock().Add(blockDuration)
	}

	// Endpoint-level cooldown on throttle/timeout/server-error (D10
	// primary path + GAP A sticky correction). Transport timeouts, bare
	// 429s, and — since the GAP A fix — any wrap-chain 5xx land here;
	// quota-shaped verdicts are excluded — quota has its own block maps
	// and must never short-circuit into timeout cooldowns (D4/D7).
	//
	// The 5xx arm is what makes sticky rotation follow health: a plain
	// 5xx used to arm nothing, so the resolve-time sticky hook
	// (preferredEndpointLocked) kept re-pulling the dead remembered
	// endpoint to the front and the turn ping-ponged on it (GAP A).
	// FailureServerError is Classify's verdict for any 5xx — the same
	// bounded-retry-then-move-on semantics the throttle class gets.
	if v := VerdictForFailure(err); failedModel != nil &&
		(v.Class == FailureThrottle || v.Class == FailureServerError) {
		if r.endpointBlocks == nil {
			r.endpointBlocks = make(map[string]time.Time)
		}
		key := EndpointKey(failedModel)
		r.endpointBlocks[key] = r.clock().Add(alias.Timeout)
		r.logger.Warn("Endpoint cooldown armed",
			"alias", aliasName,
			"provider", failedModel.ProviderID,
			"model", failedModel.ModelID,
			"endpoint_key", key,
			"until", r.endpointBlocks[key].Format(time.RFC3339),
		)
	}

	// Arm default-model reversion: after the cooldown window ends, the next
	// ResolveForAlias call snaps the rotation back to default_model instead
	// of leaving it parked on the fallback. Re-armed on every failure so the
	// deadline always tracks the latest (possibly extended) cooldown.
	if alias.DefaultModel != "" {
		health.RevertAt = health.CooldownUntil
	}

	r.logger.Warn("Recorded alias failure",
		"alias", aliasName,
		"consecutive_fails", health.ConsecutiveFails,
		"cooldown_until", health.CooldownUntil.Format(time.RFC3339),
		"error", err,
	)
}

// RecordAliasSuccess records an alias-wide success, resetting the failure
// counter and cooldown, and lazily deleting EXPIRED quota block entries for
// this alias (both the per-entry and per-credential maps) plus EXPIRED
// endpoint-level timeout blocks (tree 02 leaf 04 — same single
// lazy-clearing pattern; no second mechanism) to bound map growth.
// Unexpired blocks are left in place — a success on one model says nothing
// about another model's (or credential pool's) quota window.
//
// Callers that know WHICH model served the successful request must prefer
// RecordAliasSuccessModel (bughunt 2026-09-08 item 14): this alias-wide
// form releases an armed alias-level timeout block and resets the
// consecutive-failure streak without model identity, so a straggler success
// on model A can release a block that only model B's failures earned.
// It remains the correct entry point for callers that genuinely cannot
// attribute the success to a model. As of the 2026-09-10 wave there are no
// such callers left in the tree (loop.go's success path now passes
// servedModel); the alias-wide form is retained as the documented fallback
// and for future callers without identity.
func (r *Resolver) RecordAliasSuccess(aliasName string) {
	r.recordAliasSuccessInner(aliasName, nil)
}

// RecordAliasSuccessModel records a success ATTRIBUTED to a specific model
// and clears only what that model's own failure state earned (bughunt
// 2026-09-08 item 14, identity parity with RecordAliasFailure / issue #30).
// Invariants preserved (AGENTS.md quota-resilience section):
//   - lazy clear still requires expiry+success: only block entries whose
//     deadline has passed are deleted, and only when the serving model
//     reports success — an unexpired block is never lifted by any success;
//   - quota never reaches RecordAliasFailure (that guard is upstream and
//     unchanged here);
//   - ErrAllModelsQuotaBlocked / ErrAllEndpointsBlocked are untouched.
//
// The consecutive-failure streak, alias cooldown, and armed alias-level
// timeout block are cleared only when model matches the identity that most
// recently failed (FailedProviderID/FailedModelID). A success from a
// DIFFERENT model must not erase another model's earned cooldown/block.
// A nil model, or an unknown identity when no failure is recorded,
// degrades to the alias-wide clear (nothing to misattribute).
//
// The expired quota/endpoint block sweeps are run unconditionally regardless
// of identity: they only ever delete EXPIRED entries, so they never release
// an active block early — they exist to bound map growth and must not depend
// on which model happened to succeed.
func (r *Resolver) RecordAliasSuccessModel(aliasName string, model *ModelConfig) {
	r.recordAliasSuccessInner(aliasName, model)
}

// recordAliasSuccessInner is the shared implementation: model==nil is the
// alias-wide clear (legacy callers without identity), model!=nil is the
// identity-gated clear. Callers must hold no expectations about r.mu —
// it is taken here.
func (r *Resolver) recordAliasSuccessInner(aliasName string, model *ModelConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	health := r.getOrCreateHealth(aliasName)

	// Identity gate (issue #30 parity): reset the streak and release the
	// earned cooldown / armed alias-level timeout block ONLY when the
	// succeeding model matches the identity that most recently failed.
	// model==nil keeps the alias-wide clear for callers without identity.
	if model == nil ||
		(health.FailedProviderID == "" && health.FailedModelID == "") ||
		(model.ProviderID == health.FailedProviderID && model.ModelID == health.FailedModelID) {
		health.ConsecutiveFails = 0
		health.CooldownUntil = time.Time{} // Reset cooldown
		// A success ends any consistent-failure streak and releases an armed
		// alias-level explicit-timeout block (tree 02 leaf 04).
		health.TimeoutStreak = 0
		health.TimeoutBlocks = 0
		health.TimeoutBlockUntil = time.Time{}
	}

	now := r.clock()
	for key, blockedUntil := range health.entryBlocks {
		if now.After(blockedUntil) {
			delete(health.entryBlocks, key)
		}
	}
	for key, blockedUntil := range health.credentialBlock {
		if now.After(blockedUntil) {
			delete(health.credentialBlock, key)
		}
	}
	r.sweepExpiredEndpointBlocks()
}

// getOrCreateHealth returns the health tracking for an alias, creating it if needed.
func (r *Resolver) getOrCreateHealth(aliasName string) *AliasHealth {
	health, ok := r.health[aliasName]
	if !ok {
		health = &AliasHealth{
			CurrentIndex:     0,
			ConsecutiveFails: 0,
			entryBlocks:      make(map[string]time.Time),
			credentialBlock:  make(map[string]time.Time),
		}
		// Alias-level explicit-timeout blocks arm ONLY when the alias
		// config declared timeout: (D10 opt-in rule, leaf Task 3).
		if alias := r.aliases[aliasName]; alias != nil {
			health.TimeoutArmed = alias.timeoutArmed
		}
		r.health[aliasName] = health
	}
	return health
}

// HasAlias checks if an alias exists.
func (r *Resolver) HasAlias(aliasName string) bool {
	_, ok := r.aliases[aliasName]
	return ok
}

// ResolveModelTier resolves a model tier name to a concrete model ID.
// Known tiers:
//   - "fast": resolves to the configured small/fast model (SmallModel).
//   - "default": resolves to the default model.
//
// If the tier is empty, unknown, or the tier's model is not configured,
// the fallback model ID is returned. If fallback is also empty, an empty
// string is returned (caller should use its own default).
func (r *Resolver) ResolveModelTier(tier string, fallback string) string {
	switch tier {
	case "fast":
		if r.smallModel != nil {
			return r.smallModel.ProviderID + "/" + r.smallModel.ModelID
		}
	case "default":
		if r.defaultModel != nil {
			return r.defaultModel.ProviderID + "/" + r.defaultModel.ModelID
		}
	}
	return fallback
}

// HasHealthyModels reports whether an alias has at least one model that can
// serve a request right now. A model is considered healthy if:
//   - It is the currently active model AND not in cooldown, OR
//   - It is a non-current model (always available for rotation, per the
//     ResolveForAlias rotation semantics)
//
// Because non-current models are always considered available, this function
// HasHealthyModels checks if the alias has at least one model that is not
// in cooldown and not quota-blocked.
func (r *Resolver) HasHealthyModels(aliasName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	alias, ok := r.aliases[aliasName]
	if !ok {
		return false
	}
	if len(alias.Models) == 0 {
		return false
	}

	health := r.getOrCreateHealth(aliasName)

	// If there is more than one model, a non-current model is always available
	// for rotation — short-circuit.
	if len(alias.Models) > 1 {
		// But check if ALL models are quota-blocked
		if r.quotaEnabled() {
			for _, m := range alias.Models {
				if !r.isQuotaBlocked(health, m) {
					return true
				}
			}
			return false
		}
		return true
	}

	// Single-model alias: healthy only if not in cooldown and not quota-blocked.
	now := time.Now()
	if !health.CooldownUntil.IsZero() && now.Before(health.CooldownUntil) {
		return false
	}
	if r.quotaEnabled() && len(alias.Models) > 0 {
		return !r.isQuotaBlocked(health, alias.Models[health.CurrentIndex])
	}
	return true
}

// RotateToNextModel forces rotation to the next model in an alias and resets failure counters.
// Returns the new model config after rotation.
//
// Lock discipline: the entire rotation runs under the single Resolver.mu
// regime, so cursor mutation is atomic — no torn reads between the index
// check and the write (stress-verified by
// TestResolver_RotateToNextModel_ConcurrentStress under -race).
//
// Concurrent-session design limitation: the rotation cursor
// (AliasHealth.CurrentIndex) is PER-ALIAS, not per-session or per-caller.
// Two concurrent sessions resolving the same alias share one cursor, so a
// rotation issued by session A moves the next resolve for every other
// un-pinned session on that alias. This is accepted, documented behavior —
// rotation is best-effort failover, and callers must treat the returned
// model as "a healthy candidate now", not "a stable model across calls".
// Callers that need a stable model use sticky pins
// (BalancedStickyRequests + a callerKey to ResolveForAlias), which pin
// per-caller and are released by model identity on rotation.
func (r *Resolver) RotateToNextModel(aliasName string) (*ModelConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	alias, ok := r.aliases[aliasName]
	if !ok {
		return nil, fmt.Errorf("alias not found: %s", aliasName)
	}

	if len(alias.Models) == 0 {
		return nil, fmt.Errorf("alias %q has no models", aliasName)
	}

	health := r.getOrCreateHealth(aliasName)

	// Release sticky pins on the model being rotated out so pinned callers
	// re-pin to a healthy model on their next resolve. A forced rotation
	// leaves this model regardless of failure state, so release by identity
	// unconditionally.
	if out := alias.Models[health.CurrentIndex]; out != nil {
		health.releasePinsForModel(alias, out.ProviderID, out.ModelID)
	}

	// Rotate to the next model, skipping quota-blocked candidates. When
	// every candidate is blocked, restore the original index and fail with
	// ErrAllModelsQuotaBlocked (leaf 03 Task 2 semantics).
	prevIdx := health.CurrentIndex
	nextIdx := (health.CurrentIndex + 1) % len(alias.Models)
	rotated := false
	for range len(alias.Models) {
		if !r.isQuotaBlocked(health, alias.Models[nextIdx]) {
			rotated = true
			break
		}
		nextIdx = (nextIdx + 1) % len(alias.Models)
	}
	if !rotated {
		health.CurrentIndex = prevIdx
		return nil, fmt.Errorf("%w: alias %q (all %d model(s) quota-blocked)",
			ErrAllModelsQuotaBlocked, aliasName, len(alias.Models))
	}

	health.CurrentIndex = nextIdx
	health.ConsecutiveFails = 0
	health.CooldownUntil = time.Time{}
	health.LastFailure = time.Time{}

	newModel := alias.Models[health.CurrentIndex]

	r.logger.Info("Manually rotated to next model in alias",
		"alias", aliasName,
		"new_model", newModel.ModelID,
		"new_index", health.CurrentIndex,
	)

	return newModel, nil
}

// GetAliasHealth returns the current health status for an alias.
func (r *Resolver) GetAliasHealth(aliasName string) (currentIndex int, consecutiveFails int, cooldownUntil time.Time, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	health, ok := r.health[aliasName]
	if !ok {
		return 0, 0, time.Time{}, false
	}
	return health.CurrentIndex, health.ConsecutiveFails, health.CooldownUntil, true
}

// EndpointBlockUntil reports the CURRENT endpoint-level block deadline for
// the endpoint identified by cfg (EndpointKey: base-URL host + credential),
// for the agent loop's timeout-parking path (tree 03): a turn that failed on
// a timeout-class error against this endpoint parks until the block expires.
// The read applies the same lazy-expiry rule as resolution (isEndpointBlocked):
// an expired block reports zero — the endpoint is prober-eligible again and
// the caller must not park on a stale deadline. Callers should verify
// liveness with EndpointBlocked before parking; a zero result never blocks
// anything. A nil cfg reports zero.
func (r *Resolver) EndpointBlockUntil(cfg *ModelConfig) time.Time {
	if r == nil || cfg == nil {
		return time.Time{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.endpointBlocks[EndpointKey(cfg)]
	if !ok || !r.clock().Before(until) {
		return time.Time{}
	}
	return until
}

// EndpointBlocked reports whether the endpoint identified by cfg (EndpointKey
// identity) is currently under a timeout cooldown (D10). It is the liveness
// check paired with EndpointBlockUntil: park only when BOTH report the
// block.
func (r *Resolver) EndpointBlocked(cfg *ModelConfig) bool {
	return !r.EndpointBlockUntil(cfg).IsZero()
}

// GetAllModelsForAlias returns all models configured for an alias.
func (r *Resolver) GetAllModelsForAlias(aliasName string) ([]*ModelConfig, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	alias, ok := r.aliases[aliasName]
	if !ok {
		return nil, false
	}
	// Return a copy to prevent modification
	models := make([]*ModelConfig, len(alias.Models))
	copy(models, alias.Models)
	return models, true
}

// SetPricingSyncer sets the pricing syncer for live cost enrichment on resolved models.
func (r *Resolver) SetPricingSyncer(ps *PricingSyncer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pricingSyncer = ps
	if ps == nil {
		return
	}

	// Enrich all models with live pricing
	enrich := func(m *ModelConfig) {
		if m == nil {
			return
		}
		key := m.ProviderID + "/" + m.ModelID
		if price := ps.GetPrice(key); price != nil {
			m.CostPerMillionInput = price.InputCost
			m.CostPerMillionOutput = price.OutputCost
		}
	}

	for _, m := range r.allModels {
		enrich(m)
	}
	for _, alias := range r.aliases {
		for _, m := range alias.Models {
			enrich(m)
		}
	}
	enrich(r.defaultModel)
	enrich(r.smallModel)
	enrich(r.decisionModel)
}

// SetContextLimits applies discovered context lengths to the resolver's
// model pointer sets under r.mu (audit R3 — there is no separate mutable
// registry: r.allModels, alias members, defaultModel and smallModel are
// the pointers per-call modelConfigFrom copies and ResolveForAlias hand
// out, so mutating them is what makes the runtime ContextLimit consumer —
// context_firewall.go's budget math on ModelConfig.ContextLimit — observe
// fresh values). Precedence per master Contract 3:
//  1. an explicit models.json5 context_limit wins ALWAYS — on ModelConfig
//     a non-zero ContextLimit can only come from json5 (modelConfigFrom
//     never consults the static catalog), so `!= 0` marks explicit;
//  2. a 0/absent value accepts the discovered value; when the model's own
//     value is 0 but the display catalog carries one (catalog-only cell),
//     the catalog value is treated as current;
//  3. `override` (allow_context_override) additionally lets a discovered
//     value replace that non-zero value.
func (r *Resolver) SetContextLimits(discovered map[string]int, override bool, logger *slog.Logger) {
	if len(discovered) == 0 {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// catalogContexts snapshots the display catalog's current values for
	// this pass, so the "catalog-only" precedence cell can be evaluated
	// even when the model's own ContextLimit is 0: modelConfigFrom never
	// consults the catalog, so a zero on ModelConfig with a non-zero
	// catalog entry means the effective current value is the catalog's.
	catalogContexts := make(map[string]int, len(discovered))
	for key := range discovered {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		if entry, ok := GetModel(parts[0], parts[1]); ok {
			catalogContexts[key] = entry.ContextWindow
		}
	}

	apply := func(m *ModelConfig) {
		if m == nil {
			return
		}
		key := m.ProviderID + "/" + m.ModelID
		discoveredVal, ok := discovered[key]
		if !ok {
			return
		}
		if m.ContextLimit != 0 {
			// Rule 1: an explicit models.json5 context_limit is
			// authoritative in EVERY override mode. modelConfigFrom never
			// consults the static catalog, so a non-zero ContextLimit on
			// ModelConfig can only be json5-explicit — override never
			// lifts this.
			return
		}
		// The model's own value is 0/absent: evaluate the rule against
		// the catalog-only cell (catalog value as current, 0 when the
		// catalog has none) so override governs exactly the non-zero
		// CATALOG case per Contract 3.
		newVal, changed := resolvedContext(catalogContexts[key], discoveredVal, override)
		if !changed {
			return
		}
		logger.Info("context window updated", "provider", m.ProviderID, "model", m.ModelID,
			"from", m.ContextLimit, "to", newVal)
		m.ContextLimit = newVal
	}

	for _, m := range r.allModels {
		apply(m)
	}
	for _, alias := range r.aliases {
		for _, m := range alias.Models {
			apply(m)
		}
	}
	apply(r.defaultModel)
	apply(r.smallModel)
	apply(r.decisionModel)
}

// quotaEntryKey returns a unique key for a provider/model pair.
func quotaEntryKey(providerID, modelID string) string {
	return providerID + "|" + modelID
}

// isQuotaBlocked checks if a model is currently quota-blocked.
// Callers must hold Resolver.mu.
func (r *Resolver) isQuotaBlocked(health *AliasHealth, m *ModelConfig) bool {
	if health == nil || m == nil {
		return false
	}
	entryKey := quotaEntryKey(m.ProviderID, m.ModelID)
	blockedUntil, hasEntry := health.entryBlocks[entryKey]
	if hasEntry && time.Now().Before(blockedUntil) {
		return true
	}
	// Check credential-level block
	credKey := QuotaCredentialKey(m.ProviderID, m)
	if blockedUntil, ok := health.credentialBlock[credKey]; ok && time.Now().Before(blockedUntil) {
		return true
	}
	return false
}

// isEndpointBlocked reports whether a model is excluded by an endpoint-level
// timeout cooldown (Resolver-wide endpointBlocks map, keyed by
// EndpointKey) or by an armed alias-level explicit-timeout block (tree 02
// leaf 04, DECISIONS.md D10). Blocks are consulted LAZILY: an expired entry
// simply no longer matches (and is swept by the next RecordAliasSuccess,
// mirroring the quota-block clearing pattern — there is no second clearing
// mechanism). Callers must hold Resolver.mu.
func (r *Resolver) isEndpointBlocked(health *AliasHealth, m *ModelConfig) bool {
	if m == nil {
		return false
	}
	now := r.clock()
	if health != nil && health.TimeoutArmed && now.Before(health.TimeoutBlockUntil) {
		// The alias-level explicit-timeout block covers every member.
		return true
	}
	blockedUntil, ok := r.endpointBlocks[EndpointKey(m)]
	return ok && now.Before(blockedUntil)
}

// sweepExpiredEndpointBlocks lazily deletes EXPIRED endpoint-block entries
// to bound map growth, mirroring RecordAliasSuccess's quota-block sweep.
// Callers must hold Resolver.mu.
func (r *Resolver) sweepExpiredEndpointBlocks() {
	now := r.clock()
	for key, blockedUntil := range r.endpointBlocks {
		if now.After(blockedUntil) {
			delete(r.endpointBlocks, key)
		}
	}
}

// clock returns the injected time source (SHARED-CONVENTIONS §5); nil
// field means the wall clock.
func (r *Resolver) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// BlockQuotaEntry records a quota block for a provider/model pair.
func (r *Resolver) BlockQuotaEntry(aliasName, providerID, modelID string, unblockAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	health := r.getOrCreateHealth(aliasName)
	health.entryBlocks[quotaEntryKey(providerID, modelID)] = unblockAt
}

// BlockQuotaCredential records a quota block for a credential key.
func (r *Resolver) BlockQuotaCredential(aliasName, credentialKey string, unblockAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	health := r.getOrCreateHealth(aliasName)
	health.credentialBlock[credentialKey] = unblockAt
}

// ClearQuotaBlocks clears all quota blocks for an alias.
func (r *Resolver) ClearQuotaBlocks(aliasName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	health := r.getOrCreateHealth(aliasName)
	health.entryBlocks = make(map[string]time.Time)
	health.credentialBlock = make(map[string]time.Time)
}

// QuotaBlockedUntil returns the earliest unblock time for a credential key across all aliases.
func (r *Resolver) QuotaBlockedUntil(credentialKey string) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	earliest := time.Time{}
	for _, health := range r.health {
		if blockedUntil, ok := health.credentialBlock[credentialKey]; ok {
			if earliest.IsZero() || blockedUntil.Before(earliest) {
				earliest = blockedUntil
			}
		}
	}
	return earliest
}

// ActiveQuotaBlocks returns all active quota block statuses.
func (r *Resolver) ActiveQuotaBlocks() []QuotaBlockStatus {
	r.mu.Lock()
	defer r.mu.Unlock()

	var blocks []QuotaBlockStatus
	for aliasName, health := range r.health {
		for entryKey, blockedUntil := range health.entryBlocks {
			if time.Now().Before(blockedUntil) {
				parts := strings.SplitN(entryKey, "|", 2)
				if len(parts) == 2 {
					blocks = append(blocks, QuotaBlockStatus{
						AliasName:  aliasName,
						ProviderID: parts[0],
						ModelID:    parts[1],
						ResetAt:    blockedUntil,
						Remaining:  time.Until(blockedUntil),
					})
				}
			}
		}
		for credKey, blockedUntil := range health.credentialBlock {
			if time.Now().Before(blockedUntil) {
				blocks = append(blocks, QuotaBlockStatus{
					AliasName:     aliasName,
					CredentialKey: credKey,
					ResetAt:       blockedUntil,
					Remaining:     time.Until(blockedUntil),
				})
			}
		}
	}
	return blocks
}
