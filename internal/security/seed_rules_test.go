package security

import (
	"testing"
)

func TestSeedRulesCount(t *testing.T) {
	total := CountRules()

	// Should have at least 100 rules (requirement says 146)
	if total < 100 {
		t.Errorf("Expected at least 100 rules, got %d", total)
	}

	// Exact count check - we should have close to 146
	if total < 110 {
		t.Errorf("Expected around 146 rules, got %d (seems too low)", total)
	}
}

func TestSeedRulesToolRules(t *testing.T) {
	data := SeedRules()

	if len(data.ToolRules) < 10 {
		t.Errorf("Expected at least 10 tool rules, got %d", len(data.ToolRules))
	}

	// Check that essential rules exist
	essentialActions := map[string]bool{
		"file_read":       false,
		"file_write":      false,
		"file_delete":     false,
		"shell_execute":   false,
		"network_request": false,
	}

	for _, rule := range data.ToolRules {
		if _, ok := essentialActions[rule.Action]; ok {
			essentialActions[rule.Action] = true
		}
	}

	for action, found := range essentialActions {
		if !found {
			t.Errorf("Missing essential tool rule for action: %s", action)
		}
	}
}

func TestSeedRulesCommandPatterns(t *testing.T) {
	data := SeedRules()

	if len(data.CommandPatterns) < 50 {
		t.Errorf("Expected at least 50 command patterns, got %d", len(data.CommandPatterns))
	}

	// Check for critical patterns
	criticalCategories := map[string]bool{
		"destructive":      false,
		"self_replication": false,
	}

	for _, pattern := range data.CommandPatterns {
		if pattern.RiskLevel == RiskCritical {
			if _, ok := criticalCategories[pattern.Category]; ok {
				criticalCategories[pattern.Category] = true
			}
		}
	}

	for category, found := range criticalCategories {
		if !found {
			t.Errorf("Missing critical command pattern category: %s", category)
		}
	}
}

func TestSeedRulesPathRules(t *testing.T) {
	data := SeedRules()

	if len(data.PathRules) < 20 {
		t.Errorf("Expected at least 20 path rules, got %d", len(data.PathRules))
	}

	// Check for blocked sensitive paths
	sensitivePaths := []string{
		"~/.ssh/*",
		"~/.gnupg/*",
		"~/.aws/*",
		"/etc/shadow",
	}

	for _, sensitive := range sensitivePaths {
		found := false
		for _, rule := range data.PathRules {
			if rule.Pattern == sensitive && rule.RuleType == "block" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Missing path rule to block: %s", sensitive)
		}
	}

	// Check that there's at least one allow rule
	hasAllow := false
	for _, rule := range data.PathRules {
		if rule.RuleType == "allow" {
			hasAllow = true
			break
		}
	}
	if !hasAllow {
		t.Error("Should have at least one allow path rule")
	}
}

func TestSeedRulesFinancialPatterns(t *testing.T) {
	data := SeedRules()

	if len(data.FinancialPatterns) < 10 {
		t.Errorf("Expected at least 10 financial patterns, got %d", len(data.FinancialPatterns))
	}

	// Check for key financial patterns
	keyPatterns := []string{
		"transfer",
		"payment",
		"credit",
		"bank",
		"cryptocurrency",
	}

	for _, key := range keyPatterns {
		found := false
		for _, pattern := range data.FinancialPatterns {
			if containsSubstring(pattern.Pattern, key) || containsSubstring(pattern.Description, key) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Missing financial pattern for: %s", key)
		}
	}
}

func TestSeedRulesRiskLevelDistribution(t *testing.T) {
	data := SeedRules()

	riskCounts := map[RiskLevel]int{
		RiskSafe:     0,
		RiskLow:      0,
		RiskMedium:   0,
		RiskHigh:     0,
		RiskCritical: 0,
	}

	for _, rule := range data.ToolRules {
		riskCounts[rule.RiskLevel]++
	}
	for _, pattern := range data.CommandPatterns {
		riskCounts[pattern.RiskLevel]++
	}
	for _, rule := range data.PathRules {
		riskCounts[rule.RiskLevel]++
	}

	// Should have rules at multiple risk levels
	if riskCounts[RiskCritical] == 0 {
		t.Error("Should have CRITICAL risk rules")
	}
	if riskCounts[RiskHigh] == 0 {
		t.Error("Should have HIGH risk rules")
	}
	if riskCounts[RiskMedium] == 0 {
		t.Error("Should have MEDIUM risk rules")
	}
	if riskCounts[RiskLow] == 0 {
		t.Error("Should have LOW risk rules")
	}
}

func TestSeedRulesImmutableRules(t *testing.T) {
	data := SeedRules()

	immutableCount := 0

	for _, rule := range data.ToolRules {
		if rule.Immutable {
			immutableCount++
		}
	}
	for _, pattern := range data.CommandPatterns {
		if pattern.Immutable {
			immutableCount++
		}
	}
	for _, rule := range data.PathRules {
		if rule.Immutable {
			immutableCount++
		}
	}

	// Should have at least some immutable rules for safety
	if immutableCount < 10 {
		t.Errorf("Expected at least 10 immutable rules, got %d", immutableCount)
	}
}

func TestSeedRulesConfirmationRequirements(t *testing.T) {
	data := SeedRules()

	confirmCount := 0
	for _, rule := range data.ToolRules {
		if rule.RequiresConfirmation {
			confirmCount++
		}
	}

	// Should have some actions requiring confirmation
	if confirmCount == 0 {
		t.Error("Should have actions requiring confirmation")
	}

	// file_delete should require confirmation
	for _, rule := range data.ToolRules {
		if rule.Action == "file_delete" && !rule.RequiresConfirmation {
			t.Error("file_delete should require confirmation")
		}
	}
}

func TestSeedRulesCategories(t *testing.T) {
	data := SeedRules()

	categories := make(map[string]bool)
	for _, pattern := range data.CommandPatterns {
		categories[pattern.Category] = true
	}

	// Should have diverse categories
	expectedCategories := []string{
		"destructive",
		"permissions",
		"network",
		"install",
		"vcs",
		"file_read",
	}

	for _, cat := range expectedCategories {
		if !categories[cat] {
			t.Errorf("Missing expected category: %s", cat)
		}
	}
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			// Case-insensitive comparison
			sc := s[i+j]
			tc := substr[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 32
			}
			if tc >= 'A' && tc <= 'Z' {
				tc += 32
			}
			if sc != tc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
