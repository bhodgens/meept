// internal/configui/sections_workspace.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildWorkspaceFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Workspace
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("base_dir", "base dir", s.BaseDir),
		NewToggleField("auto_commit", "auto commit", s.AutoCommit),
		NewToggleField("commit_on_plan", "commit on plan", s.CommitOnPlan),
		NewToggleField("commit_on_step", "commit on step", s.CommitOnStep),
		NewToggleField("cleanup_completed", "cleanup completed", s.CleanupCompleted),
	}
}
