# Wiring: Config Registration and Summarizer Injection - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files — explore with search_files or terminal cat.
> After writing a file, do NOT read it back to verify — write once and
> stop. After completing, report what you built, what files you touched,
> and any deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Daemon wiring for the new transcript knobs: gate
  summarize-capable construction on `[transcript] summarize_enabled`,
  inject the summarizer chatter (existing `c.SummarizerClient`, or a
  dedicated client when `summarize_model` names a different model), and
  thread FallbackOutputDir.
- **Dependencies:** 01 + 02 COMMITTED (their exported symbols exist at
  HEAD: output_path, SetSummarizer, TranscriptConfig.FallbackOutputDir/
  SummarizeEnabled/SummarizeModel)
- **Estimated Context:** 40K
- **Concurrency Group:** C (alone). This leaf is the SINGLE WRITER of
  internal/daemon/components.go in this tree.

## Goal

Leaves 01-02 built the capability; nothing is wired. This leaf connects
config to construction: transcript_fetch gets its fallback output dir
and — only when summarize_enabled — a live `llm.Chatter` so summarize
mode works. Without this leaf, summarize=true returns the nil-chatter
config error and FallbackOutputDir stays "~/.meept/media" default.

## Context

The wiring block is internal/daemon/components.go:5619-5647 (the
transcript registration added by the skill-authoring tree; grep
"NewTranscriptFetchTool" — currently ONE call site inside
registerBuiltinTools, gated `if transcriptCfg.Enabled`).

The summarizer already exists: `c.SummarizerClient` (*llm.Client,
implements llm.Chatter) built at components.go:843 via
`createAuxiliaryLLMClientWithResolver(c.ModelsConfig, summarizerRef,
c.LLMResolver, ...)` where summarizerRef = SummarizerModel ->
SmallModel fallback. llm.Client implements llm.Chatter
(internal/llm/interface.go:31). registerBuiltinTools already receives
`summarizerChatter llm.Chatter` (param 12; the call site passes
`c.LLMProvider`) — but that is the MAIN provider, not the local
summarizer. The clean route: this leaf uses c.SummarizerClient directly
at the transcript registration site (it is a Components field, in scope
inside registerBuiltinTools? NO — registerBuiltinTools is a free
function; the value must be threaded as a new parameter OR the
registration moves to a small helper called from NewComponents. Prefer
the MINIMAL change: add one parameter `transcriptCfg` already exists —
add `summarizerClient *llm.Client` after it; pass `c.SummarizerClient`
at the call site components.go:2237).

For `summarize_model` naming a DIFFERENT model than the chain default:
build a dedicated client in NewComponents near the SummarizerClient
block (components.go:843) using the same
`createAuxiliaryLLMClientWithResolver` helper with the configured ref,
stored as a new Components field (e.g. `TranscriptSummarizerClient`),
nil when summarize_enabled is false or summarize_model is "" (chain
default -> SummarizerClient is correct). Single construction site.

Key files:
- internal/daemon/components.go — registration block + NewComponents
  auxiliary-client construction + call site at :2237
- internal/daemon/skill_tools_wiring_test.go — existing harness
  (skillToolsTestConfig / newSkillToolsTestComponents) to mirror
- internal/config/schema.go — NOT this leaf (leaf 02 owns it); read-only

## Interface Contracts (From Parent)

```
// components.go, transcript registration block (post-conditions):
//   - tool constructed with TranscriptConfig{ PythonPath, ModuleName,
//     TimeoutSeconds, FallbackOutputDir: transcriptCfg.FallbackOutputDir }
//   - when transcriptCfg.SummarizeEnabled:
//       chatter := c.SummarizerClient (chain default)
//       when transcriptCfg.SummarizeModel != "" AND a dedicated
//       TranscriptSummarizerClient was built: chatter = that client
//       tool.SetSummarizer(chatter)  // nil client -> leave nil; the
//                                    // tool's nil-chatter error is
//                                    // then the honest runtime answer
//     when !SummarizeEnabled: SetSummarizer is NOT called
//   - log line when summarize wiring lands: "transcript summarize
//     enabled", "model", <ref or "summarizer chain">
//
// components.go, near the SummarizerClient block (~:843):
//   if cfg.Transcript.SummarizeEnabled && cfg.Transcript.SummarizeModel != "" {
//       c.TranscriptSummarizerClient = createAuxiliaryLLMClientWithResolver(
//           c.ModelsConfig, cfg.Transcript.SummarizeModel, c.LLMResolver,
//           logger.With("component", "transcript-summarizer-llm"), budgetTracker)
//   }
//   (guard the whole block on the LLM-configured branch as the
//   SummarizerClient construction is guarded)
//
// Components struct: + TranscriptSummarizerClient *llm.Client (nil
// default; comment explaining when it is non-nil)
//
// registerBuiltinTools: + one param `summarizerClient *llm.Client`
// (after skillRegistry, before tokenStore) — call site updated; other
// callers of registerBuiltinTools: grep FIRST (there may be test
// callers) and update all.
```

