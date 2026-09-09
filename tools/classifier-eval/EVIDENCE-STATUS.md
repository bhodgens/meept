# Evidence Status — uncommitted campaign artifacts (2026-09-08 audit)

Snapshot of `git status` on branch `classifier-iteration` at audit time,
scoped to what this campaign produced. **Nothing was staged or committed
by this audit** (directive: stage nothing, commit nothing).

## Untracked (never committed) — campaign measurement evidence

| path | contents |
|---|---|
| `tools/classifier-eval/results/iter-10/` | harvest wave 2 measurement |
| `tools/classifier-eval/results/iter-11/` | micro-guards (rejected) measurement |
| `tools/classifier-eval/results/iter-12/` | harvest wave 3 measurement |
| `tools/classifier-eval/results/iter-13/` | ModernBERT raw-embeddings (rejected) measurement |
| `tools/classifier-eval/results/iter-14/` | divergent Stage-0.5 fine-tune measurement |
| `tools/classifier-eval/results/iter-15/` | research-driven retrain measurement |
| `tools/classifier-eval/results/iter-16/` | 3-stage cascade measurement (superseded verdict — see iter-16-corrected) |
| `tools/classifier-eval/results/iter-7/summary-20260908-120017.json`, `summary-20260908-120419.json` | two extra iter-7 sweep summaries not captured in the iter-7 commit |
| `tools/classifier-eval/results/iter-16-corrected/` | NEW this audit: corrected-verdict report |
| `tools/classifier-eval/iter18_cases.json5` | appeared during this audit, NOT created by it — unattributed untracked file (concurrent session/campaign prep?); owner should claim or remove |
| `tools/classifier-eval/EVIDENCE-STATUS.md` | NEW this audit: this file |

**Consequence:** ITERATION-LOG rows 10-16 currently cite evidence that
exists only in the working tree. Until these directories are committed,
the campaign's iteration history is not reconstructible from git alone.

## Modified, uncommitted — fold-assignment.json

`tools/classifier-eval/fold-assignment.json` carries a +51-line working-tree
diff. AUDITED and VALID:

- **Append-only on substance**: the diff adds 51 new `"<hash>": <fold>`
  entries (iter-10..16 corpus growth) and exactly ONE moved line —
  `"fe548411f888b56c": 3` — which is a pure JSON-formatting artifact (the
  last entry gains a trailing comma before the new closing brace; its
  value is unchanged).
- **Formula-consistent**: every added key was verified reproducible from
  the documented fold formula (eval_harness.py `assign_folds`:
  `sha256("42:<case_key>") % 5`, where `case_key = sha256(text)[:16]`);
  no pre-existing entry's fold changed.
- This file is the campaign's split-stability guarantee (master.md
  protocol) — committing it is what makes iters 10-16 numbers
  reproducible from a fresh checkout.

Also in the working tree but OUTSIDE this audit's ownership (other
sessions/campaigns): `internal/llm/client.go`, `internal/llm/lfm_tool_calls.go`,
`internal/metrics/store.go` modifications, `cmd/glmprobe-tmp/`, and other
untracked docs/scratch paths. Listed here only to bound this file's claim.
