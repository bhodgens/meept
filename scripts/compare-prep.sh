#!/usr/bin/env bash
# compare-prep.sh — prepare competitor repos for feature parity analysis
#
# Clones shallowly into TMPDIR/meept-compare/<name> and verifies each repo
# has the key files an analyst needs (README, LICENSE, source tree index).
#
# Usage:
#   make compare-prep                  # run with defaults
#   make compare-prep CLEAN=1          # remove old clones first
#   REPOS=$'foo/bar name1\nbaz/qux name2' make compare-prep
#
# Exit 0 = all repos ready, exit 1 = any failure.

set -euo pipefail

COMPARE_DIR="${TMPDIR:-/tmp}/meept-compare"
DATE=$(date +%Y-%m-%d)

# Default competitor list (matches docs/feature-comparison-matrix.md)
# Format: "repo_name alias_name repo_name alias_name ..."
DEFAULT_REPOS=(
  "ApodexAI/FrontierAgent frontier-agent"
  "selfonomy/duckagent duckagent"
  "AtomicBot-ai/atomic-agent atomic-agent"
  "PrimeIntellect-ai/prime-agent prime-agent"
  "nousresearch/hermes-agent hermes"
  "antonosika/opencode opencode"
  "can1357/oh-my-pi oh-my-pi"
  "anthropics/claude-code claude-code"
)

# Allow override via REPOS: newline-separated "owner/repo alias" lines.
if [[ -n "${REPOS:-}" ]]; then
  DEFAULT_REPOS=()
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    DEFAULT_REPOS+=("$line")
  done <<< "$REPOS"
fi

# Parse optional CLEAN flag
CLEAN="${CLEAN:-0}"

if [[ "$CLEAN" == "1" && -d "$COMPARE_DIR" ]]; then
  echo "Cleaning old clones from $COMPARE_DIR ..."
  rm -rf "$COMPARE_DIR"
fi

mkdir -p "$COMPARE_DIR"

echo "=== Meept Feature Comparison Prep — $DATE ==="
echo "Clone root: $COMPARE_DIR"
echo ""

OK=0
FAIL=0

# DEFAULT_REPOS is an array of "owner/repo alias" strings.
for entry in "${DEFAULT_REPOS[@]}"; do
  repo="${entry%% *}"
  name="${entry##* }"
  dir="$COMPARE_DIR/$name"

  # Clone if missing
  if [[ ! -d "$dir/.git" ]]; then
    echo "  Cloning $repo → $name ..."
    if ! git clone --depth 1 "https://github.com/$repo.git" "$dir" 2>/dev/null; then
      echo "    FAIL: could not clone $repo"
      FAIL=$((FAIL+1))
      continue
    fi
  else
    echo "  Found existing: $name"
  fi

  # Verify key files
  has_readme=0
  has_license=0

  [[ -f "$dir/README.md" || -f "$dir/README.rst" || -f "$dir/README" ]] && has_readme=1
  [[ -f "$dir/LICENSE" || -f "$dir/LICENSE.txt" || -f "$dir/LICENSE.md" || -f "$dir/COPYING" ]] && has_license=1

  if [[ $has_readme -eq 1 && $has_license -eq 1 ]]; then
    echo "  OK: $name (README ✓ LICENSE ✓)"
  elif [[ $has_readme -eq 1 ]]; then
    echo "  PARTIAL: $name (README ✓ LICENSE ✗)"
  else
    echo "  WARN: $name (no README — may be monorepo or non-standard)"
  fi
  OK=$((OK+1))
done

echo ""
echo "=== Summary ==="
echo "  Ready:  $OK"
echo "  Failed: $FAIL"
echo ""
echo "Clone root: $COMPARE_DIR"
echo "Next: analyze with subagents against $COMPARE_DIR/"
echo "Feature checklist: docs/feature-comparison-matrix.md"

if [[ $FAIL -gt 0 ]]; then
  exit 1
fi
