#!/usr/bin/env bash
# verify-gui-connect.sh — prove the fresh-install GUI connect path works.
#
# Reproduces the "GUI stuck on connecting... with no daemon-side log lines"
# scenario and verifies the fix end-to-end, from the same install step users
# run (`make install` -> `make gui-connect-setup`):
#
#   1. fresh MEEPT_HOME seeded with a config whose transport.http has NO
#      "enabled" key (the shipped/field shape that produced the bug);
#   2. `make gui-connect-setup` must make it GUI-ready and provision the key;
#   3. the GUI build must embed that endpoint + key (checked via
#      `make -n build-gui`, no Flutter build needed);
#   4. the daemon started with that home must log an HTTP listener and NOT
#      "HTTP transport disabled";
#   5. a live client probe (TLS + cert pin, /api/v1/health, WebSocket upgrade
#      with the GUI key, and auth rejection of a bogus key) must pass.
#
# Every check prints PASS / FAIL / SKIP with a reason. SKIP is used only for
# checks that cannot run in this environment (missing tool, denied network) —
# never to hide a failure. Exit code is 0 only when nothing FAILed.
#
# Usage:
#   scripts/verify-gui-connect.sh [--port N] [--keep] [--repo DIR]

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT_OVERRIDE=""
KEEP=""
while [ $# -gt 0 ]; do
  case "$1" in
    --port) PORT_OVERRIDE="$2"; shift 2 ;;
    --keep) KEEP=1; shift ;;
    --repo) REPO_ROOT="$2"; shift 2 ;;
    -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0

pass() { PASS_COUNT=$((PASS_COUNT + 1)); printf 'PASS  %s — %s\n' "$1" "$2"; }
fail() { FAIL_COUNT=$((FAIL_COUNT + 1)); printf 'FAIL  %s — %s\n' "$1" "$2"; }
skip() { SKIP_COUNT=$((SKIP_COUNT + 1)); printf 'SKIP  %s — %s\n' "$1" "$2"; }

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required tool: $1" >&2
    exit 2
  fi
}
need python3
need go

# ---------------------------------------------------------------------------
# Hermetic home. We set HOME (not MEEPT_HOME) so the run reproduces a default
# `make install` exactly: MEEPT_HOME is unset in real installs, so make's
# `MEEPT_HOME ?= $(HOME)/.meept`, the daemon/CLI dev-key resolution
# ($HOME/.meept/dev_key, pkg/constants/api_key.go) and the GUI's runtime
# key/cert reads all resolve to the same directory.
#
# NOTE: when MEEPT_HOME *is* set to a non-default value, pkg/constants/api_key.go
# still reads $HOME/.meept/dev_key while everything else honours MEEPT_HOME —
# a latent mismatch reported in the RCA (fix belongs in pkg/constants, not here).
# ---------------------------------------------------------------------------
REAL_HOME="$HOME"
REAL_GOPATH="$(go env GOPATH)"
REAL_GOMODCACHE="$(go env GOMODCACHE)"
REAL_GOCACHE="$(go env GOCACHE)"

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/meept-verify-gui.XXXXXX")"
export HOME="$WORKDIR/home"
unset MEEPT_HOME
# Keep the Go toolchain caches pointing at the real ones: a fresh HOME would
# otherwise produce an empty module cache and try to re-download dependencies.
export GOPATH="$REAL_GOPATH" GOMODCACHE="$REAL_GOMODCACHE" GOCACHE="$REAL_GOCACHE"

MEEPT_HOME="$HOME/.meept"
mkdir -p "$MEEPT_HOME"

# A free loopback port, unless overridden.
if [ -n "$PORT_OVERRIDE" ]; then
  PORT="$PORT_OVERRIDE"
else
  PORT="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
fi

echo "== verify-gui-connect =="
echo "   repo:      $REPO_ROOT"
echo "   HOME:      $HOME   (MEEPT_HOME unset: default $MEEPT_HOME)"
echo "   port:      $PORT"
echo

# ---------------------------------------------------------------------------
# 0. Seed the failing shape: transport.http exists but "enabled" is absent.
# ---------------------------------------------------------------------------
cat >"$MEEPT_HOME/meept.json5" <<JSON5
{
  // Field/legacy shape: transport.http with an addr but NO "enabled" key.
  // internal/config defaults Enabled to false, so the daemon binds no HTTP
  // listener at all — the GUI retries forever and the daemon logs nothing.
  "daemon": { "log_level": "INFO", "data_dir": "$MEEPT_HOME" },
  "transport": {
    "rpc": { "enabled": true, "socket_path": "$MEEPT_HOME/meept.sock" },
    "http": { "addr": "127.0.0.1:$PORT" }
  }
}
JSON5
chmod 600 "$MEEPT_HOME/meept.json5"

