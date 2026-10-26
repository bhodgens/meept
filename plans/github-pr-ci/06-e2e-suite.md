# Hermetic E2E Suite — Implementation Leaf (06)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Gate tests EXIST and are committed — make them pass; do NOT weaken them.
> Do NOT commit. Explore with search_files or terminal cat, not read_file.

## Meta

- **Parent:** ../master.md
- **Scope:** `e2e/suites/github-pr-ci/` hermetic suite + manifest entry.
- **Dependencies:** 01, 02, 03 (pool + tools); runs against the real
  daemon binary with a fake GitHub.
- **Estimated Context:** 55K
- **Concurrency Group:** C

## Goal

Prove the loop end-to-end without touching github.com: daemon configured
against a fake GitHub server, chat turn drives PR create + CI wait to
green, effects dedup holds across a restart, and the disabled-by-default
guarantee holds.

## Context

E2E policy (AGENTS.md): all new-feature tests in the hermetic e2e tier.
Suites live in e2e/suites/<name>/ with the `e2e` build tag; manifest +
path->suite mapping in e2e/manifest.json. The harness builds
meept-daemon/meept once per test process (e2e/harness/daemon.go); do NOT
mkdir your own temp bins (scratch-bin hygiene). Pre-commit check [18/18]
blocks new packages without a manifest entry.

Key files:
- e2e/manifest.json — path_map + suite registration
- e2e/harness/daemon.go — daemon spawn + readiness
- any existing e2e/suites/*/ — suite shape reference

## Interface Contracts (From Parent)

### What This Leaf Exposes

Suite `github-pr-ci` in e2e/manifest.json with path_map entries for
`internal/githubsvc/` and the new builtin tool files. Scenarios:

1. `TestE2ECreateWaitGreen` — fake GitHub (httptest inside the suite,
   stubbing POST /repos/:o/:r/pulls, GET pulls/:n, GET
   commits/:sha/check-runs, GET actions/runs) serving first in_progress
   then success; daemon `[github].base_url` pointed at it; chat turn
   "open a PR from branch X and wait for CI"; assert tool events show
   create + green wait; fake saw exactly one create POST (effects dedup).
2. `TestE2EDisabledByDefault` — daemon without [github] block; tool list
   in chat contains no github_* tools.
3. `TestE2EEffectsReconcile` — pre-seed a claimed-but-unexecuted
   github_pr_create effect key in the daemon's DB; chat turn tries the
   identical create; assert the turn surfaces the reconciliation prompt
   instead of a second POST.

### What This Leaf Consumes

```
e2e harness helpers (existing)
fake GitHub server — implement in-suite (package-local fake_github.go)
```

## Tasks

### Task 1: Fake GitHub + suite skeleton + manifest

**Files:**
- Create: `e2e/suites/github-pr-ci/fake_github.go` (httptest server with
  scripted responses + request counters)
- Create: `e2e/suites/github-pr-ci/main_test.go` (suite shell,
  scenario 2 first)
- Modify: `e2e/manifest.json` (suite + path_map)

Run: `make e2e-fast-area AREA=github-pr-ci` -> scenario 2 PASS.

### Task 2: Create-wait-green scenario

**Files:**
- Modify: `e2e/suites/github-pr-ci/main_test.go`

Run -> scenario 1 PASS. Assert counters: exactly one pulls POST; check-runs
polled <= 6 times (backoff floor 15s would exceed suite budget — the fake
returns success after the 2nd poll; assert <= 6 to catch hammering).

### Task 3: Effects reconcile scenario

**Files:**
- Modify: `e2e/suites/github-pr-ci/main_test.go`

Run -> scenario 3 PASS.

## Self-Verification Checklist

- [ ] Suite in manifest; path_map covers internal/githubsvc/ + tool files
- [ ] All three scenarios PASS via make e2e-fast-area
- [ ] No live network (fake only); no leaked temp bin dirs
- [ ] Suite is hermetic: no ordering dependence, no sleeps (bounded polls)

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] `make e2e-fast` unaffected elsewhere (no manifest breakage)
- [ ] Counters assert no double-POST (effects contract at e2e level)
- [ ] Fake covers only endpoints the tools call

## Notes

- If the harness lacks a chat-turn helper for tool-event assertions, use
  the existing suite closest to this shape as the reference and note the
  adaptation — do not invent a new harness API.
