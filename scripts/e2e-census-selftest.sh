#!/usr/bin/env bash
#
# e2e-census-selftest.sh — regression battery for .githooks/pre-commit-e2e
# Mode 2 (new-package census).
#
# Why this exists: commit f18fea35 rewrote the census into a deterministic
# gate with two test seams (MEEPT_E2E_CENSUS_STAGED_FILE,
# MEEPT_E2E_MANIFEST_FILE). The leaf proved 14 fixture cases by hand but
# pinned none of them as repo tests — a future hook edit could silently
# disarm the gate. This battery IS that pin: it runs the real hook in seam
# mode (the hook exits before Mode 1 in seam mode, so no `go test` ever
# runs), plus one real-index case (modified-only census exemption,
# proven in a scratch git repo with a stubbed affected-suite script so the
# shared worktree's index is never touched).
#
# Bash 3.2-compatible (macOS /bin/bash), stdlib python3 for fixture
# manifests. Runs identically on the Linux CI runner:
#   bash scripts/e2e-census-selftest.sh
#
# Non-vacuity floors: the battery itself fails loudly if fewer than 12
# cases executed (a battery that silently skips everything is the false
# green the census gate itself hunts), and any FAIL exits non-zero.

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
HOOK="$REPO_ROOT/.githooks/pre-commit-e2e"
MIN_CASES=12

if [ ! -f "$HOOK" ]; then
    echo "e2e-census-selftest: cannot find $HOOK — aborting." >&2
    exit 1
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/e2e-census-selftest.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# write_manifest OUT JSON — fixture manifest generation (stdlib python3 only).
write_manifest() {
    python3 -c '
import json, sys
out, raw = sys.argv[1], sys.argv[2]
if raw == "@garbage":
    open(out, "w").write("{ this is not json at all ]]]")
else:
    json.dump(json.loads(raw), open(out, "w"))
' "$1" "$2"
}

# CASES counter + tallies (bash 3.2: plain vars, no arrays/maps).
CASES_RAN=0
CASES_OK=0
CASES_FAIL=0

# run_case NAME EXPECTED_RC EXPECTED_SUBSTRING COMMAND...
# Executes COMMAND, captures merged output, asserts exit code and that
# EXPECTED_SUBSTRING (fixed-string grep) appears in the output.
run_case() {
    name="$1"; want_rc="$2"; want_pat="$3"; shift 3
    out="$("$@" 2>&1)"
    rc=$?
    CASES_RAN=$((CASES_RAN + 1))
    ok=0
    if [ "$rc" = "$want_rc" ]; then
        if [ -z "$want_pat" ] || printf '%s\n' "$out" | grep -qF -- "$want_pat"; then
            ok=1
        fi
    fi
    if [ "$ok" = "1" ]; then
        CASES_OK=$((CASES_OK + 1))
        echo "CASE $name: rc=$rc expected=$want_rc OK"
    else
        CASES_FAIL=$((CASES_FAIL + 1))
        echo "CASE $name: rc=$rc expected=$want_rc FAIL"
        if ! printf '%s\n' "$out" | grep -qF -- "$want_pat"; then
            echo "    missing expected output substring: $want_pat"
        fi
        echo "    ---- hook output ----"
        printf '%s\n' "$out" | sed 's/^/    | /'
        echo "    ---------------------"
    fi
}

# mkstaged FILE PATHS... — write a fixture staged-additions list.
mkstaged() {
    f="$1"; shift
    : > "$f"
    for p in "$@"; do printf '%s\n' "$p" >> "$f"; done
}

# One shared "normal" manifest: covers internal/agent/ + cmd/meept/,
# covers nothing else. Cases that need different coverage write their own.
write_manifest "$WORK/base.json" '{"path_map": {"internal/agent/": ["smoke"], "cmd/meept/": ["smoke"]}, "exempt": {}}'

# ── (a) unmapped new package dir -> blocked, exit 1, names the dir ──────
mkstaged "$WORK/a.txt" "internal/newpkg/foo.go"
run_case unmapped_new_dir_blocks 1 "internal/newpkg" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/a.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/base.json" \
        bash "$HOOK"

