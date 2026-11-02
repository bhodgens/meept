package security

// groups_backstop.go — the C5 file-level backstop (agent-groups plan tree,
// docs/plans/20261030-agent-groups/03-membership/03-mutation-gate.md).
//
// C5 (verbatim): "FILE BACKSTOP: when groups.default_managed=true,
// BlockedPaths gains config/agents/**, the users file, and the groups
// config section - for NON-ADMIN agents only."
//
// The stage runs inside CheckPermissionForAgent AFTER the fence stage and
// BEFORE confirmation gating, keyed on details["agent_id"] (the executor
// injects it on every tool call). It is configured ONCE at daemon wiring
// time through Config.GroupsBackstop (immutable after construction), so
// per-session PermissionChecker clones (ConfigSnapshot) carry it
// automatically — no per-session wiring needed.
//
// Deliberately data-driven: pkg/security imports nothing internal. The
// daemon derives the users-file and main-config paths from its loaded
// config (the same values the auth store and config loader use), so there
// is exactly one path resolution and no drift.

import (
	"fmt"
	"path/filepath"
	"strings"
)

// GroupsBackstopConfig is the daemon-injected backstop configuration
// (zero value = stage disabled: byte-identical legacy behavior).
type GroupsBackstopConfig struct {
	// Enabled arms the stage. True only when security.groups.
	// default_managed=true — the fail-closed default-management knob IS
	// the backstop's arming switch (C5 verbatim).
	Enabled bool
	// Home is the resolved meept home directory (config.MeeptHome()).
	// Paths are compared beneath it after cleaning; empty disables.
	Home string
	// UsersFile is the resolved multi-user users store path
	// (config.MultiUser.UsersFile after tilde expansion).
	UsersFile string
	// AdminAgents lists agent ids exempt from the backstop (security.
	// groups.admin_agents, default ["agent-update"]): the membership
	// admin keeps its file tools on the protected surfaces.
	AdminAgents map[string]struct{}
}

// protected returns whether path is one of the C5 backstop surfaces:
// the users store file, the agent-definitions directory
// (<home>/agents/**), or the daemon main config file (the groups config
// section carrier). All comparisons are lexically cleaned; the caller
// (executor) already resolved symlinks for file paths, and shell targets
// are best-effort substring probes (documented limitation: variable
// expansion bypasses the shell lane — the file-tool lane is the hard
// guarantee, same posture as credentialPathPattern).
func (g GroupsBackstopConfig) protected(path string) bool {
	if !g.Enabled || g.Home == "" || path == "" {
		return false
	}
	clean := filepath.Clean(path)
	home := filepath.Clean(g.Home)

	// The users store file (the F4 fixture gap: not covered by the
	// unconditional config_guard name list).
	if g.UsersFile != "" && clean == filepath.Clean(g.UsersFile) {
		return true
	}

	within := func(root string) bool {
		return clean == root || strings.HasPrefix(clean, root+string(filepath.Separator))
	}
	// Agent definitions directory (config/agents/** in daemon-speak:
	// the roster lives under <home>/agents).
	if within(filepath.Join(home, "agents")) {
		return true
	}
	// The main config file carries the groups section.
	if within(home) && (clean == filepath.Join(home, "meept.json5") || clean == filepath.Join(home, "meept.toml")) {
		return true
	}
	return false
}

// check denies file/shell actions by NON-ADMIN agents against the C5
// backstop surfaces. agentID is the caller agent (details["agent_id"]);
// an empty agent id is treated as NON-admin (fail closed).
func (g GroupsBackstopConfig) check(agentID, path string) error {
	if !g.protected(path) {
		return nil
	}
	if _, admin := g.AdminAgents[agentID]; admin && agentID != "" {
		// Membership admin keeps its tools (leaf checklist: the backstop
		// must not lock OUT the admin).
		return nil
	}
	return fmt.Errorf("groups backstop: agent %q is not a membership admin; writes to %s are denied while security.groups.default_managed is enabled (use the groups.* RPC surface as an admin identity instead)", agentID, path)
}

// checkCommand probes a shell command string for literal references to the
// backstop surfaces. Best-effort by design — see the protected() comment.
func (g GroupsBackstopConfig) checkCommand(agentID, command string) error {
	if !g.Enabled || command == "" {
		return nil
	}
	candidates := make([]string, 0, 3)
	if g.UsersFile != "" {
		candidates = append(candidates, filepath.Clean(g.UsersFile))
	}
	if g.Home != "" {
		candidates = append(candidates,
			filepath.Join(filepath.Clean(g.Home), "agents"),
			filepath.Join(filepath.Clean(g.Home), "meept.json5"),
			filepath.Join(filepath.Clean(g.Home), "meept.toml"),
		)
	}
	for _, c := range candidates {
		if c != "" && strings.Contains(command, c) {
			if _, admin := g.AdminAgents[agentID]; admin && agentID != "" {
				return nil
			}
			return fmt.Errorf("groups backstop: agent %q is not a membership admin; shell commands touching %s are denied while security.groups.default_managed is enabled", agentID, c)
		}
	}
	return nil
}
