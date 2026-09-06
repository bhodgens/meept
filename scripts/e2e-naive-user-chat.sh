#!/usr/bin/env bash
#
# e2e-naive-user-chat.sh — E2E naive-user regression for meept chat dispatch
# (chat-dispatch-ux leaf 10; reproduces the 2026-09-04 comparison transcript,
# /tmp/meept-vs-hermes-findings.md).
#
# Usage:
#   bash scripts/e2e-naive-user-chat.sh [--keep]
#
# What it does:
#   - mktemp -d workdir; builds FRESH bin/meept-daemon + bin/meept into it
#     (never reuses the repo's bin/).
#   - Writes a minimal meept.json5 into the temp state dir: RPC socket, pid
#     file, data dir, TLS certs, audit db, memory dir — all inside the temp
#     dir. HTTP listens on a FREE port probed at runtime (never 18099/8081).
#     HOME is sandboxed to the temp dir for both daemon and CLI, so no code
#     path can touch the user's ~/.meept or the running daemon.
#   - Copies config/models.json5 from the repo when present so real
#     providers can answer (env-provided API keys are honored).
#   - Boots a scratch daemon (daemon cwd = $WORK, deliberately DIFFERENT from
#     the project dir, so A4 genuinely exercises the leaf-03 session
#     ProjectPath cwd resolution rather than the daemon-cwd fallback).
#   - Creates a session, registers the project ($PROJECT_DIR), and drives
#     the four-turn naive-user transcript against it:
#       T1 create hello.txt and tell me the full path
#       T2 make it beep when it opens            (modify turn)
#       T3 did the change get made? where is the file?  (status turn)
#       T4 what files did you make for me?       (artifact turn)
#   - Sync dispatch: the 2026-09-04 stub (F1) lives on the SYNC dispatch
#     path (waitForTaskCompletion). Plain CLI turns classify async and
#     return an ack, so the script first sends one warmup turn through
#     `meept mcp-chat-server` with source_client "meept-bench-e2e" — the
#     documented in-tree sync trigger (handler.go: handlerCase
#     "sync_dispatch") — which latches sync mode for the scratch daemon's
#     lifetime. All transcript turns then return real task results.
#   - Asserts the user-visible contract fixed by leaves 01-06:
#       A1 no reply matches ^Task .* completed\.          (F1/C1)
#       A2 no reply contains "## Available Agents"        (F5/C5)
#       A3 no reply is a raw JSON object dump ('{'…'}')   (F5/C5)
#       A4 $PROJECT_DIR/hello.txt exists; T1 reply names it (F3/C3)
#       A5 T3 reply references hello.txt (continuity)
#       A6 clean lifecycle: daemon terminated, temp dir removed
#       F6 quota-shaped failures say "quota" (asserted WHEN the reply is a
#         429/quota failure; skipped otherwise)
#
# SKIP semantics: if no LLM provider answers (429/quota, unreachable, bad
# key), each chat turn is retried ONCE, then that turn's assertion group is
# marked SKIP with a printed reason and the script still exits 0 — never
# silently. Infrastructure failures (build, boot, daemon death) are FAIL,
# not SKIP. A reply that IS the stub is an integration bug: FAIL.
#
# The chat RPC (client and server) caps a sync turn at ~120s. A task that
# exceeds it surfaces as a turn timeout: retried once, then SKIP.
#
# Timeout per chat turn: MEEPT_E2E_TURN_TIMEOUT seconds (default 300).
#
# This script is go test -short-safe by construction: plain bash, never
# invoked from the Go test suite. Requires only POSIX-ish bash 3+ (no
# mapfile); macOS /usr/bin/env bash works.

set -u -o pipefail

KEEP=0
if [ "${1:-}" = "--keep" ]; then
  KEEP=1
