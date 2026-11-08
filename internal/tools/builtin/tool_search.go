package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// ToolSearchDefaultLimit is the number of matches returned when the caller
// omits a positive limit. Pinned by the 00A cost sketch
// (docs/research/token-efficiency-2026-10/meept-token-audit.md): 8 keeps the
// result payload well under the ~168-token always-full budget.
const ToolSearchDefaultLimit = 8

// ToolSearchMaxLimit caps the caller-supplied limit. Values above this are
// clamped down to the cap so a single over-eager call cannot blow the token
// budget or the result envelope.
const ToolSearchMaxLimit = 20

// toolSearchDigestMaxLines bounds the category digest emitted on a no-match
// result. The 00A sketch pins the digest to ~10 lines / ~150 tokens; capping
// the line count keeps the model-visible payload inside that budget.
const toolSearchDigestMaxLines = 10

// toolSearchDescMaxLen is the one-line description cap applied to each match
// (and to each tool name in the digest) so a long multi-sentence tool
// description cannot push a single row past one visual line.
const toolSearchDescMaxLen = 140

// categorizer is the optional Category() seam. The base [tools.Tool]
// interface does not carry Category(); ~118 builtins add it. Tools that lack
// it fall back to the "general" group in the no-match digest.
type categorizer interface {
	Category() string
}

// ToolSearchTool is the search meta-tool for indexed/predicted tool-exposure
// modes (docs/research/dynamic-tools-design.md §3.2, §6). Given a short
// query it scans the FULL scope registry — the caller-supplied role ceiling,
// never the filtered active/exposed subset — matching tool names and their
// one-line descriptions, and returns the top-k matches as structured JSON
// (name + one-line description each). On a zero-hit query it returns a
// structured no-match result that names the category digest so the model can
// retry with a broader query (§6 row 2). The meta-tool is always-full and
// always active; it is the recovery vehicle for unlisted-but-in-ceiling
// tools (pair with tool_view to expand a hit to its full schema).
type ToolSearchTool struct {
	tools.ToolDefaults

	// scope is the search domain: the agent's role ceiling. It is the FULL
	// registry or a ceiling-filtered view; it is never the active/exposed
	// subset. Nil (zero value) yields a no-match digest result.
	scope agent.ToolRegistry
}

// NewToolSearchTool creates a tool_search meta-tool bound to scope, the
// caller-supplied role ceiling. scope is the FULL registry or a
// ceiling-filtered view (agent.NewFilteredToolRegistry); it is deliberately
// never the filtered active/exposed subset so unlisted-but-in-ceiling tools
// stay discoverable. scope may be nil; a nil scope reports a no-match result
// with an empty digest.
func NewToolSearchTool(scope agent.ToolRegistry) *ToolSearchTool {
	return &ToolSearchTool{scope: scope}
}

// SetScope re-binds the search domain to a (possibly narrower, ceiling-
// scoped) view. The engine (dynamic-tools-design.md §3.2) uses this to hand
// the tool the agent's role ceiling per episode; the daemon-side
// registration defaults to the full registry. Nil-safe: a nil argument
// clears the scope so the next search reports a no-match digest result.
func (t *ToolSearchTool) SetScope(scope agent.ToolRegistry) {
	if t == nil {
		return
	}
	t.scope = scope
}

func (t *ToolSearchTool) Name() string { return "tool_search" }

func (t *ToolSearchTool) Category() string { return "meta" }

func (t *ToolSearchTool) Description() string {
	return "Search the tool catalogue by name or description. " +
		"Use it to find a tool that is not in your current list; " +
		"then expand a hit with tool_view{name} before calling it."
}

func (t *ToolSearchTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropQuery: {
				Type:        schemaTypeString,
				Description: "Substring to match against tool names and descriptions.",
			},
			schemaPropLimit: {
				Type: schemaTypeInteger,
				Description: fmt.Sprintf(
					"Max matches to return (1-%d). Defaults to %d.",
					ToolSearchMaxLimit, ToolSearchDefaultLimit),
			},
		},
		Required: []string{schemaPropQuery},
	}
}

// toolSearchMatch is one row of the result payload.
type toolSearchMatch struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// toolSearchNoMatch is the structured zero-hit payload. It names the
// category digest so the model can broaden its query (§6 row 2).
type toolSearchNoMatch struct {
	Query      string   `json:"query"`
	Digest     []string `json:"category_digest"`
	RetryHint  string   `json:"retry_hint"`
	TotalCount int      `json:"total_tools"`
}

// toolSearchPayload is the always-success result envelope. Matches is set on
// a hit; NoMatch is set on a zero-hit query. Exactly one of the two is
// non-nil.
type toolSearchPayload struct {
	Query   string             `json:"query"`
	Limit   int                `json:"limit"`
	Matches []toolSearchMatch  `json:"matches,omitempty"`
	NoMatch *toolSearchNoMatch `json:"no_match,omitempty"`
}

