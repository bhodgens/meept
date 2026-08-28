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

node() { local h="$1"; shift; ssh root@"$h" "$@"; }
all_nodes() { local h; for h in "${NODES[@]}"; do node "$h" "$@"; done; }

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
  echo "== deps (go, git, build headers) on all nodes"
  for h in "${NODES[@]}"; do
    node "$h" "apt-get install -y -qq golang-go git libsqlite3-dev libasound2-dev pkg-config >/dev/null 2>&1 || true"
  done

  echo "== repo on totem1 (build host)"
  node totem1 "mkdir -p /root/git && tar xzf /tmp/meept-head.tar.gz -C /root/git/ 2>/dev/null || true; ls $REPO_DIR/cmd/meept-daemon >/dev/null && echo repo-present"

  echo "== build on totem1"
  node totem1 "cd $REPO_DIR && go build -o bin/meept-daemon ./cmd/meept-daemon && go build -o bin/meept ./cmd/meept && echo build-ok"

  echo "== distribute binaries + source to totem2/3"
  for h in totem2 totem3; do
    node "$h" "mkdir -p $REPO_DIR/bin"
    scp -q root@totem1:"$REPO_DIR"/bin/meept-daemon root@totem1:"$REPO_DIR"/bin/meept root@"$h":"$REPO_DIR"/bin/
    scp -q /tmp/meept-head.tar.gz root@"$h":/tmp/ 2>/dev/null && \
      node "$h" "mkdir -p /root/git && tar xzf /tmp/meept-head.tar.gz -C /root/git/ 2>/dev/null; mkdir -p $REPO_DIR/bin" || true
  done

  echo "== registry bare repo on totem1"
  node totem1 "git init --bare -q $REGISTRY_DIR 2>/dev/null || true; git -C $REGISTRY_DIR symbolic-ref HEAD refs/heads/main"

  echo "== host keys + cluster config on all nodes"
  for h in "${NODES[@]}"; do
    node "$h" "ssh-keyscan -H ${IP[totem1]} >> /root/.ssh/known_hosts 2>/dev/null || true"
    write_cluster_config "$h"
  done

  echo "== daemon config (template + multiuser on + http off)"
  for h in "${NODES[@]}"; do
    node "$h" "cp -f $REPO_DIR/config/meept.json5 $STATE/meept.json5
cp -f $REPO_DIR/config/models.json5 $STATE/models.json5
mkdir -p $STATE/config && cp -f $REPO_DIR/config/*.json5 $STATE/config/ 2>/dev/null || true
python3 - <<'PYEOF'
import re
p = '/root/.meept/meept.json5'
s = open(p).read()
s = re.sub(r'(\"http\":\s*\{)', r'\1\n      \"enabled\": false,', s, count=1)
if '\"multiuser\"' not in s:
    s = s.replace('\"transport\": {',
        '\"multiuser\": { \"enabled\": true, \"users_file\": \"/root/.meept/users.json5\" },\n  \"transport\": {', 1)
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
