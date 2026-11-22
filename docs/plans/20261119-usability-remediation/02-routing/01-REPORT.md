# 01-veto-and-notice REPORT

Plan: 20261119-usability-remediation / 02-routing / leaf 01
Branch: plan/usability-remediation (worktree .hermes/worktrees/usability-remediation)
NOT COMMITTED (orchestrator commits after review).

## 1. Files touched

- internal/agent/dispatcher.go
  - Veto (~1774): dropped the hasLeadingImperativeVerb conjunct. Veto now
    fires on IntentGit && !inputContainsGitVerb(input) for ANY input shape
    (question/declarative/polite/non-leading edit requests included).
    F41 branch order preserved: recall arbitration still FIRST; genuine
    git-verb inputs untouched; GitVerbAgreementVeto=false escape hatch
    byte-identical. Comment + log line updated (ASCII).
  - Intent.Notice field (classification_notice,omitempty): degraded-lane
    guidance carrier.
  - classifyIntent: degradedNotice captured from
    llm.ClassificationUserGuidance(err) on LLM classifier failure; attached
    on llm_empty_fallback_chat (directly from err), keyword, semantic,
    heuristic_fallback, and final fallback lanes.
  - ClassifyAndRoute + ResumeAfterClarification: copy intent.Notice into
    DispatchResult.ClassificationNotice (the field handler.go already read).
- internal/agent/llm_classifier.go
  - lastFailure stored under unavailMu at cooldown arming; the cooldown-skip
    error now wraps the arming cause with %w so the cooldown lane's guidance
    names the real failure kind (was an unclassifiable string -> generic
    "unexpected error" text).
- internal/agent/strategic.go
  - fitToolHint (new) runs after validateToolHint in parsePlanOutput.
    Research-shaped description (research verb AND web evidence, both
    regexes) + hint whose config.ToolHintAgent executor lacks web_fetch
    (registry AgentSpec.HasTool ground truth; nil registry -> remap+warn,
    never silent) => remap hint to "research" via the authoritative table
    (research -> researcher) + publish routing.warning
    (reason=hint_capability_mismatch, required_tool=web_fetch, remap keys).
    Capability-matched hints pass through byte-identical (code-step guard).
- internal/agent/handler.go
  - TurnTerminalEvent.RoutingNotice (routing_notice,omitempty) additive key;
    populated from result.ClassificationNotice at the sync turn.terminal
    funnel (C1). Reply-suffix readers at ~1080/1290 were already live; the
    write side was the gap.
- schemas/ws_events.schema.json
  - turn_terminal declares routing_notice (C1 contract; drift gate
    TestWSSchemaDrift reads struct tags via reflect).
- docs/plans/.../02-routing/01-PROGRESS.md, 01-REPORT.md (artifacts).

Gate files dispatcher_veto_test.go / dispatcher_notice_test.go: UNTOUCHED
(git diff empty for both).

## 2. Verify output

Command (overlay masks ONLY sibling-red ralph_storm_test.go):

    go test -overlay /tmp/agent-gate-overlay/overlay2.json ./internal/agent/ \
      -run 'TestGitVetoShapeIndependent|TestNoticePopulated|TestHintCapabilityFit' -count=1

Result: ok github.com/caimlas/meept/internal/agent 0.746s (all T1+T2 GREEN).
go build ./... OK at gate time; gofmt applied to all touched .go files.

T1 detail: frozen p1/p6 edit prompts, question/declarative/polite shapes all
vetoed off the git lane; genuine git inputs (4 shapes) kept; recall priority
(platform_recall_arbitration) preserved; disabled escape hatch keeps raw
verdict. Mutation probe logged survived=true pre-fix semantics flip.
T2 detail: empty-response lane carries ErrEmptyResponse guidance; unavailable
lane carries APIError guidance on the answering lane; cooldown lane carries
the wrapped arming cause's guidance; research hint=code remapped to research
(researcher, web_fetch granted) + routing.warning; code-step guard silent.

## 3. Live-gate evidence

Not run in this leaf session (no daemon rig spun). The frozen-prompt routing
contract is pinned hermetically by gate T1 against the REAL classifyIntent
with canned verdicts over testdata/usability_prompts.json (verbatim probe
copy). Orchestrator live-gate (frozen p1/p6 on rig) remains the acceptance
step per leaf checklist.

## 4. Deviations

- Old non-gate pin conflict RESOLVED: dispatcher_git_veto_test.go
  TestGitVerbAgreementVeto_NonImperativeGitVerdictKept pinned the OLD
  shape-gated behavior (git verdict survives on "what is the state of the
  repository root"). That is precisely the defect T1 closes. Updated
  minimally to TestGitVerbAgreementVeto_NonImperativeGitVerdictVetoed:
  same input/preconditions, assertion flipped to non-git/non-committer,
  comment documents the supersession, config import added. NO gate test
  touched (dispatcher_veto_test.go / dispatcher_notice_test.go git diff
  empty).
- Shared-tree churn: sibling leaves were mid-edit on internal/bus and
  internal/agent during the post-gate regression sweep (transient compile
  RED: b.publishErr/b.turnOutbox, threadConvIDForResult). Gate run above
  executed in a green window and was independently re-verified by the
  orchestrator. Whole-tree regression sweep is orchestrator-owned once
  siblings land; this leaf touched no sibling code.
