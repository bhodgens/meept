#!/usr/bin/env bash
#
# gates-speedup-selftest.sh — offline regression battery for the pre-commit
# gate-speedup change (items 1-6). Bash 3.2-compatible; git + python3 +
# coreutils only; NO network, NO daemon, NO `go test` of real suites (the
# affected-runner seam MEEPT_E2E_GO_TEST_CMD and the deadcode stub seam
# MEEPT_DEADCODE_BIN make every case deterministic). Never touches the
# shared worktree's index: index-mode census cases run in scratch repos.
#
# What it pins:
#   [1] census half of pre-commit-e2e still BLOCKS an unmapped new package
#       dir (fixture seam AND real-index mode); the suite sweep is NOT run
#       by default at commit time, and MEEPT_E2E_PRECOMMIT_SWEEP=1 opts
#       back in (stubbed affected script proves the call).
#   [2] MEEPT_E2E_SCOPE=direct: nested package path selects nothing where
#       full scope fans out; exact-package paths still select their own
#       suites; direct never fires the smoke fallback.
#   [3] flake-retry: a BUILD failure is not retried (SKIPRETRY, blocks);
#       a real flake is retried in parallel and recovers; a double failure
#       still blocks.
#   [5] deadcode cache: MISS -> PASS -> HIT with identical verdict output;
#       a .go content change invalidates; a FAILING run exits non-zero
#       twice (never stored, never replayed as success).
#   [4] parallel dispatcher (MEEPT_HOOKS_SELFTEST seam): aggregate exit
#       non-zero on a blocking failure, verdicts replay in numbered order
#       regardless of completion order, wall time proves concurrency,
#       non-blocking failures never flip the aggregate, serial mode
#       fail-stops at the first blocking failure, and the aggregate exit
#       survives an output pipe (PIPESTATUS[0]) — plus a static scan: no
#       side-effecting command piped into tail/head without pipefail in
#       the dispatch chain files (pipeline-mask guard).
#   [6] snapshot primitive: checkout-index of an index COPY yields the
#       STAGED bytes and excludes the sibling's unstaged edit (the
#       langcode failure class this gates against).
#   Empty-index no-ops for gates 19 (real mode) and 20.
#   bash -n on every file the speedup touched.
#
# Non-vacuity floors: >= 14 executed cases required; any FAIL exits 1.

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
HOOK_E2E="$REPO_ROOT/.githooks/pre-commit-e2e"
HOOK_DEADCODE="$REPO_ROOT/.githooks/pre-commit-deadcode"
HOOK_DISPATCH_LIB="$REPO_ROOT/.githooks/pre-commit-dispatch-lib"
PRE_COMMIT="$REPO_ROOT/.githooks/pre-commit"
AFFECTED="$REPO_ROOT/scripts/e2e-affected.sh"
MIN_CASES=14

for f in "$HOOK_E2E" "$HOOK_DEADCODE" "$HOOK_DISPATCH_LIB" "$PRE_COMMIT" "$AFFECTED"; do
    if [ ! -f "$f" ]; then
        echo "gates-speedup-selftest: cannot find $f — aborting." >&2
        exit 1
    fi
done

WORK="$(mktemp -d "${TMPDIR:-/tmp}/gates-speedup-selftest.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

CASES_RAN=0
CASES_OK=0
CASES_FAIL=0

check() { # check NAME OK_FLAG(1=pass) [detail...]
    name="$1"; okrc="$2"; shift 2
    CASES_RAN=$((CASES_RAN + 1))
    if [ "$okrc" = "1" ]; then
        CASES_OK=$((CASES_OK + 1))
        echo "CASE $name: OK"
    else
        CASES_FAIL=$((CASES_FAIL + 1))
        echo "CASE $name: FAIL — $*"
    fi
}

