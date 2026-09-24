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
# bash-3.2 compatible (macOS /bin/bash); python3 does the JSON parsing
# (repo convention in scripts/).

set -u -o pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

MANIFEST="e2e/manifest.json"
SMOKE_DIR="e2e/suites/smoke"
LIST_ONLY=0
FROM_DIFF=0

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
trap 'rm -f "$TMP"' EXIT

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

mode="dirs"
[ "$LIST_ONLY" = "1" ] && mode="names"

map_out="$(python3 - "$MANIFEST" "$mode" "$TMP" <<'PYEOF'
import json, sys

manifest = json.load(open(sys.argv[1]))
mode = sys.argv[2] if len(sys.argv) > 2 else "dirs"
suites_by_name = {s["name"]: s["dir"] for s in manifest.get("suites", [])}
path_map = manifest.get("path_map", {})

paths = [ln.strip() for ln in open(sys.argv[3]) if ln.strip()]
matched = set()
go_changed = 0
for p in paths:
    if p.endswith(".go"):
        go_changed = 1
    for prefix, names in path_map.items():
        if p == prefix or p.startswith(prefix):
            matched.update(n for n in names if n in suites_by_name)

for name in sorted(matched):
    if mode == "names":
        print(name)
    else:
        print(suites_by_name[name])
print("---META---")
print("go_changed=%d" % go_changed)
print("matched=%d" % (1 if matched else 0))
PYEOF
)"

if [ "$LIST_ONLY" = "1" ]; then
    printf '%s\n' "$map_out" | awk '/^---META---$/{exit} NF{print}'
    exit 0
fi

dirs="$(printf '%s\n' "$map_out" | awk '/^---META---$/{exit} NF{print}')"
meta="$(printf '%s\n' "$map_out" | awk '/^---META---$/{f=1;next} f')"
go_changed="$(printf '%s\n' "$meta" | sed -n 's/^go_changed=//p')"
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

if [ -z "$run_dirs" ]; then
    if [ "$go_changed" = "1" ]; then
        if [ "$matched" = "1" ]; then
            echo "e2e-affected: all affected suites are pending — falling back to the smoke suite."
        else
            echo "e2e-affected: no path_map hit, but Go files changed — running the smoke suite."
        fi
        run_dirs=" $SMOKE_DIR"
    else
        echo "e2e-affected: no relevant changed paths — nothing to do."
        exit 0
    fi
fi

# bash-3.2: no arrays — positional params carry the dir args.
set --
for d in $run_dirs; do
    set -- "$@" "./$d/..."
done

echo "e2e-affected: running hermetic e2e suites:"
for d in $run_dirs; do
    echo "  - $d"
done

exec go test -tags e2e -count=1 -p 2 "$@"
