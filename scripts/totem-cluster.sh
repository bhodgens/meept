#!/usr/bin/env bash
# scripts/totem-cluster.sh — provision / control / teardown the 3-node
# meept multiuser test cluster on the totem PVE hosts (totem1/2/3).
#
# Usage:
#   ./scripts/totem-cluster.sh provision   # deps + repo + build + config on all nodes
#   ./scripts/totem-cluster.sh start       # launch daemons on all nodes
#   ./scripts/totem-cluster.sh stop        # kill daemons on all nodes
#   ./scripts/totem-cluster.sh status      # users list per node + registry freshness
#   ./scripts/totem-cluster.sh sync [user] # create a user+key on totem1, print raw key
#   ./scripts/totem-cluster.sh teardown    # stop daemons + wipe test state (users, registry clone)
#                                          # (keeps /root/git/meept checkouts and the bare repo)
#
# DRY-RUN: prefix with DRY_RUN=1 (or `DRY_RUN=1 ./scripts/totem-cluster.sh
# provision`) to print every remote command instead of executing it. Local
# read-only steps still run; nothing is installed or written on any node.
#
# Go toolchain policy: the cluster pins the upstream toolchain named in the
# repo's go.mod (GO_VERSION below, currently go1.26.5). Distro golang-go is
# NEVER installed: it is unpinned, and a newer ambient go (1.27+) breaks the
# repo's x/tools analyzers — the same reason the repo pins GOTOOLCHAIN
# in-repo. The pinned toolchain installs to /usr/local/go-<ver> and is
# exposed as /usr/local/go (symlink, only when absent or already ours);
# a pre-existing REAL /usr/local/go directory (e.g. a manually installed
# toolchain someone else owns) is left untouched and precedence is handled
# via PATH export instead. Re-running provision is idempotent: a node whose
# pinned toolchain already reports the right version is skipped entirely.
#
# Requirements from the workstation: root SSH to totem1/2/3 (totem1 must be
# able to root-ssh to totem2/3 for the registry remote, and vice versa).
#
# Topology (matches docs/plans/2026-08-26-multiuser-access live verification):
#   totem1 10.9.8.201 — also hosts the bare membership registry
#   totem2 10.9.8.202
#   totem3 10.9.8.203
set -euo pipefail

NODES=(totem1 totem2 totem3)
declare -A IP=( [totem1]=10.9.8.201 [totem2]=10.9.8.202 [totem3]=10.9.8.203 )
REGISTRY_DIR=/srv/git/meept-cluster.git
REPO_DIR=/root/git/meept
STATE=/root/.meept
GOSSIP_PORT=9701

# Pinned Go toolchain — MUST match the `toolchain` line in the repo's go.mod.
GO_VERSION=go1.26.5
GO_TARBALL="${GO_VERSION}.linux-amd64.tar.gz"
GO_URL="https://go.dev/dl/${GO_TARBALL}"
GO_SHA256="798232c0baa15b14e03a55899bdb5ea7deb154f5dffd4b4b83bcbe45dc7ada2d"
GO_INSTALL_ROOT=/usr/local
GO_INSTALL_DIR="${GO_INSTALL_ROOT}/${GO_VERSION}"
GO_SYMLINK="${GO_INSTALL_ROOT}/go"

DRY_RUN="${DRY_RUN:-0}"
if [ "$DRY_RUN" = "1" ]; then
  node() { local h="$1"; shift; echo "[dry-run] ssh root@$h $*"; return 0; }
  scpto() { local src="$1" dst="$2"; echo "[dry-run] scp -q $src $dst"; return 0; }
else
  node() { local h="$1"; shift; ssh root@"$h" "$@"; }
  scpto() { local src="$1" dst="$2"; scp -q "$src" "$dst"; }
fi
all_nodes() { local h; for h in "${NODES[@]}"; do node "$h" "$@"; done; }

