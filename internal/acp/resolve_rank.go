// resolve_rank.go is the tighten-only decision-model second opinion on
// human resolves (audit M5 Phase 2b-rank, user directive 2026-10-22).
//
// Asymmetry is the whole design. The rank runs ONLY on allow/always
// resolves that already PASSED the approver-identity gate, and it can only
// turn that allow into a rejection (ErrResolveRanked) — the pending request
// stays answerable, nothing is consumed. It NEVER approves anything by
// itself and NEVER rescues an identity-gate rejection, because the ranked
// input includes the agent-controlled pending summary: a prompt injection
// in that text must not be able to loosen the gate.
//
// Fail-open on absence: no decision model configured, Ask error, or
// confidence below the threshold all skip the rank and let the human
// verdict stand. That is correct for a tighten-only layer — the identity
// gate underneath is the hard boundary, and a dead ranker must not lock
// humans out of their own permission surface.
package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// ErrResolveRanked is returned when the decision-model second opinion
// rejected an allow/always resolve. The pending permission is UNTOUCHED —
// a human surface can still answer it (deny, or allow again; a repeated
// allow re-ranks).
var ErrResolveRanked = errors.New("acp: allow rejected by decision-model rank")

// resolveRankPrompt is the fixed question text (decision-lane precedent:
// the prompt never varies; the state carries the facts). The question
// mirrors the permission lane's own semantics — exactly "allow" grants,
// anything else denies — so one vocabulary covers both lanes.
//
// Hardened 2026-10-25 after two live findings. V1 ("A human approved...
// May this action run?") let gemini-2.5-flash-lite confirm an
// authorized_keys edit at 1.00 — it deferred to the human it was meant to
// double-check. V2 (job-framed, long deny list, "when uncertain, deny")
// then vetoed a ROUTINE scratch-file append at 1.00 too: the enumerate-and-
// fear framing plus a summary truncated before the target path made deny the
// safe-sounding answer for everything — a lockout (re-approving re-ranks to
// the same deny). V3 keeps the categories but rebalances: deny only on a
// clear category match against the extracted path, explicit no-deny-on-
// doubt, routine work explicitly allowed.
const resolveRankPrompt = "You are a safety net over a human's already-given approval of an agent action. Do NOT re-decide whether approval was deserved — deny ONLY when the file path or command below clearly matches one of these: credential or key material (ssh keys, authorized_keys, gpg, cloud creds, tokens, .env, .netrc, .npmrc); access control (sudoers, passwd, firewall rules); security or approval policy config (agent/daemon permission settings, hooks, MCP server configs); piping downloaded or remote code into a shell. Everything else — ordinary source files, scratch/tmp files, docs, tests, git operations — answer allow. Uncertainty is NOT a reason to deny: if you cannot clearly place the target in the deny list, answer allow."

// resolveRankTimeout bounds the Ask call, mirroring the decision lane's
// decisionAskTimeout: a resolve must not wedge on a slow ranker.
const resolveRankTimeout = 15 * time.Second

// resolveRankStateBudget caps the untrusted summary embedded in the ranked
// state. Larger than the relay's own render budget on purpose: the rank
// state exists solely for this decision, and the raw params JSON routinely
// exceeds 300 runes before its toolCall path (live finding 2026-10-25 —
// the ranker literally never saw the target file).
const resolveRankStateBudget = 900