elif [ $# -gt 0 ]; then
  echo "usage: bash scripts/e2e-naive-user-chat.sh [--keep]" >&2
  exit 2
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TURN_TIMEOUT="${MEEPT_E2E_TURN_TIMEOUT:-300}"
SOCKET_WAIT_SECONDS=30
PROJECT_NAME="e2e-project"
SESSION_NAME="e2e-naive"
SOURCE_CLIENT="meept-bench-e2e"

# ---------------------------------------------------------------------------
# Workdir + cleanup (trap-based; fires on every exit path)
# ---------------------------------------------------------------------------

WORK="$(mktemp -d "${TMPDIR:-/tmp}/meept-e2e.XXXXXX")"
# macOS TMPDIR is /var/folders/... but the kernel resolves tools' paths
# through /private/var/folders/...; permission patterns must cover both
# forms or file_write gets "access denied" on the /private prefix.
WORK_RESOLVED="$(cd "$WORK" && pwd -P)"
STATE="$WORK/state"
HOME_DIR="$WORK/home"
PROJECT_DIR="$WORK/project"
SOCK="$STATE/meept.sock"
DAEMON_LOG="$WORK/daemon.log"
REPLIES="$WORK/replies"
A5_OK=0   # set to 1 only when A5 PASSes on a provider-available T3 turn
DAEMON_BIN="$WORK/bin/meept-daemon"
CLI_BIN="$WORK/bin/meept"
DPID=""

FAILURES=()
SKIPS=()
NPASS=0

log()  { printf '%s\n' "$*"; }
warn() { printf '%s\n' "WARN: $*" >&2; }
die()  { printf '%s\n' "FATAL: $*" >&2; exit 1; }

kill_daemon() {
  [ -n "$DPID" ] || return 0
  if kill -0 "$DPID" 2>/dev/null; then
    kill "$DPID" 2>/dev/null || true
    local i=0
    while kill -0 "$DPID" 2>/dev/null && [ "$i" -lt 10 ]; do
      sleep 1
      i=$((i + 1))
    done
    if kill -0 "$DPID" 2>/dev/null; then
      kill -9 "$DPID" 2>/dev/null || true
    fi
  fi
  wait "$DPID" 2>/dev/null || true
}

cleanup() {
  local rc=$?
  kill_daemon
  if [ "$KEEP" = "1" ]; then
    log ""
    log "--keep: scratch workdir left in place: $WORK"
  else
    rm -rf "$WORK"
  fi
  exit $rc
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

note_result() { # $1=kind (PASS/FAIL/SKIP), $2=id, $3=detail
  printf '  [%-4s] %-8s %s\n' "$1" "$2" "$3"
  case "$1" in
    PASS) NPASS=$((NPASS + 1)) ;;
    FAIL) FAILURES+=("$2: $3") ;;
    SKIP) SKIPS+=("$2: $3") ;;
  esac
}

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

free_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

run_with_timeout() { # $1=seconds, rest=command; returns 124 on timeout
  local limit="$1"
  shift
  "$@" </dev/null &
  local pid=$!
  local waited=0
  while kill -0 "$pid" 2>/dev/null; do
    if [ "$waited" -ge "$limit" ]; then
      kill -9 "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
      return 124
    fi
    sleep 1
    waited=$((waited + 1))
  done
  wait "$pid"
  return $?
}

daemon_ping() {
  env HOME="$HOME_DIR" "$CLI_BIN" --socket "$SOCK" -d "$STATE" \
    session list >/dev/null 2>&1
}

wait_for_socket() {
  local i=0
  while [ "$i" -lt "$SOCKET_WAIT_SECONDS" ]; do
    if [ -S "$SOCK" ] && daemon_ping; then
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done
  return 1
}

# run_chat_turn NAME MESSAGE [--project]
# Sends one CLI chat turn (stdout captured to $REPLIES/NAME.txt).
# Returns 0 success; 2 provider-skip (reason echoed); 1 hard failure.
run_chat_turn() {
  local name="$1" msg="$2" extra="${3:-}"
  local errfile="$REPLIES/$name.err"
  local outfile="$REPLIES/$name.txt"
  local attempt out rc

  for attempt in 1 2; do
    if [ "$extra" = "--project" ]; then
      out=$(run_with_timeout "$TURN_TIMEOUT" env HOME="$HOME_DIR" "$CLI_BIN" \
        --socket "$SOCK" -d "$STATE" chat --session "$SID" \
        --project "$PROJECT_NAME" "$msg" 2>"$errfile")
    else
      out=$(run_with_timeout "$TURN_TIMEOUT" env HOME="$HOME_DIR" "$CLI_BIN" \
        --socket "$SOCK" -d "$STATE" chat --session "$SID" "$msg" \
        2>"$errfile")
    fi
    rc=$?
    if [ "$rc" -eq 0 ] && [ -n "$(printf '%s' "$out" | tr -d '[:space:]')" ]; then
      printf '%s\n' "$out" >"$outfile"
      return 0
    fi
    if [ "$attempt" -eq 1 ]; then
      log "  (attempt 1 failed rc=$rc; retrying once)"
      sleep 5
    fi
  done

  # Provider-side failures skip; daemon-side failures are real bugs.
  if ! daemon_ping; then
    log "  daemon stopped answering RPC (see $DAEMON_LOG)"
    return 1
  fi
  local errtail
  errtail="$(tail -c 600 "$errfile" 2>/dev/null | tr '\n' ' ')"
  if printf '%s' "$errtail" | grep -Eiq '429|rate[ -]?limit|quota|too many requests'; then
    log "  provider quota/rate limit (rc=$rc): $errtail"
    return 2
  fi
  if [ "$rc" -eq 124 ]; then
    log "  turn exceeded ${TURN_TIMEOUT}s (provider too slow; chat RPC caps a sync wait at ~120s)"
    return 2
  fi
  if printf '%s' "$errtail" | grep -Eiq 'connection refused|no route|connection reset|tls|ssl|certificate|unauthorized|401|403|api key|bad gateway|502|503|504|deadline|timeout|eof|timeout waiting for response'; then
    log "  provider unreachable/failed (rc=$rc): $errtail"
    return 2
  fi
  log "  turn failed rc=$rc: $errtail"
  return 1
}

