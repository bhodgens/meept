package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/services"
)

// projectPromptTierTopic is the bus topic published by the project.set RPC
// handler (internal/rpc/projects.go handleSet) after a project is marked
// active / bound to a session. Subscribe here to keep the prompt project tier
// in step with the daemon's active project at runtime.
const projectPromptTierTopic = "project.set"

// resolveProjectPromptDir returns the active project's .meept/prompts
// directory, or "" when no project is active. It never falls back to the
// process working directory (AGENTS.md: daemon CWD is NOT the user's project).
func resolveProjectPromptDir(components *Components) string {
	pm := nilSafeProjectManager(components)
	if pm == nil {
		return ""
	}
	proj, err := pm.GetActive(context.Background())
	if err != nil || proj == nil || proj.LocalPath == "" {
		return ""
	}
	return filepath.Join(proj.LocalPath, ".meept", "prompts")
}

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
//
// The Project field is a start-up snapshot. wireProjectPromptTier keeps it in
// step with the active project afterwards (project.set events).
func resolvePromptDirs(components *Components) services.PromptDirs {
	dirs := services.PromptDirs{
		User:    config.MeeptPath("prompts"),
		Bundled: resolveBundledPath("config/prompts"),
		Project: resolveProjectPromptDir(components),
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs.System = filepath.Join(home, ".config", "meept", "prompts")
	}
	return dirs
}

// wireProjectPromptTier subscribes the prompt service's project tier to
// project-change events so switching the active project at runtime changes
// prompt discovery without a daemon restart.
//
// resolve returns the project prompt directory for the currently active
// project. It is invoked once per event (never on the prompt lookup path), so
// the prompt service caches the resolved directory and does not re-read the
// project store per request. Only the project tier moves; the user, system,
// and bundled tiers are fixed at construction.
//
// The returned subscriber is nil when there is nothing to wire. The pump
// goroutine exits when the bus closes the subscriber channel, mirroring
// wireSpeakPushBridge; callers may ignore the return value.
func wireProjectPromptTier(msgBus *bus.MessageBus, prompt *services.PromptService, resolve func() string, logger *slog.Logger) *bus.Subscriber {
	if msgBus == nil || prompt == nil || resolve == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	sub := msgBus.Subscribe("prompt-project-tier", projectPromptTierTopic)
	if sub == nil {
		return nil
	}
	go func() {
		for range sub.Channel {
			// Re-resolve the authoritative active project rather than trusting
			// the event payload: ProjectManager.GetActive is the same source of
			// truth resolvePromptDirs reads at start-up.
			prompt.SetProjectDir(resolve())
		}
	}()
	logger.Info("prompt project tier bridge wired", "topic", projectPromptTierTopic)
	return sub
}
