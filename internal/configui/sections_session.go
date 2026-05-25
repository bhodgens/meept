// internal/configui/sections_session.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildSessionFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Session
	return []Field{
		NewToggleField("persistence", "persistence", s.Persistence),
		NewToggleField("branching", "branching", s.Branching),
		NewNumberField("max_branches", "max branches", s.MaxBranches),
		NewNumberField("branch_summary_threshold", "branch summary threshold", s.BranchSummaryThreshold),
		NewSelectField("auto_fork", "auto fork", s.AutoFork, []string{"never", "ask", "always"}),
		NewNumberField("restore_message_limit", "restore message limit", s.RestoreMessageLimit),
		NewToggleField("compaction", "compaction", s.Compaction),
		NewNumberField("compaction_threshold", "compaction threshold", s.CompactionThreshold),
		NewFloatField("compaction_target_ratio", "compaction target ratio", s.CompactionTargetRatio),
		NewToggleField("legacy_truncation", "legacy truncation", s.LegacyTruncation),
	}
}