// Execute searches the scope registry for tools whose name or one-line
// description contains the (case-insensitive) query. Results are ordered
// deterministically by tool name; there is no time- or map-iteration-
// dependent ordering anywhere in the path.
func (t *ToolSearchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	query, _ := args[schemaPropQuery].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	needle := strings.ToLower(query)
	limit := t.parseLimit(args[schemaPropLimit])

	// Collect the candidate tools from the scope. A nil scope is legal and
	// just yields an empty candidate set (reported as a no-match digest).
	// bughunt config-tools F-1: prefer the registry the executor injected
	// into this call's context (the AGENT's filtered view = role ceiling,
	// canonical names + group intersection already applied). The
	// daemon-wide registration bound t.scope to the FULL registry, so any
	// agent could discover out-of-ceiling names/descriptions. The ctx seam
	// is per-call (unlike SetScope on a shared singleton, which would race
	// across concurrent agents); t.scope remains the fallback for
	// standalone callers (tests, direct tool use).
	scope := agent.ToolRegistryFromContext(ctx)
	if scope == nil && t != nil {
		scope = t.scope
	}
	var candidates []tools.Tool
	if scope != nil {
		candidates = scope.List()
	}
	// List() already sorts by name, but re-sort so the guarantee holds even
	// if a future scope provider returns an unsorted view. Deterministic.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Name() < candidates[j].Name()
	})

	matches := make([]toolSearchMatch, 0, limit)
	for _, tool := range candidates {
		if tool == nil {
			continue
		}
		name := strings.ToLower(tool.Name())
		desc := strings.ToLower(oneLineDescription(tool.Description()))
		if strings.Contains(name, needle) || strings.Contains(desc, needle) {
			matches = append(matches, toolSearchMatch{
				Name:        tool.Name(),
				Description: oneLineDescription(tool.Description()),
			})
			if len(matches) >= limit {
				break
			}
		}
	}

	payload := toolSearchPayload{Query: query, Limit: limit}
	if len(matches) > 0 {
		payload.Matches = matches
	} else {
		digest := buildToolSearchDigest(candidates)
		payload.NoMatch = &toolSearchNoMatch{
			Query:      query,
			Digest:     digest,
			RetryHint:  "Try a broader query: search a category keyword or a shorter substring.",
			TotalCount: len(candidates),
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool_search result: %w", err)
	}

	return tools.ToolResult{
		Success: true,
		Result:  string(raw),
		Evidence: []models.Evidence{
			models.NewEvidence(
				models.EvidenceAPIResponse,
				query,
				fmt.Sprintf("matches=%d limit=%d", len(matches), limit),
				t.Name(),
			),
		},
	}, nil
}

// buildToolSearchDigest groups the scope's tools by category (tools without a
// Category() implementation fall into "general") and returns deterministic,
// one-line summaries. Categories are sorted by name and tool names are sorted
// within a category, so the digest is stable across calls (no time- or
// map-iteration dependence). At most toolSearchDigestMaxLines category lines
// are emitted; when there are more categories, the remainder is collapsed
// into a single trailing summary line so the digest stays within the ~150
// token 00A budget.
func buildToolSearchDigest(toolList []tools.Tool) []string {
	// Group tool names by category label.
	groups := make(map[string][]string)
	for _, tool := range toolList {
		if tool == nil {
			continue
		}
		cat := "general"
		if c, ok := tool.(categorizer); ok {
			if v := strings.TrimSpace(c.Category()); v != "" {
				cat = v
			}
		}
		groups[cat] = append(groups[cat], tool.Name())
	}

	cats := make([]string, 0, len(groups))
	for c := range groups {
		cats = append(cats, c)
	}
	sort.Strings(cats)

	// Reserve one trailing line for a collapsed summary when the category
	// count exceeds the budget; otherwise use the full budget.
	budget := toolSearchDigestMaxLines
	if len(cats) > budget {
		budget--
	}

	lines := make([]string, 0, len(cats))
	for i, c := range cats {
		if i >= budget {
			break
		}
		names := groups[c]
		sort.Strings(names)
		shown := names
		if len(names) > 6 {
			shown = names[:6]
		}
		line := fmt.Sprintf("%s (%d): %s", c, len(names), strings.Join(shown, ", "))
		if len(names) > 6 {
			line += ", ..."
		}
		lines = append(lines, line)
	}
	if len(cats) > budget {
		truncated := len(cats) - budget
		remainder := 0
		for _, c := range cats[budget:] {
			remainder += len(groups[c])
		}
		lines = append(lines, fmt.Sprintf("+ %d more categories (%d tools)", truncated, remainder))
	}
	return lines
}

// parseLimit coerces a caller-supplied limit arg (int, int64, float64,
// json.Number, or string) into a bounded 1..ToolSearchMaxLimit value,
// defaulting to ToolSearchDefaultLimit when the value is missing or
// non-positive.
func (t *ToolSearchTool) parseLimit(v any) int {
	n := ToolSearchDefaultLimit
	switch x := v.(type) {
	case int:
		n = x
	case int64:
		n = int(x)
	case float64:
		n = int(x)
	case json.Number:
		if i, err := x.Int64(); err == nil {
			n = int(i)
		}
	case string:
		if i, err := strconv.Atoi(x); err == nil {
			n = i
		}
	}
	if n <= 0 {
		return ToolSearchDefaultLimit
	}
	if n > ToolSearchMaxLimit {
		return ToolSearchMaxLimit
	}
	return n
}

// oneLineDescription collapses a tool's multi-sentence description to a
// single line: internal whitespace becomes single spaces, and the result is
// capped at toolSearchDescMaxLen runes (with an ellipsis). The cap is rune-
// based (not byte-based) so a non-ASCII description is never cut mid-rune.
func oneLineDescription(desc string) string {
	s := strings.Join(strings.Fields(desc), " ")
	runes := []rune(s)
	if len(runes) > toolSearchDescMaxLen {
		return string(runes[:toolSearchDescMaxLen]) + "…"
	}
	return s
}

// IsReadOnly reports that searching tool metadata never mutates state.
func (t *ToolSearchTool) IsReadOnly(map[string]any) bool { return true }

// IsConcurrencySafe reports that concurrent searches are safe (read-only).
func (t *ToolSearchTool) IsConcurrencySafe(map[string]any) bool { return true }

// Ensure ToolSearchTool implements the Tool interface.
var _ tools.Tool = (*ToolSearchTool)(nil)
