# B05 — Bridges-On/Off Recall Eval (P10 GATE)

**Proposal:** P10 (branch 09). **Depends on:** B02, B03, B04. **Files (2-3):**
- Create: `internal/eval/bridges_eval.go` (or a script under scripts/ if the
  eval harness is script-shaped — match existing internal/eval patterns; locate
  with search_files first)
- Output artifact: `docs/research/eval/bridges-<date>.json`
- Test: unit test for the scoring helpers

**Problem:** The paper's QA-benchmark F1 does NOT transfer automatically to
agentic memory recall (static corpora, short gold answers, no query drift).
Owner rule: evaluate before defaulting — this eval IS the gate. Deliverable is
a report to the USER; nobody flips `memory.bridges.enabled` based on it.

**Design (per docs/research/03-indexrag-integration.md conditions):**
- Dataset: 20-40 cross-session recall questions over REAL episodic memories
  from this installation (questions whose answers span 2+ memories/sessions —
  the multi-hop class bridges target). Written to a committed JSON fixture with
  question / expected-fact / source-memory-IDs.
- Arms: (A) bridges disabled (baseline), (B) bridges enabled, k_b=2, verify on.
- Metrics per arm: answer correctness (grader = the loop's general model alias,
  judged against expected facts), context tokens per query (the actual injected
  memory-context size), retrieval latency, bridge inclusion counts (from B04
  stats).
- PASS criteria (suggested, owner-tunable): correctness(B) >= correctness(A) − 2pp
  AND tokens(B) <= tokens(A) × 0.9 OR correctness(B) >= correctness(A) + 3pp at
  equal tokens. Report states results; the OWNER decides adoption.

### Tasks

**Task 1 — locate the harness:** search_files internal/eval + scripts/ for the
existing eval patterns (meept has internal/eval + meept-bench). Reuse the
grader/scoring seams; do not invent a new framework.

**Task 2 — fixture:** write the question set JSON (committed; contains NO
private memory content — memory IDs + expected facts only, sanitized). Unit
test the scorer with canned grader outputs.

**Task 3 — runner:** idempotent script/Go command: builds both arms, runs the
questions against the local model stack, writes `bridges-<date>.json` with
per-question + aggregate rows. Support `--arm=a|b|both`, resume on re-run.

**Task 4 — run + report:** execute `--arm both`; commit the JSON artifact with
the leaf commit; summarize the aggregate table in the leaf completion report
for the orchestrator to relay to the user.

**Task 5 —** `go test -p 2 ./internal/eval/` green (scorer tests). Do NOT commit.

## Self-Verification Checklist
- [ ] Fixture sanitized (no raw private memory text committed)
- [ ] Both arms run against the SAME model + temperature settings
- [ ] Token counts measured from the injected context, not estimated post-hoc

## Review Checklist (for orchestrator)
- [ ] Report goes to the USER as an adoption gate — orchestrator does NOT flip defaults
- [ ] JSON artifact committed alongside the leaf commit