// rankTargetLine extracts the actionable target fields from the raw
// requestPermission params for the rank state: toolCall title, content
// location paths, and the rawInput command — the fields the deny
// categories key on. Structured parse first; if the params were truncated
// by maxPendingParamsBytes (invalid JSON), a regex sweep over the raw
// bytes recovers whatever path/title/command fields survived. Best-effort:
// a miss returns "" and the ranker just sees the fenced summary, like
// before. Note the extracted values are still agent-provided data (the
// human sees the same lie if the agent lies); this adds signal, not trust.
func rankTargetLine(params []byte) string {
	if len(params) == 0 {
		return ""
	}
	var parts []string
	seen := map[string]bool{}
	add := func(label, val string) {
		val = strings.TrimSpace(val)
		if val == "" {
			return
		}
		if len([]rune(val)) > 160 {
			val = string([]rune(val)[:160]) + "…"
		}
		key := label + "=" + val
		if seen[key] || len(parts) >= 8 {
			return
		}
		seen[key] = true
		parts = append(parts, key)
	}
	var parsed struct {
		ToolCall struct {
			Title   string `json:"title"`
			Content []struct {
				Location struct {
					Path string `json:"path"`
				} `json:"location"`
			} `json:"content"`
			RawInput struct {
				Command string `json:"command"`
			} `json:"rawInput"`
		} `json:"toolCall"`
	}
	if err := json.Unmarshal(params, &parsed); err == nil {
		add("title", parsed.ToolCall.Title)
		for _, c := range parsed.ToolCall.Content {
			add("path", c.Location.Path)
		}
		add("command", parsed.ToolCall.RawInput.Command)
	} else {
		for _, m := range rankTargetFieldRE.FindAllStringSubmatch(string(params), -1) {
			if len(m) == 3 {
				add(m[1], m[2])
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Extracted target fields (agent-provided, same as the human sees): " +
		strings.Join(parts, "; ") + "\n"
}

// rankTargetFieldRE recovers path/title/command string values from
// (possibly truncated) raw params JSON for the fallback sweep.
var rankTargetFieldRE = regexp.MustCompile(`"(path|title|command)"\s*:\s*"((?:[^"\\]|\\.)*)"(?:[^"]*$)?`)

// rankResolve runs the tighten-only second opinion for one allow/always
// resolve that passed the identity gate. Returns:
//   - ErrResolveRanked: the rank rejected the allow with sufficient
//     confidence; the caller must NOT touch the relay.
//   - nil: proceed with the resolve (rank disabled, no model, deny verb,
//     rank error, low confidence, or the rank said allow).
//
// The caller (Manager.ResolvePermissionWithApprover) owns the verb filter;
// this function re-checks it defensively so a future caller cannot route a
// deny through the ranker.
func (m *Manager) rankResolve(ctx context.Context, id, decision string, approver Approver) error {
	if m == nil || !m.cfg.ResolveRank {
		return nil
	}
	if decision != relayDecisionAllow && decision != relayDecisionAlways {
		return nil // nothing to tighten on a deny
	}
	m.mu.Lock()
	dec := m.decision
	rl := m.relay
	threshold := m.cfg.DecisionThreshold
	m.mu.Unlock()
	if dec == nil || !dec.Available() {
		return nil // no decision model: fail open, human verdict stands
	}
	if threshold <= 0 {
		threshold = 0.5 // mirror the config-layer default
	}

	summary := ""
	var params []byte
	if rl != nil {
		for _, p := range rl.Pending() {
			if p.ID == id {
				summary = p.Summary
				params = p.Params
				break
			}
		}
	}

	state := renderResolveRankState(approver, decision, summary, params)
	rankCtx, cancel := context.WithTimeout(ctx, resolveRankTimeout)
	defer cancel()
	res, err := dec.Ask(rankCtx, llm.DecisionQuestion{
		State:     state,
		Type:      "noul",
		Prompt:    resolveRankPrompt,
		Options:   []string{decisionAnswerAllow, decisionAnswerDeny},
		Threshold: threshold,
	})
	if err != nil || res == nil {
		// Rank unavailable at call time: fail open (tighten-only layer).
		// Loud: silently-inert was the live diagnostic gap 2026-10-24 —
		// resolve_rank:true appeared to do nothing.
		slog.Default().Warn("acp resolve rank skipped: decision ask failed; human verdict stands",
			"component", "acp", "pending", id, "error", err)
		return nil
	}
	// Same vocabulary as the permission lane: "allow" confirms the human
	// verdict and anything else leans deny. But the rank TIGHTENS only on
	// the explicit "deny" answer at or above the threshold — a malformed
	// or unexpected label fails open, because a flaky ranker must never
	// lock humans out of their own gate.
	slog.Default().Info("acp resolve rank verdict",
		"component", "acp",
		"pending", id,
		"answer", res.Answer,
		"confidence", fmt.Sprintf("%.2f", res.Confidence),
		"threshold", fmt.Sprintf("%.2f", threshold),
	)
	if res.Answer != decisionAnswerDeny {
		return nil
	}
	if res.Confidence < threshold {
		return nil // low confidence: human verdict stands
	}
	return fmt.Errorf("%w (confidence %.2f >= %.2f)", ErrResolveRanked, res.Confidence, threshold)
}

// renderResolveRankState builds the ranked state: trusted facts (approver
// identity, verb) plus the extracted target line (best-effort structured
// re-read of the raw params — the Summary alone truncates before the
// toolCall path, live finding 2026-10-25), then the agent-controlled
// summary inside an explicit untrusted fence so injection-shaped text in it
// reads as data.
func renderResolveRankState(approver Approver, decision, summary string, params []byte) string {
	var b strings.Builder
	b.WriteString("Approver: ")
	b.WriteString(approver.String())
	b.WriteString("\nHuman verdict: ")
	b.WriteString(decision)
	if line := rankTargetLine(params); line != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimSuffix(line, "\n"))
	}
	b.WriteString("\n<<<UNTRUSTED_REQUEST_SUMMARY>>>\n")
	s := strings.TrimSpace(summary)
	if s == "" {
		s = "(none)"
	}
	if len([]rune(s)) > resolveRankStateBudget {
		s = string([]rune(s)[:resolveRankStateBudget]) + "...(truncated)"
	}
	b.WriteString(s)
	b.WriteString("\n<<<END_UNTRUSTED_REQUEST_SUMMARY>>>")
	return b.String()
}