scratch_repo() { # scratch_repo DIR
    d="$1"
    mkdir -p "$d"
    (
        cd "$d" || exit 1
        export GIT_AUTHOR_NAME=selftest GIT_AUTHOR_EMAIL=selftest@example.invalid
        export GIT_COMMITTER_NAME=selftest GIT_COMMITTER_EMAIL=selftest@example.invalid
        git init -q . >/dev/null 2>&1
        git config user.name selftest >/dev/null 2>&1
        git config user.email selftest@example.invalid >/dev/null 2>&1
    )
}

# Plant the growth script plus both baselines so a scratch repo can run
# pre-commit-deadcode. The hook calls the script before the no-staged-.go
# return; without these files that call exits non-zero and masks the case.
plant_growth_gate() { # plant_growth_gate DIR
    d="$1"
    mkdir -p "$d/.githooks" "$d/scripts"
    cp "$REPO_ROOT/scripts/deadcode-baseline-growth-check.sh" "$d/scripts/deadcode-baseline-growth-check.sh"
    chmod +x "$d/scripts/deadcode-baseline-growth-check.sh"
    printf 'kept\n' > "$d/.githooks/deadcode-baseline.txt"
    printf 'kept\n' > "$d/.githooks/deadcode-baseline-notest.txt"
}

# ==========================================================================
# [1] Census half of pre-commit-e2e (speedup item 1).
# ==========================================================================

# (1a) fixture seam: unmapped new pkg dir still BLOCKS.
printf 'internal/newpkg/foo.go\n' > "$WORK/1a-staged.txt"
printf '{"path_map": {"internal/agent/": ["smoke"]}, "exempt": {}}\n' > "$WORK/1a-manifest.json"
out="$(env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/1a-staged.txt" \
         MEEPT_E2E_MANIFEST_FILE="$WORK/1a-manifest.json" \
         bash "$HOOK_E2E" 2>&1)"; rc=$?
ok=1
[ "$rc" = "1" ] && printf '%s\n' "$out" | grep -qF "internal/newpkg" || ok=0
check census_seam_unmapped_blocks "$ok" "rc=$rc out=$(printf '%s' "$out" | head -c 200)"

# (1b) real-index mode in a scratch repo: staged ADD of an unmapped dir
#      blocks via the exact code path a live commit takes.
SR="$WORK/repo-1"
scratch_repo "$SR"
mkdir -p "$SR/.githooks" "$SR/scripts" "$SR/e2e" "$SR/internal/agent" "$SR/internal/newpkg"
cp "$HOOK_E2E" "$SR/.githooks/pre-commit-e2e"
printf '#!/usr/bin/env bash\necho "SWEEP-RAN- stub affected"\nexit 0\n' > "$SR/scripts/e2e-affected.sh"
chmod +x "$SR/scripts/e2e-affected.sh"
printf '{"path_map": {"internal/agent/": ["smoke"]}, "suites": [{"name":"smoke","dir":"e2e/suites/smoke"}], "exempt": {}}\n' > "$SR/e2e/manifest.json"
printf 'package agent\n' > "$SR/internal/agent/agent.go"
( cd "$SR" && git add -A && git commit -q -m base ) >/dev/null 2>&1
printf 'package newpkg\n\nvar X = 1\n' > "$SR/internal/newpkg/x.go"
( cd "$SR" && git add internal/newpkg/x.go ) >/dev/null 2>&1
out="$(cd "$SR" && .githooks/pre-commit-e2e 2>&1)"; rc=$?
ok=1
[ "$rc" = "1" ] && printf '%s\n' "$out" | grep -qF "NEW package directory" || ok=0
check census_real_index_blocks_unmapped_add "$ok" "rc=$rc"

# (1c) same repo, only a MAPPED dir staged (mod): census passes AND the
#      sweep is NOT run (no SWEEP-RAN in output) — item 1's core claim.
( cd "$SR" && git reset -q HEAD internal/newpkg/x.go
  printf 'package agent\n\nvar Y = 2\n' > internal/agent/agent.go
  git add internal/agent/agent.go ) >/dev/null 2>&1
