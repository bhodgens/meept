package llm

import (
	"fmt"
	"log/slog"
	"sort"
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

// SkillRequirements defines the capability requirements for a skill.
type SkillRequirements struct {
	Name     string
	Requires []string
}

// Resolver resolves model selection based on capability matching.
type Resolver struct {
	config       *ProvidersConfig
	defaultModel *ModelConfig
	smallModel   *ModelConfig
	allModels    []*ModelConfig
	aliases      map[string]*AliasEntry
	health       map[string]*AliasHealth
	pricingSyncer *PricingSyncer
	mu           sync.Mutex
	logger       *slog.Logger
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
				Models:   models,
				Timeout:  timeout,
				MaxFails: maxFails,
			}
		}
	}

	return r
}

// DefaultModel returns the default model configuration.
func (r *Resolver) DefaultModel() *ModelConfig {
	return r.defaultModel
}

// SmallModel returns the small/fast model configuration.
func (r *Resolver) SmallModel() *ModelConfig {
	return r.smallModel
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

	return selected, nil
}

// ResolveRef resolves a "provider/model-id" reference.
func (r *Resolver) ResolveRef(ref string) *ModelConfig {
	return ResolveModelRef(ref, r.config)
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
func (r *Resolver) ResolveForAlias(aliasName string) (*ModelConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	alias, ok := r.aliases[aliasName]
	if !ok {
		return nil, fmt.Errorf("alias not found: %s", aliasName)
	}

	health := r.getOrCreateHealth(aliasName)

	// Check if current model is in cooldown
	now := time.Now()
	if !health.CooldownUntil.IsZero() && now.Before(health.CooldownUntil) {
		// Iterate through all models in the alias to find one that isn't in cooldown.
		// We must NOT rely on health.CooldownUntil to become zero (rotateToNext resets it),
		// so we track remaining candidates explicitly.
		startIdx := (health.CurrentIndex + 1) % len(alias.Models)
		for i := 0; i < len(alias.Models)-1; i++ {
			nextIdx := (startIdx + i) % len(alias.Models)
			if nextIdx == health.CurrentIndex {
				break // wrapped around back to original
			}
			health.CurrentIndex = nextIdx
			health.ConsecutiveFails = 0
			health.CooldownUntil = time.Time{} // Reset cooldown for this candidate
			health = r.getOrCreateHealth(aliasName)
			// Check if this model is now out of cooldown
			if health.CooldownUntil.IsZero() || now.After(health.CooldownUntil) {
				break // found a healthy model
			}
		}
	}

	// Return the active model
	if health.CurrentIndex < len(alias.Models) {
		return alias.Models[health.CurrentIndex], nil
	}

	return nil, fmt.Errorf("all models in alias %q exhausted", aliasName)
}

// RecordAliasFailure records a failure for cooldown tracking.
func (r *Resolver) RecordAliasFailure(aliasName string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	health := r.getOrCreateHealth(aliasName)
	health.ConsecutiveFails++
	health.LastFailure = time.Now()

	// Calculate cooldown with exponential backoff: timeout * 2^(fails-1)
	// Cap at 2^10 = 1024x to avoid integer overflow and astronomically large backoffs.
	alias := r.aliases[aliasName]
	if alias == nil {
		return
	}
	shift := health.ConsecutiveFails - 1
	if shift > 10 {
		shift = 10
	}
	backoffFactor := 1 << uint(shift)
	cooldownDuration := alias.Timeout * time.Duration(backoffFactor)
	health.CooldownUntil = time.Now().Add(cooldownDuration)

	r.logger.Warn("Recorded alias failure",
		"alias", aliasName,
		"consecutive_fails", health.ConsecutiveFails,
		"cooldown_until", health.CooldownUntil.Format(time.RFC3339),
		"error", err,
	)
}

// RecordAliasSuccess records a success, resetting failure counter.
func (r *Resolver) RecordAliasSuccess(aliasName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	health := r.getOrCreateHealth(aliasName)
	health.ConsecutiveFails = 0
	health.CooldownUntil = time.Time{} // Reset cooldown
}

// getOrCreateHealth returns the health tracking for an alias, creating it if needed.
func (r *Resolver) getOrCreateHealth(aliasName string) *AliasHealth {
	health, ok := r.health[aliasName]
	if !ok {
		health = &AliasHealth{
			CurrentIndex:     0,
			ConsecutiveFails: 0,
		}
		r.health[aliasName] = health
	}
	return health
}

func (r *Resolver) rotateToNext(aliasName string, alias *AliasEntry) {
	health := r.health[aliasName]
	health.CurrentIndex = (health.CurrentIndex + 1) % len(alias.Models)
	health.ConsecutiveFails = 0
	health.CooldownUntil = time.Time{} // Reset cooldown

	r.logger.Info("Rotated to next model in alias",
		"alias", aliasName,
		"new_model", alias.Models[health.CurrentIndex].ModelID,
		"new_index", health.CurrentIndex,
	)
}

// HasAlias checks if an alias exists.
func (r *Resolver) HasAlias(aliasName string) bool {
	_, ok := r.aliases[aliasName]
	return ok
}

// HasHealthyModels checks if an alias has any models that are not in cooldown.
func (r *Resolver) HasHealthyModels(aliasName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	alias, ok := r.aliases[aliasName]
	if !ok {
		return false
	}

	now := time.Now()
	for i := range alias.Models {
		health := r.getOrCreateHealth(aliasName)
		// Check if this model is the current one and not in cooldown
		if i == health.CurrentIndex {
			if health.CooldownUntil.IsZero() || now.After(health.CooldownUntil) {
				return true
			}
		} else {
			// Non-current models are always available for rotation
			return true
		}
	}
	return false
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

	// Rotate to next model
	health.CurrentIndex = (health.CurrentIndex + 1) % len(alias.Models)
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
