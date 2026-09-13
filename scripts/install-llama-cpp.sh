#!/usr/bin/env bash
#
# install-llama-cpp.sh -- meept-scoped llama.cpp dependency (install + version gate).
#
# meept spawns `llama-server` from PATH. Homebrew ships a build from before
# llama.cpp's LFM2.5 native tool-call parser (June 2026); on such a build
# llama-server logs "Chat format: Generic" and constrains generation with a
# generic JSON grammar whose root also allows a `response` branch -- so a
# tool-forcing prompt can legally answer in prose and the agent narrates
# instead of calling tools.
#
# meept therefore keeps its own copy under its dependency prefix and makes the
# daemon prefer it (internal/daemon/daemonpath.go):
#
#   $MEEPT_DEPS/llama.cpp/bin/llama-server         installed layout (this script)
#   $MEEPT_DEPS/llama.cpp/build/bin/llama-server   in-tree CMake layout
#
# Both directories head the daemon PATH, in that order.
#
# Subcommands:
#   check     resolve llama-server, print its build, FAIL below the floor
#   install   idempotently install llama.cpp >= the floor into the prefix
#   path      print the meept prefix bin dirs (the head of the daemon PATH)
#   version   print the resolved binary's build number (empty if unresolved)
#   help
#
# Environment:
#   MEEPT_HOME                default: $HOME/.meept
#   MEEPT_DEPS                default: $MEEPT_HOME/deps
#   LLAMA_CPP_PREFIX          default: $MEEPT_DEPS/llama.cpp
#   LLAMA_CPP_MIN_BUILD       default: 9660 (upstream release b9660, 2026-06-15)
#   LLAMA_CPP_MIN_DATE        default: 2026-06-15 (release date of the floor)
#   LLAMA_CPP_REF             default: b$LLAMA_CPP_MIN_BUILD
#   LLAMA_CPP_METHOD          auto|prebuilt|source (auto: prebuilt on macOS arm64)
#   LLAMA_CPP_SRC_DIR         default: $MEEPT_DEPS/src/llama.cpp
#   LLAMA_CPP_REPO            default: https://github.com/ggml-org/llama.cpp
#   LLAMA_CPP_REINSTALL=1     force install even when the prefix is current
#   MEEPT_LLAMA_SKIP_CHECK=1  `check` exits 0 without inspecting anything
#   MEEPT_LLAMA_REQUIRE=1     `check` fails when no llama-server is found at all
#   MEEPT_LLAMA_STRICT=1      `check` fails on an untagged dev build whose
#                             source date cannot be verified
#   MEEPT_LLAMA_VERBOSE=1     also print the raw `llama-server --version` output
#
# Version-proof rules (both upstream --version shapes are handled):
#   version: 7730 (d34aa0719)                      legacy: build number only
#   version: 0.4.0-dev (build 1, commit 5f436dd)   current: semver + build N
# A tagged/prebuilt build passes on `build >= LLAMA_CPP_MIN_BUILD`. An untagged
# "-dev" build reports a git-derived number (1 in a --depth 1 clone), so it is
# verified by the commit date of a checkout at the prefix instead.
#
# ASCII only. bash >= 3.2 (macOS system bash).
set -euo pipefail

