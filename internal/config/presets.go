// Package config provides configuration loading and validation for meept.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Preset name constants used throughout the codebase.
const (
	PresetDevelopment = "development"
	PresetDebugging   = "debugging"
	PresetPlanning    = "planning"
	PresetCreative    = "creative"
	PresetResearch    = "research"
)

// PresetConfig represents the presets configuration.
type PresetConfig struct {
	Presets map[string]*ModelPreset `json:"presets"`
	Default string                  `json:"default"`
}

// ModelPreset represents a model preset with parameters.
type ModelPreset struct {
	Label       string      `json:"label"`
	Description string      `json:"description"`
	Params      ModelParams `json:"params"`
}

// ModelParams holds model generation parameters.
type ModelParams struct {
	Temperature      float64 `json:"temperature,omitempty"`
	TopP             float64 `json:"top_p,omitempty"`
	FrequencyPenalty float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64 `json:"presence_penalty,omitempty"`
	MaxTokens        int     `json:"max_tokens,omitempty"`
}

// LoadPresetsConfig loads model presets from a JSON5 file.
func LoadPresetsConfig(path string) (*PresetConfig, error) {
	var cfg PresetConfig
	if err := LoadJSON5(path, &cfg); err != nil {
		if os.IsNotExist(err) {
			return DefaultPresetsConfig(), nil
		}
		return nil, fmt.Errorf("failed to load presets config: %w", err)
	}

	// Set default preset if not specified
	if cfg.Default == "" {
		cfg.Default = PresetDevelopment
	}

	return &cfg, nil
}

// LoadPresetsConfigDefault loads presets from default locations.
func LoadPresetsConfigDefault() (*PresetConfig, error) {
	// Try user override first
	homeDir, err := os.UserHomeDir()
	if err == nil {
		userPath := filepath.Join(homeDir, ".meept", "presets.json5")
		if _, err := os.Stat(userPath); err == nil {
			return LoadPresetsConfig(userPath)
		}
	}

	// Try project-local
	if _, err := os.Stat("config/presets.json5"); err == nil {
		return LoadPresetsConfig("config/presets.json5")
	}

	// Return defaults
	return DefaultPresetsConfig(), nil
}

// DefaultPresetsConfig returns the default preset configuration.
func DefaultPresetsConfig() *PresetConfig {
	return &PresetConfig{
		Default: PresetDevelopment,
		Presets: map[string]*ModelPreset{
			PresetDevelopment: {
				Label:       "Development",
				Description: "Balanced for coding tasks",
				Params: ModelParams{
					Temperature:      0.3,
					TopP:             0.9,
					FrequencyPenalty: 0.0,
					PresencePenalty:  0.0,
				},
			},
			PresetDebugging: {
				Label:       "Debugging",
				Description: "Methodical troubleshooting",
				Params: ModelParams{
					Temperature:      0.2,
					TopP:             0.85,
					FrequencyPenalty: 0.0,
					PresencePenalty:  0.0,
				},
			},
			PresetPlanning: {
				Label:       "Planning",
				Description: "Structured thinking",
				Params: ModelParams{
					Temperature:      0.4,
					TopP:             0.9,
					FrequencyPenalty: 0.0,
					PresencePenalty:  0.0,
				},
			},
			PresetCreative: {
				Label:       "Creative Writing",
				Description: "High creativity mode",
				Params: ModelParams{
					Temperature:      0.9,
					TopP:             0.95,
					FrequencyPenalty: 0.5,
					PresencePenalty:  0.5,
				},
			},
			PresetResearch: {
				Label:       "Research",
				Description: "Analytical and thorough",
				Params: ModelParams{
					Temperature:      0.5,
					TopP:             0.9,
					FrequencyPenalty: 0.0,
					PresencePenalty:  0.0,
				},
			},
		},
	}
}

// ApplyPreset applies a preset to a model configuration.
func (p *PresetConfig) ApplyPreset(model *Model, presetName string) error {
	if presetName == "" {
		presetName = p.Default
	}

	preset, ok := p.Presets[presetName]
	if !ok {
		return fmt.Errorf("preset not found: %s", presetName)
	}

	// CORE-5 FIX: Always apply preset parameters unconditionally.
	// Previously, the code checked `> 0` or `!= 0` which caused preset
	// values of 0 (e.g., TopP=0, FrequencyPenalty=0) to be silently
	// ignored, leaving the model with its (potentially default) values
	// instead of the explicit zeros specified by the preset.
	model.Temperature = preset.Params.Temperature
	model.TopP = preset.Params.TopP
	model.FrequencyPenalty = preset.Params.FrequencyPenalty
	model.PresencePenalty = preset.Params.PresencePenalty

	return nil
}

// GetPreset returns a specific preset by name.
func (p *PresetConfig) GetPreset(name string) (*ModelPreset, error) {
	if name == "" {
		name = p.Default
	}

	preset, ok := p.Presets[name]
	if !ok {
		return nil, fmt.Errorf("preset not found: %s", name)
	}

	return preset, nil
}

// ListPresets returns all available preset names.
func (p *PresetConfig) ListPresets() []string {
	names := make([]string, 0, len(p.Presets))
	for name := range p.Presets {
		names = append(names, name)
	}
	return names
}
