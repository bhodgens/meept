// internal/configui/sections_projects.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildProjectsFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Projects
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("base_dir", "base dir", s.BaseDir),
		NewToggleField("auto_detect", "auto detect", s.AutoDetect),
		NewSelectField("worktree_per_plan", "worktree per plan", s.WorktreePerPlan, []string{"auto", "always", "never"}),
		NewNumberField("worktree_isolation_threshold", "worktree isolation threshold", s.WorktreeIsolationThreshold),
		NewNumberField("max_worktrees_per_project", "max worktrees per project", s.MaxWorktreesPerProject),
		NewToggleField("cleanup_orphaned_worktrees", "cleanup orphaned worktrees", s.CleanupOrphanedWorktrees),
		NewToggleField("fence_enabled", "fence enabled", s.FenceEnabled),
		NewTextField("default_branch", "default branch", s.DefaultBranch),
		NewToggleField("auto_sync_on_attach", "auto sync on attach", s.AutoSyncOnAttach),
	}
}
