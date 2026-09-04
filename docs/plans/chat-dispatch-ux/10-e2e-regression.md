# E2E Naive-User Regression - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** A self-contained e2e script that replays the naive-user transcript against a scratch daemon and asserts the harness-level behaviors fixed by leaves 01-06.
- **Dependencies:** 01, 02, 03, 04, 05, 06 (dispatch after all are REVIEWED)
- **Estimated Context:** 35K
- **Audit references:** all findings F1-F6; reproduces the 2026-09-04 comparison transcript

## Goal

The comparison findings came from a manual transcript. This leaf encodes it
as `scripts/e2e-naive-user-chat.sh`: builds binaries, boots a dedicated
daemon on a temp state dir (socket + HTTP port + data dir under a mktemp
dir — never the user's ~/.meept, never the running daemon), drives four
RPC chat turns, and asserts the user-visible contract. Skipped under `go
test -short`; runnable standalone.

## Context

Daemon boot needs a state dir with config: copy the repo's config templates
(config/ dir) into the temp dir the way `make install` does (check the
Makefile install target for the exact copy list), or write a minimal
meept.json5 pointing models at a stub. IMPORTANT: real LLM calls are NOT
required — the script may either (a) point models.json5 at an unreachable
provider and assert the FAILURE-SHAPE behaviors (honest failure text, no
stubs, no catalog dumps, quota message with a forced 429 — too invasive),
or (b) use the daemon's configured default models if credentials exist in
the environment. Default to (b) with a graceful skip when no provider
answers: each chat call retries once, then the script marks that assertion
group SKIP — the structural assertions (no stub, no roster) still hold on
error replies.

Key files to understand before implementing:
- Makefile - build + install targets (config copy list)
- cmd/meept-daemon - daemon flags: -f (foreground), -s socket, -d state dir (see AGENTS.md run block)
- cmd/meept - chat flags: --session, --cwd, --socket, --state-dir, --transport
- docs/plans/chat-dispatch-ux/master.md - the assertion contract per leaf

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// scripts/e2e-naive-user-chat.sh
//   usage: bash scripts/e2e-naive-user-chat.sh [--keep]
//   - mktemp -d workdir; builds bin/meept-daemon + bin/meept (go build)
//   - writes minimal meept.json5 + models.json5 into the temp state dir
//   - starts daemon (background, logs to $WORK/daemon.log), waits for socket
//   - runs transcript against $WORK/project:
//       T1 "create a file named hello.txt in the current directory
//           containing the word hello, then tell me the full path"
//       T2 "make it beep when it opens"            (modify turn)
//       T3 "did the change get made? where is the file?" (status turn)
//       T4 "what files did you make for me?"        (artifact turn)
//   - assertions:
//       A1 no reply matches ^Task .* completed\.$          (F1/C1)
//       A2 no reply contains "## Available Agents"          (F5/C5)
//       A3 no reply is a raw JSON object dump (starts '{' ends '}') (F5/C5)
//       A4 $WORK/project/hello.txt exists; T1 reply names the path (F3/C3)
//       A5 T3 reply references hello.txt (context continuity)
//       A6 script exits 0; daemon terminated; temp dir removed (--keep skips cleanup)
//   - exit non-zero listing failed assertions
// Owner: 10. Consumers: Integration Test Plan step 5; future CI.
```

### What This Leaf Consumes

```
// built binaries; bash; timeout port (pick 18099-range FREE port via
// python3 or nc; never hardcode the user's live 18099)
```

## Tasks

### Task 1: script skeleton + lifecycle

**Objective:** Boot/tear-down a scratch daemon reliably.

**Files:**
- Create: `scripts/e2e-naive-user-chat.sh` (executable)

**Step 1: write the lifecycle** — mktemp, build (or reuse existing bin/
 if GOFLAGS skip requested — always build fresh), config write (minimal
 json5: rpc socket in temp dir, http addr on the free port, data_dir temp;
 models: reuse env-configured defaults by copying config/models.json5 if
 present), daemon start with logs, socket wait loop (max 30s), trap EXIT
 kill + cleanup.

**Step 2: verify** — `bash scripts/e2e-naive-user-chat.sh` boots, sends T1,
prints the reply, cleans up. (Assertions come next; a live reply here is
the smoke pass. If no provider reachable: SKIP path prints reason, exit 0.)

### Task 2: assertions A1-A6

**Objective:** Encode the finding-level contract.

**Files:**
- Modify: `scripts/e2e-naive-user-chat.sh`

**Step 1: implement** each assertion as a small function accumulating a
failure list; A4 via test -f; reply capture via the CLI's stdout.

**Step 2: verify against the CURRENT tree** — run the script. Expected at
this point (leaves 01-06 landed): ALL PASS. If A1 still trips, that is a
real integration bug — STOP and report to the orchestrator (do not weaken
assertions; leaf 01 owns the stub).

### Task 3: short-mode + CI wiring note

**Objective:** Document invocation; make -short-safe by construction.

**Files:**
- Modify: `scripts/e2e-naive-user-chat.sh` (usage header)
- Modify: `docs/workflows/agent-orchestration.md` (one paragraph: what the
  script proves, how to run it) — coordinate with orchestrator's AGENTS.md
  update in the integration phase; this leaf only writes the workflows doc.

**Step 1:** usage header with examples; no LLM creds → SKIP semantics
documented inline.

**Step 2: verify** — `bash -n scripts/e2e-naive-user-chat.sh` (syntax) and
one full run.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented; script runs end-to-end
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] Never touches the user's ~/.meept or the running daemon (pid checks,
      temp-only state dir, temp socket, free port)
- [ ] Cleans up processes + temp dirs on both success and failure paths

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Lifecycle bulletproof: trap-based cleanup, socket wait, no hardcoded ports
- [ ] Assertions map 1:1 to findings F1/F3/F5 (+context continuity)
- [ ] SKIP path honest (prints reason, exit 0) — never silent
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The user's daemon (pid 8910 at authoring time) runs on the default
  socket. ANY use of the default socket/port in this script is a bug.
- agnes free-tier rate limits (429s) may make T-turns flaky; the retry-once
  + SKIP convention covers it. A quota 429 reply should now contain the
  quota message (leaf 06) — assert it WHEN the reply is a 429 failure, else
  skip that sub-check.
- bash >= 4 required (macOS Homebrew bash at /opt/homebrew/bin/bash per
  repo hooks convention); use #!/usr/bin/env bash and avoid mapfile.