out="$(cd "$SR" && .githooks/pre-commit-e2e 2>&1)"; rc=$?
ok=1
[ "$rc" = "0" ] \
    && printf '%s\n' "$out" | grep -qF "Suite sweep not run" \
    && ! printf '%s\n' "$out" | grep -qF "SWEEP-RAN" || ok=0
check sweep_skipped_by_default_at_commit "$ok" "rc=$rc out=$(printf '%s' "$out" | head -c 300)"

# (1d) opt-in MEEPT_E2E_PRECOMMIT_SWEEP=1 DOES run the affected script.
out="$(cd "$SR" && MEEPT_E2E_PRECOMMIT_SWEEP=1 .githooks/pre-commit-e2e 2>&1)"; rc=$?
ok=1
[ "$rc" = "0" ] && printf '%s\n' "$out" | grep -qF "SWEEP-RAN" || ok=0
check sweep_optin_runs_affected "$ok" "rc=$rc"

# ==========================================================================
# [2] MEEPT_E2E_SCOPE=direct (speedup item 2) — real manifest, --list + run.
# ==========================================================================

DEEPPATH="internal/agent/not-a-real-subpkg/deep.go"
full_list="$(printf '%s\n' "$DEEPPATH" | MEEPT_E2E_SCOPE=full bash "$AFFECTED" --list 2>/dev/null)"
direct_list="$(printf '%s\n' "$DEEPPATH" | MEEPT_E2E_SCOPE=direct bash "$AFFECTED" --list 2>/dev/null)"
ok=1
[ -n "$full_list" ] && [ -z "$direct_list" ] || ok=0
check direct_scope_no_fanout "$ok" "full='$(printf '%s' "$full_list" | tr '\n' ',')' direct='$(printf '%s' "$direct_list" | tr '\n' ',')'"

q_full="$(printf 'internal/queue/queue.go\n' | MEEPT_E2E_SCOPE=full bash "$AFFECTED" --list 2>/dev/null | sort | tr '\n' ' ')"
q_direct="$(printf 'internal/queue/queue.go\n' | MEEPT_E2E_SCOPE=direct bash "$AFFECTED" --list 2>/dev/null | sort | tr '\n' ' ')"
ok=1
[ -n "$q_direct" ] && [ "$q_full" = "$q_direct" ] || ok=0
check direct_scope_exact_pkg_selects_own_suites "$ok" "full='$q_full' direct='$q_direct'"

# direct + no match -> smoke fallback DISABLED, exit 0, nothing executed
# (GO_CMD=/usr/bin/false would fail the run if anything launched).
out="$(printf '%s\n' "$DEEPPATH" | MEEPT_E2E_SCOPE=direct MEEPT_E2E_GO_TEST_CMD=/usr/bin/false bash "$AFFECTED" 2>&1)"; rc=$?
ok=1
[ "$rc" = "0" ] && printf '%s\n' "$out" | grep -qF "NOTHING selected" || ok=0
check direct_scope_no_smoke_fallback "$ok" "rc=$rc"

# ==========================================================================
# [3] Flake-retry: parallel + build-failure skip (speedup item 3).
# ==========================================================================

cat > "$WORK/stub-buildfail" <<'STUB'
#!/usr/bin/env bash
echo "# pkg"
echo "FAIL	pkg	[build failed]"
exit 2
STUB
chmod +x "$WORK/stub-buildfail"
out="$(printf 'e2e/suites/smoke/suite_test.go\n' | MEEPT_E2E_GO_TEST_CMD="$WORK/stub-buildfail" bash "$AFFECTED" 2>&1)"; rc=$?
ok=1
[ "$rc" != "0" ] \
    && printf '%s\n' "$out" | grep -qF "SKIPRETRY(build)" \
    && ! printf '%s\n' "$out" | grep -qF "RETRY(flake-check)" || ok=0
check build_failure_retry_skipped "$ok" "rc=$rc out=$(printf '%s' "$out" | head -c 300)"