MEEPT_HOME="${MEEPT_HOME:-$HOME/.meept}"
MEEPT_DEPS="${MEEPT_DEPS:-$MEEPT_HOME/deps}"
LLAMA_CPP_PREFIX="${LLAMA_CPP_PREFIX:-$MEEPT_DEPS/llama.cpp}"
LLAMA_CPP_MIN_BUILD="${LLAMA_CPP_MIN_BUILD:-9660}"
# Release date of the floor build; the fallback proof for untagged dev builds.
LLAMA_CPP_MIN_DATE="${LLAMA_CPP_MIN_DATE:-2026-06-15}"
LLAMA_CPP_REF="${LLAMA_CPP_REF:-b$LLAMA_CPP_MIN_BUILD}"
LLAMA_CPP_SRC_DIR="${LLAMA_CPP_SRC_DIR:-$MEEPT_DEPS/src/llama.cpp}"
LLAMA_CPP_REPO="${LLAMA_CPP_REPO:-https://github.com/ggml-org/llama.cpp}"
LLAMA_CPP_METHOD="${LLAMA_CPP_METHOD:-auto}"
LLAMA_CPP_REINSTALL="${LLAMA_CPP_REINSTALL:-0}"
MEEPT_LLAMA_SKIP_CHECK="${MEEPT_LLAMA_SKIP_CHECK:-0}"
MEEPT_LLAMA_STRICT="${MEEPT_LLAMA_STRICT:-0}"
MEEPT_LLAMA_REQUIRE="${MEEPT_LLAMA_REQUIRE:-0}"
MEEPT_LLAMA_VERBOSE="${MEEPT_LLAMA_VERBOSE:-0}"

_TMP_DIR=""
cleanup() {
    if [ -n "${_TMP_DIR:-}" ]; then
        rm -rf "$_TMP_DIR"
    fi
}
trap cleanup EXIT

log() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# prefix_search_path prints the meept prefix bin dirs in the daemon's
# precedence order (internal/daemon/daemonpath.go meeptPrefixPathDirs):
# bin/ (installed layout), then build/bin/ (in-tree CMake layout).
prefix_search_path() {
    printf '%s:%s\n' "$LLAMA_CPP_PREFIX/bin" "$LLAMA_CPP_PREFIX/build/bin"
}

# resolve_llama_server prints the llama-server the DAEMON would run: the meept
# dependency prefix bin dirs first, then the inherited PATH. Prints nothing
# when no executable is found.
resolve_llama_server() {
    local bin
    bin="$(PATH="$(prefix_search_path):${PATH:-}" command -v llama-server 2>/dev/null || true)"
    if [ -n "$bin" ] && [ -x "$bin" ]; then
        printf '%s\n' "$bin"
    fi
}

# detect_build prints the upstream build number parsed from
# `llama-server --version`, or nothing. Two output shapes exist upstream:
#
#   version: 7730 (d34aa0719)                      legacy (Homebrew b7730, Jan 2026)
#   version: 0.4.0-dev (build 1, commit 5f436dd)   current (semver + build N)
#
# A source build from a --depth 1 clone reports "build 1" because the number is
# derived from git describe/rev-list, so it is not proof by itself (see
# report_check: an untagged dev build is verified by its source date instead).
detect_build() {
    local bin="$1" out n
    out="$("$bin" --version 2>&1 || true)"
    n="$(printf '%s\n' "$out" | sed -n 's/.*(build \([0-9][0-9]*\),.*/\1/p' | head -n 1)"
    if [ -n "$n" ]; then
        printf '%s\n' "$n"
        return 0
    fi
    printf '%s\n' "$out" | sed -n 's/^version: \([0-9][0-9]*\) (.*/\1/p' | head -n 1
}

# detect_version prints the semantic version (e.g. "0.4.0-dev", "0.3.0") or
# nothing. A bare integer ("7730") is a legacy BUILD NUMBER, not a version, so
# the pattern requires major.minor.
detect_version() {
    local bin="$1" out
    out="$("$bin" --version 2>&1 || true)"
    printf '%s\n' "$out" | sed -n 's/^version: \([0-9][0-9]*\.[0-9][^ ]*\) .*/\1/p' | head -n 1
}

version_line() {
    local bin="$1" out
    out="$("$bin" --version 2>&1 || true)"
    printf '%s\n' "$out" | sed -n 's/^version: .*$/&/p' | head -n 1
}

# prefix_source_date prints the HEAD commit date (ISO-8601) of a llama.cpp git
# checkout living at the prefix (the in-tree build layout), or nothing.
prefix_source_date() {
    if [ -d "$LLAMA_CPP_PREFIX/.git" ]; then
        git -C "$LLAMA_CPP_PREFIX" log -1 --format=%cI 2>/dev/null || true
    fi
}

