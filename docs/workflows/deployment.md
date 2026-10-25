Deploying, upgrading, and rolling back meept on the totem cluster
==================================================================

Status: operator runbook. Audience: whoever runs meept on totem1-4
(10.9.8.201+, Proxmox LXC, root SSH from the workstation).

Related:
  scripts/totem-cluster.sh       fresh cluster provision / start / stop
  scripts/config-drift-check.py  template-vs-installed config drift report
  Makefile                       build, install, sync-config
  docs/workflows/backup.md       state backup details


1. Fresh deploy
---------------

Provision the 3-node test cluster (totem1/2/3):

  ./scripts/totem-cluster.sh provision

What provision does, in order:

  1. Installs the PINNED Go toolchain (GO_VERSION at the top of the
     script, currently go1.26.5 - matching the `toolchain` line in
     go.mod) into /usr/local/go-<ver> on every node, exposes it as the
     /usr/local/go symlink and via /etc/profile.d/pinned-go.sh
     (PATH + GOTOOLCHAIN export). The tarball is sha256-verified against
     the hash pinned in the script. Distro golang-go is NEVER installed:
     it is unpinned and a newer ambient go (1.27+) breaks the repo's
     x/tools analyzers. The step is idempotent - a node already running
     the pinned version is skipped without downloading.
  2. Installs non-Go deps (git, libsqlite3-dev, libasound2-dev,
     pkg-config).
  3. Unpacks /tmp/meept-head.tar.gz into /root/git/meept on totem1 (the
     build host) and builds the daemon + CLI there with the pinned
     toolchain (GOTOOLCHAIN derived from go.mod inline as a second belt).
  4. Distributes binaries + source to totem2/3.
  5. Creates the bare membership registry at
     /srv/git/meept-cluster.git on totem1 (idempotent).
  6. Seeds host keys, cluster config, and - NO-CLOBBER - the daemon
     config: installed $MEEPT_HOME/meept.json5 / models.json5 are only
     written when absent. Re-running provision never overwrites an
     existing installed config.

Dry-run the whole thing without touching any node:

  DRY_RUN=1 ./scripts/totem-cluster.sh provision

First boot, in order:

  ./scripts/totem-cluster.sh start
  ./scripts/totem-cluster.sh status
  ./scripts/totem-cluster.sh sync <username>   # create first user + key


2. Upgrade procedure (single host or whole cluster)
---------------------------------------------------

A failed upgrade on a live daemon is a state-mutation outage, not a
build error - the sequence below is ordered so every destructive step
has a verified escape hatch.

2.1 Build from a clean worktree

`make install` (and the other make install targets) build from the
WORKING TREE, not from git HEAD. Stash, commit, or verify before
building - `git status --porcelain` must be clean of anything you do
not want shipped:

  cd ~/git/meept
  git status --porcelain          # must be empty (or understood)
  make build                      # or: make install-cli (headless hosts)

2.2 Stop the daemon

Restarting reaps supervised local runtimes and brings them back, but an
upgrade replaces the binary underneath a live process - stop first:

  ./scripts/totem-cluster.sh stop
  # single host instead:
  ssh root@totem1 'pgrep -af meept-daemon'    # confirm it is down

2.3 Install

  make install-cli                 # headless LXC nodes (no Flutter)
  # or the full install where the GUI is wanted:
  make install

2.4 Sync config (no-clobber)

  make sync-config

sync-config runs scripts/install-sync.py: user-modified installed files
are KEPT, new defaults land as <file>.new for manual review, and it
reports top-level keys the shipped templates declare but the installed
copies lack. Reconcile what it reports; never hand-copy a template over
an installed config wholesale - that is how operator state is lost.

Audit drift explicitly, from the repo checkout against the installed
home (report-only; exit 1 = unclassified drift):

  python3 scripts/config-drift-check.py
  python3 scripts/config-drift-check.py --json     # machine-readable

Classifications (mirrors the installed-config review doctrine):

  template-defect   the template itself is wrong; fix in config/.
  local-drift       the install diverged intentionally; never clobber.
  correct-by-schema the install adds fields the schema allows; sync
                    must preserve them.