cat > "$WORK/stub-flake" <<STUB
#!/usr/bin/env bash
# Fails the FIRST invocation per package arg, passes later ones — an
# offline stand-in for a load-dependent boot flake.
state="$WORK/stub-flake-state"
mk="\$(printf '%s' "\$*" | tr -dc 'a-zA-Z0-9/._' | cksum | tr -d ' ')"
n=0
[ -f "\$state.\$mk" ] && n="\$(cat "\$state.\$mk")"
n=\$((n+1))
printf '%s\n' "\$n" > "\$state.\$mk"
if [ "\$n" -le 1 ]; then
    echo "flaky: boot timeout waiting for daemon"
    exit 1
fi
exit 0
STUB
chmod +x "$WORK/stub-flake"
out="$(printf 'e2e/suites/smoke/suite_test.go\n' | MEEPT_E2E_GO_TEST_CMD="$WORK/stub-flake" bash "$AFFECTED" 2>&1)"; rc=$?
ok=1
[ "$rc" = "0" ] \
    && printf '%s\n' "$out" | grep -qF "RETRY(flake-check)" \
    && printf '%s\n' "$out" | grep -qF "passed on re-run" || ok=0
check flake_retry_recovers "$ok" "rc=$rc out=$(printf '%s' "$out" | head -c 300)"

cat > "$WORK/stub-alwaysfail" <<'STUB'
#!/usr/bin/env bash
echo "FAIL	pkg	--- regression"
exit 1
STUB
chmod +x "$WORK/stub-alwaysfail"
out="$(printf 'e2e/suites/smoke/suite_test.go\n' | MEEPT_E2E_GO_TEST_CMD="$WORK/stub-alwaysfail" bash "$AFFECTED" 2>&1)"; rc=$?
ok=1
[ "$rc" != "0" ] && printf '%s\n' "$out" | grep -qF "failed TWICE" || ok=0
check double_failure_still_blocks "$ok" "rc=$rc"

# ==========================================================================
# [5] Deadcode cache (speedup item 5) — scratch repo + stub deadcode.
# ==========================================================================
DC="$WORK/repo-dc"
scratch_repo "$DC"
mkdir -p "$DC/cmd/a"
plant_growth_gate "$DC"
printf 'module example.com/dc\n\ngo 1.24\n\ntoolchain go1.24.0\n' > "$DC/go.mod"
printf 'package a\n\nfunc Bar() {}\n\nfunc Baz() {}\n' > "$DC/cmd/a/a.go"
( cd "$DC" && git add -A && git commit -q -m base ) >/dev/null 2>&1
cat > "$WORK/dcstub" <<'STUB'
#!/usr/bin/env bash
# Stub deadcode: deterministic output regardless of tree content (the
# cache DIGEST, not this output, is what the test exercises).
if [ "$1" = "-test" ]; then
    printf './cmd/a/a.go:3:2: unreachable func: Bar\n./cmd/a/a.go:5:1: unreachable func: Baz\n'
else
    printf './cmd/a/a.go:3:2: unreachable func: Bar\n'
fi
exit 0
STUB
chmod +x "$WORK/dcstub"
printf './cmd/a/a.go unreachable func: Bar\n./cmd/a/a.go unreachable func: Baz\n' > "$WORK/dc-baseline-test.txt"
printf './cmd/a/a.go unreachable func: Bar\n' > "$WORK/dc-baseline-notest.txt"
printf '' > "$WORK/dc-baseline-empty.txt"
DCCACHE="$WORK/dc-cache"
dc_env_test="$WORK/dc-baseline-test.txt"
dc_env_notest="$WORK/dc-baseline-notest.txt"
dc_run() { # dc_run BASELINE_TEST  (first arg overrides the -test baseline)
    bt="${1:-$dc_env_test}"
    ( cd "$DC" && env \
        GOTOOLCHAIN=local \
        MEEPT_DEADCODE_BIN="$WORK/dcstub" \
        MEEPT_DEADCODE_MAINS="./cmd/a" \
        MEEPT_DEADCODE_BASELINE="$bt" \
        MEEPT_DEADCODE_BASELINE_NOTEST="$dc_env_notest" \
        MEEPT_DEADCODE_FULL_TREE=1 \
        MEEPT_DEADCODE_CACHE_DIR="$DCCACHE" \
        bash "$HOOK_DEADCODE" 2>&1 )
}
r1="$(dc_run)"; rc1=$?
ok=1
[ "$rc1" = "0" ] && printf '%s\n' "$r1" | grep -qF "deadcode cache: MISS" || ok=0
check deadcode_first_run_miss_pass "$ok" "rc=$rc1 out=$(printf '%s' "$r1" | head -c 300)"