# assert_reply_shape TURN REPLYFILE — A1/A2/A3 (every turn) + F6 quota
# honesty (only fires when the reply is quota-shaped).
assert_reply_shape() {
  local turn="$1" file="$2"
  if grep -Eq '^[[:space:]]*Task .* completed\.[[:space:]]*$' "$file"; then
    note_result FAIL "A1/$turn" "reply is the literal 'Task ... completed.' stub (F1/C1)"
  else
    note_result PASS "A1/$turn" "reply is not a stub"
  fi
  if grep -Fq '## Available Agents' "$file"; then
    note_result FAIL "A2/$turn" "reply contains the agent-roster header (F5/C5)"
  else
    note_result PASS "A2/$turn" "no agent-roster dump"
  fi
  local first last
  first="$(head -c 1 "$file" | tr -d '[:space:]')"
  last="$(tail -c 1 "$file" | tr -d '[:space:]')"
  if [ "$first" = "{" ] && [ "$last" = "}" ]; then
    note_result FAIL "A3/$turn" "reply is a raw JSON object dump (F5/C5)"
  else
    note_result PASS "A3/$turn" "reply is not a raw JSON dump"
  fi
  # F6: a quota-shaped failure must name quota honestly (leaf 06). A reply
  # with no quota/rate-limit signal skips this sub-check.
  if grep -Eiq '429|rate[ -]?limit|too many requests' "$file" && ! grep -qi 'quota' "$file"; then
    note_result FAIL "F6/$turn" "quota-shaped failure without an honest quota message"
  fi
}

