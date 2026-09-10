# Harvest + Dashboards - Implementation Leaf

## DISPATCH INSTRUCTION

> **Implementing agent:** Implement ALL tasks below. Do NOT commit.
> Verify by running the tool against a fixture DB and the --dry-run path.

## Meta

- **Parent:** docs/plans/classifier-outcome-loop/master.md
- **Scope:** Nightly harvest turning dispatch_log outcomes + near-miss
  margins into corpus candidates and per-door accuracy views.
- **Dependencies:** 01 (columns), 02 (margins), 03 (outcomes)
- **Estimated Context:** ~50K
- **Concurrency Group:** C

## Goal

A python tool (tools/classifier-eval/harvest_outcomes.py) that reads
metrics.db READ-ONLY and produces:
1. A correction-candidate sheet (hash, session, ts, classifier choice,
   corrected_agent) for user adjudication.
2. A local-only join step recovering candidate text from session
   transcripts (gitignored output — verbatim text never enters git).
3. Near-miss abstention buckets from Door-1 margins.
4. Per-door accuracy SQL views (persisted into metrics.db as VIEWs).

## Context

The campaign's proven loop is: harvest -> adjudicate -> corpus append
(provenance + cosine>0.95 dedup) -> centroid rebuild. Existing
patterns to mirror:
- tools/classifier-eval/harvest_hermes.py — UNTRACKED-output pattern
  (verbatim text stays out of git)
- tools/classifier-eval/iter7_harvest.py:67-69 — dedup guard
- scripts/build_prefilter_centroids.py — accepts any corpus file
- docs/plans/classifier-outcome-loop/../classifier-outcome-loop/01 —
  the schema this reads (dispatch_log: input_hash, model, margin,
  turn_no, outcome, corrected_agent, classifier_method, session_id,
  timestamp, task_id)

Privacy invariants (design.md S4):
- metrics.db contains NO message text (leaf 01 removed it)
- the join step runs on the SAME machine as the daemon and its output
  file MUST be gitignored before first write
- the tool is READ-ONLY on metrics.db

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
tools/classifier-eval/harvest_outcomes.py
  --db PATH          default ~/.meept/metrics.db (opened READ-ONLY)
  --since DATE       lower bound (ISO), default 24h back
  --window N         re-route window (informational; matches L3's 3)
  --margin-band X,Y  near-miss band, default "0.025,0.035"
  --dry-run          print candidate sheet to stdout, write nothing
  --out DIR          output dir (default ./harvest-YYYYMMDD/, created
                     in tools/classifier-eval/, immediately gitignored)

Outputs (unless --dry-run):
  candidates.json    [{input_hash, session_id, ts, method, intent,
                      agent, corrected_agent, confidence}]  (SAFE: tracked OK)
  nearmiss.json      [{input_hash, ts, margin, asserted_intent,
                      verdict}]                                   (SAFE)
  sheet.local.md     adjudication sheet with recovered text       (NEVER tracked)
  views.sql          the SQL applied to metrics.db (CREATE VIEW IF NOT EXISTS)
```

### What This Leaf Consumes

dispatch_log schema from leaves 01-03; session transcript locations
(from harvest_hermes.py — ~/.hermes/sessions/session_*.json).

## Tasks

### Task 1: Views + views.sql

SQL views (CREATE VIEW IF NOT EXISTS), applied to metrics.db via a
write connection ONLY when --apply-views passed (default off; dry-run
never writes):

- v_door_accuracy: per classifier_method, COUNT + SUM(outcome='ok') +
  SUM(outcome='corrected') + SUM(outcome='failed_replan') over rows
  WHERE outcome != 'pending' AND classifier_method != ''
- v_correction_rate: per session, corrections/total over classified rows
- v_margin_hist: Door-1 margin histogram (CASE buckets 0.00-0.01,
  ..., 0.09+) split by verdict (routed/abstain/suppressed)
- v_fallback_trend: fallback rows (classifier_method='fallback') per day

Include a small `--apply-views` mode and document that views are
idempotent (IF NOT EXISTS).

### Task 2: Correction candidates + near-miss buckets

Candidates: SELECT rows WHERE outcome IN ('corrected','failed_replan')
AND ts >= since, ordered by session_id, ts. Emit candidates.json.

Near-miss: SELECT rows WHERE margin IS NOT NULL AND outcome IN
('ok','corrected') AND classifier_method='embedding_prefilter' AND
margin BETWEEN band -> nearmiss.json (the abstention-side near misses
carry method='llm' with margin from the observer — include those too:
WHERE margin IS NOT NULL AND margin BETWEEN band, verdict from the
log/margin fields as available).

### Task 3: Local-only text join (gitignored)

For each candidate, scan ~/.hermes/sessions/session_*.json for the
matching session (session_id in the transcript) and nearest-timestamp
user message; emit sheet.local.md in the SILVER-ADJUDICATION-SHEET
format (message / silver label / system answer / blank `your label:`
line). CRITICAL: write .gitignore entry into the harvest output dir
BEFORE writing any text file; print the gitignore path in the summary.

### Task 4: --dry-run + summary

--dry-run prints counts (candidates, near-misses, views that WOULD be
created) and a 10-row candidate preview (hash + corrected_agent only —
no text). Exit 0 always (measurement tool convention).

### Task 5: Docs + workflow wiring

Add a short "Harvest loop" section to
docs/workflows/classification-architecture.md pointing at the tool,
the cadence (nightly launchd/cron is the operator's choice — meept
does not self-schedule), and the dedup/provenance rules for accepted
candidates (added_in: "harvest-YYYYMMDD", source: "live-session",
cosine > 0.95 reject, then build_prefilter_centroids.py rebuild).

## Self-Verification Checklist

- [ ] Tool runs read-only on metrics.db (verify: open with
      `file:...?mode=ro`)
- [ ] views.sql idempotent; --apply-views default OFF; --dry-run
      writes nothing anywhere
- [ ] sheet.local.md output dir gitignored BEFORE text is written
- [ ] Dedup guard reference and provenance format in the docs
- [ ] ASCII; runs under /opt/homebrew/bin/python3.14 (stdlib only —
      no sklearn needed here)

**Verification (no commit):** run against the scratch daemon's
metrics.db (/tmp/qp-smoke/home/metrics.db) with --dry-run and show the
summary. Then --apply-views against a COPY of that db in /tmp (never
the live one without asking).

**DO NOT COMMIT.** Orchestrator handles git operations.

**Deviations from spec:** document any.

## Review Checklist (For Review Agent)

- [ ] Read-only DB access enforced (mode=ro)
- [ ] Privacy: text only in .local.md inside a gitignored dir;
      candidates.json carries hashes only
- [ ] Views match the accuracy-denominator rule (exclude pending,
      exclude empty classifier_method)
- [ ] Near-miss band configurable, default 0.025-0.035
- [ ] Docs section added with cadence + provenance rules

Output: APPROVED or specific gaps.

## Notes

- The tool SHIPS VALUE WITH WHATEVER ROWS EXIST — run it the day L1-3
  land; empty result sets are a fine first output.
- The gold-replay acceptance run (campaign M4) is a separate deliverable
  and does NOT depend on this leaf.
- Keep harvest_hermes.py untouched; the session-scan logic may borrow
  its globbing pattern.
