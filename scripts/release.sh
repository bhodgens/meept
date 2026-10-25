#!/usr/bin/env bash
# scripts/release.sh — release cut helper for meept.
#
# Modes (DRY_RUN defaults to 1 — never publish without opting in):
#   DRY_RUN=1 ./scripts/release.sh              # snapshot: build + package, no publish
#   DRY_RUN=0 ./scripts/release.sh v0.1.2       # real release with --rm-dist
#
# Reproduces the `make build-release` equivalent inline (the Makefile is not
# touched by this script): version stamping via git describe/rev-parse, then
# goreleaser carries internal/version.{Version,Commit,BuildTime}.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

DRY_RUN="${DRY_RUN:-1}"
TAG="${1:-}"

die() { echo "release.sh: $*" >&2; exit 1; }

# --- preconditions ---------------------------------------------------------

git diff --quiet || die "working tree has unstaged changes; commit or stash first"
git diff --cached --quiet || die "working tree has staged changes; commit first"
[ -z "$(git ls-files --others --exclude-standard)" ] || {
    # Untracked files: .goreleaser output (dist/) is fine; anything else is not.
    dirty_untracked="$(git ls-files --others --exclude-standard | grep -v '^dist/' || true)"
    [ -z "$dirty_untracked" ] || die "untracked files present:
$dirty_untracked"
}

command -v goreleaser >/dev/null 2>&1 || die "goreleaser is not installed (brew install goreleaser)"
command -v go >/dev/null 2>&1 || die "go is not installed"

# --- version stamping (mirror Makefile GO_LDFLAGS_VERSION) ------------------

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo "dev")"
COMMIT="$(git rev-parse HEAD 2>/dev/null || echo "unknown")"
BUILD_TIME="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

echo "==> version:  $VERSION"
echo "==> commit:   $COMMIT"
echo "==> build:    $BUILD_TIME"
echo "==> dry run:  $DRY_RUN"

# --- sanity build (make build-release equivalent, Go side only) -------------

export CGO_ENABLED=1
LDFLAGS="-s -w \
-X github.com/caimlas/meept/internal/version.Version=${VERSION} \
-X github.com/caimlas/meept/internal/version.Commit=${COMMIT} \
-X github.com/caimlas/meept/internal/version.BuildTime=${BUILD_TIME}"

echo "==> sanity build (linux native)..."
go build -ldflags "$LDFLAGS" -o /dev/null ./cmd/meept-daemon
go build -ldflags "$LDFLAGS" -o /dev/null ./cmd/meept
go build -ldflags "$LDFLAGS" -o /dev/null ./cmd/meept-lite

# --- release ----------------------------------------------------------------

if [ "$DRY_RUN" = "1" ]; then
    echo "==> goreleaser release --snapshot --clean (no publish)"
    goreleaser release --snapshot --clean
    echo "==> snapshot artifacts in dist/ (not published)"
else
    [ -n "$TAG" ] || die "DRY_RUN=0 requires a tag argument: $0 v0.1.2"
    case "$TAG" in
        v*) ;;
        *) die "tag must look like v0.1.2 (got: $TAG)" ;;
    esac
    git rev-parse "$TAG" >/dev/null 2>&1 || die "tag $TAG does not exist; git tag -a $TAG -m ... first"
    [ "$(git rev-parse "$TAG")" = "$(git rev-parse HEAD)" ] || die "tag $TAG is not HEAD"

    echo "==> goreleaser release --clean --rm-dist equivalent (publish)"
    # --clean wipes dist/ before building; goreleaser publishes the release
    # for the tag when run on the tagged commit in CI. Locally this requires
    # GITHUB_TOKEN and an origin with releases enabled.
    goreleaser release --clean
fi
