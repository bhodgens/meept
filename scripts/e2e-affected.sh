#!/usr/bin/env bash
#
# e2e-affected.sh — run the hermetic Go e2e suites for the areas affected by
# a set of changed paths.
#
# Changed paths (stdin, positional args, or --from-diff) are mapped through
# e2e/manifest.json "path_map" to suite names; each suite's test directory is
# resolved from "suites"[].dir. Suites whose directory does not exist yet
# (manifest status: todo) are reported as pending and skipped; if no affected
# suite dir exists yet but Go files changed, the smoke suite runs as the
# fallback so the gate stays non-decorative during the rollout.
#
# Usage:
#   scripts/e2e-affected.sh [--list] [--from-diff] [path ...]
#
# Options:
#   --list       print the affected suite names only; run nothing
#   --from-diff  take changed paths from `git diff --name-only HEAD`
#   path ...     changed paths as positional args (else stdin; none = no-op)
#
# Env:
#   MEEPT_E2E_SCOPE=full|direct   suite-selection breadth (default: full).
#     full   — the historical gate: every path_map key that PREFIXES a changed
#              path contributes its suites (so internal/agent/sub/x.go selects
#              the internal/agent/ suites too), with the smoke fallback when
#              Go changed but nothing matched. This is the thorough sweep the
#              pre-push gate runs.
#     direct — no fan-out: a changed path selects suites ONLY from the path_map
#              key equal to the file's own package dir (internal/kanban/x.go ->
#              kanban-board; nothing else). Nothing matched selects NOTHING —
#              the smoke fallback is disabled in this mode (it is exactly the
#              fan-out direct mode exists to avoid), and that is printed
#              loudly. Local dev escape valve for running the sweep cheaply
#              where the full fan-out would sweep half the manifest.
#   MEEPT_E2E_GO_TEST_CMD   test seam: replaces the `go test` command prefix
#              (default "go test"). Lets offline selftests
#              (scripts/gates-speedup-selftest.sh) drive the runner and the
#              flake-retry logic without the Go toolchain.
#   MEEPT_E2E_RETRY_PARALLEL  max concurrent suite retries (default 2).
#
# bash-3.2 compatible (macOS /bin/bash); python3 does the JSON parsing
# (repo convention in scripts/).

set -u -o pipefail

# Pin the toolchain to go.mod's `toolchain` line for standalone runs (make
# targets already export GOTOOLCHAIN). See .githooks/pre-commit:35-45.
if [ -z "${GOTOOLCHAIN:-}" ] || [ "${GOTOOLCHAIN:-}" = "auto" ]; then
    export GOTOOLCHAIN="$(sed -n 's/^toolchain[[:space:]]//p' "$PWD/go.mod" 2>/dev/null | head -1)"
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

MANIFEST="e2e/manifest.json"
SMOKE_DIR="e2e/suites/smoke"
LIST_ONLY=0
FROM_DIFF=0

# Suite-selection scope (see header). Anything other than "direct" is "full".
E2E_SCOPE="${MEEPT_E2E_SCOPE:-full}"
if [ "$E2E_SCOPE" != "direct" ]; then
    E2E_SCOPE="full"
fi
GO_CMD="${MEEPT_E2E_GO_TEST_CMD:-go test}"
RETRY_PARALLEL="${MEEPT_E2E_RETRY_PARALLEL:-2}"

usage() {
    echo "usage: scripts/e2e-affected.sh [--list] [--from-diff] [path ...]" >&2
    echo "  --list       print affected suite names only (run nothing)" >&2
    echo "  --from-diff  read changed paths from \`git diff --name-only HEAD\`" >&2
    echo "  path ...     changed paths as args (else stdin; none = no-op)" >&2
    exit 2
}

while [ $# -gt 0 ]; do
    case "$1" in
        --list)     LIST_ONLY=1 ;;
        --from-diff) FROM_DIFF=1 ;;
        -h|--help)  usage ;;
        --*)        echo "e2e-affected: unknown option: $1" >&2; usage ;;
        *)          break ;;
    esac
    shift
