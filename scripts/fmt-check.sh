#!/usr/bin/env bash
# fmt-check.sh - gofmt gate for the meept Go tree.
#
# One walker for two Makefile targets:
#   make fmt-check   -> check-only; exits 1 and lists offenders (CI / hooks)
#   make fmt         -> apply-mode; runs gofmt -w on the same offender set
#
# Scope mirrors the golangci-lint v2 `formatters` block in .golangci.yml:
#   - the whole worktree is walked (`gofmt -l .`), so no directory list can
#     drift out of date;
#   - the paths golangci-lint excludes are excluded here too: archive/,
#     third_party/, testdata/, vendor/, .worktrees/;
#   - nested Go modules are skipped, because `gofmt -l .` descends into them
#     and their formatting is owned elsewhere. sdk/go is a separate module
#     (own go.mod) that `go list ./...` never visits; the analyzer fixtures
#     under tools/analyzers/*/testdata/ carry their own go.mod and are already
#     dropped by the testdata rule.
#
# A missing gofmt is a HARD ERROR, never a silent pass: a gate that cannot
# verify the tree must not report the tree clean (same policy as
# fmt-check-gui's missing-dart handling).
#
# Usage: scripts/fmt-check.sh [--write]

set -uo pipefail

# Repo root = parent of this script's directory, resolved through any symlink.
SOURCE="${BASH_SOURCE[0]}"
while [ -L "$SOURCE" ]; do
  DIR="$(cd -P "$(dirname "$SOURCE")" && pwd)"
  SOURCE="$(readlink "$SOURCE")"
  case "$SOURCE" in
    /*) ;;
    *) SOURCE="$DIR/$SOURCE" ;;
  esac
done
ROOT="$(cd -P "$(dirname "$SOURCE")/.." && pwd)"
cd "$ROOT" || { echo "fmt-check: cannot cd to $ROOT" >&2; exit 1; }

GOFMT="${GOFMT:-}"
if [ -z "$GOFMT" ]; then
  GOFMT="$(command -v gofmt 2>/dev/null || true)"
fi
if [ -z "$GOFMT" ] || [ ! -x "$GOFMT" ]; then
  echo "FAIL: gofmt not found (set GOFMT=/path/to/gofmt or install the Go toolchain)." >&2
  echo "      Refusing to report the Go tree as formatted without checking it." >&2
  exit 1
fi

EXCLUDE_RE='(^|/)archive/|(^|/)third_party/|(^|/)testdata/|(^|/)vendor/|(^|/)\.worktrees/'

# Nested modules own their own formatting; skip their trees wholesale.
NESTED_MODULES=()
while IFS= read -r mod; do
  [ -n "$mod" ] && NESTED_MODULES+=("$mod")
done < <(find . -name go.mod -not -path './go.mod' -not -path './*/testdata/*' \
           -not -path './vendor/*' -not -path './.worktrees/*' 2>/dev/null \
         | sed 's|^\./||; s|/go.mod$||' | sort -u)

is_excluded() {
  local rel="$1" mod
  printf '%s\n' "$rel" | grep -Eq "$EXCLUDE_RE" && return 0
  for mod in "${NESTED_MODULES[@]}"; do
    case "$rel" in
      "$mod"/*) return 0 ;;
    esac
  done
  return 1
}

# Collect offenders: every tracked-or-untracked Go file gofmt would rewrite,
# minus exclusions. `-l .` walks the worktree once; paths are normalized to
# repo-relative form.
OFFENDERS=()
while IFS= read -r rel; do
  [ -n "$rel" ] || continue
  is_excluded "$rel" && continue
  OFFENDERS+=("$rel")
done < <("$GOFMT" -l . 2>/dev/null | sed 's|^\./||' | sort -u)

if [ "${#OFFENDERS[@]}" -eq 0 ]; then
  echo "gofmt: clean (all Go files formatted)."
  exit 0
fi

echo "gofmt: ${#OFFENDERS[@]} file(s) are not gofmt-formatted:" >&2
printf '  %s\n' "${OFFENDERS[@]}" >&2

if [ "${1:-}" = "--write" ]; then
  "$GOFMT" -w "${OFFENDERS[@]}"
  echo "" >&2
  echo "gofmt: applied gofmt -w to ${#OFFENDERS[@]} file(s)." >&2
  echo "       Stage the result and re-commit." >&2
  exit 0
fi

echo "" >&2
echo "FAIL: Go tree is not gofmt-clean. Run 'make fmt' to fix, then stage and re-commit." >&2
exit 1