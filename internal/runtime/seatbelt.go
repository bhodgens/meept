// Seatbelt confinement policy for the local backend on macOS (seatbelt /
// sandbox-exec). This file carries the policy model and the profile builder,
// both OS-independent so they compile and unit-test on every platform. The
// application (wrapping the child in /usr/bin/sandbox-exec) lives in
// network_class_darwin.go; Linux and other hosts ignore the policy and keep
// the network-class confinement unchanged.
//
// Design notes, pinned by probes against /usr/bin/sandbox-exec on macOS 26:
//   - seatbelt rules are LAST-MATCH-WINS: (deny file-write*) followed by
//     (allow file-write* (subpath X)) allows writes under X and denies them
//     everywhere else. Deny rules that must override an allow-list are
//     emitted after the allows.
//   - (deny process-exec*) emitted bare blocks the launch itself:
//     sandbox-exec applies the profile to itself before its execvp of the
//     child, so the child's own exec is governed too. Only per-path exec
//     denies are emitted here, and builder validation refuses any entry
//     whose subtree covers the launcher's exec target.
//   - (deny process-fork) reduces the child shell to builtins only (every
//     external command forks), so it is deliberately NOT offered.
//   - (deny process-info*) blocks ps and process inspection while leaving
//     normal execution, signals to self, and pipes working.
//   - a global (deny file-read*) plus a partial allow-list aborts the child
//     (dyld needs paths outside any hand-kept list), so reads are offered
//     only as subtree denies, never as an allow-list.
package runtime

import (
	"fmt"
	"strings"
)

// SeatbeltConfig is the confinement policy applied to local-backend children
// on macOS. Zero value (Enabled=false) is the legacy posture: network-class
// confinement only, byte-identical to pre-seatbelt behavior everywhere.
type SeatbeltConfig struct {
	// Enabled activates fs/process confinement in addition to the network
	// class. False ignores every other field.
	Enabled bool `json:"enabled"`
	// DenyRead lists absolute path subtrees the child cannot read (for
	// example ~/.ssh). Reads stay allowed everywhere else.
	DenyRead []string `json:"deny_read"`
	// AllowWrite, when non-empty, confines writes: every file-write is
	// denied except under these subtrees (and the device nodes a shell
	// needs: /dev/null et al.).
	AllowWrite []string `json:"allow_write"`
	// DenyWrite lists subtrees that stay write-denied even inside an
	// AllowWrite root. Emitted after the allows (last match wins).
	DenyWrite []string `json:"deny_write"`
	// BlockProcessInfo denies process-info*: ps, proc listing and process
	// inspection fail with EPERM; normal execution is unaffected.
	BlockProcessInfo bool `json:"block_process_info"`
	// DenyExec lists binaries (or subtrees) the child may not exec, for
	// example /usr/bin/sudo. Validation refuses entries that would cover
	// the launcher's own exec target and break every launch.
	DenyExec []string `json:"deny_exec"`
}

// SeatbeltActive reports whether a policy pointer calls for confinement.
func SeatbeltActive(cfg *SeatbeltConfig) bool {
	return cfg != nil && cfg.Enabled
}

// seatbeltDeviceAllows are the write exceptions a confined shell still
// needs: null/sink/entropy/console. Proven sufficient for sh -c pipelines.
var seatbeltDeviceAllows = []string{
	"/dev/null", "/dev/zero", "/dev/dtracehelper", "/dev/random",
	"/dev/urandom", "/dev/tty", "/dev/tty1",
}

// seatbeltCriticalExecTargets are paths the confinement must never deny:
// sandbox-exec execvp's the child after applying the profile, so denying
// any of these (as a literal or an enclosing subtree) breaks every launch.
var seatbeltCriticalExecTargets = []string{"/bin/sh", "/bin/bash", "/bin/dash", "/usr/bin/env"}