done

TMP="$(mktemp "${TMPDIR:-/tmp}/e2e-affected.XXXXXX")"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/e2e-affected-run.XXXXXX")"
trap 'rm -f "$TMP"; rm -rf "$WORK"' EXIT

if [ "$FROM_DIFF" = "1" ]; then
    git diff --name-only HEAD > "$TMP" || exit 1
elif [ $# -gt 0 ]; then
    printf '%s\n' "$@" > "$TMP"
elif [ ! -t 0 ]; then
    cat > "$TMP"
fi

if [ ! -s "$TMP" ]; then
    echo "e2e-affected: no changed paths — nothing to do."
    exit 0
fi

mode="$E2E_SCOPE"
[ "$LIST_ONLY" = "1" ] && [ "$E2E_SCOPE" = "direct" ] && mode="direct-list"
[ "$LIST_ONLY" = "1" ] && [ "$E2E_SCOPE" = "full" ] && mode="names"

map_out="$(python3 - "$MANIFEST" "$mode" "$TMP" <<'PYEOF'
import json, os, sys

manifest = json.load(open(sys.argv[1]))
mode = sys.argv[2] if len(sys.argv) > 2 else "dirs"
suites_by_name = {s["name"]: s for s in manifest.get("suites", [])}
path_map = manifest.get("path_map", {})

paths = [ln.strip() for ln in open(sys.argv[3]) if ln.strip()]
matched = set()
go_changed = 0
dart_changed = 0

# Scope semantics (MEEPT_E2E_SCOPE): full matches any path_map KEY that
# prefixes the changed path (a nested dir inherits its ancestor package's
# suites — the fan-out); direct matches ONLY the key equal to the changed
# file's own package dir, so one staged package selects exactly its own
# suite(s) and nothing else.
for p in paths:
    if p.endswith(".go"):
        go_changed = 1
    if p.endswith(".dart"):
        dart_changed = 1
    if mode in ("direct", "direct-list"):
        pkg = os.path.dirname(p)
        for key_fmt in ("{}/", "{}"):
            names = path_map.get(key_fmt.format(pkg))
            if names:
                matched.update(n for n in names if n in suites_by_name)
    else:
        for prefix, names in path_map.items():
            # Slash-boundary prefix match (same semantics as the census
            # hook): a bare prefix would false-cover siblings — key
            # "internal/agent" must not match "internal/agentfoo".
            if p == prefix or (p.startswith(prefix)
                               and (prefix.endswith("/") or p[len(prefix)] == "/")):
                matched.update(n for n in names if n in suites_by_name)

# Split by runner so a Dart-tier suite (e.g. gui-flows, whose dir is
# ui/flutter_ui/test/e2e and is driven by `flutter test`, not `go test`)
# never lands in the go test package list.
go_dirs, go_names, dart_suites = [], [], []
for name in sorted(matched):
    suite = suites_by_name[name]
    if suite.get("runner") == "dart":
        dart_suites.append(name)
    else:
        go_names.append(name)
        go_dirs.append(suite["dir"])

if mode in ("names", "direct-list"):
    # --list contract: affected SUITE NAMES (both tiers).
    for name in go_names:
        print(name)
    for name in dart_suites:
        print(name)
else:
    for d in go_dirs:
        print(d)
print("---DART---")
for name in dart_suites:
    print(name)
print("---META---")
print("go_changed=%d" % go_changed)
print("dart_changed=%d" % dart_changed)
print("matched=%d" % (1 if matched else 0))
PYEOF
)"

if [ "$LIST_ONLY" = "1" ]; then
    printf '%s\n' "$map_out" | awk '/^---DART---$|^---META---$/{exit} NF{print}'
    exit 0
fi

