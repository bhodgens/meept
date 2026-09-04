package agent

import (
	"strings"
)

// sanitizeCatalogReply guards the chat reply path against machine-shaped
// tool output leaking through as the user-facing answer. The platform_*
// tools (platform_status / platform_tools / platform_agents) emit large
// catalogs, rosters, and status dumps; three 2026-09-04 runs returned such
// output verbatim to a naive user (audit finding F5). A reply matching any
// detector below is replaced with a short lowercase user-language fallback
// naming what was looked up. Genuine prose (even markdown-heavy) passes
// through byte-identical.
//
// Detection heuristic (chat-path only; delegation and TUI panels still get
// the raw tool outputs via their own RPC surfaces):
//
//	rawJSON     = trimmed reply starts with '{' or '[' AND contains a
//	              platform_* payload key: "status": "running",
//	              "uptime_seconds", "tools": [, "agents": [
//	agentRoster = contains the "## Available Agents" header
//	toolCatalog = contains "*Total: " AND (" tools*" or " agents*")
//
// Detection only runs on non-empty replies that do NOT look like prose
// (>40% non-symbol word content outside a matched header means genuine
// prose — pass through). Catalog replies are dominated by '-'/'*' bullets
// and JSON braces, so the cheap shape check keeps the false-positive rate
// near zero on ordinary answers.
func sanitizeCatalogReply(reply string) string {
	if reply == "" {
		return reply
	}

	category := ""
	switch {
	case looksLikeRawPlatformJSON(reply):
		category = "status"
	case strings.Contains(reply, "## Available Agents"):
		category = "agents"
	case strings.Contains(reply, "*Total: ") &&
		(strings.Contains(reply, " tools*") || strings.Contains(reply, " agents*")):
		category = "tools"
	}
	if category == "" {
		return reply
	}

	// Prose pass-through: a reply whose non-blank lines are mostly running
	// text (>40% outside structural bullet/header/brace lines) is the model
	// speaking, not a dump. Catalog replies are dominated by '-'/'*' bullet
	// lines, markdown headers, and JSON braces, so they stay under the bar.
	if proseLineRatio(reply) > 0.40 {
		return reply
	}

	return "i looked up the platform " + category + " information, but that isn't a useful answer on its own. ask me to do something specific — for example 'make me a program that ...' — and i'll get to work."
}

// looksLikeRawPlatformJSON reports whether the reply is (mostly) a raw JSON
// document carrying platform_status/platform_tools/platform_agents payload
// keys, rather than prose that merely mentions them.
func looksLikeRawPlatformJSON(reply string) bool {
	trimmed := strings.TrimSpace(reply)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return false
	}
	for _, key := range []string{
		`"status": "running"`,
		`"uptime_seconds"`,
		`"tools": [`,
		`"agents": [`,
	} {
		if strings.Contains(reply, key) {
			return true
		}
	}
	return false
}

// proseLineRatio estimates what fraction of a reply's non-blank lines are
// running text rather than structural catalog scaffolding. It is
// deliberately cheap (no regex): bullet lines ('-'/'*' led), markdown
// headers ('#'), table rows ('|'), and JSON structural lines (braces,
// brackets, `"key": value` pairs) vote as structure; lines of plain text
// vote as prose. A single prose sentence inside an otherwise machine-shaped
// dump (e.g. the roster's embedded system-prompt fragment) stays under the
// threshold, while an ordinary multi-line written answer passes cleanly.
func proseLineRatio(reply string) float64 {
	lines := strings.Split(reply, "\n")
	prose := 0.0
	structure := 0.0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isStructuralLine(trimmed) {
			structure++
			continue
		}
		prose++
	}
	total := prose + structure
	if total == 0 {
		return 1
	}
	return prose / total
}

// isStructuralLine reports whether a single trimmed line looks like catalog,
// roster, or JSON scaffolding rather than a sentence of prose.
func isStructuralLine(trimmed string) bool {
	// JSON structural lines: braces, brackets, or "key": value pairs.
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "}") ||
		strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "]") {
		return true
	}
	// JSON key/value line: starts with a quoted key followed by ": ".
	if strings.HasPrefix(trimmed, `"`) && strings.Contains(trimmed, `": `) {
		return true
	}
	if strings.HasPrefix(trimmed, "#") {
		return true
	}
	if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") {
		return true
	}
	if strings.HasPrefix(trimmed, "|") {
		return true
	}
	return false
}

// applyReplyGuard is the RunOnce response-assembly seam (plan leaf 05
// Task 2). The single final-response return site calls this on the
// assistant text before it is persisted and returned as
// ChatResponse.Reply. Kept as a one-line seam so the guard is testable
// without driving a full reasoning turn.
func applyReplyGuard(final string) string {
	return sanitizeCatalogReply(final)
}