# date_ge A B -- true when ISO-8601 date A is on/after B (lexicographic).
date_ge() { [[ "$1" > "$2" || "$1" == "$2" ]]; }

# num_ge A B -- true when A >= B as integers.
num_ge() { [ "$1" -ge "$2" ] 2>/dev/null; }

in_prefix() {
    case "$1" in
        "$LLAMA_CPP_PREFIX/bin"/*) return 0 ;;
        "$LLAMA_CPP_PREFIX/build/bin"/*) return 0 ;;
        *) return 1 ;;
    esac
}

# report_check inspects the resolved binary and returns:
#   0  at or above the floor (or absent and enforcement not requested)
#   1  below the floor, or absent with MEEPT_LLAMA_REQUIRE=1
report_check() {
    local min="$LLAMA_CPP_MIN_BUILD"
    local bin build semver src_date where source_label

    log "llama.cpp dependency check"
    log "  meept prefix: $LLAMA_CPP_PREFIX"
    log "  search order: $(prefix_search_path)"
    log "  build floor:  b$min"

    bin="$(resolve_llama_server)"
    if [ -z "$bin" ]; then
        log "  resolved:     (none)"
        if [ "$MEEPT_LLAMA_REQUIRE" = "1" ]; then
            printf '\nFAIL: no llama-server found on the daemon PATH.\n' >&2
            printf 'The daemon looks in %s first, then PATH.\n' "$(prefix_search_path)" >&2
            printf 'Fix: make deps-llama   (installs llama.cpp b%s into the prefix)\n' "$min" >&2
            return 1
        fi
        log "  status:       SKIPPED (no llama-server found; not enforced)"
        log "                install one with 'make deps-llama', or set"
        log "                MEEPT_LLAMA_REQUIRE=1 to make this a hard failure."
        return 0
    fi

    build="$(detect_build "$bin")"
    semver="$(detect_version "$bin")"
    where="$bin"
    if in_prefix "$bin"; then
        source_label="meept prefix"
    else
        source_label="external (not the meept prefix)"
    fi
    log "  resolved:     $bin"
    log "  source:       $source_label"
    log "  llama-server --version: $(version_line "$bin")"
    if [ "$MEEPT_LLAMA_VERBOSE" = "1" ]; then
        log "  --- raw --version ---"
        "$bin" --version 2>&1 || true
        log "  ---------------------"
    fi
    log "  detected:     build ${build:-unknown}${semver:+ (version $semver)}"

    if [ -n "$build" ] && num_ge "$build" "$min"; then
        log "  status:       OK (build $build >= b$min)"
        return 0
    fi

    # Untagged/source build: "version: <semver>-dev (build N, commit <sha>)",
    # where N is derived from git and is 1 in a --depth 1 clone -- so the
    # number alone proves nothing. Verify the source instead: a checkout at
    # the prefix whose HEAD is on/after LLAMA_CPP_MIN_DATE is newer than the
    # floor. Anything else is unverifiable.
    case "$semver" in
        *-dev | *-unknown)
            src_date="$(prefix_source_date)"
            if [ -n "$src_date" ] && date_ge "$src_date" "$LLAMA_CPP_MIN_DATE"; then
                log "  source date:  $src_date (>= $LLAMA_CPP_MIN_DATE)"
                log "  status:       OK (untagged source build; verified by commit date)"
                return 0
            fi
            if [ "$MEEPT_LLAMA_STRICT" = "1" ]; then
                printf '\nFAIL: %s is an untagged dev build (version %s, build %s) whose\n' \
                    "$where" "$semver" "${build:-unknown}" >&2
                printf 'source is not verifiable (no checkout at %s newer than %s).\n' \
                    "$LLAMA_CPP_PREFIX" "$LLAMA_CPP_MIN_DATE" >&2
                return 1
            fi
            log "  status:       UNVERIFIED (untagged dev build ${semver}; build number"
            log "                derived from git and unverifiable here) -- passing with"
            log "                a warning. Set MEEPT_LLAMA_STRICT=1 to enforce a floor."
            return 0
            ;;
    esac

    if [ -z "$build" ]; then
        printf '\nFAIL: could not parse a build number from: %s --version\n' "$bin" >&2
        printf 'Cannot prove it is at or above the floor b%s.\n' "$min" >&2
        return 1
    fi

    printf '\nFAIL: llama-server build %s (%s) is below the meept floor b%s.\n' \
        "$build" "$where" "$min" >&2
    cat >&2 <<MSG

Why this floor exists:
  llama.cpp gained its LFM2.5 native tool-call parser in June 2026 (PRs
  #21242, #24071, #24178, #24234, final fix #24667 -- released as b$min on
  2026-06-15). Older builds log "Chat format: Generic" and fall back to a
  generic JSON grammar whose root also allows a "response" branch, so a
  tool-forcing prompt can legally answer in prose instead of calling a tool.
  That is the root cause of the agent narration failures this floor prevents.

Fix:
  make deps-llama          # install llama.cpp b$min+ into the meept prefix
  make deps-llama-check    # re-verify
Inspect with: MEEPT_LLAMA_VERBOSE=1 make deps-llama-check
MSG
    return 1
}

cmd_check() {
    if [ "$MEEPT_LLAMA_SKIP_CHECK" = "1" ]; then
        log "llama.cpp dependency check skipped (MEEPT_LLAMA_SKIP_CHECK=1)"
        return 0
    fi
    report_check
}

cmd_path() { prefix_search_path; }

cmd_version() {
    local bin
    bin="$(resolve_llama_server)"
    if [ -z "$bin" ]; then
        return 0
    fi
    detect_build "$bin"
}

install_prebuilt() {
    local os arch asset url root
    os="$(uname -s)"
    arch="$(uname -m)"
    case "$os/$arch" in
        Darwin/arm64) asset="llama-$LLAMA_CPP_REF-bin-macos-arm64.tar.gz" ;;
        Darwin/x86_64) asset="llama-$LLAMA_CPP_REF-bin-macos-x64.tar.gz" ;;
        Linux/x86_64) asset="llama-$LLAMA_CPP_REF-bin-ubuntu-x64.tar.gz" ;;
        Linux/aarch64) asset="llama-$LLAMA_CPP_REF-bin-ubuntu-arm64.tar.gz" ;;
        *) die "no official prebuilt llama.cpp asset for $os/$arch; use LLAMA_CPP_METHOD=source" ;;
    esac
    url="$LLAMA_CPP_REPO/releases/download/$LLAMA_CPP_REF/$asset"

    _TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/meept-llama.XXXXXX")"
    log "Downloading $url"
    curl -fL --retry 3 --connect-timeout 20 -o "$_TMP_DIR/$asset" "$url" \
        || die "download failed: $url"
    tar -xzf "$_TMP_DIR/$asset" -C "$_TMP_DIR" || die "extract failed: $asset"

    root="$(find "$_TMP_DIR" -maxdepth 3 -type f -path '*/bin/llama-server' -print 2>/dev/null | head -n 1)"
    [ -n "$root" ] || die "no bin/llama-server inside $asset"
    install_tree "$(dirname "$(dirname "$root")")"
}

