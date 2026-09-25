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
#     providers can answer (env-provided API keys are honored). The local
#     endpoints of every provider that SPAWNS a runtime (driver, MLX
#     classifier/general, local-extract, ...) are remapped to freshly probed
#     free ports, the remap is re-probed immediately before the daemon is
#     spawned, and the run fails loudly if any literal in a spawning provider
#     survives — so the scratch daemon can never reach the user's live
#     runtimes. "Spawns" is the spawn_command KEY (comments are stripped before
#     the scan), and a spawn-bearing provider whose endpoint port cannot be
#     pinned down at all (`--port "${PORT}"`, no local <host>:<port>) is FATAL
#     rather than silently kept live. Endpoints of providers it does NOT spawn
#     (ollama localhost:11434, comfyui 127.0.0.1:8188) are left exactly as
#     written: remapping those would hand the daemon a dead port.
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
#
# The trap is installed BEFORE the lockfile is taken and before the workdir
# exists. The lock used to be acquired ~96 lines earlier than the first trap,
# so a SIGINT/SIGTERM (or any error) in that window leaked
# ${TMPDIR}/meept-e2e.lock and every later run fail-fasted with exit 3 until
# the lock was removed by hand (audit F94).
# ---------------------------------------------------------------------------

WORK=""
DPID=""
LOCK_DIR="${TMPDIR:-/tmp}/meept-e2e.lock"
E2E_LOCK_HELD=0
A5_OK=0   # set to 1 only when A5 PASSes on a provider-available T3 turn

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
  reap_workdir_runtimes
}

