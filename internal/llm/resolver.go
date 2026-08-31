package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	allModels     []*ModelConfig
	aliases       map[string]*AliasEntry
	health        map[string]*AliasHealth
	pricingSyncer *PricingSyncer
	routingLogger *RoutingLogger
	mu            sync.Mutex
	logger        *slog.Logger
	// quotaCfg holds quota-aware retry settings. Nil disables quota blocking.
	quotaCfg *QuotaWaitConfig
}

// NewResolver creates a new model resolver.
func NewResolver(cfg *ProvidersConfig, logger *slog.Logger) *Resolver {
	if logger == nil {
		logger = slog.Default()
	}

	r := &Resolver{
		config:    cfg,
		allModels: GetAllModels(cfg),
		aliases:   make(map[string]*AliasEntry),
		health:    make(map[string]*AliasHealth),
		logger:    logger,
	}

	// Resolve default and small models
	if cfg.Model != "" {
		r.defaultModel = ResolveModelRef(cfg.Model, cfg)
	}
	if cfg.SmallModel != "" {
		r.smallModel = ResolveModelRef(cfg.SmallModel, cfg)
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
			if timeout == 0 {
				timeout = 30 * time.Second // Default timeout
			}
			maxFails := aliasEntry.MaxFails
			if maxFails == 0 {
				maxFails = 3 // Default max fails
			}
			r.aliases[aliasName] = &AliasEntry{
				Models:                 models,
				Timeout:                timeout,
				MaxFails:               maxFails,
				DefaultModel:           aliasEntry.DefaultModel,
				BalancedStickyRequests: aliasEntry.BalancedStickyRequests,
			}
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
func (r *Resolver) ResolveForAlias(aliasName string, callerKey string) (*ModelConfig, error) {
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
	if alias.BalancedStickyRequests && callerKey != "" {
		return r.resolveStickyCaller(alias, aliasName, health, callerKey, now), nil
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
			for i := 0; i < len(alias.Models); i++ {
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

	if pinnedIdx, ok := health.StickyPins[callerKey]; ok {
		switch {
		case pinnedIdx >= len(alias.Models):
			// Stale pin beyond the configured model list (config changed).
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
	health.StickyPins[callerKey] = nextIdx
	health.CurrentIndex = (nextIdx + 1) % len(alias.Models)
	return r.recordStickyDecision(alias, aliasName, nextIdx, "sticky_request_new")
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

// RecordAliasFailure records a failure for cooldown tracking. failedModel
// identifies the model that failed; pass nil when unknown (the failure is
// then attributed to no specific model and no sticky pins are released by
// identity). Callers should pass the ModelConfig the failed request was
// served by.
func (r *Resolver) RecordAliasFailure(aliasName string, err error, failedModel *ModelConfig) {
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
	if failedModel != nil {
		health.FailedProviderID = failedModel.ProviderID
		health.FailedModelID = failedModel.ModelID
		health.releasePinsForModel(alias, failedModel.ProviderID, failedModel.ModelID)
	}
	shift := min(health.ConsecutiveFails-1, 10)
	backoffFactor := 1 << uint(shift)
	cooldownDuration := alias.Timeout * time.Duration(backoffFactor)
	health.CooldownUntil = time.Now().Add(cooldownDuration)

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

// RecordAliasSuccess records a success, resetting failure counter and
// lazily deleting EXPIRED quota block entries for this alias (both the
// per-entry and per-credential maps) to bound map growth. Unexpired blocks
// are left in place — a success on one model says nothing about another
// model's (or the credential pool's) quota window.
func (r *Resolver) RecordAliasSuccess(aliasName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	health := r.getOrCreateHealth(aliasName)
	health.ConsecutiveFails = 0
	health.CooldownUntil = time.Time{} // Reset cooldown

	now := time.Now()
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
	for i := 0; i < len(alias.Models); i++ {
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
