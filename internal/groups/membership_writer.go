package groups

// membership_writer.go — the subject-kind-routed membership writers
// (agent-groups leaf 03-mutation-gate T2, contract C5).
//
// Membership lives in TWO stores (C5 verbatim anchor):
//   - agent membership: the AGENT.md `groups:` frontmatter — mutated here
//     via a careful textual frontmatter edit with a parse round-trip
//     (ParseAgentText) guarding every write. There is no dedicated
//     agent-definition write path in the RPC layer today (the HTTP config
//     service rewrites the whole file and would drop unknown frontmatter),
//     so this writer edits ONLY the `groups:` line and validates the
//     result through the production parser before persisting.
//   - user membership: auth.Store.SetUserGroups — the store owns its own
//     persistence + rollback; this package only routes to it.
//
// Identity discipline (issue #76 class): these writers take NO key
// material. The CALLER (the RPC gate) must already have resolved and
// authorized the subject via the store/kernel-backed identity seam.

import (
	"fmt"
	"os"
	"strings"

	"github.com/caimlas/meept/internal/agents"
)

// AgentDefinitionPath resolves the AGENT.md file for an agent id. Order
// mirrors discovery priority (user tier first, then bundled): the id must
// be non-empty and free of path separators (validated, never joined raw).
// ok is false when the agent has no definition file on this host.
func AgentDefinitionPath(home, bundledDir, id string) (string, bool) {
	if id == "" || strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return "", false
	}
	candidates := []string{}
	if home != "" {
		candidates = append(candidates, fmt.Sprintf("%s/agents/%s/AGENT.md", strings.TrimRight(home, "/"), id))
	}
	if bundledDir != "" {
		candidates = append(candidates, fmt.Sprintf("%s/%s/AGENT.md", strings.TrimRight(bundledDir, "/"), id))
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// SetAgentGroups replaces an agent's frontmatter `groups:` membership.
// The file is rewritten ONLY when the new list differs; the write is
// textual (frontmatter line replacement / insertion), and the result is
// parsed back through the production parser and its Groups field compared
// before the rename lands. Passing an empty/nil list clears membership
// (the line is removed). Unknown agent (no definition file) is an error —
// the caller decides whether that is fatal.
func SetAgentGroups(path string, newGroups []string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from AgentDefinitionPath's validated candidates
	if err != nil {
		return fmt.Errorf("set agent groups: read %s: %w", path, err)
	}
	updated, err := rewriteFrontmatterGroups(string(data), newGroups)
	if err != nil {
		return fmt.Errorf("set agent groups: %w", err)
	}
	// Parse round-trip: the updated text must parse and carry exactly the
	// requested membership (leaf contract: agent-file writes go through
	// the same validation as manual edits).
	def, perr := agents.ParseAgentText(updated)
	if perr != nil {
		return fmt.Errorf("set agent groups: round-trip parse failed: %w", perr)
	}
	if !equalStrings(def.Groups, newGroups) {
		return fmt.Errorf("set agent groups: round-trip mismatch: parsed %v, want %v", def.Groups, newGroups)
	}
	// Atomic write: temp file in the same directory, then rename.
	return atomicWriteFile(path, []byte(updated), 0o644)
}

// rewriteFrontmatterGroups performs the textual `groups:` edit. The
// frontmatter block is located with the production splitter's delimiter
// rules (opening ---, closing --- on its own line); the groups line is
// replaced in place, or inserted after the opening marker when absent.
func rewriteFrontmatterGroups(text string, newGroups []string) (string, error) {
	frontmatter, body, err := splitFrontmatterBlock(text)
	if err != nil {
		return "", err
	}

	lines := strings.Split(frontmatter, "\n")
	out := make([]string, 0, len(lines)+1)
	replaced := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "groups:") {
			if !replaced {
				if len(newGroups) > 0 {
					out = append(out, groupsLine(newGroups))
				}
				replaced = true
			}
			// Drop duplicate groups lines (a malformed file would carry
			// several; the parser keeps the first — normalize to one).
			continue
		}
		out = append(out, line)
	}
	if !replaced && len(newGroups) > 0 {
		// Insert after the opening --- (first line of the block).
		if len(out) == 0 {
			out = append(out, groupsLine(newGroups))
		} else {
			out = append(out[:1], append([]string{groupsLine(newGroups)}, out[1:]...)...)
		}
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(strings.Join(out, "\n"))
	b.WriteString("\n---\n")
	if body != "" && !strings.HasPrefix(body, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(body)
	return b.String(), nil
}

// groupsLine renders the flow-style groups line the parser reads natively
// (single-word key, comma-separated list) — the same shape the e2e seeds
// and the shipped roster use. Entries containing YAML-special characters
// are double-quoted (YAML double-quoted scalar rules: backslash and
// double-quote escaped).
func groupsLine(groups []string) string {
	quoted := make([]string, len(groups))
	for i, g := range groups {
		quoted[i] = yamlScalar(g)
	}
	return "groups: [" + strings.Join(quoted, ", ") + "]"
}

// yamlScalar renders one scalar in flow context: bare when it is plain
// word-like (letters, digits, -, _, .), double-quoted otherwise.
func yamlScalar(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '/':
		default:
			q := strings.ReplaceAll(s, "\\", "\\\\")
			q = strings.ReplaceAll(q, "\"", "\\\"")
			return "\"" + q + "\""
		}
	}
	return s
}

// splitFrontmatterBlock locates the frontmatter block and returns its
// inner lines and the body. Same delimiter discipline as the production
// parser: opening --- first, closing --- on its own line.
func splitFrontmatterBlock(text string) (frontmatter, body string, err error) {
	trimmed := strings.TrimLeft(text, " \t\n\r")
	if !strings.HasPrefix(trimmed, "---") {
		return "", "", fmt.Errorf("no YAML frontmatter in agent definition")
	}
	rest := trimmed[3:]
	nl := strings.Index(rest, "\n")
	if nl == -1 {
		return "", "", fmt.Errorf("malformed frontmatter opening")
	}
	rest = rest[nl+1:]
	closePos := strings.Index(rest, "\n---")
	if closePos == -1 {
		return "", "", fmt.Errorf("no frontmatter closing marker")
	}
	frontmatter = rest[:closePos]
	after := rest[closePos+4:]
	if _, body, ok := strings.Cut(after, "\n"); ok {
		return frontmatter, body, nil
	}
	return frontmatter, "", nil
}

// equalStrings compares unordered-equal string slices (nil == empty).
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
		if seen[s] < 0 {
			return false
		}
	}
	return true
}

// atomicWriteFile writes data to path via a same-directory temp file and
// rename (crash-atomic on POSIX), preserving 0-prior content permissions
// expectations via perm.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := dirOf(path)
	tmp, err := os.CreateTemp(dir, ".groups-membership-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

func dirOf(path string) string {
	i := strings.LastIndexByte(path, '/')
	if i <= 0 {
		return "."
	}
	return path[:i]
}