r2="$(dc_run)"; rc2=$?
r1_body="$(printf '%s\n' "$r1" | grep -v '^deadcode cache')"
r2_body="$(printf '%s\n' "$r2" | grep -v '^deadcode cache')"
ok=1
[ "$rc2" = "0" ] \
    && printf '%s\n' "$r2" | grep -qF "deadcode cache: HIT" \
    && [ "$r1_body" = "$r2_body" ] || ok=0
check deadcode_second_run_hit_identical "$ok" "rc=$rc2"

# Any .go content change -> MISS.
printf '\n// touched by selftest\n' >> "$DC/cmd/a/a.go"
r3="$(dc_run)"; rc3=$?
ok=1
[ "$rc3" = "0" ] && printf '%s\n' "$r3" | grep -qF "deadcode cache: MISS" || ok=0
check deadcode_go_change_invalidates "$ok" "rc=$rc3 out=$(printf '%s' "$r3" | head -c 200)"

# Fail-closed: a failing verdict (empty -test baseline) must never be
# stored — run it TWICE; both runs exit non-zero (a cached-success replay
# would flip the second to 0).
r4="$(dc_run "$WORK/dc-baseline-empty.txt")"; rc4=$?
r5="$(dc_run "$WORK/dc-baseline-empty.txt")"; rc5=$?
ok=1
[ "$rc4" != "0" ] && [ "$rc5" != "0" ] \
    && printf '%s\n' "$r4" | grep -qF "newly unwired" || ok=0
check deadcode_failure_never_cached_as_success "$ok" "rc4=$rc4 rc5=$rc5"

# ==========================================================================
# [4] Parallel dispatcher (speedup item 4) via MEEPT_HOOKS_SELFTEST seam.
# ==========================================================================
mkhook() { # mkhook NAME SLEEP RC MARKER -> echoes path
    f="$WORK/hook-$1"
    {
        echo '#!/usr/bin/env bash'
        [ "$2" != "0" ] && echo "sleep $2"
        echo "echo $4"
        echo "exit $3"
    } > "$f"
    chmod +x "$f"
    printf '%s' "$f"
}
H1="$(mkhook pass1 2 0 MARK_ONE)"
H2="$(mkhook fail1 0 1 MARK_TWO)"
H3="$(mkhook pass2 2 0 MARK_THREE)"
HNO="$(mkhook nb 0 3 MARK_NB)"

t0=$SECONDS
env MEEPT_HOOKS_SELFTEST=1 \
    MEEPT_HOOKS_SELFTEST_HOOKS="$H1 $H2 $H3" \
    bash "$PRE_COMMIT" > "$WORK/dispatch-a.out" 2>&1
rc_a=$?
wall=$((SECONDS - t0))
n1="$(grep -n 'selftest 1' "$WORK/dispatch-a.out" | head -1 | cut -d: -f1)"
n2="$(grep -n 'selftest 2' "$WORK/dispatch-a.out" | head -1 | cut -d: -f1)"
n3="$(grep -n 'selftest 3' "$WORK/dispatch-a.out" | head -1 | cut -d: -f1)"
ok=1
[ "$rc_a" = "1" ] || ok=0
[ -n "$n1" ] && [ -n "$n2" ] && [ -n "$n3" ] || ok=0
[ "$n1" -lt "$n2" ] 2>/dev/null || ok=0
[ "$n2" -lt "$n3" ] 2>/dev/null || ok=0
grep -qF "FAIL (rc=1)" "$WORK/dispatch-a.out" || ok=0
grep -qF "MARK_THREE" "$WORK/dispatch-a.out" || ok=0
[ "$wall" -lt 4 ] || ok=0
check dispatch_parallel_order_exit_concurrency "$ok" "rc=$rc_a wall=${wall}s n1=$n1 n2=$n2 n3=$n3 (see $WORK/dispatch-a.out)"

