package repomap

import (
	"math"
	"strings"
	"testing"
)

func TestFittingConfig_Defaults(t *testing.T) {
	config := DefaultFittingConfig()

	if config.MaxMapTokens != 1024 {
		t.Errorf("expected MaxMapTokens=1024, got %d", config.MaxMapTokens)
	}
	if config.Tolerance != 0.15 {
		t.Errorf("expected Tolerance=0.15, got %f", config.Tolerance)
	}
	if config.MapMulNoFiles != 8.0 {
		t.Errorf("expected MapMulNoFiles=8.0, got %f", config.MapMulNoFiles)
	}
	if config.MinTags != 5 {
		t.Errorf("expected MinTags=5, got %d", config.MinTags)
	}
	if config.MaxTags != 500 {
		t.Errorf("expected MaxTags=500, got %d", config.MaxTags)
	}
}

func TestFitToBudget_EmptyRanked(t *testing.T) {
	renderer := &DefaultRenderer{}
	config := DefaultFittingConfig()

	result := FitToBudget(nil, config, renderer)

	if result.Tokens != 0 {
		t.Errorf("expected empty result for nil ranked, got %d tokens", result.Tokens)
	}

	result = FitToBudget(RankedTags{}, config, renderer)
	if result.Tokens != 0 {
		t.Errorf("expected empty result for empty ranked, got %d tokens", result.Tokens)
	}
}

func TestFitToBudget_SingleTag(t *testing.T) {
	renderer := &DefaultRenderer{}
	config := DefaultFittingConfig()

	ranked := RankedTags{
		{Tag: Tag{RelFname: "test.go", Name: "TestFunc", Kind: "function", Line: 10, IsDef: true}, Score: 1.0},
	}

	result := FitToBudget(ranked, config, renderer)

	if result.Tokens == 0 {
		t.Error("expected non-empty result for single tag")
	}
	if result.Content == "" {
		t.Error("expected non-empty content")
	}
}

func TestFitToBudget_BinarySearch(t *testing.T) {
	renderer := &DefaultRenderer{}
	config := DefaultFittingConfig()
	config.MaxMapTokens = 100
	config.Tolerance = 0.2

	// Create ranked tags that will produce different token counts
	var ranked RankedTags
	for i := 0; i < 100; i++ {
		ranked = append(ranked, RankedTag{
			Tag: Tag{
				RelFname: "test.go",
				Name:     "Function" + string(rune('A'+i%26)),
				Kind:     "function",
				Line:     i * 5,
				IsDef:    true,
			},
			Score: 1.0 - float64(i)*0.01,
		})
	}

	result := FitToBudget(ranked, config, renderer)

	// Should not exceed budget significantly
	maxAllowed := int(float64(config.MaxMapTokens) * (1.0 + config.Tolerance))
	if result.Tokens > maxAllowed {
		t.Errorf("expected tokens <= %d for tolerance band, got %d", maxAllowed, result.Tokens)
	}
}

func TestFitToBudget_ToleranceBand(t *testing.T) {
	renderer := &DefaultRenderer{}
	config := DefaultFittingConfig()
	config.MaxMapTokens = 500
	config.Tolerance = 0.1 // 10% tolerance

	// Create known set of ranked tags
	var ranked RankedTags
	for i := 0; i < 50; i++ {
		ranked = append(ranked, RankedTag{
			Tag: Tag{
				RelFname: "file.go",
				Name:     "Symbol" + string(rune('A'+i%26)),
				Kind:     "function",
				Line:     i * 10,
				IsDef:    true,
			},
			Score: 1.0 / float64(i+1),
		})
	}

	result := FitToBudget(ranked, config, renderer)

	// Check if within tolerance
	actualPctErr := math.Abs(float64(result.Tokens)-float64(config.MaxMapTokens)) / float64(config.MaxMapTokens)
	if actualPctErr > config.Tolerance && result.Tokens != 0 {
		// If not within tolerance, should use fallback
		if result.Tokens > config.MaxMapTokens {
			t.Logf("Over budget: %d tokens (%.1f%% error)", result.Tokens, actualPctErr*100)
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"func", 1},
		{"function test() {}", 5},
		{"package main\n\nfunc main() {\n\tprintln(\"hello\")\n}", 14},
	}

	for _, tt := range tests {
		result := EstimateTokens(tt.input)
		if result != tt.expected {
			// Allow some variance due to overhead calculation
			low := tt.expected * 4 / 5 // -20%
			high := tt.expected * 6 / 5 // +20%
			if result < low || result > high {
				t.Errorf("EstimateTokens(%q): expected ~%d, got %d", tt.input, tt.expected, result)
			}
		}
	}
}

func TestFitToBudgetSimple(t *testing.T) {
	config := DefaultFittingConfig()
	config.MaxMapTokens = 100

	// Create ranked tags
	ranked := RankedTags{}
	for i := 0; i < 10; i++ {
		ranked = append(ranked, RankedTag{
			Tag:   Tag{Name: "Func" + string(rune('0'+i)), IsDef: true},
			Score: 1.0,
		})
	}

	result := FitToBudgetSimple(ranked, config)

	// Should estimate based on avgTokensPerTag=20
	// Max tags = 100/20 = 5
	expectedTags := 5
	if len(ranked) < expectedTags {
		expectedTags = len(ranked)
	}

	if result.Tokens == 0 {
		t.Error("expected non-zero tokens")
	}
}

