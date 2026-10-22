# Bughunt fix wave 2026-10-16 -- shared fixer contract

Repo: /Users/caimlas/git/meept (Go monorepo + Flutter GUI). HEAD 3a482477 plus
uncommitted working-tree changes from sibling sessions. You fix ONLY the findings
named in your brief, in the files you OWN.

## Hard rules
1. Touch ONLY the files your brief names as owned. The working tree carries other
   agents' uncommitted work -- PRESERVE every existing uncommitted change; never
   git add/commit/checkout/stash/restore/revert anything.
2. Never rm -rf (denied). Scratch: mktemp -d.
3. No long-lived processes. Kill anything you start.
4. Test budget: AT MOST 4 test commands, each scoped to your packages
   (go test -p 2 -run <Name> ./<pkg>/...). go build + go vet on your packages
   only. Never `go test ./...` or `make test` -- the parent runs full gates.
5. Do NOT run tools/analyzers/* (broken here: export-data version mismatch).
6. Bug-fix unit tests are allowed (repo policy: new FEATURES go e2e, bug fixes
   may use unit tests). Add a regression test per fix unless the brief says
   otherwise.
7. If a build fails on a file you do NOT own (sibling mid-edit), wait ~60s and
   retry once. If it persists, make the MINIMAL intent-preserving repair, flag
   it in your report, move on.
8. go.mod drift: if a build promotes a transitive require to direct, restore
   go.mod/go.sum with `git checkout -- go.mod go.sum` ONLY if those files were
   clean for you at start (check `git status --short go.mod go.sum` first).

## Fix quality bar
- Fix the root cause, not the symptom. Match surrounding style.
- For each fix: a falsifiable regression test that FAILS before the fix and
  passes after (run it both ways when feasible; if pre-fix state won't compile,
  say so in the report).
- Deliberate designs documented in code comments are NOT bugs -- if your brief's
  finding turns out to be documented-intentional on inspection, STOP, write the
  evidence in your report, and make no change.

## Report file (your ONLY non-code write)
.hermes/audits/2026-10-16-wave/fix-<slug>.md -- per finding: what changed
(file:line), the regression test name, before/after test output tails, anything
you found but did not touch (sweep list for the parent).
Last line: `gates_run: <exact build/vet/test commands + outcomes>`.
Write the report as soon as your first fix lands; append after.

## macOS note
Full test sweeps need -p 2 (ephemeral port exhaustion). Scoped -run tests only.
