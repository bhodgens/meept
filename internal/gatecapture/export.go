package gatecapture

import (
	"strings"
	"unicode/utf8"

	"github.com/caimlas/meept/internal/jevgate"
)

// serializeContext is the single serialization seam to the C2 template
// (internal/jevgate.SerializeCommandContext). The pending-capture pipeline
// NEVER accepts env or secret inputs - only command text, cwd, and session
// facts rendered by the byte-stable template - so a captured ContextBlob is
// structurally incapable of carrying secret values beyond the command text.
func serializeContext(command, cwd string, facts map[string]string) string {
	return jevgate.SerializeCommandContext(command, cwd, facts)
}

// maxPairText bounds chosen/rejected texts in exported rows so a pathological
// context blob cannot dominate a training batch. Large enough that no real
// command context clips.
const maxPairText = 16 * 1024

// clipText bounds s to maxPairText, preferring a whole-rune boundary when the
// clip lands mid-rune (best-effort; the contexts are command text).
func clipText(s string) string {
	if len(s) <= maxPairText {
		return s
	}
	cut := maxPairText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// exportRow is one DPO training row (Contract C5). The field mapping is
// FROZEN: prompt = ContextBlob (C2 bytes), chosen = the human decision text,
// rejected = the model's contrary position, pair_kind = "record" | "near_miss".
type exportRow struct {
	Prompt   string  `json:"prompt"`
	Chosen   string  `json:"chosen"`
	Rejected string  `json:"rejected"`
	PairKind string  `json:"pair_kind"`
	Margin   float64 `json:"margin"`
}

// pair kind vocabulary (frozen in C5).
const (
	pairKindRecord   = "record"
	pairKindNearMiss = "near_miss"
)

// Decision text labels (the chosen/rejected sides of a DPO row). The human
// verdict is the trustworthy chosen side; the rejected side is the model's
// contrary position (research section 2.1).
const (
	decisionTextAllowed = "allow: the human approver permitted this command"
	decisionTextDenied  = "deny: the human approver refused this command"
)

// modelScoreText renders the model's prior position text for a record:
// when the gate scored the command, the rejected side names the model's
// contrary band; when no prior exists, it is the neutral contrary position.
func modelScoreText(rec CommandGateRecord) string {
	var b strings.Builder
	b.WriteString("the model's prior position: ")
	switch {
	case rec.PriorConfidence > 0 || rec.PriorScore > 0 || rec.PriorDegraded:
		b.WriteString("score ")
		b.WriteString(formatBand(rec.PriorScore))
		if rec.PriorDegraded {
			b.WriteString(" (degraded)")
		}
	default:
		b.WriteString("no prior score recorded")
	}
	return b.String()
}

// formatBand renders a 0..1 score on the frozen 0.1-step label grid
// (llm.CommandScoreBands vocabulary), clamped.
func formatBand(score float64) string {
	switch {
	case score < 0:
		score = 0
	case score > 1:
		score = 1
	}
	// Nearest band label on the 0.1 grid.
	band := int(score*10 + 0.5)
	whole, frac := band/10, band%10
	return []string{"0.0", "0.1", "0.2", "0.3", "0.4", "0.5",
		"0.6", "0.7", "0.8", "0.9", "1.0"}[whole][0:1] + "." +
		string(rune('0'+frac))
}

// NearMissPrefixLen is the frozen near-miss prefix rule (Contract C5, C6):
// two records are a near-miss CONTRASTIVE group when their commands share a
// common prefix of at least this many characters (after trim) and differ in
// HumanDecision. The prefix rule is deliberately character-based and
// constant - deterministic over the same input set, order-independent, no
// similarity model, no randomness (research section 2.2: one pair per
// minimal differing prefix group).
const NearMissPrefixLen = 8

// sharesNearMissPrefix reports whether two command texts share the frozen
// minimum common prefix (case-sensitive: command text is captured verbatim).
func sharesNearMissPrefix(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if len(a) < NearMissPrefixLen || len(b) < NearMissPrefixLen {
		return false
	}
	return a[:NearMissPrefixLen] == b[:NearMissPrefixLen]
}

// deriveNearMissPairs derives contrastive pairs deterministically (Contract
// C5): for each minimal differing prefix group (records sharing the frozen
// prefix with divergent HumanDecision), exactly one near_miss pair emits,
// chosen = the allowed verdict's row, rejected = the denied verdict's row.
// Group membership iterates in the caller-provided (newest-first) record
// order; the derived set is identical for any permutation of the same input
// because a group's chosen/rejected sides are pinned by verdict, and one
// pair per group makes selection order-free.
func deriveNearMissPairs(records []CommandGateRecord) []exportRow {
	type groupKey struct {
		prefix      string
		hasAllow    bool
		hasDeny     bool
		allowRow    exportRow
		denyRow     exportRow
		allowSource CommandGateRecord
		denySource  CommandGateRecord
	}
	groups := make(map[string]*groupKey)
	var order []string

	for _, rec := range records {
		prefix := trimmedPrefix(rec.Command, NearMissPrefixLen)
		if prefix == "" {
			continue
		}
		g, ok := groups[prefix]
		if !ok {
			g = &groupKey{prefix: prefix}
			groups[prefix] = g
			order = append(order, prefix)
		}
		switch rec.HumanDecision {
		case DecisionAllowed:
			if !g.hasAllow {
				g.hasAllow = true
				g.allowRow = recordRow(rec, pairKindNearMiss)
				g.allowSource = rec
			}
		case DecisionDenied:
			if !g.hasDeny {
				g.hasDeny = true
				g.denyRow = recordRow(rec, pairKindNearMiss)
				g.denySource = rec
			}
		}
	}

	out := make([]exportRow, 0)
	for _, prefix := range order {
		g := groups[prefix]
		if !g.hasAllow || !g.hasDeny {
			continue
		}
		// ONE contrastive row per group: a DPO row IS a pair. Chosen
		// carries the ALLOWED verdict (the trustworthy side, research
		// section 2.1); rejected carries the DENIED verdict. The prompt is
		// the allowed record's C2 blob (the group shares the frozen
		// command prefix; the first-seen allowed record is deterministic
		// under the pinned verdict sides).
		row := g.allowRow
		row.Chosen = decisionTextAllowed
		row.Rejected = clipText(decisionTextDenied + "; " + modelScoreText(g.denySource))
		out = append(out, row)
	}
	return out
}

// trimmedPrefix returns the trimmed command's first n chars ("" when too
// short), the group key for the near-miss rule.
func trimmedPrefix(command string, n int) string {
	command = strings.TrimSpace(command)
	if len(command) < n {
		return ""
	}
	return command[:n]
}

// recordRow builds the export row for one record (the frozen C5 mapping).
// Margin is the constant 1.0: a human verdict is a hard label.
func recordRow(rec CommandGateRecord, pairKind string) exportRow {
	chosen := decisionTextDenied
	rejected := decisionTextAllowed
	if rec.HumanDecision == DecisionAllowed {
		chosen = decisionTextAllowed
		rejected = decisionTextDenied
	}
	return exportRow{
		Prompt:   clipText(rec.ContextBlob),
		Chosen:   chosen,
		Rejected: clipText(rejected + "; " + modelScoreText(rec)),
		PairKind: pairKind,
		Margin:   1.0,
	}
}