install_source() {
    local jobs src="$LLAMA_CPP_SRC_DIR"
    command -v cmake >/dev/null 2>&1 || die "cmake not found; use LLAMA_CPP_METHOD=prebuilt"
    command -v git >/dev/null 2>&1 || die "git not found"

    # A llama.cpp checkout already AT the prefix builds in place: the binary
    # lands in <prefix>/build/bin and needs no cmake --install (that layout is
    # the second entry in the daemon's prefix search path).
    if [ -d "$LLAMA_CPP_PREFIX/.git" ]; then
        src="$LLAMA_CPP_PREFIX"
        log "Using the existing llama.cpp checkout at $src (in-tree layout)"
    fi

    if [ -d "$src/.git" ]; then
        log "Fetching $LLAMA_CPP_REF in $src"
        git -C "$src" fetch --depth 1 origin "refs/tags/$LLAMA_CPP_REF" \
            || die "git fetch failed in $src"
        git -C "$src" checkout --detach FETCH_HEAD \
            || die "git checkout FETCH_HEAD failed in $src"
    else
        log "Cloning $LLAMA_CPP_REPO ($LLAMA_CPP_REF) -> $src"
        mkdir -p "$(dirname "$src")"
        git clone --depth 1 --branch "$LLAMA_CPP_REF" "$LLAMA_CPP_REPO" "$src" \
            || die "git clone failed"
    fi

    jobs="$( (sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 4) )"
    log "Building llama.cpp ($jobs jobs)"
    cmake -S "$src" -B "$src/build" \
        -DCMAKE_BUILD_TYPE=Release \
        -DLLAMA_BUILD_SERVER=ON \
        -DLLAMA_CURL=OFF \
        -DGGML_METAL=ON \
        || die "cmake configure failed"
    cmake --build "$src/build" --config Release -j "$jobs" \
        || die "cmake build failed"

    if [ "$src" = "$LLAMA_CPP_PREFIX" ]; then
        log "In-tree build: binary at $src/build/bin/llama-server"
    else
        mkdir -p "$LLAMA_CPP_PREFIX"
        cmake --install "$src/build" --prefix "$LLAMA_CPP_PREFIX" --config Release \
            || die "cmake install failed"
        log "Installed llama.cpp -> $LLAMA_CPP_PREFIX"
    fi
}

