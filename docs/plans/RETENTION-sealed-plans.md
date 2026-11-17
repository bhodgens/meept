# Sealed-plan stub retention sweep

Swept 2026-11-17 (issue #147).

- What was here: 1089 files matching docs/plans/sealed-plan-<N>.md —
  128-byte runtime stubs (status: planning, empty Summary/Notes).
  Total 139,962 bytes. Verified at sweep time: ALL 1089 were empty
  skeletons; none carried content; none were referenced by any file in
  the repo; none were tracked by git (docs/plans/sealed-plan*.md is in
  .gitignore, line 161).
- Where they went: archived intact to
  /Users/caimlas/.hermes/archives/sealed-plans-20261116.tar.gz
  (94,901 bytes gzipped; docs-plans/ layout preserved inside). Nothing
  was deleted; the tarball is the sole copy.
- Restore command: tar -xzf ~/.hermes/archives/sealed-plans-20261116.tar.gz
  -C /Users/caimlas/git/meept/docs/../tmp-restore   (files land under
  docs-plans/; move back to docs/plans/ as needed).
- Emitter status: no code in this repo writes the numbered stubs — a
  repo-wide Go grep for 'sealed-plan' hits only one test comment
  (internal/plan/manager_test.go:646, about the unnumbered
  sealed-plan.md fixture). The generator is external tooling (the
  plan-seal path of an agent harness). Until that emitter is changed to
  write under $MEEPT_HOME/plans/ or a dot-subdir, new stubs may
  reappear here; re-run this sweep periodically (they are gitignored,
  so they never pollute commits — only local tree listings).