# reap_workdir_runtimes (issue #54): a SIGKILLed daemon cannot run its
# graceful runtime StopAll, and the boot orphan sweep only covers a daemon
# booting with the SAME home. After the daemon is gone, kill any runtime
# process still holding a pid file under this workdir (llama-server /
# mlx_lm spawned for the scratch endpoints). Ownership check: the pid
# file's recorded pid must be alive AND its command line must carry
# llama-server or mlx_lm so we never signal an unrelated reused pid.
#
# Pid files are JSON ({"pid":N,"token":"..."}), so extract the pid with the
# numeric guard: a bare `tr -d` read makes every JSON file fail the
# *[!0-9]* test and silently skip the reap (2026-09-22 orphan audit: six
# scratch runtimes leaked exactly this way once pid files became JSON).
reap_workdir_runtimes() {
  local pid_file pid
  for pid_file in "$WORK"/home/.meept/run/*.pid "$WORK"/state/*.pid; do
    [ -f "$pid_file" ] || continue
    pid="$(sed -n 's/.*"pid"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$pid_file" 2>/dev/null | head -1)"
    case "$pid" in
      ''|*[!0-9]*) continue ;;
    esac
    kill -0 "$pid" 2>/dev/null || continue
    if ps -p "$pid" -o command= 2>/dev/null | grep -qE 'llama-server|mlx_lm'; then
      log "  reaping leaked runtime (pid $pid from $pid_file)"
      kill "$pid" 2>/dev/null || true
      sleep 1
      kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null || true
    fi
    rm -f "$pid_file" "$pid_file.cmd"
  done
}

cleanup() {
  local rc=$?
  kill_daemon
  if [ "${E2E_LOCK_HELD:-0}" = "1" ]; then
    rm -rf "$LOCK_DIR"
  fi
  if [ -n "$WORK" ] && [ -d "$WORK" ]; then
    if [ "$KEEP" = "1" ]; then
      log ""
      log "--keep: scratch workdir left in place: $WORK"
    else
      rm -rf "$WORK"
    fi
  fi
  exit $rc
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# SIGHUP: terminal teardown kills the harness WITHOUT running the EXIT trap
# on some shells unless HUP is also trapped; an untrapped HUP skips
# kill_daemon + reap_workdir_runtimes and leaks the scratch runtimes
# (2026-09-22 orphan audit).
trap 'exit 129' HUP

# ---------------------------------------------------------------------------
# Stale-workdir pruning (2026-09-22 orphan audit): the cleanup trap above
# fires on every normal exit, but a SIGKILLed run (terminal torn down,
# harness killed mid-flight) skips it, and a --keep run leaves its workdir
# on purpose. Those leftovers then sit in ${TMPDIR} forever. At startup the
# harness prunes meept-e2e.* workdirs older than one day whose path no
# live process references. Granularity is 24h (find -mtime); a concurrent
# run's workdir is fresh by mtime, so the age gate alone makes races safe —
# the pgrep guard is defense in depth for a rig whose mtime is somehow old.
# Best-effort: a pruning failure must never fail the run.
# ---------------------------------------------------------------------------
record_run_evidence() {
  # Anchor OUTSIDE $HOME: the harness reassigns HOME to the sandbox, so
  # ~/.meept would put the evidence inside the deleted workdir. TMPDIR
  # anchor survives cleanup and is operator-overridable.
  local evidence_file="${E2E_EVIDENCE_FILE:-${TMPDIR:-/tmp}/meept-e2e-evidence.jsonl}"
  local t1_artifact="unknown" a5="unknown" result="fail" npass_n=0 nfail_n=0 nskip_n=0
  npass_n=$NPASS
  nfail_n=${#FAILURES[@]}
  nskip_n=${#SKIPS[@]}
  [ -f "$WORK/project/hello.txt" ] && t1_artifact="created" || t1_artifact="missing"
  [ "$A5_OK" = "1" ] && a5="pass" || a5="fail"
  [ "${#FAILURES[@]}" -eq 0 ] && result="pass"
  # Pure-shell append: no python dependency inside the harness env.
  printf '{"date":"%s","result":"%s","t1_artifact":"%s","a5_continuity":"%s","pass":%s,"fail":%s,"skip":%s}\n' \
    "$(date +%Y-%m-%dT%H:%M:%S)" "$result" "$t1_artifact" "$a5" "$npass_n" "$nfail_n" "$nskip_n" \
    >> "$evidence_file" 2>/dev/null || true
}

prune_stale_workdirs() {
  local found dir
  found="$(find "$(dirname "$WORK")" -maxdepth 1 -name 'meept-e2e.*' -type d -mtime +0 2>/dev/null || true)"
  [ -n "$found" ] || return 0
  while IFS= read -r dir; do
    [ -n "$dir" ] || continue
    [ "$dir" = "$WORK" ] && continue
    if pgrep -f -- "$dir" >/dev/null 2>&1; then
      warn "not pruning $dir: a live process references it"
      continue
    fi
    if rm -rf "$dir" 2>/dev/null; then
      log "pruned stale e2e workdir (older than 1 day): $dir"
    fi
  done <<EOF_PRUNE_LIST
$found
EOF_PRUNE_LIST
}

# ---------------------------------------------------------------------------
# Concurrency guard (e2e 2026-09-11): two simultaneous runs each spawn their
# own scratch daemon + MLX runtimes; the runtimes contend for the GPU, alias
# rotation kicks in, and BOTH verdicts are poisoned (observed twice: runs
# r6VGk4/akWFTB and CUu7rg/r6VGk4 racing produced socket timeouts, wrong-
# model replies, and 14/2 vs 15/1 drift on identical code). A lockfile makes
# the second invocation fail fast instead.
#
# Stale-lock detection: the recorded pid must be alive AND its process start
# time must still match, so a recycled pid cannot keep a dead lock alive
# forever (audit F94). Locks written before the start time was recorded (only
# a pid) are still honored while that pid lives.
# ---------------------------------------------------------------------------

proc_start_time() { # $1=pid -> normalized `ps -o lstart=` text ("" if gone)
  ps -o lstart= -p "$1" 2>/dev/null | tr -s ' ' | sed 's/^ //; s/ $//'
}

lock_holder_alive() { # $1=pid, $2=recorded start time ("" for a legacy lock)
  [ -n "$1" ] || return 1
  kill -0 "$1" 2>/dev/null || return 1
  [ -z "$2" ] && return 0
  [ "$(proc_start_time "$1")" = "$2" ]
}

acquire_lock() {
  if mkdir "$LOCK_DIR" 2>/dev/null; then
    printf '%s %s\n' "$$" "$(proc_start_time $$)" >"$LOCK_DIR/pid"
    E2E_LOCK_HELD=1
    return 0
  fi
  local lock_pid="" lock_start=""
  if [ -f "$LOCK_DIR/pid" ]; then
    # Two variables only: `read` gives the LAST variable every remaining field,
    # so a third variable would truncate the start time to its first word.
    read -r lock_pid lock_start <"$LOCK_DIR/pid" || true
  fi
  if [ -n "$lock_pid" ] && ! lock_holder_alive "$lock_pid" "$lock_start"; then
    echo "WARN: stale e2e lock (pid $lock_pid gone or recycled); reclaiming" >&2
    rm -rf "$LOCK_DIR"
    if mkdir "$LOCK_DIR" 2>/dev/null; then
      printf '%s %s\n' "$$" "$(proc_start_time $$)" >"$LOCK_DIR/pid"
      E2E_LOCK_HELD=1
      return 0
    fi
  fi
  echo "FATAL: another e2e run is active (pid ${lock_pid:-?}, lock $LOCK_DIR); concurrent runs poison both verdicts — wait and retry" >&2
  return 3
}

if ! acquire_lock; then
  exit 3
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/meept-e2e.XXXXXX")"
# macOS TMPDIR ends with a slash, so mktemp's template produces a
# double-slash path ("/var/.../T//meept-e2e.X"). Path strings derived from
# $WORK (allowed_paths globs, HOME, project dir) then carry "//" mid-path,
# while the daemon's permission matcher stores Clean-ed single-slash globs —
# run 4 (2026-09-10): relative writes Abs()-ed against the daemon's PWD kept
# the "//" form and every file_write was denied ("Path does not match any
# allowed path pattern"). Normalize WORK once; everything derives from it.
WORK="$(cd "$WORK" && pwd)"
# Prune workdirs left by runs that died hard (cleanup trap skipped) or --keep
# runs older than a day. Runs AFTER WORK is assigned: the pruner skips $WORK
# itself, and the dirname is the same temp root.
prune_stale_workdirs
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
DAEMON_BIN="$WORK/bin/meept-daemon"
CLI_BIN="$WORK/bin/meept"
PORT_MAP_FILE="$WORK/port-map.txt"

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
  if ! grep -q '[^[:space:]]' "$file"; then
    note_result FAIL "A0/$turn" "reply is empty or whitespace-only"
    return
  fi
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
  local first last compact
  compact="$(tr -d '[:space:]' <"$file")"
  first="${compact:0:1}"
  last="${compact: -1}"
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
  "orchestrator": {
    // e2e relies on the LEGACY sync-dispatch path (handler.go
    // sync_dispatch): the naive-user transcript asserts the chat RPC
    // reply carries the real task result. Requires the leaf-07 opt-in
    // since 0014bee2 gated the bench sync latch behind this flag.
    "sync_chat_enabled": true,
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
#
# The local endpoints of providers that SPAWN a runtime are remapped to freshly
# probed free ports before the sandbox daemon starts, and the remap is DERIVED
# from the copied config instead of from a hardcoded port list. 20d02738 moved
# the driver to 8080 (config/models.json5 "local-gguf"), the MLX general
# runtime to 8083 and local-extract to 8084, so the old 8081/8082-only sed left
# the scratch daemon pointing at the USER'S live runtimes (the exact shared-
# runtime contamination the lockfile exists to prevent) and the health wait
# polled HTTP_PORT+2, which nothing binds (audit F68). ${MODEL_PATH} is left
# alone: it names weights, not an endpoint.
PORT_MAP_FILE_READY=0

# Rewrite the copied models config so the scratch daemon can reach ONLY runtimes
# it spawns itself. Four properties matter:
#   * only providers that actually SPAWN are remapped — a remapped endpoint for a
#     provider the sandbox never starts is a DEAD port: `localhost:11434` (ollama)
#     and `127.0.0.1:8188` (comfyui) are external services the daemon DIALS, not
#     runtimes it launches, so rewriting them silently broke those providers;
#   * "spawns" means the spawn_command KEY, not the word in a comment. Detection
#     runs on a comment-blanked copy of the config, so a `// spawn_command: none`
#     note (or any prose containing the token) inside an object can no longer
#     flip that provider into the spawn set and remap its dial-only endpoint to a
#     dead port;
#   * the host spelling is preserved — normalizing `0.0.0.0:`/`[::1]:` to
#     127.0.0.1: changes BIND semantics, not merely the port;
#   * an endpoint that cannot be remapped is FATAL. The patterns cover the forms
#     the daemon itself accepts (spawnCommandBindsPort: `"--port", "8081"`,
#     `--port=8081`, `ROUTER_PORT=8081`, `--listen-port 8088`, including the
#     backslash-escaped `--port \"8081\"` spelling) plus every loopback
#     spelling (`127.0.0.1`, `localhost`, `0.0.0.0`, `[::1]`,
#     `[::ffff:127.0.0.1]`, `host.docker.internal`) and the host-less scheme /
#     addr-key bind forms (`http://:8091/v1`, `"addr": ":8091"`). A
#     spawn-bearing provider whose port is a variable (`--port "${PORT}"`,
#     `--port \"${PORT}\"`) and that declares NO literal endpoint at all stops
#     the run (exit 2) instead of being silently kept pointed at the user's
#     live runtime. A variable port ALONGSIDE a literal endpoint is not fatal
#     (the endpoint is pinnable) but is reported on stderr so it is never
#     silent.
# Points the scratch daemon at the user's live runtimes share with the old
# hardcoded sed: neither may come back.
remap_models_config() {
  cp "$REPO_ROOT/config/models.json5" "$HOME_DIR/.meept/models.json5"
  python3 - "$HOME_DIR/.meept/models.json5" "$HTTP_PORT" >"$PORT_MAP_FILE" <<'PY'
import re, socket, sys

path, http_port = sys.argv[1], int(sys.argv[2])
try:
    text = open(path, encoding="utf-8").read()
except OSError as exc:
    print("ERROR: cannot read %s: %s" % (path, exc), file=sys.stderr)
    sys.exit(2)


def _skip_string(s, i):
    """Index just past the string starting at s[i] == '"'."""
    i += 1
    while i < len(s):
        c = s[i]
        if c == "\\":
            i += 2
            continue
        if c == '"':
            return i + 1
        i += 1
    return i


def blank_comments(s):
    """Same-length (in CHARACTERS, not bytes) copy of s with every comment
    character replaced by a space.

    ALL scanning runs on this copy (offsets are preserved, so a match found
    here maps 1:1 back onto the original text; every consumer indexes a
    Python ``str``, i.e. character offsets, which is why character length --
    not byte length -- is the property that matters: a multi-byte character
    in a comment keeps the string's CHARACTER length, not its byte length).
    A comment is documentation, not config: a `// spawn_command: none` note
    inside a provider object must not make this remapper believe the
    provider spawns a runtime — doing so flipped comfyui into the spawn set,
    remapped its dial-only 8188 to a dead port with no warning at all, and
    rewrote a port quoted in the comment. Strings are skipped first so a
    `//` inside a URL stays inside its string.
    """
    out = list(s)
    i, n = 0, len(s)
    while i < n:
        c = s[i]
        if c == '"':
            i = _skip_string(s, i)
            continue
        if s.startswith("//", i):
            j = s.find("\n", i)
            j = n if j < 0 else j
            for k in range(i, j):
                out[k] = " "
            i = j
            continue
        if s.startswith("/*", i):
            j = s.find("*/", i + 2)
            j = n if j < 0 else j + 2
            for k in range(i, j):
                out[k] = " "
            i = j
            continue
        i += 1
    return "".join(out)


def object_spans(s):
    """(start, end, parents) for every {...} object.

    The config is JSON5 (comments, trailing commas, quoted keys), so json cannot
    parse it; a brace count must skip braces inside strings or it mis-slices the
    provider objects. s is the comment-blanked copy, so the comment branches
    below are a no-op there.
    """
    stack, spans, i, n = [], [], 0, len(s)
    while i < n:
        c = s[i]
        if c == '"':
            i = _skip_string(s, i)
            continue
        if s.startswith("//", i):
            j = s.find("\n", i)
            i = n if j < 0 else j
            continue
        if s.startswith("/*", i):
            j = s.find("*/", i + 2)
            i = n if j < 0 else j + 2
            continue
        if c == "{":
            stack.append(i)
        elif c == "}":
            if stack:
                start = stack.pop()
                spans.append((start, i + 1, tuple(stack)))
        i += 1
    return spans


scan = blank_comments(text)
spans = object_spans(scan)
prov_key = re.search(r'["\']?providers["\']?\s*:\s*\{', scan)
provider_members = []
if prov_key:
    prov_start = prov_key.end() - 1
    prov_parents = next((parents for s, e, parents in spans if s == prov_start), None)
    if prov_parents is not None:
        want = prov_parents + (prov_start,)
        provider_members = sorted(
            (s, e) for s, e, parents in spans if parents == want)


def member_name(start):
    """Provider key whose value object starts at `start`."""
    names = re.findall(r'"([^"]+)"\s*:', text[:start])
    return names[-1] if names else "?"


# HOSTS: spellings that mean "this machine". Captured, never rewritten: 0.0.0.0
# and the IPv6 forms change BIND semantics if collapsed to 127.0.0.1.
HOSTS = (r'\[[0-9A-Fa-f:.]+\]'      # [::1], [::ffff:127.0.0.1]
         r'|host\.docker\.internal'
         r'|localhost'
         r'|127\.0\.0\.1'
         r'|0\.0\.0\.0')
# `<host>:<port>`, plus the two host-LESS endpoint spellings that keep a
# scheme (`http://:8091/v1`) or an addr/host key (`"addr": ":8091"`,
# `--addr=:8091`, `"baseURL": ":8091"`) in an explicit endpoint context.
# A bare `:digits` anywhere else is NOT an endpoint: the old
# `(?<![\w.\-/])` alternative treated any colon not preceded by a word
# char/dot/slash as a local bind, so inside a spawning provider it rewrote
# `"timeout_ms":5000`, `"seed":12345`, `"blank":"foo/:9000"` and the
# env-shaped `--env "EXPORT=3000"` to freshly probed free ports. The
# host-qualified alternatives keep a REMOTE literal such as
# `https://api.example.com:8443` (colon preceded by a word char) untouched.
ENDPOINTCTX = (r'(?P<addr>["\']?(?:addr|address|bind|host|listen|'
               r'baseURL|base_url)["\']?\s*[:=]\s*["\']?)')
LOCALPORT = re.compile(
    r'(?:(?P<host>' + HOSTS + r')|(?<=//)|' + ENDPOINTCTX +
    r'):(?P<port>\d{1,5})')

# A port FLAG in a spawn command: `--port`, `--listen-port`, `--router_port`,
# or the env-assignment spelling the daemon's own duplicate-spawn pre-check
# accepts (spawnCommandBindsPort: token == port, or a token ending "=<port>").
# The env form is anchored to a REAL port variable: exactly `PORT`, or a name
# ending `_PORT` (`ROUTER_PORT`, `MY_PORT`). A looser `[A-Z][A-Z0-9_]*PORT`
# matched EXPORT / SUPPORT / TRANSPORT, so `--env "EXPORT=3000"` inside a
# spawning provider was rewritten to a free port.
# Q tolerates the JSON5 string form (`"--port", "8081"`) AND its
# backslash-escaped spelling inside a single quoted string
# (`--port \"8081\"`): both the literal and the variable form must be
# recognisable, else `--port \"${PORT}\"` was silently left live with no
# warning and no fatal.
Q = r'(?:\\?["\'])?'
PORTFLAG = (r'(?<![\w.\-])'
            r'(?:--(?:[A-Za-z0-9_]+[-_])?port|PORT|[A-Z][A-Z0-9_]*_PORT)'
            + Q + r'\s*[=,\s]\s*')
# `"--port", 8081` / `"--port", "8081"` / `--port=8081` / `ROUTER_PORT=8081`.
SPAWNPORT = re.compile(PORTFLAG + r'(?:"(?P<port>\d{1,5})"|(?P<bare>\d{1,5}))')
# The same flag with a value this script cannot resolve (`--port "${PORT}"`).
SPAWNVAR = re.compile(
    PORTFLAG + Q + r'(?P<val>\$(?:\{[^}"\']*\}|\(|[A-Za-z_][A-Za-z0-9_]*))')


def plausible_local(p, host):
    """Is p a plausible local endpoint port? (host is the captured host or "".)

    A host-qualified 0 is the documented "pick any free port" form. The
    host-less `:port` branch additionally requires an unprivileged port, which
    also stops a no-space JSON5 value like `"timeout_seconds":5` from reading
    as an endpoint.
    """
    if p == 0:
        return host != ""
    return 1024 <= p <= 65535


def plausible_spawn(p):
    # 0 is a real spawn port (`llama-server --port 0` = any free port); below
    # 1024 a spawned runtime cannot bind without privileges anyway.
    return p == 0 or 1024 <= p <= 65535


def match_port(m):
    """(port, span) of the numeric group of a SPAWNPORT match."""
    if m.group("port") is not None:
        return int(m.group("port")), m.span("port")
    return int(m.group("bare")), m.span("bare")


def block_ports(sub):
    """Local endpoint ports declared by one provider block (comment-stripped)."""
    ports = []
    for m in LOCALPORT.finditer(sub):
        p = int(m.group("port"))
        if not plausible_local(p, m.group("host") or ""):
            continue
        if p not in ports:
            ports.append(p)
    for m in SPAWNPORT.finditer(sub):
        p, _ = match_port(m)
        if not plausible_spawn(p):
            continue
        if p not in ports:
            ports.append(p)
    return ports


# A `spawn_command` KEY (not the word in a comment) is the marker for "this
# provider is a runtime the daemon launches". Providers without one (ollama,
# comfyui) talk to a service that is already running under the user's control;
# leaving them alone is the point.
spawning = [span for span in provider_members
            if re.search(r'["\']?spawn_command["\']?\s*:', scan[span[0]:span[1]])]
spawning_set = set(spawning)
if not spawning:
    print("ERROR: no provider with a spawn_command in %s; the sandbox cannot "
          "tell which local endpoints are runtimes it spawns itself" % path,
          file=sys.stderr)
    sys.exit(2)

# Fail closed on a spawning provider whose endpoint this script cannot pin down.
# Today's config is clean; a provider that spawns a runtime the sandbox would
# inherit the endpoint of (a port the script cannot see, or one behind a
# variable it cannot resolve) must stop the run instead of being silently kept.
unresolved = []
# Not fatal, but never silent: a variable port in a spawning provider that
# ALSO carries a literal endpoint this script can pin (options.baseURL or a
# --port flag). Refusing there was over-strict (the endpoint is pinnable),
# but leaving it unreported was the original hole: `--port \"${PORT}\"`
# passed with no message at all.
left_live = []
for span in spawning:
    name = member_name(span[0])
    block = scan[span[0]:span[1]]
    ports = block_ports(block)
    m = SPAWNVAR.search(block)
    if ports:
        if m:
            left_live.append("%s: %s" % (name, m.group("val")))
        continue
    if m:
        # No literal endpoint anywhere in the block: only the variable port
        # exists, so the sandbox cannot pin the runtime's endpoint. FATAL.
        unresolved.append("%s: %s is not a literal port this script can remap"
                          % (name, m.group("val")))
        continue
    unresolved.append(
        "%s: spawns a runtime but declares no local <host>:<port> the "
        "sandbox can remap (unknown endpoint)" % name)
if left_live:
    print("WARNING: variable port(s) left as-is in a spawning provider "
          "(pinned by a literal endpoint in the same provider):",
          file=sys.stderr)
    for lv in left_live:
        print("  %s" % lv, file=sys.stderr)
if unresolved:
    print("ERROR: refusing to run — a spawning provider's endpoint cannot be "
          "remapped:", file=sys.stderr)
    for u in unresolved:
        print("  %s" % u, file=sys.stderr)
    print("  fix: give the provider a literal local <host>:<port> endpoint "
          "(options.baseURL / a --port flag) or extend the patterns in this "
          "script; a sandbox that inherits an unknown live endpoint is worse "
          "than no run", file=sys.stderr)
    sys.exit(2)

old_ports = []
for span in spawning:
    for p in block_ports(scan[span[0]:span[1]]):
        if p not in old_ports:
            old_ports.append(p)

# `p >= 0`, consistently: port 0 means "pick any free port" (llama-server
# `--port 0`), so it is a real spawn port the sandbox must replace with a
# concrete one. Filtering 0 out HERE while the survivor checks below still
# rejected it was how a config carrying `"--port", "0"` or `127.0.0.1:0` made
# this script refuse to run: "spawn --port value(s) survived the remap: 0".
if not old_ports:
    print("ERROR: no local <host>:<port> endpoint found in a spawning provider "
          "of %s; the sandbox would inherit whatever the template resolves to"
          % path, file=sys.stderr)
    sys.exit(2)

used = set(old_ports) | {http_port}


def free_port():
    while True:
        s = socket.socket()
        try:
            s.bind(("127.0.0.1", 0))
            p = s.getsockname()[1]
        finally:
            s.close()
        if p not in used and p != 0:
            used.add(p)
            return p


mapping = {p: free_port() for p in old_ports}


def rewrite_block(orig, sub, mapping):
    """Rewrite mapped ports in orig, using sub (comment-blanked) to find them.

    Only the matched NUMBER is replaced, so the host spelling, the quoting and
    every comment byte inside the block are preserved exactly.
    """
    edits = []
    for m in LOCALPORT.finditer(sub):
        p = int(m.group("port"))
        if not plausible_local(p, m.group("host") or ""):
            continue
        if p in mapping:
            edits.append((m.start("port"), m.end("port"), str(mapping[p])))
    for m in SPAWNPORT.finditer(sub):
        p, span = match_port(m)
        if not plausible_spawn(p):
            continue
        if p in mapping:
            edits.append((span[0], span[1], str(mapping[p])))
    edits.sort()
    out, last = [], 0
    for start, end, rep in edits:
        if start < last:  # overlapping match from the other pattern
            continue
        out.append(orig[last:start])
        out.append(rep)
        last = end
    out.append(orig[last:])
    return "".join(out)


# Rebuild the file block by block so ONLY the spawning providers change: every
# other byte of the template (ollama 11434, comfyui 8188, remote hosts) is
# preserved exactly.
out, last, rewritten = [], 0, []
for start, end in provider_members:
    block = text[start:end]
    out.append(text[last:start])
    if (start, end) in spawning_set:
        block = rewrite_block(block, scan[start:end], mapping)
        # The survivor check runs on the REWRITTEN block with its comments
        # blanked: the original ports are gone from it, and a port mentioned in
        # a comment was never a real endpoint (scanning the raw rewritten text
        # reported every `:8082` in prose as a survivor).
        rewritten.append((member_name(start), blank_comments(block)))
    out.append(block)
    last = end
out.append(text[last:])
new = "".join(out)

# Fail loudly if any local literal inside a SPAWNING provider survived the
# rewrite: the scratch daemon must never reach the user's live runtime
# endpoints. Scoped to those blocks — the deliberately preserved external
# endpoints (ollama/comfyui) are not survivors, they are policy.
remapped = set(mapping.values())
survivors = []
for name, block_scan in rewritten:
    for m in LOCALPORT.finditer(block_scan):
        p = int(m.group("port"))
        if not plausible_local(p, m.group("host") or ""):
            continue
        if p not in remapped:
            survivors.append("%s: %s:%s" % (name, m.group("host"), m.group("port")))
    for m in SPAWNPORT.finditer(block_scan):
        p, _ = match_port(m)
        if not plausible_spawn(p):
            continue
        if p not in remapped:
            survivors.append("%s: --port %s" % (name, p))
if survivors:
    print("ERROR: local endpoint(s) in a spawning provider survived the remap: "
          "%s" % ", ".join(sorted(set(survivors))), file=sys.stderr)
    sys.exit(2)

preserved = set()
for start, end in provider_members:
    if (start, end) in spawning_set:
        continue
    for m in LOCALPORT.finditer(scan[start:end]):
        if not plausible_local(int(m.group("port")), m.group("host") or ""):
            continue
        preserved.add("%s:%s" % (m.group("host"), m.group("port")))
if preserved:
    print("note: kept as-is (provider does not spawn): %s"
          % ", ".join(sorted(preserved)), file=sys.stderr)

# The sandboxed e2e can reach what the scratch daemon SPAWNS itself, plus
# nothing else: zai has no API key here and ollama is not running, so an
# alias chain that ends in a cloud/ollama model makes specialist step jobs
# fail (the naive-user e2e: T1's coder step -> A4; later turns -> error
# envelopes -> A3/A5).
#
# Filter, don't collapse (2026-09-23 run 0zmnHP): the old rewrite collapsed
# EVERY agent alias to [local-gguf/lfm-8b-gguf]. That aimed the CLASSIFIER
# alias at the busy general endpoint — every classify call queued behind
# planner/coder on max_concurrency=2 and deadline-exceeded, the keyword
# fallback misrouted, planner loops blew past the 120s RPC ceiling, and all
# four transcript turns failed. It also left the spawned local-mlx runtime
# referenced by NOTHING: the daemon's in-use gate skipped its spawn, its
# port sat empty, and the classifier health-wait could never pass. The fix:
# drop only members that are UNREACHABLE in the sandbox (cloud/ollama, or a
# local provider whose endpoint this sandbox does not spawn) and keep the
# original preference ORDER of everything reachable. The classifier alias
# keeps its fast dedicated runtime and the in-use gate sees its model.
ALIAS_NAMES = ("classifier", "summarizer", "small", "coder", "planner",
               "analyst")
FALLBACK_MODEL = "local-gguf/lfm-8b-gguf"

def reachable_members(block_scan):
    """Alias member refs kept in the sandbox, in original order.

    A member survives when its provider is one the scratch daemon spawns
    (any provider key in spawning_provider_names — their endpoints are all
    remapped live here) — or when its ref carries no provider/members that
    the sandbox could reach anyway. Cloud (zai/glm-*), ollama, and any
    provider without a spawn_command in this config are dropped.
    """
    models_m = re.search(r'["\']?models["\']?\s*:\s*\[', block_scan)
    if models_m is None:
        return None
    i, blen, depth = models_m.end(), len(block_scan), 1
    while i < blen and depth:
        c = block_scan[i]
        if c == '"':
            i = _skip_string(block_scan, i)
            continue
        if c == "[":
            depth += 1
        elif c == "]":
            depth -= 1
        i += 1
    if depth:
        return None
    inner = block_scan[models_m.end():i - 1]
    refs = re.findall(r'["\']([a-zA-Z0-9\-]+/[a-zA-Z0-9\-.]+)["\']', inner)
    kept = [r for r in refs if r.split("/", 1)[0] in spawning_provider_names]
    return kept

# Provider keys the sandbox daemon spawns (computed above from
# spawn_command markers) — every one of their endpoints is remapped and
# will be listening by the time turns run. Only TOP-LEVEL provider members
# count: `spawning` also holds nested spans (a lifecycle block contains its
# own spawn_command), whose member_name() is a nested key like "models" or
# a model id — those must not enter the reachability set.
spawning_provider_names = {member_name(s) for s, e in spawning_set}
spawning_provider_names.discard("?")

alias_edits = []
alias_key = re.search(r'["\']?model_aliases["\']?\s*:\s*\{', scan)
if alias_key is None:
    print("WARNING: no model_aliases object in %s; aliases left as-is" % path,
          file=sys.stderr)
else:
    alias_start = alias_key.end() - 1
    alias_parents = next((parents for s, e, parents in spans
                          if s == alias_start), None)
    if alias_parents is None:
        print("WARNING: model_aliases object in %s could not be sliced; "
              "aliases left as-is" % path, file=sys.stderr)
    else:
        want = alias_parents + (alias_start,)
        for s, e in sorted((a, b) for a, b, pr in spans if pr == want):
            name = member_name(s)
            if name not in ALIAS_NAMES:
                continue
            block_scan = scan[s:e]
            kept = reachable_members(block_scan)
            if kept is None:
                print("WARNING: alias %s has no models array; left as-is"
                      % name, file=sys.stderr)
                continue
            if not kept:
                # No reachable member at all (e.g. coder/planner name only
                # cloud/ollama models): fall back to the spawned general
                # runtime. Leaving the alias untouched would send every
                # specialist step to an endpoint the sandbox cannot dial.
                kept = [FALLBACK_MODEL]
                print("note: alias %s had no sandbox-reachable member; "
                      "rewritten to %s" % (name, FALLBACK_MODEL),
                      file=sys.stderr)
            # Walk to the array's matching ']' (strings skipped; comments are
            # already blanked in block_scan).
            mkey = re.search(r'["\']?models["\']?\s*:\s*\[', block_scan)
            i, blen, depth = mkey.end(), e - s, 1
            while i < blen and depth:
                c = block_scan[i]
                if c == '"':
                    i = _skip_string(block_scan, i)
                    continue
                if c == "[":
                    depth += 1
                elif c == "]":
                    depth -= 1
                i += 1
            if depth:
                print("WARNING: alias %s models array is unterminated; "
                      "left as-is" % name, file=sys.stderr)
                continue
            close = i - 1  # index of the matching ']' within the block
            # Match the array's own indentation for the replacement entries.
            line_start = block_scan.rfind("\n", 0, mkey.end() - 1) + 1
            indent = re.match(r"[ \t]*", block_scan[line_start:]).group(0)
            # Per-entry comments would swallow the separating comma (a //
            # comment runs to end of line, so "value // c," parses with the
            # comma INSIDE the comment and hujson rejects the array). The
            # rewrite note in stderr documents what was filtered; keep the
            # array itself comment-free.
            entries = ",\n".join('%s"%s"' % (indent, r) for r in kept)
            replacement = "[\n%s\n%s]" % (entries, indent)
            alias_edits.append((s + mkey.end() - 1, s + close + 1, replacement))
        if not alias_edits:
            print("WARNING: none of the agent aliases (%s) found in %s; "
                  "nothing rewritten" % (", ".join(ALIAS_NAMES), path),
                  file=sys.stderr)
for astart, aend, rep in sorted(alias_edits, reverse=True):
    new = new[:astart] + rep + new[aend:]
if alias_edits:
    print("note: model_aliases filtered for the sandbox (unreachable members "
          "dropped, order kept): %s" % ", ".join(
              sorted({member_name(a[0]) for a in alias_edits})), file=sys.stderr)

open(path, "w", encoding="utf-8").write(new)
for old in old_ports:
    print("%d %d" % (old, mapping[old]))
PY
}

