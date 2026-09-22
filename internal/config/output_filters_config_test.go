package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultConfig_OutputFiltersEnabledDefault verifies the 2026-09-22
// decision: output filters are ON by default, with the documented caps and
// an empty (nil) filter list meaning "host-adaptive defaults resolve at
// wiring time" (leaf 04 Task 1, revised by user direction).
func TestDefaultConfig_OutputFiltersDisabledFrozen(t *testing.T) {
	cfg := DefaultConfig()
	ofs := cfg.Daemon.OutputFilters
	if !ofs.Enabled {
		t.Error("Daemon.OutputFilters.Enabled = false; want true (enabled-by-default)")
	}
	if ofs.MaxPasses != 2 {
		t.Errorf("Daemon.OutputFilters.MaxPasses = %d, want 2", ofs.MaxPasses)
	}
	if ofs.MaxFilterRetries != 2 {
		t.Errorf("Daemon.OutputFilters.MaxFilterRetries = %d, want 2", ofs.MaxFilterRetries)
	}
	if ofs.Filters != nil {
		t.Errorf("Daemon.OutputFilters.Filters = %v, want nil", ofs.Filters)
	}
}

// TestOutputFiltersConfig_JSON5Parse verifies the documented JSON5 config
// shape parses into the struct with every key honored.
func TestOutputFiltersConfig_JSON5Parse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meept.json5")
	content := `{
		// JSON5 comment to prove the JSON5 loader path
		"daemon": {
			"output_filters": {
				"enabled": true,
				"max_passes": 3,
				"max_filter_retries": 4,
				"filters": ["json_format", "language_en"],
			},
		},
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadJSON5Config(path)
	if err != nil {
		t.Fatalf("LoadJSON5Config: %v", err)
	}
	ofs := cfg.Daemon.OutputFilters
	if !ofs.Enabled {
		t.Error("Enabled = false; want true")
	}
	if ofs.MaxPasses != 3 {
		t.Errorf("MaxPasses = %d, want 3", ofs.MaxPasses)
	}
	if ofs.MaxFilterRetries != 4 {
		t.Errorf("MaxFilterRetries = %d, want 4", ofs.MaxFilterRetries)
	}
	if len(ofs.Filters) != 2 || ofs.Filters[0] != "json_format" || ofs.Filters[1] != "language_en" {
		t.Errorf("Filters = %v, want [json_format language_en]", ofs.Filters)
	}
}