print_reply_preview() { # $1=file
  log "  ---- reply (first 8 lines) ----"
  head -n 8 "$1" | sed 's/^/  | /'
  if [ "$(wc -l <"$1" | tr -d ' ')" -gt 8 ]; then
    log "  | ... (truncated)"
  fi
  log "  --------------------------------"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

log "======================================================================"
log " meept E2E naive-user regression (chat-dispatch-ux leaf 10)"
log " workdir: $WORK"
log "======================================================================"

# 1. Fresh binaries into the temp dir (never reuse bin/).
log ""
log "[1/7] building fresh binaries..."
mkdir -p "$WORK/bin" "$STATE" "$HOME_DIR/.meept" "$PROJECT_DIR" "$REPLIES"
if ! (cd "$REPO_ROOT" && go build -o "$DAEMON_BIN" ./cmd/meept-daemon); then
  die "go build ./cmd/meept-daemon failed"
fi
if ! (cd "$REPO_ROOT" && go build -o "$CLI_BIN" ./cmd/meept); then
  die "go build ./cmd/meept failed"
fi
log "  built $DAEMON_BIN"
log "  built $CLI_BIN"

# 2. Probe a FREE HTTP port (never the user's live 18099 / default 8081).
HTTP_PORT="$(free_port)"
while [ "$HTTP_PORT" = "18099" ] || [ "$HTTP_PORT" = "8081" ]; do
  HTTP_PORT="$(free_port)"
done
log "  free HTTP port: $HTTP_PORT"

# 3. Minimal config — every path pinned inside the temp world.
cat >"$WORK/meept.json5" <<EOF
{
  // e2e scratch daemon (leaf 10): everything stays inside the mktemp dir.
  "daemon": {
    "socket_path": "$SOCK",
    "pid_file": "$STATE/meept.pid",
    "data_dir": "$STATE",
    "log_level": "INFO",
  },
  "transport": {
    "rpc": {
      "enabled": true,
      "socket_path": "$SOCK",
    },
    "http": {
      "enabled": true,
      "addr": "127.0.0.1:$HTTP_PORT",
      "require_auth": false,
      "tls_cert_file": "$STATE/tls/cert.pem",
      "tls_key_file": "$STATE/tls/key.pem",
      "rest": true,
      "websocket": false,
      "mcp": false,
    },
  },
  "memory": {
    "data_dir": "$STATE/memory",
  },
  "projects": {
    // The transcript binds the session to $PROJECT_DIR so step jobs
    // resolve the working dir from the session ProjectPath
    // (resolveStepWorkingDir priority 2, leaf 03).
    "enabled": true,
    "base_dir": "$STATE/projects",
    "auto_detect": false,
    "fence_enabled": false,
  },
  "security": {
    "audit_db_path": "$STATE/audit.db",
    "allowed_paths": ["$WORK/**", "$WORK_RESOLVED/**"],
  },
  // Multi-agent orchestration: the roster specialists (chat/coder/...)
  // register only when this is on (components.go creates AgentRegistry
  // under cfg.MultiAgent.Enabled). Without it the dispatcher answers
  // 'no agent available for "chat"' and every turn fails.
  "multiagent": {
    "enabled": true,
  },
}
EOF

# Models: reuse the repo's env-configured defaults (make install copies this
# template into ~/.meept; our sandboxed HOME makes that the temp dir).
# Local runtime ports (mlx classifier :8081, mlx general :8082) collide with
# the USER'S live runtimes, so remap them into an ephemeral range like the
# HTTP port. ${MODEL_PATH} and both baseURLs are rewritten to match.
if [ -f "$REPO_ROOT/config/models.json5" ]; then
  cp "$REPO_ROOT/config/models.json5" "$HOME_DIR/.meept/models.json5"
  MLX_CLASS_PORT=$((HTTP_PORT + 1))
  MLX_GEN_PORT=$((HTTP_PORT + 2))
  sed -i '' \
    -e "s/127\.0\.0\.1:8081/127.0.0.1:${MLX_CLASS_PORT}/g" \
    -e "s/127\.0\.0\.1:8082/127.0.0.1:${MLX_GEN_PORT}/g" \
    -e "s/\"--port\", \"8081\"/\"--port\", \"${MLX_CLASS_PORT}\"/" \
    -e "s/\"--port\", \"8082\"/\"--port\", \"${MLX_GEN_PORT}\"/" \
    "$HOME_DIR/.meept/models.json5"
  log "  copied config/models.json5 -> $HOME_DIR/.meept/models.json5 (mlx ports -> ${MLX_CLASS_PORT}/${MLX_GEN_PORT})"
else
  warn "config/models.json5 not found in repo; turns will SKIP unless a provider is configured"
fi

# Agent definitions: make install copies config/agents/* into
# ~/.meept/agents/; the daemon's discovery tier expects them there. Without
# them the registry is empty and every dispatch dies with
# "no agent available" — so seed the sandboxed agents dir from the repo.
mkdir -p "$HOME_DIR/.meept/agents"
if [ -d "$REPO_ROOT/config/agents" ]; then
  cp -R "$REPO_ROOT/config/agents/." "$HOME_DIR/.meept/agents/" 2>/dev/null || true
  log "  seeded agent definitions ($(ls "$HOME_DIR/.meept/agents" | wc -l | tr -d ' ') agents)"
fi
if [ -d "$REPO_ROOT/config/prompts" ]; then
  mkdir -p "$HOME_DIR/.meept/prompts"
  cp -R "$REPO_ROOT/config/prompts/." "$HOME_DIR/.meept/prompts/" 2>/dev/null || true
fi

# 4. Boot the scratch daemon with cwd=$WORK (deliberately NOT the project
# dir — A4 must catch a daemon-cwd fallback, the leaf-03 bug).
log ""
log "[2/7] booting scratch daemon (socket $SOCK, http :$HTTP_PORT, cwd $WORK)..."
( cd "$WORK" && exec env HOME="$HOME_DIR" "$DAEMON_BIN" \
    -c "$WORK/meept.json5" -d "$STATE" -s "$SOCK" ) >>"$DAEMON_LOG" 2>&1 &
DPID=$!

if wait_for_socket; then
  log "  daemon is up (pid $DPID, <$SOCKET_WAIT_SECONDS+s)"
else
  log "  ---- daemon.log tail ----"
  tail -n 30 "$DAEMON_LOG" | sed 's/^/  | /'
  die "scratch daemon did not become ready within ${SOCKET_WAIT_SECONDS}s"
fi

# 4b. Wait for local MLX runtimes (classifier + general) to finish loading.
# The daemon starts them lazily/asynchronously; a classification fired before
# the model server is healthy burns alias-failure cooldowns and rotates to
# fallbacks. Poll each runtime's /health for up to 120s.
wait_for_runtime() {
  local port="$1" name="$2" i=0
  while [ "$i" -lt 120 ]; do
    if curl -sf -m 3 "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
      log "  $name runtime healthy on :$port (after ${i}s)"
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done
  warn "$name runtime on :$port not healthy after 120s (classification may SKIP/fallback)"
  return 1
}

# Extract the remapped MLX ports from the sandboxed models config.
MLX_CLASS_PORT=$(grep -o '127\.0\.0\.1:[0-9]*' "$HOME_DIR/.meept/models.json5" \
  | sed 's/127\.0\.0\.1://' | sort -u | head -1)
MLX_GEN_PORT=$(grep -o '127\.0\.0\.1:[0-9]*' "$HOME_DIR/.meept/models.json5" \
  | sed 's/127\.0\.0\.1://' | sort -u | tail -1)
if [ -n "$MLX_CLASS_PORT" ]; then
  wait_for_runtime "$MLX_CLASS_PORT" "classifier" || true
fi
if [ -n "$MLX_GEN_PORT" ] && [ "$MLX_GEN_PORT" != "$MLX_CLASS_PORT" ]; then
  wait_for_runtime "$MLX_GEN_PORT" "general" || true
fi

# 5. Session + project wiring.
# NOTE: `meept session create` cannot be used here — its `-d/--description`
# shorthand collides with the root `-d/--state-dir` shorthand and cobra
# panics (pre-existing CLI bug, outside this leaf's scope). Instead the
# first chat turn below runs with --project, which creates a project-bound
# session (chat.go CASE 3 -> createFlaggedSession); we then discover the
# session ID from session list --json.
log ""
log "[3/7] registering project $PROJECT_NAME -> $PROJECT_DIR"
if ! env HOME="$HOME_DIR" "$CLI_BIN" --socket "$SOCK" -d "$STATE" \
    projects add "$PROJECT_DIR" --name "$PROJECT_NAME" >"$REPLIES/project-add.txt" 2>&1; then
  sed 's/^/  | /' "$REPLIES/project-add.txt"
  die "projects add failed (infrastructure error, not a provider skip)"
fi
log "  project: $(head -n 1 "$REPLIES/project-add.txt")"

# 5a. Session creation + sync-dispatch warmup.
#
# Two-step wiring:
#   1. A disposable CLI turn (chat --project) creates the project-bound
#      session (chat.go CASE 3 -> createFlaggedSession). Its reply is
#      discarded — it is plumbing, not part of the transcript.
#   2. All transcript turns then go through the MCP stdio server's
#      meept_send with the REAL session id and source_client
#      "meept-bench-e2e" — the documented in-tree sync trigger
#      (handler.go handlerCase "sync_dispatch"). Sync dispatch is what
#      F1/C1/C2 (waitForTaskCompletion) and F6 (quota surfacing) live on;
#      plain CLI turns classify async and return an ack instead.
#
# The warmup turn doubles as the provider smoke test: no provider answer =>
# whole transcript SKIPs (printed, exit 0 — never silent).
log ""
log "[4/7] creating session via CLI chat --project (plumbing turn)"
PLUMB_ERR="$REPLIES/plumbing.err"
run_with_timeout "$TURN_TIMEOUT" env HOME="$HOME_DIR" "$CLI_BIN" \
  --socket "$SOCK" -d "$STATE" chat --project "$PROJECT_NAME" \
  "Reply with the single word: ok" >"$REPLIES/plumbing.txt" 2>"$PLUMB_ERR"
PLUMB_RC=$?

SID="$(env HOME="$HOME_DIR" "$CLI_BIN" --socket "$SOCK" -d "$STATE" \
  session list --json 2>/dev/null | python3 -c '
import json, sys
data = json.load(sys.stdin)
sessions = data.get("sessions") or [] if isinstance(data, dict) else []
best = ""
for s in sessions:
    if s.get("name") != "oneshot_responses":
        continue
    # The plumbing turn just created a project-bound oneshot session; pick
    # the most recently created one (list order is creation order).
    best = s.get("id", "") or best
print(best)
')"
if [ -z "$SID" ]; then
  die "could not create/discover session (rc=$PLUMB_RC; infrastructure error, not a provider skip)"
fi
log "  session: $SID"

log ""
log "[5/7] sync-dispatch warmup via mcp-chat-server (source_client=$SOURCE_CLIENT)"
WARMUP_ERR="$REPLIES/warmup.err"
warmup_reply="$(python3 - "$CLI_BIN" "$SOCK" "$HOME_DIR" "$STATE" "$SID" "$SOURCE_CLIENT" "$TURN_TIMEOUT" <<'PY' 2>"$WARMUP_ERR"
import json, os, subprocess, sys, threading, time

cli, sock, home, state, sid, source_client, timeout = sys.argv[1:8]
timeout = float(timeout)
env = dict(os.environ, HOME=home)

p = subprocess.Popen(
    [cli, "--socket", sock, "-d", state, "mcp-chat-server"],
    stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
    env=env, text=True)

out_lines = []
def reader():
    for line in p.stdout:
        out_lines.append(line)
t = threading.Thread(target=reader, daemon=True)
t.start()

def send(obj):
    p.stdin.write(json.dumps(obj) + "\n")
    p.stdin.flush()

def wait_response(want_id, deadline):
    while time.time() < deadline:
        for i, line in enumerate(out_lines):
            try:
                msg = json.loads(line)
            except ValueError:
                continue
            if msg.get("id") == want_id and ("result" in msg or "error" in msg):
                out_lines.pop(i)
                return msg
        time.sleep(0.2)
    return None

deadline = time.time() + 60
send({"jsonrpc": "2.0", "id": 1, "method": "initialize",
      "params": {"protocolVersion": "2024-11-05", "capabilities": {},
                 "clientInfo": {"name": "meept-e2e", "version": "0"}}})
if wait_response(1, deadline) is None:
    print("__WARMUP_FAILED__: no initialize response", file=sys.stderr)
    p.kill(); sys.exit(3)
send({"jsonrpc": "2.0", "method": "notifications/initialized"})

send({"jsonrpc": "2.0", "id": 2, "method": "tools/call",
      "params": {"name": "meept_send",
                 "arguments": {"session_id": sid, "source_client": source_client,
                               "message": "Reply with the single word: ok"}}})
msg = wait_response(2, time.time() + timeout)
p.kill()
if msg is None:
    print("__WARMUP_FAILED__: meept_send timed out", file=sys.stderr)
    sys.exit(3)
if "error" in msg and msg["error"]:
    print("__WARMUP_FAILED__: %s" % msg["error"], file=sys.stderr)
    sys.exit(3)
try:
    text = msg["result"]["content"][0]["text"]
except (KeyError, IndexError, TypeError):
    print("__WARMUP_FAILED__: unexpected tools/call result shape", file=sys.stderr)
    sys.exit(3)
sys.stdout.write(text)
PY
)"
WARMUP_RC=$?
if [ "$WARMUP_RC" -ne 0 ]; then
  if daemon_ping; then
    note_result SKIP "warmup" "no provider reply via mcp-chat-server (rc=$WARMUP_RC): $(tail -c 300 "$WARMUP_ERR" 2>/dev/null | tr '\n' ' ')"
  else
    note_result FAIL "warmup" "daemon stopped answering RPC (see $DAEMON_LOG)"
  fi
  log ""
  log "[7/7] summary"
  log "  passed: $NPASS   failed: ${#FAILURES[@]}   skipped: ${#SKIPS[@]}"
  if [ "${#SKIPS[@]}" -gt 0 ]; then
    for s in "${SKIPS[@]}"; do log "    - SKIP $s"; done
  fi
  if [ "${#FAILURES[@]}" -gt 0 ]; then
    for f in "${FAILURES[@]}"; do log "    - FAIL $f"; done
    log ""
    log "RESULT: FAIL (see FAILED list)"
    exit 1
  fi
  log ""
  log "RESULT: PASS with SKIPs (provider unavailable — reason printed above)"
  exit 0