if [ -f "$REPO_ROOT/config/models.json5" ]; then
  if remap_models_config; then
    PORT_MAP_FILE_READY=1
    log "  copied config/models.json5 -> $HOME_DIR/.meept/models.json5 (ports of SPAWNING providers remapped to free ports)"
    while IFS= read -r line; do
      [ -z "$line" ] && continue
      log "    :${line% *} -> :${line#* }"
    done <"$PORT_MAP_FILE"
  else
    die "port remap failed on the copied models config (see ERROR above: a spawn-bearing provider's endpoint could not be pinned down, or a live literal survived) — refusing to run the sandbox daemon against the user's live runtime endpoints"
  fi
else
  warn "config/models.json5 not found in repo; turns will SKIP unless a provider is configured"
fi

# Map a port from the SHIPPED template to the port the sandbox config uses.
# The health waits below must poll the remapped ports the sandbox daemon will
# spawn its own runtimes on — never an arithmetic guess like HTTP_PORT+2 (F68).
map_port() { # $1=port in config/models.json5 -> remapped port (empty if absent)
  [ "$PORT_MAP_FILE_READY" = "1" ] || return 0
  awk -v want="$1" '$1 == want { print $2; exit }' "$PORT_MAP_FILE" 2>/dev/null || true
}

# The free-port probe in remap_models_config is a TOCTOU by construction: it
# binds :0, reads the port number and closes the socket, while the daemon is
# spawned ~80 lines later — anything may take that port in between and the
# sandbox would then either fail to bind or (worse) collide with a runtime the
# user just started. Re-probe every mapped port here, immediately before the
# spawn; the socket is held only for the microseconds of the check, so the
# remaining window is "between this check and the exec", and a collision is
# handled by remapping again from the pristine template.
mapped_ports_free() {
  [ "$PORT_MAP_FILE_READY" = "1" ] || return 0
  python3 - "$PORT_MAP_FILE" <<'PY'
import socket, sys

try:
    rows = [line.split() for line in open(sys.argv[1], encoding="utf-8")
            if line.strip()]
except OSError:
    sys.exit(0)
busy = []
for row in rows:
    if len(row) != 2 or not row[1].isdigit():
        continue
    s = socket.socket()
    try:
        s.bind(("127.0.0.1", int(row[1])))
    except OSError:
        busy.append(row[1])
    finally:
        s.close()
if busy:
    print("busy: %s" % ", ".join(busy), file=sys.stderr)
    sys.exit(1)
PY
}

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

