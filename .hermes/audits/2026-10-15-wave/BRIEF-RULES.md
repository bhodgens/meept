# Auditor rules -- bughunt wave 2026-10-15 (read this first)

Repo: /Users/caimlas/git/meept -- Go monorepo ("meept"), HEAD 623f33b1.
Daemon: cmd/meept-daemon. CLI: cmd/meept. Invariant docs: root AGENTS.md plus
internal/{llm,daemon,agent,comm}/AGENTS.md.

## Hard rules

1. READ-ONLY audit. Do not edit, create, or delete any file in the repo except
   YOUR ONE report file (named in your goal). No commits, no `git stash`, no
   `git checkout -- <file>`.
2. Never run `rm -rf` (this environment denies it and burns your budget). Use
   `mktemp -d` for any scratch copy.
3. Do NOT run `go test ./...` or any package-wide test sweep -- the parent runs
   the full suite and parallel sweeps exhaust localhost ports. You may run:
   `go build ./<pkg>/...`, `go vet ./<pkg>/...`, and at most 3 tests scoped
   with `-run <Name>` inside your own packages.
4. Do not start servers, model processes, watchers, or daemons.
5. TIME-BOXED. Target ~40 tool calls, finish inside ~15 minutes. grep-first:
   never read a file longer than 1500 lines end-to-end; read +/-25 lines around
   a hit. Prioritise the numbered tasks in your goal so partial work still has
   value.
6. Do not re-report findings already closed or deferred: read
   `.hermes/audits/2026-10-13-bughunt-10day.md` (tables list FIXED / DEFERRED /
   deliberately KEPT items).
7. The working tree carries uncommitted sibling edits in
   `internal/config/schema.go`, `internal/configui/sections_memory.go`,
   `internal/memory/manager.go`, plus untracked
   `internal/config/memory_backend_test.go`. Audit them as current code. Never
   revert or "fix" them.
8. Deliberate design trade-offs documented in code comments or config defaults
   (opt-in defaults that ship false, fail-open posture, report-only doctor) are
   NOT bugs. Do not "fix" architecture.

## Finding format (required)

Numbered findings. Each one:

- Severity: CRITICAL / HIGH / MEDIUM / LOW
- Location: `file:line`
- What breaks: one or two plain sentences naming the mechanism (not just the
  rule violated)
- Falsifiable test: the exact command or probe that would show it
- Evidence: SOURCE (you read the code) or LIVE (you ran something and paste the
  output tail)
- Confidence: high / medium

End your report with:

- `## Checked and clean` -- the packages/files you covered that you found no bug
  in, so coverage is auditable.
- `## Gates run` -- the exact build/vet/test commands you ran and their results.
- `## Not covered` -- what you deliberately skipped.

Report at most 15 findings. Quality over volume: a wrong finding costs more than
a missed minor one.
