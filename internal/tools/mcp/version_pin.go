package mcp

import "strings"

// VersionPinError reports an MCP command that resolves its package at
// launch time. Issue #166: every npx/uvx command must carry an exact
// version so a compromised upstream release cannot execute with the
// daemon's environment.
type VersionPinError struct {
	Server string
	Reason string
}

func (e *VersionPinError) Error() string {
	return "SECURITY: MCP server '" + e.Server + "' uses an unpinned command '" + e.Reason + "'; refusing to launch. Pin an exact version."
}

// ValidateVersionPin refuses npx/uvx commands that do not pin an exact
// version. npx accepts @scope/pkg@version or pkg@version; uvx accepts
// pkg==version (it has no @version form). Other runtimes (native
// binaries, HTTP transports) are outside this check.
func ValidateVersionPin(server string, command []string) *VersionPinError {
	if len(command) == 0 {
		return nil
	}
	bin := command[0]
	if !strings.Contains(bin, "npx") && !strings.Contains(bin, "uvx") {
		return nil
	}
	if len(command) < 2 {
		return &VersionPinError{Server: server, Reason: strings.Join(command, " ")}
	}
	// Skip launcher flags such as npx's -y before the package name.
	idx := 1
	for idx < len(command) && strings.HasPrefix(command[idx], "-") {
		idx++
	}
	if idx >= len(command) {
		return &VersionPinError{Server: server, Reason: strings.Join(command, " ")}
	}
	pkg := command[idx]
	if strings.Contains(bin, "uvx") {
		if strings.Contains(pkg, "==") {
			return nil
		}
		return &VersionPinError{Server: server, Reason: strings.Join(command, " ")}
	}
	// npx: find the version marker after the package name. A scoped
	// package starts with @; its version separator is the LAST '@'.
	at := strings.LastIndex(pkg, "@")
	if at <= 0 {
		return &VersionPinError{Server: server, Reason: strings.Join(command, " ")}
	}
	return nil
}
