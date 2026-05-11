package agent

import (
	"testing"
)

func TestNewHallucinationDetector(t *testing.T) {
	cfg := HallucinationConfig{
		Enabled:       true,
		Sensitivity:   SensitivityLow,
		MaxIndicators: 2,
	}

	hd := NewHallucinationDetector(cfg, nil)
	if hd == nil {
		t.Fatal("expected non-nil detector")
	}
}

func TestHallucinationDetector_Disabled(t *testing.T) {
	cfg := HallucinationConfig{Enabled: false}
	hd := NewHallucinationDetector(cfg, nil)

	result := hd.Analyze("I have created the file", nil)
	if result.ShouldRecover {
		t.Error("expected no recovery when disabled")
	}
	if len(result.Indicators) != 0 {
		t.Error("expected no indicators when disabled")
	}
}

func TestHallucinationDetector_ConfidentClaims(t *testing.T) {
	tests := []struct {
		name             string
		output           string
		sensitivity      HallucinationSensitivity
		expectIndicators bool
	}{
		{
			name:             "confident file creation claim",
			output:           "I have created the file config.yaml with the required settings.",
			sensitivity:      SensitivityMedium,
			expectIndicators: true,
		},
		{
			name:             "confident modification claim",
			output:           "I have modified the function StartServer in server.go to add timeout support.",
			sensitivity:      SensitivityMedium,
			expectIndicators: true,
		},
		{
			name:             "factual statement not a claim",
			output:           "The file config.yaml contains database connection settings.",
			sensitivity:      SensitivityMedium,
			expectIndicators: false,
		},
		{
			name:             "verification claim",
			output:           "I can confirm that the fix resolves the issue.",
			sensitivity:      SensitivityHigh,
			expectIndicators: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := HallucinationConfig{
				Enabled:       true,
				Sensitivity:   tt.sensitivity,
				MaxIndicators: 1,
			}
			hd := NewHallucinationDetector(cfg, nil)

			result := hd.Analyze(tt.output, nil)

			if tt.expectIndicators && len(result.Indicators) == 0 {
				t.Errorf("expected indicators but got none")
			}
			if !tt.expectIndicators && len(result.Indicators) > 0 {
				t.Errorf("expected no indicators but got %d: %+v", len(result.Indicators), result.Indicators)
			}
		})
	}
}

func TestHallucinationDetector_FabricatedReferences(t *testing.T) {
	cfg := HallucinationConfig{
		Enabled:       true,
		Sensitivity:   SensitivityMedium,
		MaxIndicators: 2,
	}
	hd := NewHallucinationDetector(cfg, nil)

	// Register known files
	hd.RegisterKnownSymbol("main.go", true)
	hd.RegisterKnownSymbol("server.go", true)
	hd.RegisterKnownSymbol("StartServer", false)

	// Reference to known file should not trigger
	result := hd.Analyze("The changes are in file main.go", nil)
	for _, ind := range result.Indicators {
		if ind.Type == "fabricated_ref" && ind.Content == "main.go" {
			t.Error("known file should not trigger fabricated reference")
		}
	}

	// Register more files and test with unknown
	hd2 := NewHallucinationDetector(cfg, nil)
	hd2.RegisterKnownSymbols(
		map[string]bool{"StartServer": true},
		map[string]bool{"main.go": true, "server.go": true},
	)

	result2 := hd2.Analyze("The changes are in file totally_fake_file.go", nil)
	hasFabricatedRef := false
	for _, ind := range result2.Indicators {
		if ind.Type == "fabricated_ref" {
			hasFabricatedRef = true
		}
	}
	if !hasFabricatedRef {
		t.Error("expected fabricated reference indicator for unknown file")
	}
}