# ── (b) same dir WITH a path_map key -> exit 0 ──────────────────────────
mkstaged "$WORK/b.txt" "internal/newpkg/foo.go"
write_manifest "$WORK/b.json" '{"path_map": {"internal/newpkg/": ["smoke"]}, "exempt": {}}'
run_case mapped_new_dir_passes 0 "census-seam mode" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/b.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/b.json" \
        bash "$HOOK"

# ── (c) subpackage under a covered parent (bidirectional prefix) ────────
mkstaged "$WORK/c.txt" "internal/agent/newsub/x.go"
run_case subpackage_under_covered_parent 0 "all mapped" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/c.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/base.json" \
        bash "$HOOK"

# ── (c') slash-boundary: key 'internal/new/' must NOT cover 'internal/newpkg' ──
mkstaged "$WORK/cp.txt" "internal/newpkg/foo.go"
write_manifest "$WORK/cp.json" '{"path_map": {"internal/new/": ["smoke"]}, "exempt": {}}'
run_case slash_boundary_not_covered 1 "internal/newpkg" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/cp.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/cp.json" \
        bash "$HOOK"

# ── (d) empty path_map -> VACUOUS, exit 1 ───────────────────────────────
mkstaged "$WORK/d.txt" "internal/newpkg/foo.go"
write_manifest "$WORK/d.json" '{"path_map": {}, "exempt": {}}'
run_case empty_path_map_vacuous 1 "VACUOUS" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/d.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/d.json" \
        bash "$HOOK"

# ── (d') unparseable manifest JSON -> VACUOUS, exit 1 ───────────────────
write_manifest "$WORK/garbage.json" "@garbage"
run_case unparseable_manifest_vacuous 1 "VACUOUS" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/d.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/garbage.json" \
        bash "$HOOK"

# ── (e) empty staged list -> exit 0 (nothing to census) ─────────────────
mkstaged "$WORK/e.txt"
run_case empty_staged_list_passes 0 "census-seam mode" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/e.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/base.json" \
        bash "$HOOK"

# ── (f) exempt entry WITH reason -> exit 0 + residual-risk printout ─────
mkstaged "$WORK/f.txt" "internal/newpkg/foo.go"
write_manifest "$WORK/f.json" '{"path_map": {"internal/agent/": ["smoke"]}, "exempt": {"internal/newpkg": "legacy surface; e2e suite tracked in issue #123"}}'
run_case exempt_with_reason_prints_residual 0 "ANNOTATED EXEMPTIONS" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/f.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/f.json" \
        bash "$HOOK"

# ── (f') exempt entry with EMPTY/whitespace reason -> exit 1 ────────────
write_manifest "$WORK/fp.json" '{"path_map": {"internal/agent/": ["smoke"]}, "exempt": {"internal/newpkg": "   "}}'
run_case exempt_empty_reason_blocks 1 "internal/newpkg" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/f.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/fp.json" \
        bash "$HOOK"

# ── (g) test-only addition -> 0 census dirs -> exit 0 ───────────────────
mkstaged "$WORK/g.txt" "internal/newpkg/foo_test.go"
run_case test_only_addition_passes 0 "census-seam mode" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/g.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/base.json" \
        bash "$HOOK"

# ── (h) generated path addition -> exit 0 ───────────────────────────────
mkstaged "$WORK/h.txt" "internal/newpkg/generated/x.go"
run_case generated_path_passes 0 "census-seam mode" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/h.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/base.json" \
        bash "$HOOK"

# ── (i) dead escape hatch: staging e2e/suites/ files must NOT bypass the
#       census — the unmapped new dir still blocks (exit 1) ───────────────
mkstaged "$WORK/i.txt" "e2e/suites/newsuite/suite_test.go" "internal/newpkg/foo.go"
write_manifest "$WORK/i.json" '{"path_map": {"e2e/suites/newsuite/": ["newsuite"]}, "exempt": {}}'
run_case suite_escape_hatch_is_dead 1 "internal/newpkg" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/i.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/i.json" \
        bash "$HOOK"

