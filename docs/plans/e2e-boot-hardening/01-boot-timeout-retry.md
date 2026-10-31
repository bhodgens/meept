# Boot Timeout + Affected-Suite Retry — Implementation Leaf

> **For the implementing agent:** Implement ALL tasks in order (TDD: Task 1
> writes the failing test FIRST). Do NOT commit. Do NOT git add. Shared repo:
> touch ONLY the files named below. No read_file→write_file; inspect with
> terminal cat/sed; write once and stop.

## Meta

- **Parent:** ./master.md
- **Scope:** Env-tunable e2e daemon boot health deadline (default 120s) + one labeled retry per failed affected suite in the pre-push script.
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** A

## Context

- The flake class and incidents: master.md Goal section (read it).
- `e2e/harness/daemon.go:715` — `deadline := time.Now().Add(60 * time.Second)` inside the boot-wait loop; :737 the Fatalf. The loop also fast-fails on process death (two checks — keep them exactly).
- `scripts/e2e-affected.sh` — read it fully first; find the per-suite `go test` invocation and its rc handling.
- Harness package build tags: check the top of e2e/harness/daemon.go and any existing harness *_test.go (`ls e2e/harness/`) — match whatever tag convention exists (if the package builds untagged, no tag; if `//go:build e2e`, use it).
- Recent sibling commits to be aware of (do not revert): 30eef943 (restart+reload flake fixes + daemon.go +14 lines), 92b26a72 (bin-dir watchdog). Re-read daemon.go around :700-750 at implementation time; line numbers may have shifted.

## Tasks

### Task 1: Harness unit test FIRST (gate F1)

**Files:** Create `e2e/harness/boot_timeout_test.go` (tag per package convention).

Test `e2eBootTimeout()` (the helper Task 2 adds — RED via undefined symbol is the correct starting state):
- unset/empty env → 120s
- `MEEPT_E2E_BOOT_TIMEOUT=45s` → 45s; `=2m` → 120s-equivalent (2*time.Minute)
- invalid (`=banana`) → 120s default, no panic
Use `t.Setenv`. Doc comment names matrix row F1.

### Task 2: daemon.go boot deadline (gate F2)

**Files:** Modify `e2e/harness/daemon.go`.

1. Add unexported helper:
```go
// e2eBootTimeout returns the daemon boot health-wait budget.
// MEEPT_E2E_BOOT_TIMEOUT overrides the 120s default (duration string);
// an unset or unparseable value falls back to the default. The default was
// raised from 60s after four load-dependent boot-timeout flakes
// (2026-10-25..27, e2e-boot-hardening tree): healthy boots take 2-20s, the
// failures clustered at the 60s wall under concurrent machine load.
func e2eBootTimeout() time.Duration {
	const def = 120 * time.Second
	v := strings.TrimSpace(os.Getenv("MEEPT_E2E_BOOT_TIMEOUT"))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}
```
(strings/os imports likely present; add if needed.)
2. Replace the boot-wait deadline literal with `time.Now().Add(e2eBootTimeout())` and update the Fatalf message to include the actual budget (`not healthy within %s`, e2eBootTimeout()). Touch NOTHING else in the loop — both process-death fast-fails stay byte-identical.

### Task 3: e2e-affected.sh retry (gate F3)

**Files:** Modify `scripts/e2e-affected.sh`.

Wrap the per-suite `go test` invocation: on non-zero rc, echo
`RETRY(flake-check): <suite> failed (rc=<N>), re-running once` to stderr (or
the same stream the script uses), re-run the identical command once, and let
the SECOND rc decide. Preserve set -e semantics (if the script uses set -e,
guard with `if ! cmd; then ... fi` — do not let the first failure abort).
Selection logic, -p flags, tags: untouched. Keep the script's existing style
(quoting, indentation).

## Self-Verification Checklist

- [ ] `go vet ./e2e/harness/` clean (with the package's tag if needed: `go vet -tags e2e ./e2e/harness/`)
- [ ] Harness test GREEN: `go test -count=1 ./e2e/harness/ -run TestE2EBootTimeout` (add `-tags e2e` if the package requires it)
- [ ] `bash -n scripts/e2e-affected.sh` OK; `shellcheck scripts/e2e-affected.sh` → no NEW findings (run it before your change too; record both)
- [ ] `make e2e-fast-area AREA=smoke` GREEN
- [ ] gofmt -l on touched Go files → empty
- [ ] `git status --short | grep -v '^??'` → only e2e/harness/daemon.go + scripts/e2e-affected.sh (+ your new test file untracked)

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Diff on daemon.go confined to: helper addition, deadline expression, Fatalf message; process-death fast-fails byte-identical
- [ ] Retry is exactly one, labeled, second-rc-decides; no selection change
- [ ] Test covers three states incl. invalid input; uses t.Setenv
- [ ] No .githooks/ or MEEPT_E2E_SKIP changes
- [ ] Sibling commits 30eef943/92b26a72 content not reverted

Output: APPROVED or gaps with file:line.