env MEEPT_HOOKS_SELFTEST=1 \
    MEEPT_HOOKS_SELFTEST_HOOKS="$H1" \
    MEEPT_HOOKS_SELFTEST_NONBLOCKING="$HNO" \
    bash "$PRE_COMMIT" > "$WORK/dispatch-b.out" 2>&1
rc_b=$?
ok=1
[ "$rc_b" = "0" ] || ok=0
grep -qF "non-blocking" "$WORK/dispatch-b.out" || ok=0
check dispatch_nonblocking_failure_does_not_block "$ok" "rc=$rc_b"

env MEEPT_HOOKS_SELFTEST=1 \
    MEEPT_HOOKS_SELFTEST_HOOKS="$H2 $H1" \
    MEEPT_HOOKS_SELFTEST_SERIAL=1 \
    bash "$PRE_COMMIT" > "$WORK/dispatch-c.out" 2>&1
rc_c=$?
ok=1
[ "$rc_c" = "1" ] || ok=0
grep -qF "blocked at [1/21]" "$WORK/dispatch-c.out" || ok=0
# fail-stop: the second hook must NOT have run.
if grep -qF "MARK_ONE" "$WORK/dispatch-c.out"; then ok=0; fi
check dispatch_serial_failstop "$ok" "rc=$rc_c"

# (4d) PIPELINE-MASK GUARD (2026-11-16 incident: `git commit ... | tail`
# returns TAIL's status — a failed commit nearly read as success).
# Two halves:
#  - behavioral: a blocking-failure dispatch, with its stdout drained
#    through a pipe, must still expose rc=1 via PIPESTATUS[0] (the
#    aggregate exit rides `return`/`exit`, never a pipe stage).
#  - static: no side-effecting command (git/go/make) may be piped into a
#    status-swallowing filter (tail/head) in the dispatch chain files
#    unless the file sets pipefail. Text-book grep loop, no eval.
env MEEPT_HOOKS_SELFTEST=1 \
    MEEPT_HOOKS_SELFTEST_HOOKS="$H2 $H1" \
    bash "$PRE_COMMIT" 2>/dev/null | tail -3 >/dev/null
piped_rc="${PIPESTATUS[0]}"
ok=1
[ "$piped_rc" = "1" ] || ok=0
check dispatch_blocking_rc_survives_output_pipe "$ok" "PIPESTATUS[0]=$piped_rc want=1"

mask_scan_fail=0
mask_probe() { # $1 = file; echoes nothing, returns 0 if a masking line found
    grep -q "pipefail" "$1" && return 1
    grep -vE '^[[:space:]]*#' "$1" \
        | grep -qE '(^|[;&|(]|[[:space:]])(git|go|make)[[:space:]][^|]*\|[[:space:]]*(tail|head)([[:space:]]|$|\))'
}
# Non-vacuity: the probe must catch a planted violation and must not fire
# on the hardened safe forms (PIPESTATUS capture, pipefail-set file).
printf '#!/bin/bash\nset -e\ngit commit -m x | tail -1\n' > "$WORK/mask-probe-bad.sh"
printf '#!/bin/bash\nsome_command | tail -1; rc=${PIPESTATUS[0]}\nset -o pipefail\ngit push | tail -1\n' > "$WORK/mask-probe-ok.sh"
mask_probe "$WORK/mask-probe-bad.sh" || mask_scan_fail=1
! mask_probe "$WORK/mask-probe-ok.sh" || mask_scan_fail=1
for f in "$PRE_COMMIT" "$HOOK_DISPATCH_LIB" "$REPO_ROOT/.githooks/pre-commit-build" \
         "$REPO_ROOT/.githooks/pre-commit-feature-docs" "$REPO_ROOT/.githooks/pre-push" \
         "$REPO_ROOT/.githooks/pre-push-vet" "$REPO_ROOT/.githooks/pre-push-bench"; do
    if mask_probe "$f"; then
        echo "  pipe-masking pattern found in $(basename "$f"):"
        grep -nE '(^|[;&|(]|[[:space:]])(git|go|make)[[:space:]][^|]*\|[[:space:]]*(tail|head)([[:space:]]|$|\))' "$f" | grep -vE ':[[:space:]]*#' | sed 's/^/    /'
        mask_scan_fail=1
    fi
