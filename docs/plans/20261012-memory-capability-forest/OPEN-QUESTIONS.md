# OPEN-QUESTIONS.md — Memory Capability Forest

Summary: ALL RESOLVED by user 2026-10-12. Decisions below are binding plan
contracts; leaves already reflect them.

| Q | Decision | Plan effect |
|---|----------|-------------|
| Q1 | **(a) Defer** L3 persona | Tree 01 unchanged (L0 + L1/L2 only) |
| Q5 | **(b) Daemon-only** verdicts | Tree 03 L2: offline fallback REMOVED; `meept memory verdict` errors clearly when daemon unreachable |
| Q2 | **(b) Port LLM scene segmentation** | Tree 01 L2: L2 scenes are LLM-segmented (scene-name + message_ids per segment), not session-per-day |
| Q3 | **(a) 30 days** L0 retention | unchanged |
| Q4 | **(a) batched budget** | unchanged |
| Q6 | **(b) Include TUI view** | Tree 03 gains L3: TUI review view (parity rule satisfied in-tree) |
| Q7 | **(b) Also port code indexing** | Tree 04 gains L4: CodeGraph-style Go code index via go/ast into the knowledge store |

Historical context for each question is preserved below the table.

---

Original questions (resolved):

## Tree 01 — layered async distill

**Q1 (BLOCKING) — L3 persona: port or defer?**
- Rec: Defer L3 persona generation. Meept already has PersonalityMemory
  (`internal/memory/personality.go`) refreshed every N conversations. Tree 01
  ships L0 + async L1/L2 only; a later leaf can extend.
- Impact if we port: +1 leaf (persona generator with checkpoint/backup),
  wave 2 grows to 4 leaves, ~1 extra day of dispatch work.

**Q2 — L2 scene granularity: what is a "scene" in meept?**
- Rec: One scene per working session per day, derived from session_id + date,
  no LLM scene segmentation in v1. TAM's LLM scene segmentation is a v2
  refinement.
- Impact: cheaper (no extra LLM call), less precise context blocks. Reversible;
  scene schema carries `segment_method: "session" | "llm"`.

**Q3 — retention for L0 raw conversations.**
- Rec: 30-day default retention with `memory.l0_retention_days` config;
  prune during consolidation runs. Raw transcripts contain user text —
  same gitignored treatment as calibration JSONL.
- Impact: none on function; disk bounded.

## Tree 02 — conflict merge

**Q4 — merge budget.**
- Rec: Conflict detection runs once per L1 extract job (batched, top-5 recall
  per candidate, one LLM call per batch of ≤20 candidates). No per-turn
  detection.
- Impact: bounds cost; conflicts introduced between extract runs wait for the
  next run (acceptable — extraction is already async).

## Tree 03 — human governance

**Q5 (BLOCKING) — offline review mode?**
- Rec: CLI verdicts work in offline mode when possible (direct SQLite write
  via the store), falling back to RPC when the daemon runs. Review LIST
  requires the daemon (bus query) — document the asymmetry.
- Impact if daemon-only: simpler, one code path; but `meept memory verdict`
  then fails on a stopped daemon, which is the common case for review work.

**Q6 — review queue visibility surface.**
- Rec: CLI + RPC only in this tree. TUI/GUI parity per repo rule would add a
  TUI view — defer to a follow-up tree (state the deviation in docs).
- Impact: repo UI-parity rule flagged as deliberately deferred; GUI parity
  also deferred.

## Tree 04 — knowledge assets

**Q7 — ingestion scope for v1.**
- Rec: Markdown files from a watched directory (`memory.knowledge.ingest_dir`)
  + `knowledge_ingest` tool (agent passes text). No repo/URL crawlers, no
  CodeGraph — meept already has RepoMap for code; duplicating TAM's CodeGraph
  is out of scope.
- Impact: no external network fetchers in daemon; graph/impact analysis
  explicitly not attempted in v1.
