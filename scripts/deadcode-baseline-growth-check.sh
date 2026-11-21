#!/usr/bin/env bash
# deadcode-baseline-growth-check.sh — fail if a deadcode allow-list grew.
#
# The pre-commit deadcode gate diffs unreachable functions against the
# committed baselines. It does not notice an agent who runs --rebaseline
# and commits a larger allow-list. This script closes that hole.
#
# Usage (from any directory inside the git work tree):
#   scripts/deadcode-baseline-growth-check.sh <base-sha>
#
# Growth is a line present in the working-tree file and absent from
# `git show <base>:<file>`. Compared with `comm -13` on LC_ALL=C sorted
# copies, so a reorder of the same lines is not growth. A path missing
# at <base> is a new file: every current line is growth. A working-tree
# baseline path that is missing is an error (exit 2). Do not substitute
# an empty file — that would report zero growth for a deleted allow-list.
# Shrink (lines only in the base) is allowed and prints a one-line
# count. Zero growth exits 0. Any growth exits 1. A base ref that is not
# a commit exits 2 (including 40 zeros).
#
# There is no skip switch and no MEEPT_* override. The only human exception
# is the pull-request label deadcode-baseline-growth, applied by CI before
# this script is invoked. This script does not know about that label.
set -euo pipefail
export LC_ALL=C

if [ "$#" -ne 1 ] || [ -z "${1:-}" ]; then
  echo "usage: scripts/deadcode-baseline-growth-check.sh <base-sha>" >&2
  exit 2
fi
base=$1

repo=$(git rev-parse --show-toplevel 2>/dev/null || true)
if [ -z "$repo" ]; then
  echo "deadcode-baseline-growth-check: not inside a git work tree" >&2
  exit 2
fi
cd "$repo"

if ! git rev-parse --verify --quiet "${base}^{commit}" >/dev/null; then
  echo "deadcode-baseline-growth-check: base ref is not a commit: ${base}" >&2
  exit 2
fi

files=(
  .githooks/deadcode-baseline.txt
  .githooks/deadcode-baseline-notest.txt
)

work=$(mktemp -d "${TMPDIR:-/tmp}/deadcode-baseline-growth.XXXXXX")
trap 'rm -rf "$work"' EXIT

# Print up to 40 added lines. Extra lines are counted, not dumped.
print_capped() {
  cap_file=$1
  cap_total=$2
  n=0
  while IFS= read -r line; do
    n=$((n + 1))
    if [ "$n" -le 40 ]; then
      printf '%s\n' "$line"
    fi
  done < "$cap_file"
  if [ "$cap_total" -gt 40 ]; then
    echo "... and $((cap_total - 40)) more"
  fi
}

grew=0
i=0
for rel in "${files[@]}"; do
  i=$((i + 1))
  base_raw="$work/$i.base"
  cur_raw="$work/$i.cur"
  base_sorted="$work/$i.base.sorted"
  cur_sorted="$work/$i.cur.sorted"
  growth_out="$work/$i.growth"
  shrink_out="$work/$i.shrink"

  # Missing at the base ref: the file is new. Every current line is growth.
  if git cat-file -e "${base}:${rel}" 2>/dev/null; then
    git show "${base}:${rel}" > "$base_raw"
  else
    : > "$base_raw"
  fi

  if [ ! -f "$rel" ]; then
    echo "deadcode-baseline-growth-check: working-tree baseline missing: ${rel}" >&2
    exit 2
  fi
  cp "$rel" "$cur_raw"

  LC_ALL=C sort -o "$base_sorted" "$base_raw"
  LC_ALL=C sort -o "$cur_sorted" "$cur_raw"
  # Growth: lines in the working tree absent from the base ref.
  LC_ALL=C comm -13 "$base_sorted" "$cur_sorted" > "$growth_out"
  # Shrink: lines only in the base. Allowed.
  LC_ALL=C comm -23 "$base_sorted" "$cur_sorted" > "$shrink_out"

  shrink_count=$(wc -l < "$shrink_out" | tr -d '[:space:]')
  echo "${rel}: shrink ${shrink_count}"

  growth_count=$(wc -l < "$growth_out" | tr -d '[:space:]')
  if [ "$growth_count" -gt 0 ]; then
    grew=1
    echo "${rel}: growth ${growth_count}"
    print_capped "$growth_out" "$growth_count"
  fi
done

if [ "$grew" -ne 0 ]; then
  echo "growth is refused. Wire the function or delete it. A human exception is the pull-request label deadcode-baseline-growth only."
  exit 1
fi
exit 0