Anything UNCLASSIFIED needs a human decision before restart.

2.5 Classifier-port collision check (BEFORE restart)

A foreign listener on the classifier port fails the boot gate (F-D8 -
internal/daemon/classifier_boot_gate.go): the daemon logs and exits
non-zero instead of booting degraded. The template classifier port is
8143 (config/models.json5, provider lifecycle spawn_command); OLDER
installs used 8081, which is ALSO the transport.http (GUI) port - a
leftover server on 8081 collides with the GUI listener, and one on 8143
starves the classifier. Check BOTH on every node before restart:

  ssh root@totem1 'lsof -nP -iTCP:8143 -sTCP:LISTEN; lsof -nP -iTCP:8081 -sTCP:LISTEN'

Expected: nothing on 8143, and on 8081 either nothing (pre-start) or
only the previous meept-daemon (already stopped in 2.2). Kill or
re-point any foreign listener before continuing.

2.6 Start and verify

  ./scripts/totem-cluster.sh start           # or: ssh root@totem1 '.../meept-daemon -f &'

Verification bar - all of these, in order:

  # a) boot gate: the F-D8 line must show the classifier chain healthy.
  #    Absence of the line means the gate never ran - treat as failure.
  ssh root@totem1 'grep -i "F-D8" $HOME/.meept/daemon.log | tail -3'

  # b) listeners: daemon owns RPC + HTTP(+WS); classifier runtime on 8143.
  ssh root@totem1 'lsof -nP -iTCP:8143 -sTCP:LISTEN; lsof -nP -iTCP:8081 -sTCP:LISTEN'

  # c) GUI path end-to-end (from the repo checkout, against the host):
  make gui-connect-probe

If (a) shows an F-D8 fatal, go straight to 2.5, fix the collision or
classifier health, and start again. Do not "fix" it by setting
orchestrator.classifier_boot_fail_fast=false - that trades a loud
boot failure for silently degraded classification on every turn.


3. Rollback
-----------

3.1 Binary rollback

Keep the previous binaries. The install targets overwrite in place, so
retain a copy before upgrading:

  cp -a $GOPATH/bin/meept-daemon $HOME/meept-rollback/meept-daemon.$(date +%Y%m%d)
  # (cluster hosts: binaries live at /root/git/meept/bin/)

Rollback is then: stop daemon (2.2), restore the previous binary,
start, run the 2.6 verification bar.

3.2 State rollback is RESTORE-FROM-BACKUP only

Meept's internal state migration is FORWARD-ONLY. A binary rollback
does not undo state-schema migrations the newer daemon already applied
to $MEEPT_HOME stores (sessions, users, audit chain, effects). The only
supported state rollback is restoring $MEEPT_HOME state from a backup
taken BEFORE the upgrade (see docs/workflows/backup.md; state under
$MEEPT_HOME, users/registry mirrored through the cluster registry).

Operational rule: snapshot state before every upgrade, e.g.

  ssh root@totem1 'tar czf /root/meept-home-$(date +%Y%m%d-%H%M).tgz -C /root .meept'

and never attempt to hand-edit migrated state back to an older schema.

Note on the migration registry: a migration-registry package
(internal/migrate) is being added to the repo in this same wave. If it
is present at your finish time (check: ls internal/migrate), it is the
authoritative registry of applied migrations; the forward-only rule
above still holds - the registry records what ran, it does not make
migrations reversible.


4. Command quick reference
--------------------------

  DRY_RUN=1 ./scripts/totem-cluster.sh provision   # dry-run everything
  ./scripts/totem-cluster.sh {provision|start|stop|status|sync|teardown}
  make build / install-cli / sync-config           # from clean worktree
  python3 scripts/config-drift-check.py [--json]   # drift audit
  make gui-connect-probe                           # post-upgrade GUI path
  lsof -nP -iTCP:8143 -sTCP:LISTEN                 # classifier port free?
  lsof -nP -iTCP:8081 -sTCP:LISTEN                 # legacy/GUI port clear?
  grep -i F-D8 ~/.meept/daemon.log                 # boot gate verdict
