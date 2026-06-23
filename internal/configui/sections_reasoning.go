// internal/configui/sections_reasoning.go
package configui

import "fmt"

// buildReasoningFields creates config UI fields for the global reasoning
// settings (tier→budget mapping). Per-agent reasoning is configured via the
// agents section / AGENT.md frontmatter, not here.
//nolint:unused -- reserved for future reasoning config section
func buildReasoningFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Reasoning

	tiers := []string{"low", "medium", "high", "xhigh", "max"}

	fields := make([]Field, 0, len(tiers))
	for _, tier := range tiers {
		budget := 0
		if s.Budgets != nil {
			budget = s.Budgets[tier]
		}
		f := NewNumberField(
			fmt.Sprintf("budgets.%s", tier),
			fmt.Sprintf("%s budget tokens", tier),
			budget,
		)
		f.SetHelp(fmt.Sprintf("token budget for %s reasoning tier (0 = use default)", tier))
		fields = append(fields, f)
	}

	return fields
}
