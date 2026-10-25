package daemon

import (
	"io"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func discardTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func multiModelProviderCfg(defaultModel string) *config.ModelsConfig {
	return &config.ModelsConfig{
		Providers: map[string]config.Provider{
			"agnes": {
				API: "openai",
				Options: config.ProviderOptions{
					BaseURL:      "https://example.test/v1",
					APIKey:       "key-1",
					DefaultModel: defaultModel,
				},
				Models: map[string]config.Model{
					"agnes-2.5-flash": {Name: "agnes-2.5-flash"},
					"agnes-2.0-flash": {Name: "agnes-2.0-flash"},
					"agnes-3.0-flash": {Name: "agnes-3.0-flash"},
				},
			},
		},
	}
}

// TestBuildProviderConfigs_DeterministicSortedFirst pins the BUG-1 fix: a
// multi-model provider WITHOUT a defaultModel option picks the sorted-first
// model id deterministically (the old range-over-map + break was random).
func TestBuildProviderConfigs_DeterministicSortedFirst(t *testing.T) {
	logger := discardTestLogger()
	cfg := multiModelProviderCfg("")

	for i := range 20 {
		got := buildProviderConfigs(cfg, logger, nil)
		if len(got) != 1 {
			t.Fatalf("run %d: len(configs) = %d, want 1", i, len(got))
		}
		if got[0].ModelID != "agnes-2.0-flash" {
			t.Fatalf("run %d: ModelID = %q, want sorted-first %q", i, got[0].ModelID, "agnes-2.0-flash")
		}
	}
}

// TestBuildProviderConfigs_DefaultModelPreference pins the defaultModel
// option: when the provider's defaultModel names an existing model, that
// model is registered.
func TestBuildProviderConfigs_DefaultModelPreference(t *testing.T) {
	logger := discardTestLogger()
	cfg := multiModelProviderCfg("agnes-2.5-flash")

	got := buildProviderConfigs(cfg, logger, nil)
	if len(got) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(got))
	}
	if got[0].ModelID != "agnes-2.5-flash" {
		t.Errorf("ModelID = %q, want the defaultModel %q", got[0].ModelID, "agnes-2.5-flash")
	}
	if got[0].ProviderID != "agnes" || got[0].BaseURL != "https://example.test/v1" || got[0].APIKey != "key-1" {
		t.Errorf("provider fields not carried: %+v", got[0])
	}
}

// TestBuildProviderConfigs_DefaultModelMissingFallsBackSorted pins the
// fallback: a defaultModel naming a model NOT in provider.Models falls back
// to the sorted-first pick and still registers the provider.
func TestBuildProviderConfigs_DefaultModelMissingFallsBackSorted(t *testing.T) {
	logger := discardTestLogger()
	cfg := multiModelProviderCfg("agnes-9.9-nonexistent")

	got := buildProviderConfigs(cfg, logger, nil)
	if len(got) != 1 {
		t.Fatalf("len(configs) = %d, want 1 (provider must still register)", len(got))
	}
	if got[0].ModelID != "agnes-2.0-flash" {
		t.Errorf("ModelID = %q, want sorted-first fallback %q", got[0].ModelID, "agnes-2.0-flash")
	}
}