## Tasks

### Task 1: Dedicated summarizer client construction

**Objective:** TranscriptSummarizerClient built exactly when configured.

**Files:**
- Modify: internal/daemon/components.go
- Test: internal/daemon/skill_tools_wiring_test.go (or a new
  transcript_wiring_test.go in the same package — mirror the existing
  harness)

**Step 1: Failing test** — construct Components via the existing test
harness with cfg.Transcript{Enabled: true, SummarizeEnabled: true,
SummarizeModel: "nonexistent-model-for-test"}; assert the Components
field TranscriptSummarizerClient is NON-nil (the helper may resolve
lazily — if createAuxiliaryLLMClientWithResolver returns non-nil for
unknown refs, assert non-nil and that its Config() model matches the
ref; if nil for unknown refs, use a ref that resolves in the test
config instead — read the helper first). Also the negative: with
SummarizeModel "" the field stays nil.

**Step 2:** FAIL. **Step 3:** implement (construction + field). **Step
4:** PASS.

### Task 2: Registration block + chatter injection

**Objective:** The tool carries FallbackOutputDir and the right chatter.

**Files:**
- Modify: internal/daemon/components.go (registration block +
  registerBuiltinTools signature + call site)
- Test: internal/daemon transcript wiring test (Task 1's file)

**Step 1: Failing tests**
- transcript enabled + summarize enabled: ToolRegistry.Get
  ("transcript_fetch") non-nil; the tool summarizes — functional probe:
  execute with a URL and summarize=true and assert the error is NOT the
  "summarization not configured" string (it will be a subprocess/exec
  error from the fake python path — reuse TestTranscriptToolGatedOnConfig's
  nonexistent-python trick; the point is the chatter got injected, so
  the nil-chatter error path is skipped). Assert via errors-free
  observation: with a nonexistent python path AND summarize=true, error
  mentions the python path (fetch failed first) — NOT the config error.
- transcript enabled + summarize DISABLED: execute with summarize=true
  -> error IS "summarization not configured" (nil chatter — honest).
- registration still gated on Enabled (existing test stays green).

**Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.

### Task 3: gofmt + scoped green

gofmt -l components.go + test files (components.go has PRE-EXISTING
gofmt deltas at HEAD — import order + one blank line; leave them, do
not reformat the whole file). `go build ./internal/daemon/ &&
go vet ./internal/daemon/ && go test ./internal/daemon/ -run
'TestTranscript|TestSkillAuthoring' -count=1` — all pass.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Contract satisfied: gating, injection, field semantics
- [ ] registerBuiltinTools callers ALL updated (grep first)
- [ ] No schema.go edits (leaf 02's); no transcript_fetch edits
- [ ] gofmt clean on YOUR changes; pre-existing deltas untouched
- [ ] No debug artifacts; log line matches contract

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contract match: gating on SummarizeEnabled, chain vs dedicated
      client selection, SetSummarizer NOT called when disabled
- [ ] Dedicated client built ONLY when summarize_enabled AND
      summarize_model != "" (both conditions)
- [ ] FallbackOutputDir threaded through construction
- [ ] Existing wiring tests (TestTranscriptToolGatedOnConfig etc.)
      still green
- [ ] No scope creep (no skill edits, no docs, no agent-loop changes)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The nil-chatter honest error is a FEATURE: summarize_enabled=true
  with no resolvable model still yields a clear config error at call
  time rather than a silent no-op.
- Keep the new parameter adjacent to transcriptCfg in the signature;
  the call site is ONE line (components.go:2237) plus any test callers
  you find.
- AGENTS.md: no new packages, no new build targets — state the
  no-change verification in your report.
