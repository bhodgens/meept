# BRIEF-RULES — bughunt wave 2026-11-19b (READ-ONLY auditors)

Repo: /Users/caimlas/git/meept (Go monolith + Flutter GUI). HEAD at dispatch: 7a5da96e.

## Authorization
READ-ONLY. No edits to any tracked file. No commits. No `rm -rf` (denied here — use `mktemp -d` for scratch). Your deliverable is ONE report file you own (path given in your brief).

## Prior wave — do not re-report without re-verifying
.hermes/audits/2026-11-19-bughunt-20day.md holds the prior wave (H1-H6, M1-M18, L1-L19) at anchor b37bc202. If a finding of THAT list intersects your scope, re-verify it at CURRENT HEAD and classify STILL-OPEN / FIXED-AT-<sha> / OVERSTATED, citing the current file:line. Do not copy it verbatim.

## Context budget (HARD — the delegate model has 65536 tokens)
- Every read_file call MUST carry limit<=80. Never read a file >1200 lines whole.
- Max 6 large reads per turn. Locate with search_files first, read ±15-line windows around hits.
- Never read docs/*.md or AGENTS.md whole; extract contract lines with search_files.
- If the same tool call repeats twice without new information, STOP and write the report with what you have.

## Findings format
Defects only. Each: ID (F1,F2,...), severity HIGH/MED/LOW, file:line (verify the line at CURRENT HEAD before citing), mechanism (2-4 sentences), falsifiable test or repro shape, commit provenance (`git log -1 --format=%h -- <file>`). Max 15 findings. A clean area = one line in a "Checked clean" list at the end, never a finding. Spec-vs-fake-test defects (a gate test pinning behavior the real binary/vendor rejects) are HIGH when the fake is in-repo.

## Test budget
Parent runs the full suite. You may run at most: `go build ./<your-scope>/...`, `go vet ./<your-scope>/...`, and a few `go test -p 2 -count=1 -run TestName ./<your-pkg>/`. NEVER run the full suite. NEVER start daemons, servers, watchers, or voice calls. End the report with a `gates_run:` line naming exactly what you executed and its outcome.

## Report file discipline
FIRST substantive action: write your report file with title + scope lines. APPEND each finding as you make it (the file is the deliverable; your chat answer may be truncated). Final chat answer: one line + finding count.