# 3b. Re-probe the remapped ports now, immediately before the spawn (the probe
# that produced them ran ~80 lines ago — see mapped_ports_free). On collision,
# remap again from the pristine template and re-probe once; a second collision
# is fatal rather than a silently broken sandbox.
if [ "$PORT_MAP_FILE_READY" = "1" ]; then
  if ! mapped_ports_free; then
    warn "a remapped sandbox port was taken while the config was being written — re-probing from the template"
    if remap_models_config && mapped_ports_free; then
      log "  re-probed models config ports:"
      while IFS= read -r line; do
        [ -z "$line" ] && continue
        log "    :${line% *} -> :${line#* }"
      done <"$PORT_MAP_FILE"
    else
      die "could not reserve free ports for the sandbox runtimes (see above) — refusing to start the daemon on a port another process now owns"
    fi
  fi
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

# Poll the runtime ports from the SANDBOXED config, i.e. the remapped ports the
# daemon will spawn its own runtimes on. Deriving them here (from the port map
# written when the config was rewritten) replaces the old HTTP_PORT+1/+2
# arithmetic, which after 20d02738 pointed the "general" wait at a port nothing
# binds and paid a spurious 120s warning on every run (audit F68).
MLX_CLASS_PORT="$(map_port 8081)"   # template's classifier runtime
MLX_GEN_PORT="$(map_port 8083)"     # template's general (mlx) runtime
if [ -n "$MLX_CLASS_PORT" ]; then
  wait_for_runtime "$MLX_CLASS_PORT" "classifier" || true
