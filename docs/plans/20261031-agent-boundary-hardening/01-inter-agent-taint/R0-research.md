# Inter-Agent Defense Research - Research Leaf (R0)

> **For the implementing agent:** You are the researcher for this leaf.
> This is a RESEARCH leaf: web_search + reading this repo's existing
> defenses, producing a findings document. NO production code. Do NOT
> commit - the orchestrator handles all git operations after review.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Research prompt-injection defense BETWEEN agents
  (spotlighting/marker techniques, taint tracking, over-blocking costs)
  and distill design decisions for the branch's implementation leaves.
- **Dependencies:** none
- **Estimated Context:** 25K
- **Concurrency Group:** A
- **Audit references:** master proposal 5; docs/plans/20261031-agent-boundary-hardening/

## Goal

Produce `docs/research/agent-boundary-taint-findings.md` (new file) that
grounds two design decisions the implementation leaves must honor:

1. How to label untrusted SEGMENTS of a handoff prompt without tainting
   the driver's own framing (spotlighting/datamarking vs strict taint).
2. What the deny-pattern set should cover and where over-blocking would
   hurt legitimate collaboration (default-deny vs warn modes).

## Context

The repo already has: `SanitizeInput` + `WrapUserInput`
(internal/agent/loop.go:2908 / ~:3016) and `SecurityTransformContext`
(internal/agent/security_hooks.go:100-137) - pattern-level defenses that
work today. `PromptGuard` markers exist at
internal/security/prompt_guard.go:88-129. Context isolation defaults to
`artifact_only` (internal/agent/isolation.go:26-28, fail-closed :93-97).
This research must COMPOSE with those, not duplicate them: read them first,
then research what they miss (cross-agent handoff trust, segment-level
labeling, forged control messages).

## Verified Anchors

- internal/agent/taint_hooks.go:156-198 - checkMessageTaint (the flaw:
  whole-message TaintUntrusted at :169)
- internal/security/taint/taint.go:29-30 - TaintUntrusted label exists
- internal/security/prompt_guard.go:88-129 - existing marker machinery
- internal/agent/isolation.go:26-28, :93-97 - artifact_only fail-closed

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Survey existing in-repo defenses listed in Context (read, do not modify) | read-only | grep confirms each symbol exists | n/a | survey section written | Given the anchors; When read; Then findings doc cites each with file:line |
| T2 | Web research: Microsoft/OWASP LLM guidance on prompt injection between agents; spotlighting/datamarking (delimiters, base64, labels); academic taint-tracking for LLM pipelines | docs/research/agent-boundary-taint-findings.md | file exists, citations present | missing file | section with >= 6 cited sources | Given research; When applied; Then each recommendation names its source |
| T3 | Web research: over-blocking costs of taint-based inter-agent defenses; mitigation patterns (modes, allowlists, audit-first rollout) | same file | section present | missing | modes + rollout recommendation | Given cost findings; When applied; Then default mode recommendation is explicit |
| T4 | Design decisions: (a) segment-labeling mechanism for handoffs, (b) deny-pattern inventory (impersonation, control-verb forgery, workspace_yield-style), (c) recommended config knobs + defaults | same file, "Design Decisions" section | section present | missing | decisions 1-3 written, each traceable to T2/T3 findings | Given findings; When applied; Then 01/02 leaves can cite a decision ID |

Findings doc format: ASCII markdown, sections Survey / Research / Costs /
Design Decisions (IDs D1, D2, ...). Every non-obvious claim carries a
citation (URL or file:line).

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `docs/research/agent-boundary-taint-findings.md` with a "Design
  Decisions" section whose decision IDs (D1..Dn) the implementation
  leaves cite in dispatch addenda.

### What This Leaf Consumes

- C-none (research leaf; consumes only the anchors above).

## Self-Verification Checklist

- [ ] Findings file exists at the exact path, ASCII only
- [ ] Every design decision cites a source (T2/T3) or a repo anchor (T1)
- [ ] No production code modified (`git status` shows only the new findings file)
- [ ] Recommendations compose with (not replace) SanitizeInput /
      WrapUserInput / SecurityTransformContext / PromptGuard

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [the findings file path]
2. **Verify command + output:** [grep results proving anchors cited]
3. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Findings file present, sections complete, citations real (spot-check URLs)
- [ ] Design decisions are actionable and traceable
- [ ] No production code touched

Output: APPROVED or specific gaps.

## Notes

- This leaf gates 01/02: the orchestrator converts D-decisions into dated
  addenda on their dispatch contexts. If research contradicts a contract
  in orchestrator.md (C1-A..C5-A), report it - contracts are amended by
  the orchestrator BEFORE implementation dispatches, never mid-flight.
