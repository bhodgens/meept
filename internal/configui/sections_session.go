// internal/configui/sections_session.go
package configui


func buildSessionFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Session
	autoForkStr := s.AutoFork
	if autoForkStr == "" {
		autoForkStr = "never"
	}
	return []Field{
		NewToggleField("persistence", "persistence", s.Persistence),
		NewToggleField("branching", "branching", s.Branching),
		NewNumberField("max_branches", "max branches", s.MaxBranches),
		NewNumberField("branch_summary_threshold", "branch summary threshold", s.BranchSummaryThreshold),
		NewSelectField("auto_fork", "auto fork", autoForkStr, []string{"never", "ask", "always"}),
		NewNumberField("restore_message_limit", "restore message limit", s.RestoreMessageLimit),
		NewToggleField("compaction", "compaction", s.Compaction),
		NewNumberField("compaction_threshold", "compaction threshold", s.CompactionThreshold),
		NewFloatField("compaction_target_ratio", "compaction target ratio", s.CompactionTargetRatio),
	}
}
