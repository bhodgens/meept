package llm

import (
	"fmt"
	"log/slog"
	"sort"
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
		logger:    logger,
	}

	// Resolve default and small models
	if cfg.Model != "" {
		r.defaultModel = ResolveModelRef(cfg.Model, cfg)
	}
	if cfg.SmallModel != "" {
		r.smallModel = ResolveModelRef(cfg.SmallModel, cfg)
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
