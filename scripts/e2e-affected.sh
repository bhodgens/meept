#!/usr/bin/env bash
#
# e2e-affected.sh — stub: run the hermetic Go e2e suites for areas affected
# by the current diff.
#
# Full implementation tracks changed files -> owning e2e/suites/<area>/ dir
# and invokes `make e2e-fast-area AREA=<dir>` per hit. Until wired up, this
# stub runs the smoke suite, which is fast (~20s) and hermetic.

set -u -o pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

echo "e2e-affected: stub — running the smoke suite"
exec go test -tags e2e ./e2e/suites/smoke/... -p 2