dirs="$(printf '%s\n' "$map_out" | awk '/^---DART---$|^---META---$/{exit} NF{print}')"
dart_suites="$(printf '%s\n' "$map_out" | awk '/^---DART---$/{f=1;next} /^---META---$/{f=0} f{print}')"
meta="$(printf '%s\n' "$map_out" | awk '/^---META---$/{f=1;next} f')"
go_changed="$(printf '%s\n' "$meta" | sed -n 's/^go_changed=//p')"
dart_changed="$(printf '%s\n' "$meta" | sed -n 's/^dart_changed=//p')"
matched="$(printf '%s\n' "$meta" | sed -n 's/^matched=//p')"

# Suites whose dirs do not exist yet (manifest status: todo) are pending.
run_dirs=""
pending=""
for d in $dirs; do
    if [ -d "$d" ]; then
        run_dirs="$run_dirs $d"
    else
        pending="$pending $d"
    fi
done

if [ -n "$pending" ]; then
    echo "e2e-affected: suites not yet implemented (manifest status: todo), skipped:"
    for d in $pending; do
        echo "  - $d"
    done
fi

if [ -z "$run_dirs" ] && [ -z "$dart_suites" ]; then
    if [ "$go_changed" = "1" ] && [ "$E2E_SCOPE" = "direct" ]; then
        # Direct scope selected nothing: no fan-out, and the smoke fallback
        # IS fan-out, so it stays off by design. Say so instead of silently
        # running (or silently skipping) suites the user did not select.
        echo "e2e-affected: MEEPT_E2E_SCOPE=direct — no suite mapped to the changed package dirs; NOTHING selected (smoke fallback disabled by scope)."
        exit 0
    elif [ "$go_changed" = "1" ]; then
        if [ "$matched" = "1" ]; then
            echo "e2e-affected: all affected suites are pending — falling back to the smoke suite."
        else
            echo "e2e-affected: no path_map hit, but Go files changed — running the smoke suite."
        fi
        run_dirs=" $SMOKE_DIR"
    elif [ "$dart_changed" = "1" ]; then
        # Dart changed but nothing mapped: there is no Go suite to fall
        # back to, and silently passing would be the same decorative gate
        # M12 closed. Say so loudly instead.
        echo "e2e-affected: Dart files changed but no suite matched — NOTHING RAN." >&2
        echo "e2e-affected: add a path_map entry + suite in e2e/manifest.json." >&2
        exit 1
    else
        echo "e2e-affected: no relevant changed paths — nothing to do."
        exit 0
    fi
fi

