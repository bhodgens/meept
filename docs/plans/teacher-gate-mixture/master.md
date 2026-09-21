# Teacher Gate — Mixture-of-Models A/B — Master Plan

**Branch:** `classifier-iteration`
**Date:** 2026-09-21
**Status:** READY

## Goal

Answer, with measured numbers on the adjudicated 48-case replay ruler
(`tools/classifier-eval/replay-gold.local.json5`, UNTRACKED — verbatim text
never enters git): **does a full-size "smart" mixture-of-models teacher beat
the 86.8% chain-only floor?**

This is the teacher gate for the Jev distillation path. Outcomes:

- PASS (mix beats 86.8% by >= 3 cases net): proceed to Jev distillation with
  this mixture as the soft-target teacher.
- FAIL (mix <= 86.8% or within noise): record the capability ceiling; the
  ~84-87% plateau is label/state-bound, not capability-bound. Classifier
  capability work stops; effort moves to orchestrator-layer quickplan
  resolution and corpus label repair.

## Architecture

Mixture of models, two workers + one judge, run offline ONCE over the ruler
(then over the corpus only if the gate passes):

- Worker A: `opencode-go/deepseek-v4-flash` (OpenCode Go plan)
- Worker B: `zai-coding-plan/glm-5.3-flash` (Z.AI coding plan)
- Judge: `opencode-go/deepseek-v4.1-flash` — sees both workers' verbatim
  JSON answers + confidence, and arbitrates disagreements (falls back to the
  higher-confidence worker on judge error; never fabricates).

All three run through the `opencode` CLI 1.1.51 in non-interactive `run`
mode (one prompt per case, `--model provider/model`), using the credentials
already stored in `~/.local/share/opencode/auth.json` (OpenCode Zen, Z.AI
coding plan). No keys are copied, printed, or moved. Fallback transport if
the CLI fails for a provider: direct OpenAI-compatible HTTP with the
Hermes-side env keys (`$ZAI_API_KEY` verified live against
`https://api.z.ai/api/coding/paas/v4`; `$OPENCODE_API_KEY` present).

Known environment fact: the brew `opencode` wrapper is BROKEN (node dyld
libllhttp error). Leaves MUST call the app binary
`/Applications/OpenCode.app/Contents/MacOS/opencode-cli` directly.

Prompt design is classification-shaped, not chat-shaped: the classifier's
13-lane list with exact spellings (12 from
`tools/classifier-eval/intent_descriptions.json5` + quickplan per
`internal/agent/llm_classifier.go` lane set), the full verbatim message,
and a mandatory single-line JSON verdict:
`{"intent": "<lane>", "confidence": 0.0-1.0, "reason": "<= 15 words"}`.
One question per turn: intent only. Confidence-combining is PROHIBITED by
campaign history (double-confidence FAILED 84.56% vs 86.8% floor —
completion-state-2026-09-10). The judge arbitrates by REASON, not by
averaging confidences.

Privacy hard rules (campaign rules 6, 8): case text joins happen locally in
the runner only; artifacts store `case_id` + model verdicts; no verbatim
message text is written to any TRACKED file; results JSONs are append-only —
corrections are new files, never edits.

## Interface Contracts (frozen)

C1. Runner invocation (leaf 02 implements, all leaves consume):

```
python3.12 tools/classifier-eval/teacher_mix/run_mix.py \
  --replay tools/classifier-eval/replay-gold.local.json5 \
  --outdir tools/classifier-eval/results/teacher-mix/ \
  [--limit N] [--only CASE_IDS_CSV]
```

C2. Per-case artifact — one JSON per case in `outdir/raw/`:
`<case_id>.json` with keys: `case_id` (str, e.g. "hermes:1"), `a` (worker A
verdict or null), `b` (worker B verdict or null), `judge` (verdict or null),
`final` (object: `intent` str, `confidence` float, `source` one of
"a|b|judge|fallback|error"). Worker/judge verdict shape:
`{"intent": str, "confidence": float, "reason": str}` or null on error.
Errors are null + `final.source == "error"`, never crashes.

C3. Summary artifact — `outdir/summary.json`: `{n, n_final_nonerror,
correct, accuracy, per_intent: {intent: {n, correct}}, errors: [case_id...],
mix_cost_usd_est | null, timestamp}`. Accuracy denominator = all 48
(non-errors count as WRONG, per the honest-denominator rule).

C4. Report artifact — `outdir/REPORT.md`: verdict line (PASS/FAIL vs the
86.8% floor = 41.66 -> PASS requires >= 42/48 correct AND > floor + noise
analysis), per-intent table, comparison vs iter-20 cascade 83.97% and chain
86.8%, disclosure of every error case id, NO verbatim message text.