# Pinned-toolchain bootstrap on one node. Idempotent:
#   1. pinned dir already correct version -> PATH fix only, no download
#   2. pinned dir present but wrong version -> refresh it (re-download)
#   3. absent -> download, verify sha256, install
# /usr/local/go becomes a symlink only when it does not exist yet or already
# points at our pinned dir; a real directory there is never clobbered.
install_pinned_go() {
  local h="$1"
  node "$h" bash -s -- "$GO_VERSION" "$GO_TARBALL" "$GO_URL" "$GO_SHA256" <<'GOSETUP'
set -euo pipefail
GO_VERSION="$1"; GO_TARBALL="$2"; GO_URL="$3"; GO_SHA256="$4"
GO_INSTALL_DIR="/usr/local/${GO_VERSION}"
GO_SYMLINK="/usr/local/go"
GO_BIN="${GO_INSTALL_DIR}/bin/go"

version_ok() { "$1" version 2>/dev/null | grep -q " ${GO_VERSION} " || return 1; }

if [ -x "$GO_BIN" ] && version_ok "$GO_BIN"; then
  echo "pinned go ${GO_VERSION} already installed at ${GO_INSTALL_DIR}"
elif [ -x "$GO_BIN" ]; then
  echo "stale install at ${GO_INSTALL_DIR} (wrong version); refreshing"
  rm -rf "$GO_INSTALL_DIR"
fi

if [ ! -x "$GO_BIN" ]; then
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$tmp/$GO_TARBALL" "$GO_URL"
  else
    wget -qO "$tmp/$GO_TARBALL" "$GO_URL"
  fi
  echo "$GO_SHA256  $tmp/$GO_TARBALL" | sha256sum -c -
  rm -rf "$GO_INSTALL_DIR"
  tar -C /usr/local -xzf "$tmp/$GO_TARBALL"
  mv /usr/local/go "$GO_INSTALL_DIR"
  version_ok "$GO_BIN" || { echo "installed go failed version check" >&2; exit 1; }
  echo "installed pinned ${GO_VERSION} at ${GO_INSTALL_DIR}"
fi

# Export precedence via profile.d (PATH-only; never clobbers an existing
# /usr/local/go that someone else owns as a real directory).
cat > /etc/profile.d/pinned-go.sh <<PROFILE
# managed by meept scripts/totem-cluster.sh - pinned Go for the meept build
export PATH="${GO_INSTALL_DIR}/bin:\$PATH"
export GOTOOLCHAIN="${GO_VERSION}"
PROFILE
chmod 0644 /etc/profile.d/pinned-go.sh

if [ ! -e "$GO_SYMLINK" ]; then
  ln -s "$GO_INSTALL_DIR" "$GO_SYMLINK"
  echo "symlinked ${GO_SYMLINK} -> ${GO_INSTALL_DIR}"
elif [ -L "$GO_SYMLINK" ] && [ "$(readlink "$GO_SYMLINK")" != "$GO_INSTALL_DIR" ]; then
  ln -sfn "$GO_INSTALL_DIR" "$GO_SYMLINK"
  echo "re-pointed symlink ${GO_SYMLINK} -> ${GO_INSTALL_DIR}"
fi
"${GO_BIN}" version
GOSETUP
}

write_cluster_config() {
  local h="$1"
  node "$h" "mkdir -p $STATE/cluster/nodes && cat > $STATE/cluster/config.json5" <<EOF
{
  "cluster_id": "meept-test",
  "cluster_name": "meept-multiuser-test",
  "join_key": "test-join-key",
  "node_id": "$h",
  "network": {
    "gossip_listen_addr": "0.0.0.0:$GOSSIP_PORT",
    "wireguard_subnet": "10.200.0.0/24",
    "wireguard_port": 9700,
    "mesh_interface": "wg0"
  },
  "gossip": {
    "heartbeat_interval": "3s",
    "peer_timeout": "60s",
    "event_retention": "1h",
    "max_retry_attempts": 3
  },
  "git": { "heartbeat_commit": true, "sync_interval": "1h", "remote_url": "root@${IP[totem1]}:$REGISTRY_DIR", "branch": "main" },
  "security": { "require_node_signatures": false, "ed25519_key_rotation_days": 90 }
}
EOF
  local peer
  for peer in totem1 totem2 totem3; do
    node "$h" "cat > $STATE/cluster/nodes/$peer.json5" <<EOF
{ "node_id": "$peer", "node_name": "$peer", "endpoint": "${IP[$peer]}:$GOSSIP_PORT", "status": "active" }
EOF
  done
}