# ---------------------------------------------------------------------------
# 1. Run the install step (`make install` calls this first).
# ---------------------------------------------------------------------------
echo "-- step 1: make gui-connect-setup (the install step) --"
if (cd "$REPO_ROOT" && make -s gui-connect-setup MEEPT_HOME="$MEEPT_HOME") >"$WORKDIR/install.log" 2>&1; then
  pass "make gui-connect-setup" "exit 0"
else
  fail "make gui-connect-setup" "non-zero exit — see $WORKDIR/install.log"
fi
sed 's/^/      | /' "$WORKDIR/install.log" 2>/dev/null || true
echo

# ---------------------------------------------------------------------------
# 2. Static self-check of the installed config + key.
# ---------------------------------------------------------------------------
echo "-- step 2: static connect-path check --"
if (cd "$REPO_ROOT" && MEEPT_HOME="$MEEPT_HOME" python3 scripts/gui-daemon-connect.py check) >"$WORKDIR/check.log" 2>&1; then
  pass "gui-daemon-connect.py check" "all checks passed"
else
  fail "gui-daemon-connect.py check" "at least one FAIL — see below"
fi
sed 's/^/      | /' "$WORKDIR/check.log"
echo

# ---------------------------------------------------------------------------
# 3. The GUI build must embed this endpoint and this key.
# ---------------------------------------------------------------------------
echo "-- step 3: GUI build-time defines (make -n build-gui) --"
DEFINES="$(cd "$REPO_ROOT" && make -n build-gui MEEPT_HOME="$MEEPT_HOME" 2>/dev/null | grep -o 'dart-define=[^ ]*' | sort -u)"
EP_HOST="$(cd "$REPO_ROOT" && MEEPT_HOME="$MEEPT_HOME" python3 scripts/gui-daemon-connect.py endpoint --field host)"
EP_PORT="$(cd "$REPO_ROOT" && MEEPT_HOME="$MEEPT_HOME" python3 scripts/gui-daemon-connect.py endpoint --field port)"
EP_WS="$(cd "$REPO_ROOT" && MEEPT_HOME="$MEEPT_HOME" python3 scripts/gui-daemon-connect.py endpoint --field ws_path)"
KEY_ON_DISK="$(python3 -c "import sys;print(open(sys.argv[1]).read().strip())" "$MEEPT_HOME/dev_key" 2>/dev/null)"
KEY_IN_BUILD="$(printf '%s\n' "$DEFINES" | grep -o 'MEEPT_DEV_API_KEY=[^ ]*' | head -1 | sed 's/^MEEPT_DEV_API_KEY=//')"
# Path the GUI reads at runtime (ui/flutter_ui/lib/services/storage_service.dart
# -> $HOME/.meept/dev_key) and the path the daemon accepts (pkg/constants).
GUI_RUNTIME_KEY_FILE="$HOME/.meept/dev_key"

check_define() { # name expected
  local name="$1" expected="$2" got
  got="$(printf '%s\n' "$DEFINES" | grep -o "dart-define=${name}=[^ ]*" | head -1 | sed 's/^dart-define=//' | cut -d= -f2-)"
  if [ -z "$got" ]; then
    fail "GUI define ${name}" "not present in the flutter build command"
  elif [ "$got" != "$expected" ]; then
    fail "GUI define ${name}" "build has '${got}', expected '${expected}'"
  else
    pass "GUI define ${name}" "$got"
  fi
}

check_define MEEPT_API_HOST "$EP_HOST"
check_define MEEPT_API_PORT "$EP_PORT"
check_define MEEPT_WS_PATH "$EP_WS"

if [ -z "$KEY_IN_BUILD" ]; then
  fail "GUI embeds the daemon key" "no MEEPT_DEV_API_KEY in the flutter build command"
elif [ -z "$KEY_ON_DISK" ]; then
  fail "GUI embeds the daemon key" "no $MEEPT_HOME/dev_key to compare against"
elif [ "$KEY_IN_BUILD" = "$KEY_ON_DISK" ]; then
  pass "GUI embeds the daemon key" "matches $MEEPT_HOME/dev_key (${#KEY_IN_BUILD} chars)"
elif [ "$KEY_IN_BUILD" = "meept_dev_default_key_CHANGE_ME" ]; then
  fail "GUI embeds the daemon key" "build still uses the banned public legacy default"
else
  fail "GUI embeds the daemon key" "build key differs from the dev_key file"
fi