C5. Scoring: `final.intent == case.expected_intent` (exact string; lanes are
lowercase single words: code, debug, analyze, search, chat, platform, git,
scheduling?, planning, review, reporting, recall, quickplan — the runner
maps corpus label spellings to lane spellings via the frozen table in
run_mix.py and asserts 0 unmapped labels at load).

C6. Repo layout: all new Python under `tools/classifier-eval/teacher_mix/`
(run_mix.py, prompt.py, score_mix.py, README.md); results under
`tools/classifier-eval/results/teacher-mix/` (raw/ is GITIGNORED — verbatim
join risk; summary.json + REPORT.md tracked).

C7. Python: `/opt/homebrew/opt/python@3.12/bin/python3.12` (campaign
standard). Stdlib only for the runner (urllib, json, argparse) — no new
deps. opencode CLI calls via subprocess with 120s per-case timeout.

## Child Index

| leaf | scope | deps | est context |
|---|---|---|---|
| 01-model-probe.md | CLI smoke + exact model-id resolution + cost check | none | ~25K |
| 02-teacher-sweep.md | prompt.py + run_mix.py + full 48-case run | 01 | ~60K |
| 03-scoring-analysis.md | score_mix.py + summary.json + REPORT.md + verdict | 02 | ~35K |

Sequential: 01 -> 02 -> 03 (each consumes the prior leaf's artifact).

## Dispatch Protocol

1. Read the child document. Dispatch via `delegate_task` single-task mode:
   `tasks: [{goal: <one-line imperative>, context: <full leaf brief>}]`.
2. Dispatch context MUST include: leaf path, the frozen contracts C1-C7
   verbatim, "Do NOT commit. Do NOT run git add. Write files, run
   verification, report results only. The orchestrator handles all git
   operations."
3. The orchestrator (main model) reviews in-session after each leaf — never
   a delegated reviewer. Verify the leaf's artifact files exist and spot-
   check contents; do not trust the child's self-report.
4. On gaps: re-dispatch with specific feedback (max 3 iterations), then
   complete in-session if the residue is small.
5. After review passes: orchestrator stages exact paths and commits
   (`git add <explicit paths>`; never `git add .`). Per leaf commit message
   shapes are in each leaf.
6. Update the Completion Tracking Table after every transition.

## Review Checklist

- [ ] Artifacts match contracts C2-C4 exactly (keys, shapes, denominators)
- [ ] No verbatim message text in any TRACKED file; raw/ untracked or
      gitignored before the first commit
- [ ] No credentials, tokens, or auth.json contents in any output
- [ ] Accuracy denominator is all-N with non-errors counted as wrong
- [ ] Verdict line states PASS/FAIL with the exact numbers and the
      route-count/error disclosure
- [ ] No debug prints, TODOs, placeholder values in committed files
- [ ] grep corruption check clean: `grep -rcE '^\s+[0-9]+\|' --include='*.py'
      --include='*.md' tools/classifier-eval/teacher_mix/` returns zero

## Coding Conventions

- Python 3.12 stdlib only; argparse + json + urllib/subprocess; no classes
  where a function works; type hints on public functions.
- Shell out to opencode CLI with `subprocess.run([...], timeout=120,
  capture_output=True)`; check returncode; treat CLI absence as a hard
  error with the app-binary path in the message.
- Retry policy: 2 retries with 5s backoff per case per model; after that the
  slot is null (never fabricate).
- Deterministic: no sampling params beyond temperature=0 where the transport
  allows; run order follows the file's case order.
- Follows `gofmt`-equivalent: files pass `python3.12 -m py_compile`.

## Completion Tracking Table

| leaf | status | notes |
|---|---|---|
| 01-model-probe.md | PENDING | |
| 02-teacher-sweep.md | PENDING | |
| 03-scoring-analysis.md | PENDING | |

## Integration Test Plan

After leaf 03: re-run `run_mix.py --limit 3` end-to-end to prove the
committed scripts reproduce raw artifacts from a clean outdir; verify
summary.json recomputation from raw/ by re-running score_mix.py; run the
privacy check (grep tracked candidates for any string from the replay file);
then run the repo's existing guards untouched by this work:
`cd tools/classifier-eval && python3.12 -m pytest test_hardening.py
test_provenance.py test_result_privacy.py -q`.

## Open Questions

None — the user specified the model mixture; the orchestrator resolved
transport (opencode CLI first, direct HTTP fallback) and judge-fallback
policy (higher-confidence worker) as mechanical defaults. If
`opencode-go/deepseek-v4.1-flash` does not resolve in the probe, leaf 01
escalates to the user BEFORE the sweep (do not silently substitute a
different judge model).
