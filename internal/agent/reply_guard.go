package agent

import (
	"log/slog"
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
	out, _, _ := replaceCatalogReply(reply)
	return out
}

// replyGuardMatch identifies the detector and the exact token that matched a
// machine-shaped reply. It exists so the WARN emitted on replacement can name
// the trigger: "the reply guard replaced a reply" is not diagnosable from the
// log, "the reply matched the uptime_seconds JSON key" is.
type replyGuardMatch struct {
	// Rule is the detector that matched: raw_platform_json, agents_header, or
	// tools_totals_line.
	Rule string
	// Matched is the literal text that matched, bounded to
	// replyGuardTokenLimit runes: the platform_* payload key, the roster
	// header, or the totals line fragment.
	Matched string
	// Category selects the fallback wording: status, agents, or tools.
	Category string
}

// replyGuardTokenLimit bounds the matched-token attribute so a one-line
// mega-dump cannot smuggle a full payload into the log through the token.
const replyGuardTokenLimit = 120

// replaceCatalogReply classifies a reply and, when it is machine-shaped,
// returns the user-language fallback plus the match that triggered the
// replacement. It is the pure core shared by every guard entry point: the
// second return is only meaningful when the last return is true.
func replaceCatalogReply(reply string) (string, replyGuardMatch, bool) {
	if reply == "" {
		return reply, replyGuardMatch{}, false
	}

	match, ok := detectCatalogReply(reply)
	if !ok {
		return reply, replyGuardMatch{}, false
	}

	// Prose pass-through — EXCEPT for the agent roster (A2 leak, 2026-09-04
	// e2e run: a roster wrapped in enough preamble prose cleared the 40%
	// bar and shipped to a naive user). The roster header is unambiguous:
	// no legitimate assistant reply contains it, so it always sanitizes.
	// Headerless dumps still get the prose-ratio escape hatch.
	if match.Category != "agents" && match.Category != "tool_result" && proseLineRatio(reply) > 0.40 {
		return reply, replyGuardMatch{}, false
	}

	if match.Category == "tool_result" {
		return "the tool ran, but the result came back as raw data instead of an answer. ask me to do something specific — for example 'make me a program that ...' — and i'll get to work.", match, true
	}

	return "i looked up the platform " + match.Category + " information, but that isn't a useful answer on its own. ask me to do something specific — for example 'make me a program that ...' — and i'll get to work.", match, true
}

// detectCatalogReply applies the three detectors in their original precedence
// order (raw platform JSON, then the roster header, then the totals line) and
// reports which one fired.
func detectCatalogReply(reply string) (replyGuardMatch, bool) {
	if key, ok := matchedPlatformJSONKey(reply); ok {
		return replyGuardMatch{Rule: "raw_platform_json", Matched: key, Category: "status"}, true
	}
	if strings.Contains(reply, "## Available Agents") {
		return replyGuardMatch{Rule: "agents_header", Matched: "## Available Agents", Category: "agents"}, true
	}
	if token, ok := matchedTotalsLine(reply); ok {
		return replyGuardMatch{Rule: "tools_totals_line", Matched: token, Category: "tools"}, true
	}
	if key, ok := toolResultJSONKey(reply); ok {
		return replyGuardMatch{Rule: "tool_result_json", Matched: key, Category: "tool_result"}, true
	}
	return replyGuardMatch{}, false
}

// toolResultJSONKey reports whether the reply is a raw tool-result JSON dump:
// machine output forwarded verbatim to the user (memory_store's
// {"success": true, "memory_id": ..., "type": "task"} and friends) rather than
// an answer written for a person. Observed end to end 2026-09-13: a chat turn
// replied with two concatenated memory_store result objects and the guard had
// no rule for that shape.
//
// It is deliberately narrow, because the guard REPLACES whatever it matches:
// the reply must be pure JSON (it starts with '{' or '[', ends with the
// matching brace, and every non-blank line is structural) and must carry a
// tool-result identity key. A fenced or prose-wrapped JSON answer fails the
// first test and passes through untouched.
func toolResultJSONKey(reply string) (string, bool) {
	trimmed := strings.TrimSpace(reply)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	if !strings.HasSuffix(trimmed, "}") && !strings.HasSuffix(trimmed, "]") {
		return "", false
	}
	for _, line := range strings.Split(trimmed, "\n") {
		if s := strings.TrimSpace(line); s != "" && !isStructuralLine(s) {
			return "", false
		}
	}
	for _, key := range []string{`"memory_id"`, `"task_id"`, `"job_id"`, `"tool_result"`, `"tool_output"`, `"step_id"`, `"evidence"`} {
		if strings.Contains(reply, key) {
			return key, true
		}
	}
	return "", false
}

// matchedTotalsLine reports whether the reply carries a tools/agents catalog
// totals line and returns that line (bounded) for the log.
func matchedTotalsLine(reply string) (string, bool) {
	if !strings.Contains(reply, "*Total: ") {
		return "", false
	}
	if !strings.Contains(reply, " tools*") && !strings.Contains(reply, " agents*") {
		return "", false
	}
	for _, line := range strings.Split(reply, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, "*Total: ") {
			continue
		}
		if strings.Contains(trimmed, " tools*") || strings.Contains(trimmed, " agents*") {
			return boundToken(trimmed), true
		}
	}
	return "", false
}