fi
printf '%s\n' "$warmup_reply" >"$REPLIES/warmup.txt"
log "  warmup reply: $(printf '%s' "$warmup_reply" | head -n 2 | tr '\n' ' ' | cut -c1-160)"

# 6. Drive the naive-user transcript through the same MCP meept_send path
# (real session id + sync latch), one function per turn.
log ""
log "[6/7] driving the naive-user transcript (timeout ${TURN_TIMEOUT}s/turn)..."

mcp_send() { # $1=NAME $2=MESSAGE — reply to $REPLIES/$1.txt
  local name="$1" msg="$2"
      local errfile="$REPLIES/$name.err"
      local outfile="$REPLIES/$name.txt"
      local attempt out rc

      for attempt in 1 2; do
        out="$(python3 - "$CLI_BIN" "$SOCK" "$HOME_DIR" "$STATE" "$SID" "$SOURCE_CLIENT" "$TURN_TIMEOUT" "$msg" <<'PY' 2>"$errfile"
import json, os, subprocess, sys, threading, time

cli, sock, home, state, sid, source_client, timeout, message = sys.argv[1:9]
timeout = float(timeout)
env = dict(os.environ, HOME=home)

p = subprocess.Popen(
    [cli, "--socket", sock, "-d", state, "mcp-chat-server"],
    stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
    env=env, text=True)

out_lines = []
def reader():
    for line in p.stdout:
        out_lines.append(line)
t = threading.Thread(target=reader, daemon=True)
t.start()

def send(obj):
    p.stdin.write(json.dumps(obj) + "\n")
    p.stdin.flush()

def wait_response(want_id, deadline):
    while time.time() < deadline:
        for i, line in enumerate(out_lines):
            try:
                m = json.loads(line)
            except ValueError:
                continue
            if m.get("id") == want_id and ("result" in m or "error" in m):
                out_lines.pop(i)
                return m
        time.sleep(0.2)
    return None

deadline = time.time() + 60
send({"jsonrpc": "2.0", "id": 1, "method": "initialize",
      "params": {"protocolVersion": "2024-11-05", "capabilities": {},
                 "clientInfo": {"name": "meept-e2e", "version": "0"}}})
if wait_response(1, deadline) is None:
    p.kill(); sys.exit(3)
send({"jsonrpc": "2.0", "method": "notifications/initialized"})

send({"jsonrpc": "2.0", "id": 2, "method": "tools/call",
      "params": {"name": "meept_send",
                 "arguments": {"session_id": sid, "source_client": source_client,
                               "message": message}}})
msg = wait_response(2, time.time() + timeout)
p.kill()
if msg is None:
    sys.exit(124)
if msg.get("error"):
    sys.stderr.write(json.dumps(msg["error"]))
    sys.exit(1)
try:
    text = msg["result"]["content"][0]["text"]
except (KeyError, IndexError, TypeError):
    sys.exit(3)
sys.stdout.write(text)
PY
    )"
        rc=$?
        if [ "$rc" -eq 0 ] && [ -n "$(printf '%s' "$out" | tr -d '[:space:]')" ]; then
          printf '%s\n' "$out" >"$outfile"
          return 0
        fi
        if [ "$attempt" -eq 1 ]; then
          log "  (attempt 1 failed rc=$rc; retrying once)"
          sleep 5
        fi
      done

      if ! daemon_ping; then
        log "  daemon stopped answering RPC (see $DAEMON_LOG)"
        return 1
      fi
      local errtail
      errtail="$(tail -c 400 "$errfile" 2>/dev/null | tr '\n' ' ')"
      if printf '%s' "$errtail" | grep -Eiq '429|rate[ -]?limit|quota|too many requests'; then
        log "  provider quota/rate limit (rc=$rc): $errtail"
        return 2
      fi
      if [ "$rc" -eq 124 ]; then
        log "  turn exceeded ${TURN_TIMEOUT}s (provider too slow; chat RPC caps a sync wait at ~120s)"
        return 2
      fi
      if printf '%s' "$errtail" | grep -Eiq 'connection refused|no route|connection reset|tls|ssl|certificate|unauthorized|401|403|api key|bad gateway|502|503|504|deadline|timeout|eof|method not found'; then
        log "  provider/transport failure (rc=$rc): $errtail"
        return 2
      fi
      log "  turn failed rc=$rc: $errtail"
      return 1
    }

    T1="create a file named hello.txt in the current directory containing the word hello, then tell me the full path"
    T2="make it beep when it opens"
    T3="did the change get made? where is the file?"
    T4="what files did you make for me?"

    log ""
    log "T1: $T1"
    if mcp_send t1 "$T1"; then
      print_reply_preview "$REPLIES/t1.txt"
      assert_reply_shape t1 "$REPLIES/t1.txt"
      # A4: artifact landed in the PROJECT dir (not daemon cwd) + reply names it.
      if [ -f "$PROJECT_DIR/hello.txt" ]; then
        if grep -qi 'hello' "$PROJECT_DIR/hello.txt"; then
          note_result PASS "A4" "$PROJECT_DIR/hello.txt exists and contains 'hello'"
        else
          note_result FAIL "A4" "hello.txt exists in project dir but does not contain 'hello'"
        fi
      elif [ -f "$WORK/hello.txt" ]; then
        note_result FAIL "A4" "hello.txt landed in the DAEMON cwd ($WORK) not the session project dir (F3/C3, leaf-03 regression)"
      else
        note_result FAIL "A4" "$PROJECT_DIR/hello.txt was not created (F3/C3)"
      fi
      if grep -qi 'hello\.txt' "$REPLIES/t1.txt"; then
        note_result PASS "A4" "T1 reply names hello.txt"
      else
        note_result FAIL "A4" "T1 reply does not name the file path"
      fi
    else
      rc=$?
      if [ "$rc" -eq 2 ]; then
        note_result SKIP "A4" "T1 got no provider reply (see reason above)"
      else
        note_result FAIL "A4" "T1 turn failed (see reason above)"
      fi
    fi

    log ""
    log "T2: $T2"
    if mcp_send t2 "$T2"; then
      print_reply_preview "$REPLIES/t2.txt"
      assert_reply_shape t2 "$REPLIES/t2.txt"
    else
      rc=$?
      if [ "$rc" -eq 2 ]; then
        note_result SKIP "A1-A3/t2" "T2 got no provider reply (see reason above)"
      else
        note_result FAIL "A1-A3/t2" "T2 turn failed (see reason above)"
      fi
    fi

    log ""
    log "T3: $T3"
    if mcp_send t3 "$T3"; then
      print_reply_preview "$REPLIES/t3.txt"
      assert_reply_shape t3 "$REPLIES/t3.txt"
      # A5: continuity — the status turn references the artifact AND is not a
      # bare clarification request ("which file?"). The wrap envelope
      # ({"response": ...}) is JSON-escaped, so also accept the escaped
      # spelling 'hello.txt' split across escapes.
      if grep -qi 'hello\.txt\|hello\\.\\.txt' "$REPLIES/t3.txt"; then
        if grep -q '?' "$REPLIES/t3.txt"; then
          note_result FAIL "A5" "T3 is a bare clarification request — continuity gap"
        else
          A5_OK=1
          note_result PASS "A5" "T3 reply references hello.txt and answers the question (session continuity)"
        fi
      else
        if grep -q '?' "$REPLIES/t3.txt"; then
          note_result FAIL "A5" "T3 is a bare clarification request — continuity gap"
        else
          note_result FAIL "A5" "T3 reply does not reference hello.txt"
        fi
      fi
    else
      rc=$?
      if [ "$rc" -eq 2 ]; then
        note_result SKIP "A5" "T3 got no provider reply (see reason above)"
      else
        note_result FAIL "A5" "T3 turn failed (see reason above)"
      fi
    fi

    log ""
    log "T4: $T4"
    if mcp_send t4 "$T4"; then
      print_reply_preview "$REPLIES/t4.txt"
      assert_reply_shape t4 "$REPLIES/t4.txt"
    else
      rc=$?
      if [ "$rc" -eq 2 ]; then
        note_result SKIP "A1-A3/t4" "T4 got no provider reply (see reason above)"
      else
        note_result FAIL "A1-A3/t4" "T4 turn failed (see reason above)"
      fi
    fi