// ValidateSeatbeltPath reports why path is unusable in a profile rule.
// Rules require an absolute path, no double quotes (the profile quotes the
// literal), and a subtree that does not cover a critical exec target.
func ValidateSeatbeltPath(path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path %q is not absolute", path)
	}
	if strings.ContainsAny(path, "\"\\\n") {
		return fmt.Errorf("path %q contains characters a seatbelt profile cannot quote", path)
	}
	for _, target := range seatbeltCriticalExecTargets {
		if coversSubtree(path, target) {
			return fmt.Errorf("path %q covers the confined launch target %q and would break every command", path, target)
		}
	}
	return nil
}

// coversSubtree reports whether the seatbelt subpath rule for root would
// match candidate (both already absolute). "/" and root equal root match
// candidate when candidate is under root.
func coversSubtree(root, candidate string) bool {
	if root == "/" {
		return true
	}
	if candidate == root {
		return true
	}
	return strings.HasPrefix(candidate, strings.TrimRight(root, "/")+"/")
}

// BuildSeatbeltProfile renders the sandbox-exec -p profile for one launch.
// class carries the network posture; cfg carries the fs/process policy and
// may be nil. Returns "" when no confinement applies (caller launches
// bare). With cfg inactive the result is the legacy network-only profile,
// byte-identical to the pre-seatbelt constant.
//
// Rule order (last match wins): network deny, read denies, write-deny-all,
// device and allow-list allows, write denies inside allowed roots, then
// process rules.
func BuildSeatbeltProfile(class string, cfg *SeatbeltConfig) (string, error) {
	restricts := NetworkClassRestricts(class)
	if !SeatbeltActive(cfg) {
		if !restricts {
			return "", nil
		}
		return noNetworkProfile, nil
	}

	var b strings.Builder
	b.WriteString("(version 1)(allow default)")
	if restricts {
		b.WriteString("(deny network*)")
	}

	for _, p := range cfg.DenyRead {
		if err := ValidateSeatbeltPath(p); err != nil {
			return "", fmt.Errorf("seatbelt deny_read: %w", err)
		}
		fmt.Fprintf(&b, "(deny file-read* (subpath %q))", p)
	}

	writeConfined := len(cfg.AllowWrite) > 0
	if writeConfined {
		b.WriteString("(deny file-write*)")
		var allows strings.Builder
		for _, dev := range seatbeltDeviceAllows {
			fmt.Fprintf(&allows, "(allow file-write-data (literal %q))", dev)
		}
		for _, p := range cfg.AllowWrite {
			if err := ValidateSeatbeltPath(p); err != nil {
				return "", fmt.Errorf("seatbelt allow_write: %w", err)
			}
			fmt.Fprintf(&allows, "(allow file-write* (subpath %q))", p)
		}
		b.WriteString(allows.String())
	}
	// Emitted after the allows: last match wins, so these carve holes in
	// an allowed root.
	for _, p := range cfg.DenyWrite {
		if err := ValidateSeatbeltPath(p); err != nil {
			return "", fmt.Errorf("seatbelt deny_write: %w", err)
		}
		if !writeConfined {
			// Without an allow-list the only way to deny a specific
			// subtree is a plain deny over the default-allow.
			fmt.Fprintf(&b, "(deny file-write* (subpath %q))", p)
			continue
		}
		fmt.Fprintf(&b, "(deny file-write* (subpath %q))", p)
	}

	if cfg.BlockProcessInfo {
		b.WriteString("(deny process-info*)")
	}
	for _, p := range cfg.DenyExec {
		if err := ValidateSeatbeltPath(p); err != nil {
			return "", fmt.Errorf("seatbelt deny_exec: %w", err)
		}
		fmt.Fprintf(&b, "(deny process-exec* (subpath %q))", p)
	}

	profile := b.String()
	// Belt and braces: a profile with no rule beyond the defaults cannot
	// confine anything; treat it as unconfigured rather than spawning a
	// sandbox-exec wrapper that changes nothing.
	if profile == "(version 1)(allow default)" {
		return "", nil
	}
	return profile, nil
}