// boundToken truncates a matched token to replyGuardTokenLimit runes.
func boundToken(token string) string {
	return truncateRunes(token, replyGuardTokenLimit, "...")
}

// looksLikeRawPlatformJSON reports whether the reply is (mostly) a raw JSON
// document carrying platform_status/platform_tools/platform_agents payload
// keys, rather than prose that merely mentions them.
func looksLikeRawPlatformJSON(reply string) bool {
	_, ok := matchedPlatformJSONKey(reply)
	return ok
}

// matchedPlatformJSONKey reports whether the reply is a raw platform_* JSON
// document and returns the payload key that matched, so the guard WARN can
// name the key rather than the generic shape.
func matchedPlatformJSONKey(reply string) (string, bool) {
	trimmed := strings.TrimSpace(reply)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	for _, key := range []string{
		`"status": "running"`,
		`"uptime_seconds"`,
		`"tools": [`,
		`"agents": [`,
	} {
		if strings.Contains(reply, key) {
			return key, true
		}
	}
	return "", false
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

// replyGuardContext carries the diagnosis fields for the WARN emitted when the
// guard replaces a reply. Every field is optional; the zero value is valid.
type replyGuardContext struct {
	Agent          string
	Intent         string
	SessionID      string
	ConversationID string
}

// replyGuardAgent resolves the agent attribution for the guard WARN: the
// caller's explicit override wins, otherwise the dispatcher's decision. Direct
// mode (result == nil) logs an empty agent.
func replyGuardAgent(requested string, result *DispatchResult) string {
	if requested != "" {
		return requested
	}
	if result != nil {
		return result.AgentID
	}
	return ""
}

// replyGuardIntent resolves the classified intent type for the guard WARN.
// Direct-mode replies carry no DispatchResult, so the attribute is empty.
func replyGuardIntent(result *DispatchResult) string {
	if result == nil || result.Intent == nil {
		return ""
	}
	return result.Intent.Type
}

// replyGuardPreviewLimit bounds the preview attribute attached to the guard
// WARN. The replaced text is usually a full platform catalog (tens of KB); the
// log needs enough to recognise the reply, never the payload.
const replyGuardPreviewLimit = 240

// replyGuardPreviewSuffix marks a truncated preview so an operator can tell a
// short reply from a clipped one.
const replyGuardPreviewSuffix = "...(truncated)"

// applyReplyGuard is the RunOnce response-assembly seam (plan leaf 05
// Task 2). The single final-response return site calls this on the
// assistant text before it is persisted and returned as
// ChatResponse.Reply. Kept as a one-line seam so the guard is testable
// without driving a full reasoning turn.
//
// A replacement is logged at WARN through the process default logger. Callers
// that can supply agent/intent/session context use applyReplyGuardLogged
// instead; both share replaceCatalogReply, so a reply is reported exactly once
// by whichever seam performed the replacement.
func applyReplyGuard(final string) string {
	return applyReplyGuardLogged(final, slog.Default(), replyGuardContext{})
}

// applyReplyGuardLogged applies the reply guard and, when it replaces a
// machine-shaped reply, emits one WARN naming the rule that matched.
//
// Replacement behaviour is identical to applyReplyGuard: same detectors, same
// precedence, same prose escape hatch, same fallback wording. This is an
// observability seam only — a misroute that used to be invisible (the user saw
// the canned fallback, the operator saw nothing) is now diagnosable from the
// log alone.
func applyReplyGuardLogged(final string, logger *slog.Logger, ctx replyGuardContext) string {
	guarded, match, replaced := replaceCatalogReply(final)
	if !replaced {
		return guarded
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("reply guard replaced a machine-shaped reply",
		"rule", match.Rule,
		"matched", match.Matched,
		"agent", ctx.Agent,
		"intent", ctx.Intent,
		"session_id", ctx.SessionID,
		"conversation_id", ctx.ConversationID,
		"replaced_chars", len([]rune(final)),
		"preview", replyGuardPreview(final),
	)
	return guarded
}

// replyGuardPreview returns a single-line, bounded excerpt of a reply for the
// guard WARN. Whitespace and control characters are collapsed so a multi-line
// catalog cannot inject newlines (or fake log fields) into the log, and the
// result is truncated on a rune boundary to replyGuardPreviewLimit runes plus
// the truncation marker.
func replyGuardPreview(reply string) string {
	collapsed := strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '	':
			return ' '
		default:
			if r < 0x20 || r == 0x7f {
				return ' '
			}
			return r
		}
	}, reply)
	collapsed = strings.Join(strings.Fields(collapsed), " ")
	return truncateRunes(collapsed, replyGuardPreviewLimit, replyGuardPreviewSuffix)
}

// truncateRunes truncates s to at most limit runes, appending suffix when it
// had to cut. Truncation is rune-safe: no multi-byte character is split.
func truncateRunes(s string, limit int, suffix string) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + suffix
}