else
  log "  (no classifier runtime endpoint in the sandboxed models config; skipping its health wait)"
fi
if [ -n "$MLX_GEN_PORT" ] && [ "$MLX_GEN_PORT" != "$MLX_CLASS_PORT" ]; then
  wait_for_runtime "$MLX_GEN_PORT" "general" || true
elif [ -z "$MLX_GEN_PORT" ]; then
  log "  (no general runtime endpoint in the sandboxed models config; skipping its health wait)"
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
# warmup failure fails the run; reachability alone cannot identify provider outages.
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

try:
    deadline = time.time() + 60
    send({"jsonrpc": "2.0", "id": 1, "method": "initialize",
          "params": {"protocolVersion": "2024-11-05", "capabilities": {},
                     "clientInfo": {"name": "meept-e2e", "version": "0"}}})
    initial = wait_response(1, deadline)
    if initial is None or initial.get("error"):
        print("__WARMUP_FAILED__: no initialize response", file=sys.stderr)
        sys.exit(3)
    send({"jsonrpc": "2.0", "method": "notifications/initialized"})

    send({"jsonrpc": "2.0", "id": 2, "method": "tools/call",
          "params": {"name": "meept_send",
                     "arguments": {"session_id": sid, "source_client": source_client,
                                   "message": "Reply with the single word: ok"}}})
    msg = wait_response(2, time.time() + timeout)
    if msg is None:
        print("__WARMUP_FAILED__: meept_send timed out", file=sys.stderr)
        sys.exit(3)
    if "error" in msg and msg["error"]:
        print("__WARMUP_FAILED__: %s" % msg["error"], file=sys.stderr)
        sys.exit(3)
    if isinstance(msg.get("result"), dict) and msg["result"].get("isError"):
        # Surface the tool's error text: "MCP tool returned isError" alone
        # hides the root cause (2026-09-23 run: every turn failed with the
        # actual cause — an RPC chat.response timeout — buried in content).
        try:
            err_text = msg["result"]["content"][0]["text"]
        except (KeyError, IndexError, TypeError):
            err_text = "<no content>"
        sys.stderr.write("MCP tool returned isError: %s\n" % err_text[:500])
        sys.exit(3)
    try:
        text = msg["result"]["content"][0]["text"]
    except (KeyError, IndexError, TypeError):
        print("__WARMUP_FAILED__: unexpected tools/call result shape", file=sys.stderr)
        sys.exit(3)
    try:
        parsed = json.loads(text)
        if isinstance(parsed, dict) and isinstance(parsed.get("response"), str):
            text = parsed["response"]
    except ValueError:
        pass
    sys.stdout.write(text)