# The GUI also tries to read $HOME/.meept/dev_key at runtime
# (lib/services/storage_service.dart _tryReadDevKeyFile) — same file the daemon
# authenticates against.
if [ -f "$GUI_RUNTIME_KEY_FILE" ]; then
  RUNTIME_KEY="$(python3 -c "import sys;print(open(sys.argv[1]).read().strip())" "$GUI_RUNTIME_KEY_FILE")"
  if [ "$RUNTIME_KEY" = "$KEY_ON_DISK" ]; then
    pass "GUI runtime key file matches" "$GUI_RUNTIME_KEY_FILE"
  else
    fail "GUI runtime key file matches" "$GUI_RUNTIME_KEY_FILE differs from MEEPT_HOME/dev_key"
  fi
else
  fail "GUI runtime key file matches" "$GUI_RUNTIME_KEY_FILE missing"
fi
echo

# ---------------------------------------------------------------------------
# 4. Start the daemon with the installed home and inspect its log.
# ---------------------------------------------------------------------------
echo "-- step 4: start the daemon with the installed config --"
DAEMON_BIN="$WORKDIR/meept-daemon"
if (cd "$REPO_ROOT" && go build -o "$DAEMON_BIN" ./cmd/meept-daemon) >"$WORKDIR/build.log" 2>&1; then
  pass "build meept-daemon" "go build ./cmd/meept-daemon"
else
  fail "build meept-daemon" "see $WORKDIR/build.log"
fi

DAEMON_LOG="$WORKDIR/daemon.log"
if [ ! -x "$DAEMON_BIN" ]; then
  skip "daemon start" "daemon binary not built"
else
  # exec so $! is the daemon itself (a plain subshell would leak the daemon).
  (cd "$WORKDIR" && exec "$DAEMON_BIN" -f -c "$MEEPT_HOME/meept.json5" >"$DAEMON_LOG" 2>&1) &
  DAEMON_PID=$!
  READY=""
  for _ in $(seq 1 60); do
    if grep -qE 'u?nic?ed HTTP server starting|HTTP server created|HTTP transport disabled|FATAL|failed to create daemon' "$DAEMON_LOG" 2>/dev/null; then
      READY=1
      break
    fi
    if ! kill -0 "$DAEMON_PID" 2>/dev/null; then
      READY=1
      break
    fi
    sleep 1
  done

  if grep -q "HTTP transport disabled" "$DAEMON_LOG"; then
    fail "daemon HTTP transport" "log says 'HTTP transport disabled' — GUI cannot connect"
  elif grep -qE "HTTP server created|unified HTTP server starting" "$DAEMON_LOG"; then
    pass "daemon HTTP transport" "$(grep -m1 -oE 'addr=[^ ]+' "$DAEMON_LOG" || echo 'HTTP server created')"
  else
    fail "daemon HTTP transport" "no HTTP listener line within 60 s — see $DAEMON_LOG"
  fi

  # -------------------------------------------------------------------------
  # 5. Live client probe (TLS pin + health + WebSocket handshake).
  # -------------------------------------------------------------------------
  echo "-- step 5: live probe of the GUI connect path --"
  if [ -n "$READY" ] && grep -qE "HTTP server created|unified HTTP server starting" "$DAEMON_LOG"; then
    PROBE_LOG="$WORKDIR/probe.log"
    if (cd "$REPO_ROOT" && MEEPT_HOME="$MEEPT_HOME" python3 scripts/gui-daemon-connect.py probe \
          --host "$EP_HOST" --port "$EP_PORT" --ws-path "$EP_WS" \
          --key-file "$MEEPT_HOME/dev_key") >"$PROBE_LOG" 2>&1; then
      pass "live client probe" "tls + health + ws handshake + auth enforcement"
    else
      fail "live client probe" "see $PROBE_LOG"
    fi
    sed 's/^/      | /' "$PROBE_LOG"
  else
    skip "live client probe" "daemon did not expose an HTTP listener"
  fi

  kill "$DAEMON_PID" 2>/dev/null || true
  wait "$DAEMON_PID" 2>/dev/null || true

  echo
  echo "-- daemon log: transport-relevant lines --"
  grep -nE "HTTP transport|HTTP server created|unified HTTP server|WebSocket endpoint|Authentication required|unified|TLS always enabled" \
    "$DAEMON_LOG" | sed 's/^/      | /' || echo "      | (none)"
fi

echo
echo "== summary: ${PASS_COUNT} passed, ${FAIL_COUNT} failed, ${SKIP_COUNT} skipped =="
echo "   artifacts: $WORKDIR"
[ "$FAIL_COUNT" -eq 0 ]
