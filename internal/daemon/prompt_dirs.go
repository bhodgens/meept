package daemon

import (
	"context"
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/services"
)

// resolvePromptDirs resolves the four prompt-discovery tiers to concrete
// directories so the prompt service never depends on the daemon process's
// working directory (see AGENTS.md: daemon CWD is NOT the user's project).
//
//   - Bundled: the shipped config/prompts, resolved the same way as other
//     bundled assets (repo layout or next to the executable) via
//     resolveBundledPath.
//   - User: config.MeeptPath("prompts"), i.e. $MEEPT_HOME/prompts (default
//     ~/.meept/prompts).
//   - System: the XDG-style ~/.config/meept/prompts.
//   - Project: the active project's .meept/prompts, when a project is active.
//     No active project means no project tier (never the CWD).
func resolvePromptDirs(components *Components) services.PromptDirs {
	dirs := services.PromptDirs{
		User:    config.MeeptPath("prompts"),
		Bundled: resolveBundledPath("config/prompts"),
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs.System = filepath.Join(home, ".config", "meept", "prompts")
	}
	if pm := nilSafeProjectManager(components); pm != nil {
		if proj, err := pm.GetActive(context.Background()); err == nil && proj != nil && proj.LocalPath != "" {
			dirs.Project = filepath.Join(proj.LocalPath, ".meept", "prompts")
		}
	}
	return dirs
}