done
[ "$mask_scan_fail" = "0" ] && ok=1 || ok=0
check dispatch_chain_no_sideeffect_pipe_to_filter "$ok" "see lines above"

# ==========================================================================
# [6] Snapshot primitive (speedup item 6): staged bytes, not worktree.
# ==========================================================================
SNAPR="$WORK/repo-snap"
scratch_repo "$SNAPR"
mkdir -p "$SNAPR/pkg"
printf 'module example.com/snap\n\ngo 1.24\n' > "$SNAPR/go.mod"
printf 'package pkg\n\nconst V = "staged"\n' > "$SNAPR/pkg/v.go"
( cd "$SNAPR" && git add -A && git commit -q -m base ) >/dev/null 2>&1
( cd "$SNAPR" && printf 'package pkg\n\nconst V = "STAGED-FOR-COMMIT"\n' > pkg/v.go \
    && git add pkg/v.go ) >/dev/null 2>&1
# sibling session edits the same file UNstaged after the add:
printf 'package pkg\n\nconst V = "sibling-wip-must-not-leak"\n' > "$SNAPR/pkg/v.go"
SNAPD="$WORK/snap-tree"; SNAPIDXF="$WORK/snap-index"
mkdir -p "$SNAPD"
cp "$SNAPR/.git/index" "$SNAPIDXF"
( cd "$SNAPR" && GIT_INDEX_FILE="$SNAPIDXF" git checkout-index -a --prefix="$SNAPD/" ) >/dev/null 2>&1
ok=1
grep -qF "STAGED-FOR-COMMIT" "$SNAPD/pkg/v.go" 2>/dev/null || ok=0
grep -qF "sibling-wip-must-not-leak" "$SNAPD/pkg/v.go" 2>/dev/null && ok=0
check snapshot_yields_staged_not_worktree "$ok" "checkout-index of staged index copy"

# ==========================================================================
# Empty-index no-ops.
# ==========================================================================
ER="$WORK/repo-empty"
scratch_repo "$ER"
mkdir -p "$ER/.githooks" "$ER/e2e" "$ER/docs"
plant_growth_gate "$ER"
printf '{"path_map": {"internal/agent/": ["smoke"]}, "exempt": {}}\n' > "$ER/e2e/manifest.json"
cp "$HOOK_E2E" "$ER/.githooks/pre-commit-e2e"
printf 'x\n' > "$ER/docs/readme.md"
( cd "$ER" && git add -A && git commit -q -m base ) >/dev/null 2>&1
out="$(cd "$ER" && .githooks/pre-commit-e2e 2>&1)"; rc=$?
ok=1
[ "$rc" = "0" ] && printf '%s\n' "$out" | grep -qF "no staged changes" || ok=0
check gate19_empty_index_noop "$ok" "rc=$rc"

out="$(cd "$ER" && env MEEPT_DEADCODE_BIN="$WORK/dcstub" MEEPT_DEADCODE_MAINS="./cmd/a" \
      MEEPT_DEADCODE_BASELINE="$WORK/dc-baseline-test.txt" \
      MEEPT_DEADCODE_BASELINE_NOTEST="$WORK/dc-baseline-notest.txt" \
      GOTOOLCHAIN=local bash "$HOOK_DEADCODE" 2>&1)"; rc=$?
