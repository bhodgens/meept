// internal/configui/sections_security.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildSecurityFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Security
	return []Field{
		NewToggleField("sanitize_inputs", "sanitize inputs", s.SanitizeInputs),
		NewSelectField("sanitize_strictness", "sanitize strictness", s.SanitizeStrictness, []string{"permissive", "standard", "strict"}),
		NewToggleField("llm_filter_external", "llm filter external", s.LLMFilterExternal),
		NewToggleField("monitor_output", "monitor output", s.MonitorOutput),
		NewToggleField("redact_output", "redact output", s.RedactOutput),
		NewToggleField("scan_shell_commands", "scan shell commands", s.ScanShellCommands),
		NewTextField("tirith_binary", "tirith binary", s.TirithBinary),
		NewToggleField("require_confirmation_high", "require confirmation high", s.RequireConfirmationHigh),
		NewToggleField("require_confirmation_critical", "require confirmation critical", s.RequireConfirmationCritical),
		NewToggleField("block_financial", "block financial", s.BlockFinancial),
		NewDrilldownField("allowed_paths", "allowed paths", buildStringSliceItems("path", s.AllowedPaths)),
		NewDrilldownField("blocked_paths", "blocked paths", buildStringSliceItems("path", s.BlockedPaths)),
		NewToggleField("enable_audit_log", "enable audit log", s.EnableAuditLog),
		NewTextField("audit_db_path", "audit db path", s.AuditDBPath),
	}
}

// buildStringSliceItems converts a []string into DrilldownItem items where each
// item has a single editable text field with the given fieldKey.
func buildStringSliceItems(fieldKey string, ss []string) []DrilldownItem {
	items := make([]DrilldownItem, 0, len(ss))
	for _, s := range ss {
		fields := []Field{
			NewTextField(fieldKey, fieldKey, s),
		}
		items = append(items, DrilldownItem{Name: s, Fields: fields})
	}
	return items
}