if [ -n "$run_dirs" ]; then
    echo "e2e-affected: running hermetic Go e2e suites:"
    for d in $run_dirs; do
        echo "  - $d"
    done
    # PASS 1 (serial, as before): run each suite, keeping per-suite output in
    # $WORK so the retry logic can classify the failure. Serial first pass
    # bounds the socket/CPU load the same way the old loop did.
    #
    # PASS 2 (RETRY, gates-speedup item 3): a load-dependent boot flake passes
    # on an isolated re-run (e2e-boot-hardening Contract 2), so the SECOND rc
    # decides — but a full serial re-run cost 2x the suite timeout per flake.
    # Retries now run IN PARALLEL (cap $RETRY_PARALLEL), and a suite whose log
    # shows a BUILD/setup failure (rc=2 class — "build failed", "setup failed")
    # is NOT retried at all: a compile error cannot flake, and re-running it
    # only doubles the wall time of the failure.
    go_rc=0
    retry_dirs=""
    for d in $run_dirs; do
        tag="$(printf '%s' "$d" | tr '/.' '__')"
        log="$WORK/first.$tag"
        # shellcheck disable=SC2086  # GO_CMD is intentionally word-split.
        $GO_CMD -tags e2e -count=1 -p 2 "./$d/..." 2>&1 | tee "$log"
        rc="${PIPESTATUS[0]}"
        if [ "$rc" != "0" ]; then
            if grep -qEi 'build failed|setup failed|cannot find package' "$log"; then
                # Build failure — retrying re-compiles the same error.
                go_rc="$rc"
                echo "SKIPRETRY(build): $d failed to BUILD (rc=$rc) — flake retry skipped (a compile error does not flake)" >&2
            else
                retry_dirs="$retry_dirs $d"
            fi
        fi
    done

    if [ -n "$retry_dirs" ]; then
        echo "RETRY(flake-check): parallel re-run of${retry_dirs} (max ${RETRY_PARALLEL} concurrent)" >&2
        for d in $retry_dirs; do
            tag="$(printf '%s' "$d" | tr '/.' '__')"
            # bash 3.2: no `wait -n` — cap concurrency by polling `jobs -rp`.
            while [ "$(jobs -rp | wc -l | tr -d ' ')" -ge "$RETRY_PARALLEL" ]; do
                sleep 0.2
            done
            (
                # shellcheck disable=SC2086  # GO_CMD is intentionally word-split.
                $GO_CMD -tags e2e -count=1 -p 2 "./$d/..." > "$WORK/retry.$tag" 2>&1
                rc2=$?
                # One file per suite (not a shared append) — concurrent
                # >> from two subshells can interleave partial lines.
                printf '%s\n' "$rc2" > "$WORK/retry.$tag.rc"
            ) &
        done
        wait
        for d in $retry_dirs; do
            tag="$(printf '%s' "$d" | tr '/.' '__')"
            if [ ! -f "$WORK/retry.$tag.rc" ]; then
                # The retry job died before recording its rc — fail closed.
                echo "RETRY(flake-check): $d re-run produced no result (killed?) — gate blocks" >&2
                go_rc=1
                continue
            fi
            rc2="$(cat "$WORK/retry.$tag.rc")"
            if [ "$rc2" = "0" ]; then
                echo "RETRY(flake-check): $d passed on re-run (boot flake — isolation fixed it)" >&2
            else
                echo "RETRY(flake-check): $d failed TWICE (rc=$rc2) — a real regression, gate blocks" >&2
                cat "$WORK/retry.$tag" >&2
                go_rc="$rc2"
            fi
        done
    fi
else
    go_rc=0
fi

# Dart tier: each affected suite carries its own runner command in the
# manifest (runner: dart, command: [...], workdir: ...). Run them in
# sequence and aggregate a failing exit code — a Dart suite must fail the
# gate exactly like a Go one does.
dart_rc=0
if [ -n "$dart_suites" ]; then
    echo ""
    echo "e2e-affected: running Dart e2e suites:"
    for name in $dart_suites; do
        spec="$(python3 - "$MANIFEST" "$name" <<'PYEOF'
import json, shlex, sys
manifest = json.load(open(sys.argv[1]))
suite = next((s for s in manifest.get("suites", []) if s["name"] == sys.argv[2]), None)
if suite is None:
    print("")
    sys.exit(0)
cmd = suite.get("command") or []
print(" ".join([shlex.quote(c) for c in cmd] + [shlex.quote(suite.get("workdir", "."))]))
PYEOF
)"
        [ -z "$spec" ] && continue
        # The last field is the workdir; the rest is the command.
        workdir="${spec##* }"
        cmdline="${spec% *}"
        echo "  - $name ($cmdline in $workdir)"
        if [ ! -d "$workdir" ]; then
            echo "    skipped: workdir $workdir does not exist"
            continue
        fi
        if ! command -v "${cmdline%% *}" >/dev/null 2>&1; then
            echo "    skipped: ${cmdline%% *} not found on PATH (Flutter toolchain absent)"
            continue
        fi
        # shellcheck disable=SC2086  # cmdline is intentionally word-split.
        # git exports GIT_DIR/GIT_WORK_TREE into hook environments; the
        # flutter tool reads them to resolve ITS OWN sdk version and
        # lands on the hooked repo instead, reporting 0.0.0-unknown and
        # failing pub version solving. Scrub them for the Dart runner.
        ( cd "$workdir" && env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE $cmdline ) || dart_rc=1
    done
fi

if [ "$go_rc" != "0" ]; then
    exit "$go_rc"
fi
exit "$dart_rc"
