# tmp scratch policy

This doc explains how meept scratch data lands in `/tmp`, how we name new
scratch data, and how to remove it safely. It also defines how to close a plan
worktree at the end of implementation.

Read this before you probe with a temp directory or clean `/tmp/meept*`.

## 1. four known sources of /tmp/meept* data

Four paths create scratch data under `/tmp` today.

### a. provenance-exact git-archive build clones

The bench run log names these clones. To keep provenance exact, the harness
builds a clean tree with `git archive` instead of using a dirty worktree.
Each build lands in a named `/tmp` directory. Examples from the log:

- `/tmp/meept-build-acc4` from `git archive 68478b69` (log line 97)
- `/tmp/meept-build-acc5` from `git archive 4f934dfb` (line 182)
- `/tmp/meept-build-acc7` from `git archive <HEAD>` (lines 482-483)
- `/tmp/meept-bench-build-acc9` from `git archive 4248144` (lines 832-834)

All four are in `docs/plans/20261119-usability-remediation/07-bench/02-RUN-LOG.md`.
These clones can stay for days as evidence. Do not delete a clone that a run
log still cites.

### b. e2e harness temp dirs

The e2e harness calls `os.MkdirTemp("", "meept-e2e-*")` at
`e2e/harness/daemon.go:200`. A cleanup func removes the dir at test end unless
`MEEPT_E2E_KEEP` is set (`e2e/harness/daemon.go:204-208`). A per-process reaper
also removes stale `meept-e2e-bin*` dirs in the temp root
(`e2e/harness/daemon.go:515`, func `sweepStaleBinDirs` at `:519`). The reaper
never touches dirs inside the age floor, so a live run stays safe.

### c. meept-bench scratch rig

The scratch rig calls `mktemp -d /tmp/meept-scratch-rig.XXXXXX` at
`scratch-rig.sh:110` in `meept-bench` (`meept-bench/scripts/scratch-rig.sh:110`).
A `trap cleanup EXIT` removes the rig dir and the temp bench binary on exit
(`meept-bench/scripts/scratch-rig.sh:247-271`). Crashes can leave a dir behind.

### d. ad hoc agent probes

Agents often make one-off temp dirs for a quick test. These dirs have no fixed
name and no cleanup owner. They are the most common source of stale entries.

## 2. naming convention going forward

Use one root and one name per task: `$TMPDIR/meept-scratch/<task-name>`. Reuse
the same path across probe runs for a task. Do not mint a fresh random dir for
each attempt. This keeps the number of scratch paths small.

`meept-head-check` is a protected exception. Do not reuse it for a new task and
do not let cleanup remove it. Other protected entries are any name that
contains `meept-head-check` and any name that ends in `.log` or `.txt`.
Evidence logs stay until you review them by hand.

## 3. the two make commands

Run these from the repo root. Neither command runs on its own. No hook, make
target, or build step calls them automatically. You must run them yourself.

### `make tmp-status`

Read only. Prints a table of every `/tmp/meept*` entry. For each entry it shows
human size, age in days, and whether a live process uses it. It deletes
nothing. Run this first.

### `make tmp-clean`

Interactive. Prints the same list, then asks you to confirm. It deletes only
entries that pass all three checks:

- age is at least 7 days (`MIN_AGE_DAYS = 7`),
- no live process uses the entry,
- the name is not protected.

The helper re-checks each path at delete time so a symlink is never traversed.
If stdin is not a terminal, it runs in dry-run mode and removes nothing. Answer
`y` only after you read `make tmp-status` output.

Both targets call `scripts/meept-tmp-audit.py` (`--status` and `--clean`).

## 4. close out a plan worktree

A plan is not done when the merge lands. Close the worktree in the same step,
before you claim the plan is complete.

1. Merge the plan branch to local main in the primary checkout.
2. Run `git -C .hermes/worktrees/<tree-name> status --porcelain`. Add `--force`
   to removal only when that output is empty; otherwise keep the wanted files.
3. Run `git worktree remove .hermes/worktrees/<tree-name>` (with `--force` per
   step 2). Then run `git worktree prune`.
4. Run `git branch -d plan/<tree-name>`. Never use `-D` on a merged branch.
5. Prove it. Run `git worktree list`; for every non-primary worktree check
   `git merge-base --is-ancestor <head-sha> main`. No merged plan worktree may
   remain listed.

**complete** means both conditions hold:

- the content is reachable from main, and
- the worktree no longer exists.

Content merged but worktree present is **not complete**, not a legitimate stop.
Never defer these checks to a later session.

## 5. skill enforcement

The `hierarchical-plan-execution` skill now enforces the closeout rule in
section 4. Its "closeout worktree removal" step runs in the same closeout as
the merge. It checks `status --porcelain`, removes and prunes the worktree,
deletes the branch with `-d`, then sweeps every worktree with
`merge-base --is-ancestor`. The skill also flags a merged plan worktree that
is still listed. Do not treat a skill warning as optional.