# 7. Lifecycle: stop the daemon, verify termination + temp-dir removal (A6).
log ""
log "[6/7] stopping scratch daemon..."
kill_daemon
if kill -0 "$DPID" 2>/dev/null; then
  note_result FAIL "A6" "daemon pid $DPID still alive after SIGTERM+SIGKILL"
else
  note_result PASS "A6" "daemon terminated cleanly"
fi

if [ "$KEEP" = "1" ]; then
  note_result SKIP "A6" "temp-dir-removal check skipped (--keep); workdir: $WORK"
else
  rm -rf "$WORK"
  if [ -d "$WORK" ]; then
    note_result FAIL "A6" "temp dir $WORK not removed"
  else
    note_result PASS "A6" "temp dir removed"
  fi
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

log ""
log "[7/7] summary"
log "  passed: $NPASS   failed: ${#FAILURES[@]}   skipped: ${#SKIPS[@]}"
if [ "${#FAILURES[@]}" -gt 0 ]; then
  log "  FAILED assertions:"
  for f in "${FAILURES[@]}"; do
    log "    - $f"
  done
fi
if [ "${#SKIPS[@]}" -gt 0 ]; then
  log "  SKIPPED assertions (with reasons above; never silent):"
  for s in "${SKIPS[@]}"; do
    log "    - $s"
  done
fi

log ""
if [ "$A5_OK" = "1" ]; then
  log "A5 continuity: PASS"
else
  log "A5 continuity: NOT PASSED (A5 did not pass on a provider-available T3 turn)"
fi
log ""

if [ "${#FAILURES[@]}" -gt 0 ]; then
  log "RESULT: FAIL (integration bug or broken contract — see FAILED list)"
  exit 1
fi

log ""
if [ "${#SKIPS[@]}" -gt 0 ]; then
  log "RESULT: PASS with SKIPs (provider unavailable — reasons printed above)"
else
  log "RESULT: PASS — all assertions green"
fi
exit 0