# install_tree moves a staged tree (bin/, lib/, include/) into the prefix,
# replacing any previous copy.
install_tree() {
    local src="$1"
    [ -x "$src/bin/llama-server" ] || die "no executable bin/llama-server under $src"
    # Never delete a llama.cpp source checkout that lives at the prefix (the
    # in-tree CMake layout builds it to <prefix>/build/bin). Move it aside or
    # use LLAMA_CPP_METHOD=source instead.
    if [ -d "$LLAMA_CPP_PREFIX/.git" ]; then
        die "$LLAMA_CPP_PREFIX is a llama.cpp git checkout; refusing to replace it.
       Build it in place (LLAMA_CPP_METHOD=source) or move it out of the prefix."
    fi
    mkdir -p "$(dirname "$LLAMA_CPP_PREFIX")"
    rm -rf "$LLAMA_CPP_PREFIX"
    mv "$src" "$LLAMA_CPP_PREFIX"
    log "Installed llama.cpp -> $LLAMA_CPP_PREFIX"
}

cmd_install() {
    local bin method

    bin="$(resolve_llama_server)"
    if [ -n "$bin" ] && [ "$LLAMA_CPP_REINSTALL" != "1" ] && in_prefix "$bin"; then
        # One verdict, shared with `check`: build floor, or a verified dated
        # source checkout for untagged dev builds.
        if report_check >/dev/null 2>&1; then
            log "SKIP: $bin is already at or above the floor b$LLAMA_CPP_MIN_BUILD."
            log "      Re-install anyway with LLAMA_CPP_REINSTALL=1."
            report_check
            return 0
        fi
    fi

    case "$LLAMA_CPP_METHOD" in
        auto)
            if [ "$(uname -s)" = "Darwin" ] && [ "$(uname -m)" = "arm64" ]; then
                method="prebuilt"
            else
                method="source"
            fi
            ;;
        prebuilt | source) method="$LLAMA_CPP_METHOD" ;;
        *) die "LLAMA_CPP_METHOD must be auto, prebuilt or source (got '$LLAMA_CPP_METHOD')" ;;
    esac

    log "Installing llama.cpp $LLAMA_CPP_REF (method: $method) into $LLAMA_CPP_PREFIX"
    if [ "$method" = "prebuilt" ]; then
        install_prebuilt
    else
        install_source
    fi

    log ""
    report_check
}

usage() {
    sed -n '2,52p' "$0" | sed 's/^# \{0,1\}//'
}

case "${1:-help}" in
    check) cmd_check ;;
    install) cmd_install ;;
    path) cmd_path ;;
    version) cmd_version ;;
    help | -h | --help) usage ;;
    *) usage; exit 2 ;;
esac
