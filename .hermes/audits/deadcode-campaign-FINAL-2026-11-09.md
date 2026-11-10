# Wiring-triage campaign final report (2026-11-09..10)

Run: deadcode-wiring-triage skill campaign, user-approved 'wire + do all of what remains'.

## Outcome

- Stock: 13,610 union baseline -> 1,452 true both-binary intersection -> ~450 deleted
  (425 dispositioned DELETE minus false-deletes caught in flight) -> rebaselined at 13,296 (-314 net).
- Wired (all opt-in config-gated, default false): bot/notification stack ([notifications]),
  HALO RLM trace analysis ([selfimprove].trace_analysis), vector hybrid search + memory.search.hybrid
  RPC ([memory].vector_hybrid), HALO Phase 6 atomic compaction ([agent].atomic_compaction),
  QueuePersister, TUI project_prompt modal (GUI-parity), secrets.Proxy.Stop shutdown, quota_notifier.Cleanup.
- Doc-vs-code gaps closed: git_hooks doc section removed, ValidGoalRound/EffectiveGroups/IsNoWorkingDir/
  ContextWithCallerIdentity/ServiceRegistry comments corrected, DefaultDevAPIKey stale claim fixed,
  turn.go test helpers moved out of production build.
- SkillMCPTool: UNRESOLVED-INTENT (plan marks Cluster-H complete; no registration seam named). Owner review.
- Owner-review HOLDs untouched: internal/code/ast/treectx.go, internal/security/taint/patterns.go,
  runtime docker/harness, resolver_direct.go.

## Vetting integrity

- Re-grep at HEAD before every deletion caught 4+ report errors: team_presets.go (real caller,
  self-reverted), ChatSubmitter (composition pin), SessionHistoryTool test callers, internal/debug
  (zero-caller claim false), session tree live half, FTS5 sanitize family, tlsutil fingerprints,
  validator manager, skills Outcome.String, employee handler_auth live half, task Checklist types.
- Known false-positive class confirmed and documented: single-binary union artifacts (88% of the
  original stock) and RTA interface-dispatch misses (~97 verified imprecisions, none deleted).

## Verification

- Wave gates: per-fix go build/vet/test -count=1 + gofmt on touched packages (all fixer reports + parent spot-checks).
- Campaign close: go build ./... green; go vet clean; deadcode gate exit 0 at 58b82901;
  make docs-check green; final make test 12,444 results, 0 fail (after one in-flight sibling
  talkloop commit 05819379 flushed; re-run green).
- Remaining dirty file: internal/tts/player_samplerate_test.go (sibling in-flight, untouched).

## Follow-ups (queued, not campaign-blocking)

1. Extend .githooks/pre-commit-deadcode to scan all 4 production mains
   (meept-daemon, meept, meept-lite, meept-classifier-test, tools/bridges-eval) - kills the
   union-artifact false-positive class permanently.
2. Owner review: treectx.go, taint/patterns.go, runtime docker/harness, SkillMCPTool intent.
3. secrets.Proxy.Stop shutdown wiring has no dedicated pin (covered indirectly).

## Follow-up executed (2026-11-10): 5-main gate (a7c47404, 05e596eb)

pre-commit-deadcode now analyzes all five production mains and baselines
the INTERSECTION: stock 13,296 -> 528 (-96%). Verified both directions:
clean tree passes; a staged intentionally-unwired func blocks (exit 1).
Mechanics notes baked into the hook: mktemp -d (old -t created a file),
LC_ALL=C export (locale sort broke comm's sorted-input contract),
accumulator-in-subdir (cp onto the dir path is self-copy).