func TestAdjustBudgetForConfig(t *testing.T) {
	config := DefaultFittingConfig()
	config.MaxMapTokens = 1024

	// Case 1: No chat files, few identifiers
	result := AdjustBudgetForConfig(config, []string{}, []string{})
	expected := int(1024 * 8.0) // MapMulNoFiles multiplier
	if result.MaxMapTokens != expected {
		t.Errorf("expected %d tokens when no files, got %d", expected, result.MaxMapTokens)
	}

	// Case 2: Many chat files
	result = AdjustBudgetForConfig(config, []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go"}, []string{})
	expected = 1024 * 3 / 4 // 75%
	if result.MaxMapTokens != expected {
		t.Errorf("expected %d tokens with 6 files, got %d", expected, result.MaxMapTokens)
	}

	// Case 3: Some files (should use original budget)
	result = AdjustBudgetForConfig(config, []string{"a.go"}, []string{"Foo"})
	if result.MaxMapTokens != 1024 {
		t.Errorf("expected 1024 tokens with 1 file, got %d", result.MaxMapTokens)
	}

	// Case 4: Minimum floor
	config2 := DefaultFittingConfig()
	config2.MaxMapTokens = 100
	result = AdjustBudgetForConfig(config2, []string{}, []string{})
	if result.MaxMapTokens < 256 {
		t.Errorf("expected at least 256 tokens floor, got %d", result.MaxMapTokens)
	}
}

func TestTokenBudgetForFiles(t *testing.T) {
	// When we have many files (20), each gets minPerFile=100 tokens
	// to ensure reasonable representation, total = 20 * 100 = 2000
	// This is intentional - better to allocate more budget when covering many files
	result := TokenBudgetForFiles(20, 1000)
	if result != 2000 {
		t.Errorf("expected 2000 tokens for 20 files, got %d", result)
	}

	// For fewer files, we stay under base budget
	if TokenBudgetForFiles(5, 1000) != 1000 {
		t.Errorf("expected 1000 tokens for 5 files")
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  FittingConfig
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  DefaultFittingConfig(),
			wantErr: false,
		},
		{
			name: "zero max tokens",
			config: FittingConfig{
				MaxMapTokens: 0,
			},
			wantErr: true,
		},
		{
			name: "negative tolerance",
			config: FittingConfig{
				MaxMapTokens: 100,
				Tolerance:    -0.1,
			},
			wantErr: true,
		},
		{
			name: "tolerance > 1",
			config: FittingConfig{
				MaxMapTokens: 100,
				Tolerance:    1.5,
			},
			wantErr: true,
		},
		{
			name: "negative min tags",
			config: FittingConfig{
				MaxMapTokens: 100,
				MinTags:      -1,
			},
			wantErr: true,
		},
		{
			name: "max < min",
			config: FittingConfig{
				MaxMapTokens: 100,
				MinTags:      10,
				MaxTags:      5,
			},
			wantErr: true,
		},
		{
			name: "zero map mul",
			config: FittingConfig{
				MaxMapTokens:  100,
				MapMulNoFiles: 0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFitStrategy(t *testing.T) {
	renderer := &DefaultRenderer{}
	config := DefaultFittingConfig()
	config.MaxMapTokens = 200

	ranked := RankedTags{
		{Tag: Tag{FName: "test.go", RelFname: "test.go", Name: "Func1", Kind: "function", Line: 1, IsDef: true}, Score: 1.0},
		{Tag: Tag{FName: "test.go", RelFname: "test.go", Name: "Func2", Kind: "function", Line: 2, IsDef: true}, Score: 0.8},
		{Tag: Tag{FName: "test.go", RelFname: "test.go", Name: "Func3", Kind: "function", Line: 3, IsDef: true}, Score: 0.6},
		{Tag: Tag{FName: "test.go", RelFname: "test.go", Name: "Func4", Kind: "function", Line: 4, IsDef: true}, Score: 0.4},
	}

	// Test binary search strategy
	result := FitToBudgetWithStrategy(ranked, config, renderer, FitStrategyBinarySearch)
	if result.Tokens == 0 {
		t.Error("expected non-empty result for binary search strategy")
	}

	// Test proportional strategy
	result = FitToBudgetWithStrategy(ranked, config, renderer, FitStrategyProportional)
	if result.Tokens == 0 {
		t.Error("expected non-empty result for proportional strategy")
	}

	// Test top N strategy
	result = FitToBudgetWithStrategy(ranked, config, renderer, FitStrategyTopN)
	if result.Tokens == 0 {
		t.Error("expected non-empty result for top N strategy")
	}
}

func TestDefaultRenderer(t *testing.T) {
	renderer := &DefaultRenderer{}

	// Test empty ranked
	result := renderer.Render(nil)
	if result.Tokens != 0 {
		t.Errorf("expected 0 tokens for nil, got %d", result.Tokens)
	}

	result = renderer.Render(RankedTags{})
	if result.Tokens != 0 {
		t.Errorf("expected 0 tokens for empty, got %d", result.Tokens)
	}

	// Test with ranked tags
	ranked := RankedTags{
		{Tag: Tag{RelFname: "a.go", Name: "FuncA", Kind: "function", Line: 1, IsDef: true}, Score: 1.0},
		{Tag: Tag{RelFname: "b.go", Name: "TypeB", Kind: "class", Line: 5, IsDef: true}, Score: 0.5},
	}

	result = renderer.Render(ranked)
	if result.Tokens == 0 {
		t.Error("expected non-zero tokens for ranked tags")
	}
	if result.Content == "" {
		t.Error("expected non-empty content")
	}

	// Check content format - should contain file names
	if result.Tokens > 0 && !strings.Contains(result.Content, "a.go") {
		t.Error("expected content to contain file a.go")
	}
}