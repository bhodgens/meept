// envpolicy.go implements child environment construction for command
// execution: an allowlist + deny-glob filter over the daemon's environment so
// secrets exported into the daemon process do not reach agent-run shells.
package runtime

import (
	"path"
	"sort"
	"strings"
)

// EnvMode selects child environment construction strategy.
type EnvMode string

const (
	// EnvModeAllowlist passes only explicitly-allowed variables (secure default).
	EnvModeAllowlist EnvMode = "allowlist"
	// EnvModeInherit preserves legacy full-environment inheritance.
	EnvModeInherit EnvMode = "inherit"
)

// EnvPolicyConfig configures child environment filtering. Mirrored by
// internal/config.EnvPolicyConfig (config does not import runtime; mapping
// happens in internal/daemon/components.go, matching the existing Docker
// config convention).
type EnvPolicyConfig struct {
	Mode      EnvMode  `json:"env_mode"       toml:"env_mode"`
	Allowlist []string `json:"env_allowlist"  toml:"env_allowlist"`
	DenyGlobs []string `json:"env_deny_globs" toml:"env_deny_globs"`
}

// BaseEnvKeys are always passed through in allowlist mode (when present in
// the parent environment): shell basics plus proxy settings.
var BaseEnvKeys = []string{"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "TERM", "SHELL",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"}

// SecretPlaceholderPrefix marks values that always pass through regardless of
// name-based denial. The daemon resolves secrets into this placeholder form
// before handing them to Command.Env; BuildChildEnv never strips them.
const SecretPlaceholderPrefix = "MEEPT_SECRET:"

// isDenied reports whether name matches any deny glob. Globs use path.Match
// syntax and match against the variable NAME only, never its value.
func isDenied(name string, denyGlobs []string) bool {
	for _, g := range denyGlobs {
		if ok, err := path.Match(g, name); err == nil && ok {
			return true
		}
	}
	return false
}

// isAllowedName reports whether name is in BaseEnvKeys or allowlist.
func isAllowedName(name string, allowlist []string) bool {
	for _, k := range BaseEnvKeys {
		if k == name {
			return true
		}
	}
	for _, k := range allowlist {
		if k == name {
			return true
		}
	}
	return false
}

// BuildChildEnv builds the child env slice. parentEnv is the daemon's captured
// environ (pass nil in tests). cmdEnv entries override/deny per same rules.
// Placeholder-form values (strings with prefix "MEEPT_SECRET:") pass through
// untouched. Returns stripped variable NAMES (allowlist mode only) for logging.
//
// Deterministic order: variables present in parentEnv keep parent order;
// cmdEnv-only variables are appended sorted by name.
func BuildChildEnv(cfg EnvPolicyConfig, parentEnv []string, cmdEnv map[string]string) (env []string, stripped []string) {
	if cfg.Mode == EnvModeInherit {
		env = make([]string, 0, len(parentEnv)+len(cmdEnv))
		env = append(env, parentEnv...)
		for _, k := range sortedCmdEnvKeys(cmdEnv) {
			env = append(env, k+"="+cmdEnv[k])
		}
		return env, nil
	}

	// Allowlist mode: an entry survives if its value carries the secret
	// placeholder prefix, or its name is allowlisted AND not denied by glob.
	passes := func(name, value string) bool {
		if strings.HasPrefix(value, SecretPlaceholderPrefix) {
			return true
		}
		return isAllowedName(name, cfg.Allowlist) && !isDenied(name, cfg.DenyGlobs)
	}

	parentNames := make(map[string]struct{}, len(parentEnv))
	env = make([]string, 0, len(parentEnv)+len(cmdEnv))

	// Pass 1: parent vars in parent order; cmdEnv overrides applied inline.
	for _, entry := range parentEnv {
		eq := strings.IndexByte(entry, '=')
		if eq <= 0 {
			continue // malformed entry ("", "=val"); drop silently
		}
		name := entry[:eq]
		parentNames[name] = struct{}{}

		if override, ok := cmdEnv[name]; ok {
			if passes(name, override) {
				env = append(env, name+"="+override)
			} else {
				stripped = append(stripped, name)
			}
			continue
		}
		if passes(name, entry[eq+1:]) {
			env = append(env, entry)
		} else {
			stripped = append(stripped, name)
		}
	}

	// Pass 2: cmdEnv-only vars appended sorted; same rules apply.
	for _, k := range sortedCmdEnvKeys(cmdEnv) {
		if _, inParent := parentNames[k]; inParent {
			continue
		}
		if passes(k, cmdEnv[k]) {
			env = append(env, k+"="+cmdEnv[k])
		} else {
			stripped = append(stripped, k)
		}
	}

	return env, stripped
}

// sortedCmdEnvKeys returns cmdEnv keys sorted for deterministic output.
func sortedCmdEnvKeys(cmdEnv map[string]string) []string {
	keys := make([]string, 0, len(cmdEnv))
	for k := range cmdEnv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