# empty staged diff + not FULL_TREE -> growth check (no growth) then early
# exit 0 BEFORE reachability analysis
[ "$rc" = "0" ] && ok=1 || ok=0
check gate20_empty_index_noop "$ok" "rc=$rc out=$(printf '%s' "$out" | head -c 150)"

# Missing working-tree baseline must not pass. The old script copied nothing
# to an empty file and exited 0. The hook calls the script before the
# no-staged-.go return, so a baseline-only tree with a deleted allow-list
# exits non-zero even when no .go file is staged.
MB="$WORK/repo-missing-base"
scratch_repo "$MB"
plant_growth_gate "$MB"
( cd "$MB" && git add -A && git commit -q -m base ) >/dev/null 2>&1
rm -f "$MB/.githooks/deadcode-baseline.txt"
out="$(cd "$MB" && env MEEPT_DEADCODE_BIN="$WORK/dcstub" MEEPT_DEADCODE_MAINS="./cmd/a" \
      MEEPT_DEADCODE_ALLOW_SKIP=1 GOTOOLCHAIN=local bash "$HOOK_DEADCODE" 2>&1)"; rc=$?
ok=1
[ "$rc" != "0" ] && printf '%s\n' "$out" | grep -qF "working-tree baseline missing" || ok=0
check deadcode_missing_baseline_exits_nonzero "$ok" "rc=$rc out=$(printf '%s' "$out" | head -c 200)"

# Baseline-only growth (no staged .go) must not exit 0. The old hook returned
# before any growth check when git diff --cached had no .go path.
GR="$WORK/repo-baseline-growth"
scratch_repo "$GR"
plant_growth_gate "$GR"
( cd "$GR" && git add -A && git commit -q -m base ) >/dev/null 2>&1
printf 'new-unwired\n' >> "$GR/.githooks/deadcode-baseline.txt"
( cd "$GR" && git add .githooks/deadcode-baseline.txt ) >/dev/null 2>&1
out="$(cd "$GR" && env MEEPT_DEADCODE_ALLOW_SKIP=1 GOTOOLCHAIN=local bash "$HOOK_DEADCODE" 2>&1)"; rc=$?
ok=1
[ "$rc" != "0" ] && printf '%s\n' "$out" | grep -qF "growth" || ok=0
check deadcode_baseline_only_growth_exits_nonzero "$ok" "rc=$rc out=$(printf '%s' "$out" | head -c 200)"

# ==========================================================================
# bash -n on every touched shell file.
# ==========================================================================
syntax_fail=0
for f in "$PRE_COMMIT" "$HOOK_DISPATCH_LIB" "$HOOK_E2E" "$HOOK_DEADCODE" \
         "$REPO_ROOT/.githooks/pre-push-e2e" "$AFFECTED" \
         "$REPO_ROOT/scripts/deadcode-baseline-growth-check.sh" \
         "$SCRIPT_DIR/gates-speedup-selftest.sh"; do
    if ! bash -n "$f" 2>/dev/null; then
        echo "  bash -n FAILED: $f"
        syntax_fail=1
    fi
done
[ "$syntax_fail" = "0" ] && ok=1 || ok=0
check bash_n_all_touched_files "$ok" "see lines above"

# ==========================================================================
echo ""
echo "gates-speedup-selftest: $CASES_RAN case(s) ran — $CASES_OK OK, $CASES_FAIL FAIL."
if [ "$CASES_RAN" -lt "$MIN_CASES" ]; then
    echo "gates-speedup-selftest: NON-VACUITY FLOOR BREACHED — only $CASES_RAN case(s) ran (floor: $MIN_CASES). A battery that skips its cases is a false green. Failing." >&2
    exit 1
fi
if [ "$CASES_FAIL" -ne 0 ]; then
    echo "gates-speedup-selftest: FAIL — $CASES_FAIL case(s) did not match the gate contract." >&2
    exit 1
fi
echo "gates-speedup-selftest: ALL $CASES_RAN CASES PASS."
exit 0
