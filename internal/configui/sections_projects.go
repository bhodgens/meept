// internal/configui/sections_projects.go
package configui

import (
	"github.com/caimlas/meept/internal/config"
)

func buildProjectsFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Projects
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("base_dir", "base dir", s.BaseDir),
		NewTextField("default_branch", "default branch", s.DefaultBranch),
		NewNumberField("worktree_isolation_threshold", "worktree isolation threshold", s.WorktreeIsolationThreshold),
	}
}