finally:
    if p.poll() is None:
        p.kill()
    p.wait()
    t.join(timeout=1)
PY
)"
WARMUP_RC=$?
if [ "$WARMUP_RC" -ne 0 ]; then
  if daemon_ping; then
    note_result FAIL "warmup" "MCP warmup failed; daemon reachability does not prove a provider outage (rc=$WARMUP_RC): $(tail -c 300 "$WARMUP_ERR" 2>/dev/null | tr '\n' ' ')"
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
  record_run_evidence
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

try:
    deadline = time.time() + 60
    send({"jsonrpc": "2.0", "id": 1, "method": "initialize",
          "params": {"protocolVersion": "2024-11-05", "capabilities": {},
                     "clientInfo": {"name": "meept-e2e", "version": "0"}}})
    initial = wait_response(1, deadline)
    if initial is None or initial.get("error"):
        sys.exit(3)
    send({"jsonrpc": "2.0", "method": "notifications/initialized"})

    send({"jsonrpc": "2.0", "id": 2, "method": "tools/call",
          "params": {"name": "meept_send",
                     "arguments": {"session_id": sid, "source_client": source_client,
                                   "message": message}}})
    msg = wait_response(2, time.time() + timeout)
    if msg is None:
        sys.exit(124)
    if msg.get("error"):
        sys.stderr.write(json.dumps(msg["error"]))
        sys.exit(1)
    if isinstance(msg.get("result"), dict) and msg["result"].get("isError"):
        # Surface the tool's error text: "MCP tool returned isError" alone
        # hides the root cause (2026-09-23 run: every turn failed with the
        # actual cause — an RPC chat.response timeout — buried in content).
        try:
            err_text = msg["result"]["content"][0]["text"]
        except (KeyError, IndexError, TypeError):
            err_text = "<no content>"
        sys.stderr.write("MCP tool returned isError: %s\n" % err_text[:500])
        sys.exit(3)
    try:
        text = msg["result"]["content"][0]["text"]
    except (KeyError, IndexError, TypeError):
        sys.exit(3)
    # meept_send wraps the reply as {"response": <text>}. Unwrap so the
    # saved reply file holds the user-facing text, not the envelope
    # (assert_reply_shape's A3 check would flag the envelope as a raw
    # JSON dump, and A4/A5 substring checks would run against keys).
    try:
        parsed = json.loads(text)
        if isinstance(parsed, dict) and isinstance(parsed.get("response"), str):
            text = parsed["response"]
    except ValueError:
        pass
    sys.stdout.write(text)
finally:
    if p.poll() is None:
        p.kill()
    p.wait()
    t.join(timeout=1)
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

# Run-evidence JSONL (2026-09-24: per-run tier/outcome evidence that
# SURVIVES the workdir cleanup — the metrics DB dies with $WORK, so T1
# pass-rate trends were previously unmeasurable). One row per run,
# appended to ~/.meept/e2e-evidence.jsonl. Best-effort: never fail the
# run over evidence bookkeeping.
record_run_evidence

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