cmd_provision() {
  echo "== pinned Go (${GO_VERSION}) on all nodes (never distro golang-go)"
  for h in "${NODES[@]}"; do
    install_pinned_go "$h"
  done

  echo "== non-go deps (git, build headers) on all nodes"
  for h in "${NODES[@]}"; do
    node "$h" "apt-get install -y -qq git libsqlite3-dev libasound2-dev pkg-config >/dev/null 2>&1 || true"
  done

  echo "== repo on totem1 (build host)"
  node totem1 "mkdir -p /root/git && tar xzf /tmp/meept-head.tar.gz -C /root/git/ 2>/dev/null || true; ls $REPO_DIR/cmd/meept-daemon >/dev/null && echo repo-present"

  echo "== build on totem1 (pinned toolchain from /etc/profile.d/pinned-go.sh)"
  # The pinned toolchain + GOTOOLCHAIN export live in profile.d (installed
  # above); source it explicitly because non-interactive ssh does not read it.
  # GOTOOLCHAIN is also derived from go.mod inline so the build stays pinned
  # even if the toolchain binary were wrong (go would then fetch the declared
  # toolchain itself rather than silently building with a mismatched one).
  node totem1 "cd $REPO_DIR && . /etc/profile.d/pinned-go.sh 2>/dev/null; TC=\$(sed -n 's/^toolchain[[:space:]]//p' go.mod | head -1) && \
GOTOOLCHAIN=\$TC go build -o bin/meept-daemon ./cmd/meept-daemon && \
GOTOOLCHAIN=\$TC go build -o bin/meept ./cmd/meept && echo build-ok"

  echo "== distribute binaries + source to totem2/3"
  for h in totem2 totem3; do
    node "$h" "mkdir -p $REPO_DIR/bin"
    scpto root@totem1:"$REPO_DIR"/bin/meept-daemon root@"$h":"$REPO_DIR"/bin/meept-daemon
    scpto root@totem1:"$REPO_DIR"/bin/meept root@"$h":"$REPO_DIR"/bin/meept
    scpto /tmp/meept-head.tar.gz root@"$h":/tmp/meept-head.tar.gz
    node "$h" "mkdir -p /root/git && tar xzf /tmp/meept-head.tar.gz -C /root/git/ 2>/dev/null; mkdir -p $REPO_DIR/bin" || true
  done

  echo "== registry bare repo on totem1 (idempotent: init is a no-op if it exists)"
  node totem1 "git init --bare -q $REGISTRY_DIR 2>/dev/null || true; git -C $REGISTRY_DIR symbolic-ref HEAD refs/heads/main; (git -C $REGISTRY_DIR rev-parse --verify refs/heads/main >/dev/null 2>&1 || (cd $REPO_DIR && git push -q $REGISTRY_DIR HEAD:refs/heads/main 2>/dev/null || true))"

  echo "== host keys + cluster config on all nodes"
  for h in "${NODES[@]}"; do
    node "$h" "ssh-keyscan -H ${IP[totem1]} >> /root/.ssh/known_hosts 2>/dev/null || true"
    write_cluster_config "$h"
  done

  echo "== daemon config (template + multiuser on + http off) — no-clobber"
  for h in "${NODES[@]}"; do
    # Idempotent, no-clobber: write the template only when the installed copy
    # is absent (first provision). An existing installed config is the
    # operator's state — re-provisioning must never clobber it; report drift
    # instead (scripts/config-drift-check.py is the full audit tool).
    node "$h" "[ -f $STATE/meept.json5 ] && echo '$h: $STATE/meept.json5 exists, left untouched' || { cp $REPO_DIR/config/meept.json5 $STATE/meept.json5; echo '$h: seeded $STATE/meept.json5'; }
[ -f $STATE/models.json5 ] && echo '$h: $STATE/models.json5 exists, left untouched' || cp $REPO_DIR/config/models.json5 $STATE/models.json5
mkdir -p $STATE/config && cp -f $REPO_DIR/config/*.json5 $STATE/config/ 2>/dev/null || true
python3 - <<'PYEOF'
import re
p = '/root/.meept/meept.json5'
s = open(p).read()
if '\"multiuser\"' not in s:
    s = re.sub(r'(\\\"transport\\\":\\s*\\{)',
        r'\\\"multiuser\\\": { \\\"enabled\\\": true, \\\"users_file\\\": \\\"/root/.meept/users.json5\\\" },\\n  \\1', s, count=1)
if 'users.json5' not in s:
    s = re.sub(r'(\\\"multiuser\\\":\\s*\\{)', r'\\1 \\\"users_file\\\": \\\"/root/.meept/users.json5\\\",', s, count=1)
open(p, 'w').write(s)
print('patched', p)
PYEOF"
  done
  echo "provision complete — run '$0 start' then '$0 sync <username>'"
}

cmd_start() {
  local h
  for h in "${NODES[@]}"; do
    node "$h" 'for pid in $(pgrep -f meept-daemon); do [ "$pid" = "$$" ] || kill "$pid" 2>/dev/null; done; true'
  done
  sleep 2
  for h in "${NODES[@]}"; do
    node "$h" "setsid $REPO_DIR/bin/meept-daemon -c $STATE/meept.json5 --state-dir $STATE > $STATE/daemon.log 2>&1 < /dev/null &"
    sleep 3
  done
  sleep 5
  cmd_status
}

cmd_stop() {
  # pkill -f with the full binary path kills the remote shell carrying the
  # pattern too (self-match), making ssh exit 255 mid-loop. Kill only
  # matching PIDs that are not our own shell.
  for h in "${NODES[@]}"; do
    node "$h" 'for pid in $(pgrep -f meept-daemon); do [ "$pid" = "$$" ] || kill "$pid" 2>/dev/null; done; true'
  done
  echo "daemons stopped"
}

cmd_status() {
  local h
  for h in "${NODES[@]}"; do
    echo "=== $h users"
    node "$h" "cd $REPO_DIR && ./bin/meept users list 2>&1 | head -6"
  done
  echo "=== registry heartbeat freshness (want <60s)"
  node totem1 "cd $REGISTRY_DIR && for n in totem1 totem2 totem3; do echo -n \$n: ; git show main:nodes/\$n.json5 2>/dev/null | grep -o '\"last_heartbeat\": \"[^\"]*\"' || echo missing; done"
  echo "=== peer discovery counts"
  for h in "${NODES[@]}"; do
    echo -n "$h: "; node "$h" "grep -c 'peer discovered' $STATE/daemon.log 2>/dev/null || echo 0"
  done
}

cmd_sync() {
  local name="${1:-alice}"
  echo "== create user+key on totem1 (daemon stopped to avoid cache clobber)"
  node totem1 'for pid in $(pgrep -f meept-daemon); do [ "$pid" = "$$" ] || kill "$pid" 2>/dev/null; done; true'
  sleep 2
  node totem1 "cd $REPO_DIR && uid=\$(./bin/meept users add $name 2>/dev/null | grep -o 'user-[0-9a-f]*' | head -1) \
    && ./bin/meept keys add \$uid --label cluster-test 2>&1 | grep -E '^[0-9a-f]{64}'"
  echo "== restart totem1 (daemon loads the new store)"
  node totem1 "setsid $REPO_DIR/bin/meept-daemon -c $STATE/meept.json5 --state-dir $STATE > $STATE/daemon.log 2>&1 < /dev/null &"
  sleep 3
  echo "wait ~60s, then '$0 status' — the user should appear on totem2/3 with origin_node=totem1"
}

cmd_teardown() {
  echo "== stopping daemons"
  cmd_stop
  echo "== wiping test state (users stores, registry clones, logs)"
  for h in "${NODES[@]}"; do
    node "$h" "rm -rf $STATE/users.json5 $STATE/cluster /root/heartbeat-refresh.sh $STATE/daemon.log /tmp/meept-head.tar.gz"
  done
  echo "== removing probe leftovers"
  node totem2 "rm -f $REPO_DIR/cmd/cfgprobe/main.go 2>/dev/null || true"
  node totem3 "rm -rf $REPO_DIR/cmd/cfgprobe 2>/dev/null || true"
  echo "teardown complete. Checkouts at $REPO_DIR and the bare registry at $REGISTRY_DIR were KEPT."
  echo "Full wipe (also removes those): ssh totem1 'rm -rf $REPO_DIR $REGISTRY_DIR'"
}

case "${1:-}" in
  provision) cmd_provision ;;
  start)     cmd_start ;;
  stop)      cmd_stop ;;
  status)    cmd_status ;;
  sync)      cmd_sync "${2:-}" ;;
  teardown)  cmd_teardown ;;
  *) echo "usage: $0 {provision|start|stop|status|sync [user]|teardown}"; exit 1 ;;
esac
