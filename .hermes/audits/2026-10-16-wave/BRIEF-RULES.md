# Bughunt wave 2026-10-16 -- shared audit contract

Repo: /Users/caimlas/git/meept (Go monorepo, "meept" agent daemon). HEAD 3a482477 plus
uncommitted working-tree changes. Audit the CURRENT WORKING TREE as-is.

## Hard rules
1. READ-ONLY. Never edit, create, or delete any file in the repo except YOUR ONE report
   file (path given in your brief). Never git add/commit/checkout/stash/restore.
2. Never use rm -rf (denied here). Scratch copies: mktemp -d only.
3. Never start long-lived processes (no servers, no daemons, no watchers). Kill anything
   you started.
4. Test budget: the parent runs the full suite. You may run AT MOST 3 test commands, each
   scoped: go test -p 2 -run <TestName> ./<your-package>/... plus go build / go vet on
   your own packages only. Never run `go test ./...`.
5. Do NOT run the repo's custom analyzers (tools/analyzers/*) -- they fail environmentally
   here (gox/tools export-data version mismatch). Use greps instead.
6. Ignore all prior audit artifacts (.hermes/audits/** except this file). This is a fresh
   wave; do not read or cite old wave reports.

## Method (time-boxed -- max ~25 tool calls, then write your report)
- grep-first. Never read files >2000 lines end-to-end; read +-20 lines around hits.
- Prioritized numbered checks: finish the highest-value ones first so partial work still
  yields a report.
- A finding needs: file:line, mechanism (what breaks and when), severity
  (CRITICAL/HIGH/MEDIUM/LOW), a falsifiable test or probe, evidence level
  (SOURCE = you read the code; LIVE = you ran something), and what you did NOT cover.
- Deliberate design trade-offs documented in code comments or AGENTS.md invariants are
  NOT bugs. Do not report architecture opinions.
- Cap: 10 findings max. Quality over volume. Also list areas you checked and found clean.

## Report file (your ONLY allowed write)
Write ONE markdown file at the path in your brief. Format:
- Line 1: `# <scope> -- bughunt 2026-10-16`
- `## Findings` -- numbered; each with the fields above.
- `## Checked and clean` -- bullet list of what you inspected with no finding.
- `## Residual / not covered` -- what a later pass should look at.
- Last line: `gates_run: <exact build/vet/test commands you ran and their outcomes>`
Write the report as soon as you have your first 3-5 verified findings; append refinements
after. Do not spend your last calls polishing.

## Context you may need
- Per-package invariant docs: internal/{llm,daemon,agent,comm}/AGENTS.md, docs/agents/*.md.
  Read the one for your scope FIRST -- violations of stated invariants are top-priority
  findings.
- macOS: full test sweeps need -p 2 (ephemeral port exhaustion).