# ── (j) modified (not added) files never join the census population ─────
# Not expressible through the seam (the seam file IS the additions list),
# so this runs the hook in REAL index mode inside a scratch git repo:
# - a stub scripts/e2e-affected.sh (exit 0) so Mode 1 never runs go test
# - a copy of the hook, so REPO_ROOT resolves to the scratch dir
# - the shared worktree's index is never read or written.
SCRATCH="$WORK/scratch-repo"
mkdir -p "$SCRATCH/.githooks" "$SCRATCH/scripts" "$SCRATCH/e2e" "$SCRATCH/internal/nomap"
cp "$HOOK" "$SCRATCH/.githooks/pre-commit-e2e"
printf '#!/usr/bin/env bash\necho "STUB e2e-affected: ok"\nexit 0\n' > "$SCRATCH/scripts/e2e-affected.sh"
chmod +x "$SCRATCH/scripts/e2e-affected.sh"
write_manifest "$SCRATCH/e2e/manifest.json" '{"path_map": {"internal/agent/": ["smoke"]}, "exempt": {}}'
printf 'package nomap\n\nvar X = 1\n' > "$SCRATCH/internal/nomap/x.go"
(
    cd "$SCRATCH" || exit 1
    export GIT_AUTHOR_NAME=selftest GIT_AUTHOR_EMAIL=selftest@example.invalid
    export GIT_COMMITTER_NAME=selftest GIT_COMMITTER_EMAIL=selftest@example.invalid
    git init -q . >/dev/null 2>&1
    git add . >/dev/null 2>&1
    git commit -q -m "baseline" >/dev/null 2>&1
    printf 'package nomap\n\nvar X = 2\n' > internal/nomap/x.go
    git add internal/nomap/x.go >/dev/null 2>&1
) || { echo "CASE modified_only_not_censused: rc=? expected=0 FAIL (scratch repo setup failed)"; CASES_RAN=$((CASES_RAN+1)); CASES_FAIL=$((CASES_FAIL+1)); }
if [ -d "$SCRATCH/.git" ]; then
    run_case modified_only_not_censused 0 "pre-commit-e2e: OK" \
        bash "$SCRATCH/.githooks/pre-commit-e2e"
fi

# ── (k) documented escape hatch MEEPT_SKIP_E2E=1 exits 0 early ──────────
run_case skip_env_escape_hatch 0 "E2E ENFORCEMENT SKIPPED" \
    env MEEPT_SKIP_E2E=1 \
        MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/a.txt" \
        MEEPT_E2E_MANIFEST_FILE="$WORK/garbage.json" \
        bash "$HOOK"

# ── (l) seam guard: MEEPT_E2E_MANIFEST_FILE without the staged seam is
#       fixture-only misuse -> fail-closed exit 1 ─────────────────────────
run_case manifest_seam_requires_staged_seam 1 "fixture-only" \
    env MEEPT_E2E_MANIFEST_FILE="$WORK/base.json" \
        bash "$HOOK"

# ── (m) seam guard: staged seam pointing at a missing file -> exit 1 ────
run_case staged_seam_missing_file 1 "does not exist" \
    env MEEPT_E2E_CENSUS_STAGED_FILE="$WORK/nonexistent.txt" \
        bash "$HOOK"

# ── tally + battery non-vacuity floor ────────────────────────────────────
echo ""
echo "e2e-census-selftest: $CASES_RAN case(s) ran — $CASES_OK OK, $CASES_FAIL FAIL."
if [ "$CASES_RAN" -lt "$MIN_CASES" ]; then
    echo "e2e-census-selftest: NON-VACUITY FLOOR BREACHED — only $CASES_RAN case(s) ran (floor: $MIN_CASES). A battery that skips its cases is a false green. Failing." >&2
    exit 1
fi
if [ "$CASES_FAIL" -ne 0 ]; then
    echo "e2e-census-selftest: FAIL — $CASES_FAIL case(s) did not match the gate contract." >&2
    exit 1
fi
echo "e2e-census-selftest: ALL $CASES_RAN CASES PASS."
exit 0
