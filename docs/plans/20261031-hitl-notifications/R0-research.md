# HITL Notifications Research (Slack Security + Regime Methodology) - Implementation Leaf

> **For the implementing agent:** You are the researcher for this leaf.
> You do NOT write code. You produce a research notes document via
> web_search + web_extract, with every load-bearing constant backed by a
> source URL and a verbatim quote. Do NOT commit - the orchestrator handles
> all git operations after review. ASCII only.

## Meta

- **Parent:** ../master.md
- **Scope:** Verified research notes that gate the Slack channel leaf's
  security constants and the regime-evaluation leaf's metric definitions.
- **Dependencies:** none
- **Estimated Context:** 30K
- **Concurrency Group:** A

## Goal

Two unknowns must be resolved from authoritative sources before the other
leaves dispatch: (A) the exact Slack interactive-messaging security model -
request signing verification, timestamp replay tolerance, response_url
semantics, and bot token scoping; (B) a citable methodology for evaluating
human-in-the-loop autonomy regimes - specifically intervention precision
and escape-rate metrics. Output is ONE markdown document; it becomes part
of the dispatch context for leaves 01 and 04.

## Context

Meept will add a Slack approval channel for its human permission relay
(internal/acp/relay.go:94-107 - PermissionNotifier seam, fail-closed,
120 s relay_timeout) and an evaluation harness comparing autonomy regimes.
Getting the Slack signature scheme or replay window wrong is a security
bug; inventing metric definitions without literature grounding makes the
regime report unfalsifiable. Use web_search (primary tool for this leaf),
then web_extract on the authoritative pages: api.slack.com for section A;
the safety/HITL/agent-evaluation literature for section B (prefer sources
that define intervention precision or intervention-worthy-event metrics
explicitly - e.g. Anthropic's Constitutional AI/RLHF discussions, the
"AI control" literature, and agent-safety evaluation papers; record what
you could NOT find rather than inventing definitions).

## Verified Anchors

Repo-side anchors (grep-confirmed 2026-10-31) - the notes must state how
each consumed constant maps onto these:

- internal/acp/relay.go:82-107 - relay verbs allow/deny/always;
  PermissionNotifier seam; fail-closed timeout semantics the Slack channel
  must not bypass.
- internal/acp/approver.go:14-39 - Approver{Kind, Detail}; new "slack" kind
  renders "slack:<uid>".
- internal/config/schema.go:2662-2714 - config validation pattern the
  [comm.slack] block mirrors.
- e2e/suites/step-pipeline/ - scripted-human e2e precedent the regime
  harness reuses.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
docs/plans/20261031-hitl-notifications/research-notes.md with sections:
  A. Slack interactive messaging security (constants table)
  B. HITL regime evaluation methodology (metric definitions + sources)
  C. Open research gaps (explicit list; no invented facts)
```

### What This Leaf Consumes

- none (research-only leaf)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Research Slack request signing: exact scheme, base string, HMAC details, timestamp tolerance, replay guidance | Create: research-notes.md section A | `grep -c "api.slack.com" research-notes.md` >= 3 | notes absent | section A carries the signing base string verbatim + tolerance + source URLs and quotes | Given the Slack docs pages; When extracting the verification algorithm; Then every constant has URL + quote, and a Go pseudocode verification block matches the pinned C3 contract shape |
| T2 | Research response_url semantics (lifetime, usage limits) and bot token scoping (chat.postMessage vs chat.update scopes needed for interactive buttons) | Modify: research-notes.md section A | `grep -c "response_url\|scope" research-notes.md` >= 4 | not covered | response_url lifetime/limits + minimum token scopes recorded with sources | Given docs; When a button is clicked; Then the notes state exactly which API calls the callback may make and with which token scopes |
| T3 | Research HITL regime evaluation methodology: intervention precision, escape rate, and comparable autonomy-regime metrics; define each metric operationally for meept's ground-truth sources | Modify: research-notes.md section B | `grep -c "precision\|escape" research-notes.md` >= 6 | not covered | both metrics defined operationally (numerator/denominator in terms of auditlog records, RuleSource, acp.permission.decision events) with >= 3 literature sources | Given a regime run; When computing intervention precision; Then the formula resolves to counts over the named ground-truth sources, and any literature-undefined term is listed in section C instead of invented |
| T4 | Write section C: open gaps + a mapping table "constant/metric -> consuming leaf -> contract ID" | Modify: research-notes.md section C | `grep -c "C[0-9]" research-notes.md` >= 4 | not covered | every consumed constant/metric maps to a leaf + contract ID | Given sections A/B; When the orchestrator inlines them into dispatch contexts; Then each constant names its consumer (01 or 04) and contract (C3/C7) |

Worked example (T1 shape): run `web_search("Slack signing secret v0
timestamp request signature verification")`, web_extract the
api.slack.com/interactivity page, quote the base-string sentence verbatim,
transcribe the tolerance ("five minutes" -> the exact number with URL),
then write the Go pseudocode block.

## Self-Verification Checklist

- [ ] research-notes.md exists with sections A, B, C
- [ ] Every load-bearing constant/metric has >= 1 source URL + verbatim quote
- [ ] No invented constants: anything unfound is in section C
- [ ] Mapping table covers all constants consumed by C3 and C7
- [ ] ASCII only; no code files created or modified

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** research-notes.md (+ nothing else)
2. **Verify command + output:** the grep counts from the task table
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Sections A/B/C present; every constant URL+quote backed
- [ ] Section B metrics resolve operationally to the named ground truth
- [ ] Section C honest about gaps (no fabricated citations)
- [ ] No code or spec changes outside research-notes.md

Output: APPROVED or specific gaps with section references.

## Notes

- Prefer primary sources (api.slack.com) over blog posts; if Slack docs
  changed a constant recently, note the as-of date next to it.
- Section B must stay engine-agnostic in its literature half and
  meept-specific only in its operational half.
