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
	"errors"
	"fmt"
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
const resolveRankPrompt = "A human approved this agent permission request. May this action run?"

// resolveRankTimeout bounds the Ask call, mirroring the decision lane's
// decisionAskTimeout: a resolve must not wedge on a slow ranker.
const resolveRankTimeout = 15 * time.Second

// resolveRankStateBudget caps the untrusted summary embedded in the ranked
// state (decision-lane precedent: bounded render, never the raw params).
const resolveRankStateBudget = 300

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
	if rl != nil {
		for _, p := range rl.Pending() {
			if p.ID == id {
				summary = p.Summary
				break
			}
		}
	}

	state := renderResolveRankState(approver, decision, summary)
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
		return nil
	}
	// Same vocabulary as the permission lane: "allow" confirms the human
	// verdict and anything else leans deny. But the rank TIGHTENS only on
	// the explicit "deny" answer at or above the threshold — a malformed
	// or unexpected label fails open, because a flaky ranker must never
	// lock humans out of their own gate.
	if res.Answer != decisionAnswerDeny {
		return nil
	}
	if res.Confidence < threshold {
		return nil // low confidence: human verdict stands
	}
	return fmt.Errorf("%w (confidence %.2f >= %.2f)", ErrResolveRanked, res.Confidence, threshold)
}

// renderResolveRankState builds the ranked state: trusted facts (approver
// identity, verb) as plain lines, the agent-controlled summary inside an
// explicit untrusted fence so injection-shaped text in it reads as data.
func renderResolveRankState(approver Approver, decision, summary string) string {
	var b strings.Builder
	b.WriteString("Approver: ")
	b.WriteString(approver.String())
	b.WriteString("\nHuman verdict: ")
	b.WriteString(decision)
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
