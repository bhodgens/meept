# Runtime Spike - JS Sandbox Selection - Research Leaf

> **For the implementing agent:** RESEARCH ONLY. Work in /tmp scratch dirs
> (never the repo, except writing your report file). Do NOT commit. Do NOT
> modify repo files. Small Go programs + `go mod` scratch modules are fine.
> Timebox: report after ~25 minutes even if one candidate is incomplete.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** pick the sandbox runtime for internal/codemode; pin C3 feasibility
- **Dependencies:** none
- **Estimated Context:** 60K
- **Concurrency Group:** solo

## Goal

Return a GO/NO-GO verdict per candidate (goja, wazero+quickjs, yaegi) against
criteria (a)-(f) in the parent orchestrator, with measured evidence, and a
recommended runtime + the exact bridging idiom (how a tool call crosses the
boundary, async or sync).

## Context

- pi's implementation for reference: /tmp/pi-eval/pi/packages/codemode/src
  (if TMPDIR was cleared, re-clone earendil-works/pi depth 1).
- Target usage: model writes a script that calls `tools.read({path})` etc.;
  each call maps to a Go function taking/returning JSON strings; the script's
  return value + captured text go back to the LLM.
- meept builds on macOS arm64 (dev) + linux amd64 (CI). CGO_ENABLED=0 strongly
  preferred (cross-platform invariant).

## Verified Anchors

- /tmp/pi-eval/pi/packages/codemode/README.md:3 - capability model description
- /Users/caimlas/git/meept/go.mod - current deps (no JS runtime today)

## Interface Contracts (From Parent)

### What This Leaf Exposes

Report at `/tmp/pi-eval/codemode-spike-report.md` (also paste the verdict
block in the completion report):

```
VERDICT: <runtime> | GO / NO-GO
criteria: (a) async-bridge: PASS/FAIL <evidence>
          (b) timeout-kill: PASS/FAIL <evidence>
          (c) memory-cap:   PASS/FAIL <evidence>
          (d) no-fs-net:    PASS/FAIL <evidence>
          (e) cgo-free dual-platform build: PASS/FAIL <evidence>
          (f) idiom-compat (3 pi-style scripts): PASS/FAIL <evidence>
BRIDGE IDIOM: <exact Go<->script call shape, sync or async, error mapping>
RISKS: <bullet list>
```

### What This Leaf Consumes

- C3 Options/Result shapes (../master.md) - feasibility check per field.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | goja scratch module | /tmp/pi-eval/spike-goja/ | `go build ./... && go run .` in scratch | n/a (research) | verdict rows a-f measured | Given a script with 2 tool calls + infinite-loop variant; When executed; Then bridge works, loop is killable, fs is unreachable |
| T2 | wazero+quickjs scratch | /tmp/pi-eval/spike-wazero/ | same | n/a | same | same |
| T3 | yaegi scratch | /tmp/pi-eval/spike-yaegi/ | same | n/a | same | same |
| T4 | idiom-compat scripts | in each scratch | manual | n/a | 3 pi-style scripts (sequential reads, map+aggregate, early-exit on condition) run unmodified in spirit | Given pi README example scripts; When ported; Then they run without rewriting control flow |
| T5 | Report | /tmp/pi-eval/codemode-spike-report.md | `test -f` | n/a | verdict block complete | Given all measurements; When written; Then every row has evidence |

## Self-Verification Checklist

- [ ] Repo untouched (`git status` clean in /Users/caimlas/git/meept beyond pre-existing session state)
- [ ] Every PASS row has a command + observed output in the report
- [ ] Recommendation states ONE runtime with rationale <= 10 lines

**DO NOT COMMIT.**

## Completion Report Format

1. Report path; 2. verdict block verbatim; 3. deviations.

## Review Checklist (For Review Agent)

- [ ] Verdict rows backed by real command output (spot-check one scratch module by re-running it)
- [ ] MemoryLimit mechanics named per runtime (C3 field feasibility)
- [ ] cgo status explicit per candidate

## Notes

If ALL candidates NO-GO on (b) or (d) (security-critical), the recommendation
becomes "no codemode; revisit when a maintained pure-Go sandbox exists" and
the branch is marked BLOCKED with the report archived to
docs/research/ by the orchestrator.
