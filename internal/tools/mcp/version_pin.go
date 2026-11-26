package mcp

import (
	"strconv"
	"strings"
)

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

// Pin describes a resolved package pin from an enabled server's command.
// Registry is either "npm" (npx) or "pypi" (uvx); Pkg is the package
// identifier as it appears in the respective registry API path; Version
// is the exact pinned version.
type Pin struct {
	Registry string
	Pkg      string
	Version  string
}

// ParsePin extracts the registry, package, and exact pinned version from
// a command. It mirrors ValidateVersionPin's launcher-flag handling and
// deliberately supports ONLY the two shapes the pin gate accepts:
//
//	npx [-y ...] [@scope/]pkg@version   -> npm registry
//	uvx [-...] pkg==version             -> PyPI registry
//
// An unrecognized/unpinned command returns ok=false; the caller skips it
// (the #166 boot gate already refuses to launch such a server).
func ParsePin(command []string) (Pin, bool) {
	if len(command) == 0 {
		return Pin{}, false
	}
	bin := command[0]
	isUVX := strings.Contains(bin, "uvx")
	isNPX := strings.Contains(bin, "npx")
	if !isNPX && !isUVX {
		return Pin{}, false
	}
	idx := 1
	for idx < len(command) && strings.HasPrefix(command[idx], "-") {
		idx++
	}
	if idx >= len(command) {
		return Pin{}, false
	}
	pkg := command[idx]
	if isUVX {
		eq := strings.LastIndex(pkg, "==")
		if eq <= 0 || eq+2 >= len(pkg) {
			return Pin{}, false
		}
		return Pin{Registry: "pypi", Pkg: pkg[:eq], Version: pkg[eq+2:]}, true
	}
	at := strings.LastIndex(pkg, "@")
	if at <= 0 || at+1 >= len(pkg) {
		return Pin{}, false
	}
	return Pin{Registry: "npm", Pkg: pkg[:at], Version: pkg[at+1:]}, true
}

// VersionParts is a small, honest semver-ish decomposition: up to three
// numeric components, with a prerelease suffix split off at the first
// '-' (npm dist-tags like "latest" and arbitrary labels parse as 0.0.0
// with the raw value kept as prerelease). It is intentionally not a
// full semver implementation — it only needs to decide "is the published
// latest newer than the pin" for notification purposes.
type VersionParts struct {
	Major, Minor, Patch int
	Prerelease          string
}

// ParseVersion parses a version string into VersionParts. It returns
// ok=false only when no numeric leading component can be read at all.
func ParseVersion(s string) (VersionParts, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return VersionParts{}, false
	}
	var p VersionParts
	core := s
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		core = s[:i]
		if s[i] == '-' {
			p.Prerelease = s[i+1:]
		}
	}
	parts := strings.Split(core, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			n = 0
		}
		switch i {
		case 0:
			p.Major = n
		case 1:
			p.Minor = n
		case 2:
			p.Patch = n
		}
	}
	if p.Major == 0 && p.Minor == 0 && p.Patch == 0 && core != "0" && core != "0.0" && core != "0.0.0" {
		// Non-numeric leading tag (e.g. a dist-tag): keep it as a
		// prerelease marker so it sorts below any numeric release.
		if p.Prerelease == "" {
			p.Prerelease = core
		}
	}
	return p, true
}

// CompareVersions returns <0 if a<b, 0 if equal, >0 if a>b using the
// semver-ish ordering of ParseVersion. A release (no prerelease) sorts
// above the same numeric version with a prerelease suffix.
func CompareVersions(a, b string) int {
	pa, oka := ParseVersion(a)
	pb, okb := ParseVersion(b)
	if !oka || !okb {
		return 0
	}
	for _, pair := range [][2]int{{pa.Major, pb.Major}, {pa.Minor, pb.Minor}, {pa.Patch, pb.Patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	switch {
	case pa.Prerelease == "" && pb.Prerelease != "":
		return 1
	case pa.Prerelease != "" && pb.Prerelease == "":
		return -1
	case pa.Prerelease < pb.Prerelease:
		return -1
	case pa.Prerelease > pb.Prerelease:
		return 1
	}
	return 0
}