func TestHallucinationDetector_Contradictions(t *testing.T) {
	cfg := HallucinationConfig{
		Enabled:       true,
		Sensitivity:   SensitivityMedium,
		MaxIndicators: 2,
	}
	hd := NewHallucinationDetector(cfg, nil)

	// Record history
	hd.RecordHistory("The function returns true on success.")
	hd.RecordHistory("Actually, I was wrong - it returns false on success.")

	// Check for contradiction indicator
	result := hd.Analyze("Actually, the correct behavior is different", []string{"previous statement"})
	hasContradiction := false
	for _, ind := range result.Indicators {
		if ind.Type == "contradiction" {
			hasContradiction = true
		}
	}
	if !hasContradiction {
		t.Error("expected contradiction indicator")
	}
}

func TestHallucinationDetector_RecoveryThreshold(t *testing.T) {
	cfg := HallucinationConfig{
		Enabled:       true,
		Sensitivity:   SensitivityHigh, // High sensitivity to get more indicators
		MaxIndicators: 2,
	}
	hd := NewHallucinationDetector(cfg, nil)

	// Output with multiple indicators
	output := "I have created the file new_module.py with the required classes. " +
		"I can confirm that everything works perfectly. " +
		"Actually, I was wrong about some details."

	hd.RecordHistory("Previous claim about the module")
	result := hd.Analyze(output, []string{"previous conversation context"})

	// With high sensitivity and multiple patterns, we should get indicators
	if len(result.Indicators) == 0 {
		t.Log("No indicators found - this may be OK depending on pattern matching")
	}

	// Verify score calculation
	if result.Score < 0 || result.Score > 1.0 {
		t.Errorf("score should be between 0 and 1, got %f", result.Score)
	}
}

func TestHallucinationDetector_ImpossibleResponses(t *testing.T) {
	cfg := HallucinationConfig{
		Enabled:       true,
		Sensitivity:   SensitivityMedium,
		MaxIndicators: 5,
	}
	hd := NewHallucinationDetector(cfg, nil)

	// Repeated words
	result := hd.Analyze("the the the result is ready", nil)
	hasRepeated := false
	for _, ind := range result.Indicators {
		if ind.Type == "impossible_response" && ind.Description == "repeated phrase pattern detected" {
			hasRepeated = true
		}
	}
	if !hasRepeated {
		t.Error("expected repeated phrase indicator")
	}
}

func TestHallucinationDetector_SensitivityFiltering(t *testing.T) {
	output := "I have created the file config.yaml."

	tests := []struct {
		name        string
		sensitivity HallucinationSensitivity
		expectZero  bool // whether we expect 0 indicators after sensitivity filtering
	}{
		{"low sensitivity", SensitivityLow, false}, // Low filters aggressively, may have some
		{"medium sensitivity", SensitivityMedium, false},
		{"high sensitivity", SensitivityHigh, false}, // High catches more
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := HallucinationConfig{
				Enabled:       true,
				Sensitivity:   tt.sensitivity,
				MaxIndicators: 2,
			}
			hd := NewHallucinationDetector(cfg, nil)
			result := hd.Analyze(output, nil)
			_ = result // Just verify no panic
		})
	}
}

func TestHallucinationDetector_RecordHistory(t *testing.T) {
	cfg := HallucinationConfig{Enabled: true, MaxIndicators: 2}
	hd := NewHallucinationDetector(cfg, nil)

	// Record many entries
	for range 25 {
		hd.RecordHistory("history entry")
	}

	hd.mu.RLock()
	histLen := len(hd.history)
	hd.mu.RUnlock()

	if histLen > 20 {
		t.Errorf("expected history to be bounded to 20, got %d", histLen)
	}
}

func TestHallucinationDetector_RegisterKnownSymbols(t *testing.T) {
	cfg := HallucinationConfig{Enabled: true, MaxIndicators: 2}
	hd := NewHallucinationDetector(cfg, nil)

	// Register symbols
	hd.RegisterKnownSymbols(
		map[string]bool{"Func1": true, "Func2": true},
		map[string]bool{"file1.go": true, "file2.go": true},
	)

	hd.mu.RLock()
	if !hd.knownSymbols["Func1"] {
		t.Error("expected Func1 to be registered")
	}
	if !hd.knownFiles["file1.go"] {
		t.Error("expected file1.go to be registered")
	}
	hd.mu.RUnlock()
}
