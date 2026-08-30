# Plan: Skill evolution: improve parallel-subagent-code-review

## Meta

- plan_id: plan-20260830205556-0002
- created: 2026-08-30
- status: planning

## Summary

The skill body is empty yet was injected 6 times with 2 negative outcomes (effectiveness 0.00), so every injection carried zero guidance. The successful traces supply a concrete, reusable pattern that fits this skill's name: fan out independent checks in parallel, verify state-mutating claims against the actual artifact (exists/size/sha256/read-back) before reporting anything, match content exactly against the spec, propagate upstream results to dependent checks, retry once with evidence, and emit a single terminal JSON report with typed evidence. This complements the already-accepted orchestration skill (race-on-nested-completion) rather than duplicating it: that skill governs dispatch continuation; this one governs the parallel review/verification fan-out and final reporting discipline.

Candidate content:
# Parallel Subagent Code Review

## Purpose
When code changes (or delegated code steps) need to be checked before a task is declared done, fan the review out to multiple subagents in parallel, verify every state-mutating claim against the actual artifacts, and synthesize one structured report. Do not review sequentially what can be reviewed concurrently, and do not accept a subagent's claim without file-level evidence.

This skill governs the review/verification fan-out and final reporting. General orchestration behavior (never pausing between subagent completions) still applies alongside it.

## When this applies
- Subagents (or you) have produced code changes — files written, edits made, commits created — and completion must be verified.
- You are coordinating subagents and multiple independent checks could run at once.
- A completion payload asserts work is done and needs confirmation before the final report.

## Core rules

1. **Fan out independent checks in parallel.** Dispatch every check with no upstream dependency in the same turn. Map checks to the dynamic agent list when provided (e.g., `explore` for read-only existence/location confirmation, `skeptic` to stress-test claims and edge cases, `doc-keeper` for documentation invalidated by the change); fall back to whatever read-only reviewers are available.

2. **Verify state-mutating claims with artifact evidence.** "File written" is unverified until you hold:
   - `file_exists` with path and size,
   - `file_hash` (sha256), and ideally
   - `file_read_content` with the exact bytes read back.

   Working example from a verified run: a 3-byte `answer.txt` confirmed by size, sha256, and read-back of `42\n` before any completion report was emitted.

3. **Match content exactly, not approximately.** Check against the specification literally — exact string, exact trailing newline, exact line count. "Contains 42" does not satisfy "exactly '42' on a single line".

4. **Sequence dependent checks, propagate context.** A check that consumes another step's output waits for it; dispatch it the moment the upstream result lands, and include those results in its prompt — subagents have no shared memory of prior jobs.

5. **Retry once with evidence.** If verification fails, re-run the offending step once with the failure evidence embedded in the prompt; only then report failure.

6. **One terminal report, after everything resolves.** Emit the structured completion JSON a single time — `status` (`completed`/`partial`), `accomplished`, `not_done`, `issues`, `observations`, `suggested_next_agent`, `user_decision_needed` — plus an evidence block (`claims` + typed evidence entries). Record environment anomalies under `observations` instead of silently ignoring them (e.g., "project_info reported 'no project bound' while the working directory resolved correctly").

## Failure handling
- A failed verification: one evidence-informed retry, then mark the step failed with the evidence attached.
- A reviewer finding that arrives late: wait for it — the report is emitted only after all dispatched checks resolve.
- Every dispatched check must appear in the final report as passed, failed, or blocked. Nothing is silently dropped.

## Anti-patterns
- Declaring success on a subagent's assertion without reading the artifact back.
- Running independent reviews one at a time instead of in parallel.
- Emitting the completion JSON while verification steps are still outstanding.
- Dropping a late-arriving finding instead of folding it into `issues`.
- Loose content checks where the specification is exact.


## Notes

