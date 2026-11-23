# 02-RUN-LOG - 07-bench/02 acceptance integration (incremental)

Worktree daemon: usability-remediation @ 6bdb7d29 (31 commits past baseline 1cee89d7).
Bench worktree: usability-acceptance @ b92ee71 (hardened harness).

## Step 0 - Recon (done)

- Live production daemon: NONE running (no meept-daemon processes, no ~/.meept listeners). Ports 8080/8081/8082/8084/8143 free.
- Prior rig /tmp/usability-rig exists with setup_rig.py, probes, bench-results-clean baseline (3 pass / 3 fail / 1 error: u-context-two-step FAIL, u-steer-midrun PASS, u-route-research-write FAIL, u-fail-nonexistent-read PASS, u-plan-mode-request ERROR "turn stalled", u-direct-explicit PASS). Baseline HEAD 1cee89d7.
- Model present: /Volumes/LLMs/LiquidAI/LFM2.5-8B-A1B-GGUF/LFM2.5-8B-A1B-Q4_K_M.gguf.
- Hardened suite suites/usability-probe.json: 6 tasks; every task carries terminal_delivery + llm_judge; u-plan-mode-request carries plan_artifact.

## Step 1 - Build remediated daemon (T1) (done)

- go build -o /tmp/usability-rig/meept-daemon ./cmd/meept-daemon => BUILD_OK
- go build -o /tmp/usability-rig/meept ./cmd/meept => BUILD_OK
- sha256 daemon 4952dfc314ad5eaf2ffb76c293ab9231d47ac904f50a54febe45d8d21b209d44
- sha256 cli    3f481ecb23e09c53155b4106c1d8f8a5552e6a4bfa8b6020dcffd48e1936dbfe

## Step 1b - Provision fresh rig (T1 cont.) (done)

- Note: setup script wrote the rig home via its cleanup path; config/probes landed, then provenance was reconstructed from the live config (content identical).
- Fresh rig: /tmp/usability-rig-acc (fresh MEEPT_HOME, no stale DBs), ports OS-assigned: llm 55096, router 55097, http 55098 - none in the avoided live set {8080,8081,8082,8083,8084,8143}.
- Quotas disabled: llm.budget.enabled=false, quota_retry.enabled=false (verified in written meept.json5); only local providers (rig 8B GGUF + rig-router sidecar from REMEDIATED worktree source); no cloud providers.
- Probes copied from prior rig with paths rewritten to /tmp/usability-rig-acc; fixture project git-initialized.
- provenance.json: meept_head 6bdb7d295b9efdca54ba3965e8b2111fdd29b9a3, daemon_bin_sha256 4952dfc3..., cli 3f481ecb...
- Provenance files: /tmp/usability-rig-acc/provenance.json + provenance.sha256

## Step 1c - Boot rig daemon (T1) (done)

- Booted: MEEPT_HOME=/tmp/usability-rig-acc/home/.meept HOME=/tmp/usability-rig-acc/home ./meept-daemon -f (pid 41193)
- daemon.log: "daemon: running socket=/tmp/usability-rig-acc/home/.meept/meept.sock pid=41193"
- daemon.log: 2x "Runtime started and healthy" (llama-cpp:127.0.0.1:55096 + 55097)
- Classifier boot gate passed for both providers; isolation: socket is rig-only, live ~/.meept untouched.

## Step 1d - Isolation guard proven (T2 prerequisite) (done)

- With HOME=/Users/caimlas and NO MEEPT_BENCH_SOCKET override, `meept-bench run` refuses: exit 2, "run: refusing to talk to the user's live meept daemon: socket /Users/caimlas/.meept/meept.sock resolves inside the user's state dir (~/.meept)". Evidence: /tmp/usability-rig-acc/guard-test.log

## Run 1 - CONFIG DEFECT (rig provisioning, NOT daemon): kept as evidence

- My setup script enabled plans.plan_compiler_enabled=true and omitted plans.approval.require_approval=false; the PRIOR rig had plan_compiler_enabled=false + require_approval=false.
- Effect: the plan-compiler parks every trivial task awaiting plan.seal; tasks 0001/0002/0003/0004/0007 stuck state=planning; turn-watchdog reaped turns after 10m; 0/6 verdicts meaningful (stalls/timeouts). Also security.allowed_paths missed the /private/tmp canonical form: every file_write DENIED ("Path does not match any allowed path pattern" for /private/tmp/... while allowlist said /tmp/...).
- Artifacts kept: bench-results-run1-configdefect/, bench-usability-run1-configdefect.log, daemon-run1-configdefect.log (renamed).

## Config fixes applied to rig (daemon restarted each time; provenance.sha256 updated by hand-verified config)

- Fix A (02:08): plans.plan_compiler_enabled=false + plans.approval.require_approval=false (matches prior rig). Restart pid 44809, healthy 2x.
- Fix B (02:27): security.allowed_paths += /private/tmp/usability-rig-acc/** (the fence canonicalization documented in bench commit 5ac2e50). Restart pid 47882, healthy 2x.

## Run 2 (pre-fence-fix) - 0/6 PASS (evidence kept: bench-results-run2-pre_fencefix/)

- All file writes DENIED by fence (allowlist still /tmp-only): u-* tasks all fail. One useful signal: u-fail-nonexistent-read exit_zero PASS.

## Root-cause diagnosis of the residual failure (Run 3, config fully fixed)

- Run 3 (/tmp/usability-rig-acc/bench-results/, 02:39-02:45): 0/6 PASS. But NOW writes execute (u-route-research-write fetched.txt landed + file_contains PASS + judge PASS; u-direct-explicit file_write executed with success=True twice).
- SYSTEMIC DEFECT FOUND (new residual, root cause identified):
  - internal/tools/builtin/filesystem.go returns tools.ToolResult BY VALUE (lines 395/646/733/860/1032).
  - internal/agent/executor.go:1608 asserts ONLY *tools.ToolResult (pointer), so tr.Evidence is silently dropped on the live executor path (internal/tools/registry.go handles both shapes; the executor does not).
  - Effect: tool.execution.complete events carry evidence=None; the daemon job-processor toolEvidence collector records zero file-tool evidence; the NEW 06-evidence-review fileEvidenceGate (review_evidence_gate.go) correctly fail-closes every file-shaped step with "zero file evidence" even though the write succeeded on disk.
  - Secondary effect: turns fail with honest-but-wrong rejection copy; terminal_delivery then grades 0/1 because the failed relay arrives after the bench liveness/timeout window on some tasks (u-plan-mode-request stalls entirely: planner phase-parse retries then "Plan awaiting user approval" with no terminal).
- This is a genuine residual defect of the remediated tree, not a rig artifact. Per leaf instructions, failures are data: reported, not retried-until-green.

## Step 2 - Probes p1-p6 (serial) (done)

- p1 naive continuity: T1 completed + hello.txt landed (edited to "hello meept" on disk despite T2's gate rejection); T2 FAILED via evidence-gate; T3/T4 answered with stale "Prior task ... failed" prose (session recall broken in replies). probes/p1-naive-continuity.json
- p2 isolation: secret-file turn rejected by gate; recall turns returned the honest "no recorded actions" label; recall not demonstrated. probes/p2-isolation.json
- p3 failures: missing-file -> honest failed terminal with user-shaped copy (good); exit-3 shell -> evidence-gate rejection instead of exit code. probes/p3-failures.json
- p4 plan delineation: D1 gate-rejected; D2 explicit plan request produced NO plan artifact (plans count 0); D3/D4 max-iterations failures. probes/p4-plan-delineation.json
- p5 steering: chat.steer/chat.followup -> "queue not found" on both conv and thread conv; steering unusable. probes/p5-steering.json
- p6 positive control: tool-named wording ALSO gate-rejected. probes/p6-positive-control.json

## Step 3 - Regression + smoke suites (done)

- smoke: 0/3 (write-answer-file ERROR gate; repo-inventory routing mismatch coder->orchestrator though file landed; open-ended-judge ERROR gate). /tmp/usability-rig-acc/bench-results-smoke/results.jsonl
- regression: 2/9 PASS (transcript-completeness, memory-recall-marker [xfail task PASSED]); 2 FAIL (session-workdir-bound, harness-cost-row-present); 5 ERROR (evidence gate). /tmp/usability-rig-acc/bench-results-regression/results.jsonl

## Step 4 - Report written (done)

- /tmp/usability-rig-acc/REPORT-REMEDIATION-ACCEPTANCE.md
- Copied to docs/plans/20261119-usability-remediation/07-bench/02-REPORT.md (ASCII-clean verified)
- Verdict: T1 PASS, T2 PASS (execution), T3 PASS (report), OVERALL ACCEPTANCE FAIL (0/6 usability, 0/6 probes clean, 2/9 regression, 0/3 smoke)
- New residual root-caused: executor.go:1608 asserts only *tools.ToolResult; filesystem.go returns value-type ToolResult -> Evidence dropped -> 06-evidence-review gate starved and fail-closes successful writes. Suggested one-line dual-shape fix + pinning test; follow-up leaves named in the report.
- Pre-existing flags (TestGuardRetry..., TestTacticalScheduler..., 03-context T4, F-NEW-14): not re-verified here (unit suites out of scope); remain flagged.

## Step 5 - Teardown (done)

- Rig daemon killed (SIGTERM, llama-server children auto-stopped via auto_stop_on_exit); confirmed no meept-daemon/llama-server processes remain. Rig dir /tmp/usability-rig-acc kept for evidence. No commit made.

# RUN 4 - POST-FIX RE-RUN (fix: 68478b69)

## R4 Step 0 - Recon (done)

- Live daemons: NONE (no meept-daemon/llama-server processes at run start).
- Fix commit 68478b69 verified present at HEAD of worktree usability-remediation ("fix(usability): executor accepts value-type ToolResult so file-tool evidence reaches the gate", internal/agent/executor.go only, +14/-1).
- Worktree DIRTY with sibling in-progress edits (plan_draft.go draft-status event, review_manager.go failure copy, 2 test files). To keep provenance exact, built from `git archive 68478b69` into /tmp/meept-build-acc4 (clean tree of that commit). Sibling edits are NOT part of the graded binary.

## R4 Step 1 - Rebuild (done)

- Daemon: /tmp/usability-rig-acc/meept-daemon sha256 af03953d92d7644b6d6f413e36d94b18c100caf122d4e2d643abcd73381558d8 (from 68478b69 archive)
- CLI: /tmp/usability-rig-acc/meept sha256 c67d5c9275f3c38233946f8556b73ed120575107c63f76bfa0aa710ce78b5032
- Bench binary rebuilt at bench HEAD b92ee71: sha256 888bc5a19e241db6b9b43941be028df6bfccb2e49506ce2c3777fd0d751c77c1
- Run-3 binaries preserved as meept-daemon-run3 / meept-run3 / meept-bench-run3.
- Config: UNCHANGED from run 3 final state (plan_compiler_enabled=false, approval.require_approval=false, quotas off, /private/tmp allowlist present). Hashes: see /tmp/usability-rig-acc/provenance-run4.sha256.
- Fixture repo /tmp/usability-rig-acc/project: only untracked run-3 leftover files; NO stale bench worktrees/branches (rerun blocker absent).

## R4 Step 2 - Boot (done)

- pid 62317, socket /tmp/usability-rig-acc/home/.meept/meept.sock present, daemon-run4.log: 2x "Runtime started and healthy" (llm 55096 + router 55097).
- Boot log shows "Recovered stale tasks count=1" (run-3 leftover in tasks.db - state carried over, noted as a rig-difference vs run 3 fresh home).
## R4 Step 3 - Graded suite run (done)

- Command: `meept-bench run --suite suites/usability-probe.json --repo /tmp/usability-rig-acc/project --scratch /tmp/usability-rig-acc/worktrees --auto-approved --judge-cmd scripts/judge-local.py --out bench-results-run4` with JUDGE_URL=http://127.0.0.1:55096/v1 (see R4 Step 5c -- this URL was WRONG for the graded run), MEEPT_BENCH_SOCKET=rig socket.
- Verdicts: 0 pass / 2 fail / 4 error (run 3 was 0/1/5).
  - u-context-two-step: ERROR -- gate rejection "no file operation was executed" (unchanged from run 3).
  - u-steer-midrun: ERROR -- same gate rejection; note the tactical steer/followup path this time ACCEPTED (steer at +5s accepted on the step-task conversation), but the row still errored on the gate.
  - u-route-research-write: FAIL -- file_contains FAIL (fetched.txt did NOT land this run; model refused the fetch on a raw-IP security block -- model-level variance vs run 3 where it landed), judge 404 (bad URL, see 5c), terminal_delivery 0/1.
  - u-fail-nonexistent-read: FAIL -- IMPROVED from run-3 ERROR: exit_zero PASS (honest missing-file failure terminalized inside the window; the evidence gate no longer misclassifies the read step).
  - u-plan-mode-request: ERROR -- still parked: "Parsed plan output steps=9" then "Plan awaiting user approval"; no terminal in 300s (unchanged shape from run 3).
  - u-direct-explicit: ERROR -- gate rejection despite the writes landing; KEY NEW EVIDENCE: run-4 transcripts show tool.execution.complete NOW CARRIES evidence (file_exists size=2 + file_hash) -- 68478b69 works at the executor layer. Evidence still never reaches the gate (see R4 Step 4).

## R4 Step 4 - NEW root cause found (collector never wired): second defect

- The 68478b69 fix is NECESSARY but NOT SUFFICIENT. Verified: executor dual-shape assertion present in the binary's source (git archive of 68478b69), bus events carry evidence payloads with source=file_write, type=file_exists/file_hash.
- Residual defect in internal/daemon/components.go (NewComponents):
  - Line 5107: `jobProc.WithBus(c.msgBus)` guarded by `if c.msgBus != nil`.
  - Line 5483 (later in the same function): `c.msgBus = msgBus` -- the field is assigned 376 lines AFTER the wiring site.
  - At wiring time c.msgBus is still nil, so the guard skips WithBus: the toolEvidenceCollector is NEVER subscribed in the production daemon. step-job envelopes therefore never carry tool_evidence; tactical.go's "Extracted tool-issued evidence" log fires 0 times in the whole run-4 daemon log; task_steps.evidence stays NULL; review_evidence_gate.go (correct per spec) sees zero evidence and fail-closes.
  - Unit tests pass because they call WithBus(bus) directly and never exercise NewComponents' ordering.
- Suggested fix (one-line-shaped): assign `c.msgBus = msgBus` at the top of NewComponents (or move the jobProc construction/WithBus below the assignment), plus a regression test that constructs via NewComponents with a bus and asserts tool_evidence arrives on a step envelope.

## R4 Step 5 - Probes (done)

- p5 steering (rerun): chat.steer/chat.followup STILL return "queue not found" on both the raw session conversation and the derived thread conversation. Steering queue registration remains broken. UNCHANGED from run 3.
- p5 secondary observation (new): the primary turn completed with a reply listing all 5 files as created, but the file_writes were STAGED as pending changes ("Created pending change stage-... Use 'resolve' tool") and only step1.txt (from a prior probe) existed on disk -- a staged-write + claimed-success shape; the model also batched all 5 writes into ONE iteration instead of separate calls.
- u-plan approval dependency (config experiment, EXPLICITLY OUTSIDE the graded config): restarted the daemon with orchestrator.task_approval_step_threshold=0 added. Result: the approval park is GONE -- no "Plan awaiting user approval"; the plan (8 steps) went straight to execution and steps started completing. The stall is therefore caused by the COMPLEXITY GATE (task_approval_step_threshold, default 5) which fires on plans with >= 5 steps REGARDLESS of plans.approval.require_approval=false -- NOT by require_approval itself.
  - Even with threshold=0, the graded task still failed: the 8-step plan overran the 300s harness deadline ("chat turn ... context canceled"), and individual steps still failed on the same evidence-collector defect (rev-1008 "zero file evidence"). So threshold=0 removes the park but does not make the task pass under the current 300s window + collector defect.
  - Config after experiment REVERTED to the run-3 graded config for cleanliness of the recorded state (provenance-run4.json records the graded config; the experiment is documented here only).

## R4 Step 5c - Judge URL disclosure (honest)

- The graded run's llm_judge checks all failed with "judge failed: exit status 1": I passed JUDGE_URL=http://127.0.0.1:55096/v1 but judge-local.py appends /v1 itself, producing /v1/v1/chat/completions -> HTTP 404. Run 3's passing judge checks used the bare base URL.
- Re-runs with the corrected URL (bench-results-run4-rejudge, -rejudge2):
  - u-route-research-write: FAIL again (file_contains FAIL -- fetched.txt still absent; this attempt the model refused outright; terminal_delivery 0/1; judge now reached the server).
  - u-fail-nonexistent-read: FAIL again but with a REAL judged score: exit_zero PASS; the judge scored the reply shape 0.0 ("states the file is missing without attempting to fulfill"); terminal_delivery 0/1. The honest-failure behavior itself works; the reply shape grades poorly.
- Net effect of the disclosure: the two task-level FAIL verdicts are unchanged with a working judge; no verdict in the run-4 table flips because of the judge URL. The judge scores themselves (0.0 reply-shape grades) are only valid from the rejudge runs.

## R4 Step 6 - Teardown (done)

- Rig daemon killed (SIGTERM; llama-server children confirmed gone; 0 meept-daemon/llama-server processes). No commit made.

## R4 VERDICT SUMMARY (vs run 3)

| Task | Run 3 | Run 4 | Delta |
|---|---|---|---|
| u-context-two-step | error | error | unchanged (gate) |
| u-steer-midrun | error | error | unchanged (gate); steer RPC now accepted at tactical layer |
| u-route-research-write | fail | fail | unchanged verdict; file no longer lands (model variance) |
| u-fail-nonexistent-read | error | fail | IMPROVED: honest failure terminalizes in-window (exit_zero PASS) |
| u-plan-mode-request | error | error | unchanged park; root cause refined (task_approval_step_threshold) |
| u-direct-explicit | error | error | unchanged verdict; executor evidence now flows (fix verified) but collector unwired (NEW root cause) |

Usability suite: 0/6 PASS (run 3: 0/6). p5 steering: still broken. The 68478b69 executor fix is VERIFIED WORKING at the layer it touched; acceptance still FAILS because a second defect (components.go msgBus assignment order) starves the gate downstream of the fix.





# RUN 5 - POST-FIX RE-RUN (fix: 4f934dfb, bus wiring)

## R5 Step 0 - Recon (done)

- Live daemons at run start: NONE (no meept-daemon/llama-server processes).
- Fix commit 4f934dfb verified at HEAD of worktree usability-remediation
  ("fix(usability): job processor bus wiring used nil c.msgBus field -
  toolEvidenceCollector never subscribed", internal/daemon/components.go,
  +9/-2; wires jobProc.WithBus(msgBus) to the PARAMETER instead of the
  then-still-nil c.msgBus field).
- Worktree DIRTY with the same sibling in-progress edits as run 4
  (plan_draft.go, review_manager.go, 2 test files). Provenance kept exact
  by building from `git archive 4f934dfb` into /tmp/meept-build-acc5
  (clean tree of that commit); sibling edits are NOT in the graded binary.
- Bench worktree HEAD unchanged: b92ee71 (same as run 3/4).

## R5 Step 1 - Rebuild (done)

- Daemon: /tmp/usability-rig-acc/meept-daemon sha256
  0ff5db3d3ccec68f4721111e111b59e8567de5288ce65e7725196a9d238d8e29
  (from 4f934dfb archive)
- CLI: /tmp/usability-rig-acc/meept sha256
  03ff26bef2e463a0aac1aa001bd659e048f2c817ddc147ff9aa5799fafb9e0a5
- Bench binary reused from run 4 (bench tree unchanged, HEAD b92ee71).
- Config: UNCHANGED -- meept.json5 sha256
  4507535ddad3a97e9a8b0f92e06f72aea97970c56f24b131d1ca2d4726b83432 and
  models.json5 sha256
  53b2c162934cd96644ccb7ab05411ee0b67bf003eedf398aa2d4a6bf2fed29d9 both
  match the run-3/4 provenance.sha256 lines exactly
  (plan_compiler_enabled=false, approval.require_approval=false,
  orchestrator.task_approval_step_threshold left at DEFAULT (park
  expected for u-plan, graded as-is), quotas off, /private/tmp
  allowlist). Run-4 binaries overwritten in place; run-3 binaries still
  preserved as *-run3.
- Fixture repo: same untracked run-3/4 leftover files as run 4 (grading
  is per-filename; fetched.txt/direct.txt/counter.txt/result*.txt states
  noted per-task below).

## R5 Step 2 - Boot (done)

- pid 71721 (meept-daemon -f), socket
  /tmp/usability-rig-acc/home/.meept/meept.sock present, daemon-run5.log:
  2x "Runtime started and healthy" (llm 55096 + router 55097); llama
  /health 200 on 55096. Boot recovered 1 stale task from tasks.db
  ("Recovered stale tasks count=1") -- same shape as run 4.
- Provenance: provenance-run5.json + provenance-run5.sha256 (written at
  teardown).

## R5 Step 3 - Graded suite run (done)

- Command: `meept-bench run --suite suites/usability-probe.json --repo
  /tmp/usability-rig-acc/project --scratch /tmp/usability-rig-acc/worktrees
  --auto-approved --judge-cmd scripts/judge-local.py --out
  /tmp/usability-rig-acc/bench-results-run5` with
  JUDGE_URL=http://127.0.0.1:55096 (bare base URL -- the CORRECT form per
  the run-4 disclosure), MEEPT_BENCH_SOCKET=rig socket.
- Verdicts: **0 pass / 4 fail / 2 error** (run 4: 0/2/4; run 3: 0/1/5).
  - u-context-two-step: ERROR -- gate rejection, but the MECHANISM
    changed: step-0 and step-1 (both file_write) were APPROVED with real
    evidence (extracted evidence_count=2 each, tasks.db evidence=1). The
    residual rejection is now the verify step (tool_hint=file_read): the
    8B model never executed file_read (zero tool executions, 678ms
    step), so the file-evidence gate fail-closed after retry
    ("failing step terminal ... zero file evidence after retry",
    rev-1003). Infrastructure behaves per spec; model did not run the
    verify tool.
  - u-steer-midrun: FAIL (improved from error) -- the steer turn's
    file_write now lands on disk with evidence and APPROVES
    (result.txt, size=1, tasks.db approved/evidence=1), but it wrote to
    /tmp/usability-rig-acc/result.txt (rig root) instead of the repo
    root, so file_contains missed it; terminal_delivery 0/2. chat.steer
    on the step-task conv was ACCEPTED at the tactical layer (same as
    run 4). result2.txt never created.
  - u-route-research-write: FAIL -- unchanged shape: model refused the
    raw-IP fetch in seconds (wall 6.0s); fetched.txt absent;
    terminal_delivery 0/1.
  - u-fail-nonexistent-read: FAIL -- same as run-4 rejudge: exit_zero
    PASS (no file created; the model honestly reported missing and even
    ran list_directory + shell find + tool_view, all with evidence,
    evidence_count=4, step APPROVED); terminal_delivery 0/1; llm_judge
    checker ERRORED on parsing (judge actually scored the reply 0.9 --
    "clear, contextual answer stating the file does not exist" -- but
    judge-local.py could not find the score because this seed's judge
    output was prefixed with an empty <think></think> block;
    "cannot find score in judge output"). Score parity with run 4's
    rejudge note: the reply shape is now GOOD; the failing checks are
    terminal_delivery and the judge-checker parse error.
  - u-plan-mode-request: ERROR -- unchanged park: "Parsed plan output
    steps=10" -> "Plan awaiting user approval"; no terminal in 300s
    (task_approval_step_threshold still at default 5, graded as-is).
  - u-direct-explicit: FAIL (improved from error) -- direct.txt landed
    in the task worktree (file_contains PASS), tool.execution.complete
    carries file_exists size=2 + file_hash evidence, the step was
    APPROVED by review, the turn replied "task completed" with the
    result. Residual FAIL is terminal_delivery only (0/1 non-parked
    turn.terminal within 300s) plus the judge-checker parse error.

## R5 Step 4 - Collector-subscription PROOF (the point of this run)

Four independent confirmations that 4f934dfb closed the second defect
(run 4: ALL FOUR were absent/zero):

1. tactical.go's "Extracted tool-issued evidence from execution result"
   fired **7 times** in daemon-run5.log (run 4: 0 times in the whole
   run), with evidence_count=2/4 per step.
2. Bench transcripts (tool_trace): tool.execution.complete envelopes now
   CARRY tool evidence, e.g. u-direct-explicit: evidence=[{source:
   file_write, type: file_exists, value: size=2}, {source: file_write,
   type: file_hash, value: 2689367b...}] (run 4: envelopes had none).
3. tasks.db task_steps.evidence is populated (1) for every step that
   executed a tool, and those steps now terminalize APPROVED (run 4:
   evidence stayed NULL, all file-shaped steps rejected).
4. End-to-end gate behavior: file_write steps pass evidence review and
   the file lands on disk -- the exact "file-write tasks should pass
   evidence review" prediction holds (u-direct-explicit file_contains
   PASS; u-steer write approved). The two residual task failures on
   file-writing tasks are terminal_delivery/working-dir/model issues,
   NOT evidence-starvation.

Combined with run 4's proof that 68478b69 works at the executor layer,
the evidence pipeline (executor -> bus -> collector -> envelope ->
gate) is now verified end-to-end. The "Rejected: no file operation was
executed" text still appears once (u-context verify step) but that is
the gate correctly reporting a model that ran no tools -- per spec.

## R5 Step 5 - Probes (done)

- p5 steering (rerun, probes/p5_steering_run5.log + p5-steering.json):
  chat.steer STILL returns "queue not found" (code -32603) on both the
  raw conversation and the derived thread conversation -- the steering
  queue registration defect is INDEPENDENT of the bus wiring fix and
  remains broken (3rd run in a row). Primary turn terminalized
  completed and this time ALL 5 step files actually landed on disk in
  the project dir (run 4's staged-write + claimed-success shape did not
  recur; the machine-shaped-output guard also fired and rewrote the
  reply properly). The steer itself was never delivered (queue not
  found), so final.txt is absent -- steering remains unusable.

## R5 Step 5c - Judge checker parse failure (honest disclosure)

- The llm_judge CHECKER errored on 3 tasks ("judge failed: exit status
  1") -- but the judge SERVER was reached and DID score: judge-local.py
  logged "cannot find score in judge output: '<think></think> 0.9 The
  reply provides a clear, contextual answer...'" -- this seed's judge
  model prefixed its output with an empty <think></think> block and the
  script's score regex missed the score after it. JUDGE_URL was correct
  (bare base, no double /v1). No verdict in the run-5 table relies on
  an llm_judge pass; every task's fail/error verdict stands on
  file_contains / terminal_delivery / exit_zero checkers alone. The
  u-fail reply-shape score visible in the raw judge output (0.9) is
  recorded here as evidence but is not a graded pass.

## R5 Step 6 - Teardown (done)

- Rig daemon killed (SIGTERM; llama-server + prompt-router sidecar
  confirmed gone; 0 meept-daemon/llama-server processes; daemon log
  ends "daemon: stopped"). No commit made.
- Provenance: provenance-run5.json + provenance-run5.sha256 (binary
  hashes in provenance-run5.sha256.bins). Rig dir preserved with
  bench-results-run5/, daemon-run5.log, bench-usability-run5.log,
  probes/p5_steering_run5.log.

## R5 VERDICT SUMMARY (run 3 -> run 4 -> run 5)

| Task | Run 3 | Run 4 | Run 5 | Delta |
|---|---|---|---|---|
| u-context-two-step | error | error | error | unchanged verdict; write steps now approve WITH evidence; residual = model never ran the file_read verify tool |
| u-steer-midrun | error | error | fail | IMPROVED: steer write lands + approves w/ evidence; fails on wrong-dir write + terminal_delivery 0/2 |
| u-route-research-write | fail | fail | fail | unchanged (model refuses raw-IP fetch) |
| u-fail-nonexistent-read | error | fail | fail | unchanged vs run 4: exit_zero PASS, step approved w/ evidence, reply shape judged 0.9 (raw); terminal_delivery 0/1 + judge-checker parse error |
| u-plan-mode-request | error | error | error | unchanged park (10-step plan vs threshold 5, graded as-is) |
| u-direct-explicit | error | error | fail | IMPROVED: file_contains PASS, step approved w/ hash evidence, completion reply delivered; residual = terminal_delivery 0/1 |

Usability suite: 0/6 PASS (run 4: 0/6, run 3: 0/6) but composition
improved again: 2 error / 2 fail -> 2 error / 4 fail / 0 pass, with two
tasks (u-direct-explicit, u-steer-midrun) now failing only on
terminal-delivery/working-dir shaping rather than evidence starvation.
p5 steering: still broken (independent defect, unchanged across runs).

BOTTOM LINE: the 4f934dfb bus-wiring fix is VERIFIED WORKING -- the
toolEvidenceCollector now subscribes, tool_evidence flows end-to-end,
and the evidence-review gate approves real file steps. Both root-cause
defects from runs 3/4 (executor value-type drop; nil c.msgBus wiring)
are closed. Remaining acceptance failures are a DIFFERENT class:
turn-terminal delivery (terminal_delivery 0/1 on every completed task),
p5 steering queue registration, the u-plan complexity-gate park, and
8B-model behavior (raw-IP refusal, skipped verify tool, wrong-dir
writes). OVERALL ACCEPTANCE: **STILL FAIL (0/6 pass)** -- infrastructure
defects from runs 3/4 are fixed and proven; the residual blockers are
the steering queue defect and turn-terminal delivery, which the
evidence pipeline fixes do not address.

# RUN 6 (FINAL) - 2026-11-21, post turn.terminal-subscription fix

## R6 Step 0 - Recon (done)

- Rig daemons: NONE running at start (no meept-daemon / llama-server /
  prompt-router processes; result.txt from run 5 still in rig root).
- meept worktree: usability-remediation @ 4f934dfb (daemon source
  unchanged since run 5 -- no meept-side fix in this run).
- Bench worktree: usability-acceptance HEAD **d334cc7**, 4 commits past
  run-5 bench b92ee71:
  - fab82fb fix(bench): runner subscribes turn.terminal (THE structural
    terminal_delivery fix; runner.go:255 subscription list now includes
    turn.terminal so transcript traces carry terminal rows)
  - 1258fa5 + 140dd25 + d334cc7: routing fake/fixture updates matching
    the dual-event shape.
- NEW defect discovered at recon, in the BENCH judge harness (seen in
  run 5 but not root-caused there): scripts/judge-local.py could not
  parse judge scores prefixed with an empty <think></think> block
  ("cannot find score in judge output: '<think></think> 0.9 ...'").
  Patch applied (working-tree only, NOT committed): strip
  `<think>\s*</think>\s*` before the score regex. Verified:
  scripts/test_judge_local.py selftest PASS, plus a regex unit check on
  the exact run-5 failing string -> ('0.9', 'The reply provides...').
  No other harness changes.

## R6 Step 1 - Provenance + build (done)

- meept worktree: unchanged at 4f934dfb since run 5 (daemon sha256
  0ff5db3d... == provenance-run5; cli 03ff26be... match). No rebuild.
- Bench worktree at d334cc7; bench binary REBUILT:
  /tmp/usability-rig-acc/meept-bench-run6 sha256
  e3cdc217884a272b35fdbffb51cabd21c2f6a5c631c5cc74a779ea0331e1919e
  (the run-5 bench binary 888bc5a1... is pre-fab82fb and was NOT used).
- Config: meept.json5 4507535d... + models.json5 53b2c162... --
  byte-identical to the graded run-3/4/5 state (provenance.sha256 lines
  match exactly). Quotas off, thresholds default, JUDGE_URL bare base.
- Judge harness: + the working-tree <think>-strip patch (Step 0).

## R6 Step 2 - Boot (done)

- pid 86364, socket present; 2x "Runtime started and healthy" (llm
  55096 + router 55097); llama /health 200; router /health 200.
- Recovered stale tasks count=1 (same as runs 4/5).

## R6 Step 3 - Graded usability suite (done) -- 0 pass / 4 fail / 2 error

- Command: meept-bench-run6 run --suite suites/usability-probe.json
  --repo project --scratch worktrees --auto-approved --judge-cmd
  scripts/judge-local.py --out bench-results-run6; HOME=/Users/caimlas
  + MEEPT_BENCH_SOCKET=rig socket (isolation guard satisfied; the first
  attempt with HOME=<rig home> was refused BY DESIGN because the rig
  socket then resolves inside "$HOME/.meept" -- guard behavior correct).
- Per-task verdicts (run 3 -> 4 -> 5 -> 6):

| Task | R3 | R4 | R5 | R6 | Detail |
|---|---|---|---|---|---|
| u-context-two-step | error | error | error | fail (IMPROVED) | step1 write+verify APPROVED w/ evidence; file_contains FAIL: steer-turn revision wrote "5" then failed hash-validation ("[unverified: no tools were executed this turn]" claims); primary terminal completed at+15s (terminal_delivery 1/2 -- the steer turn's own terminal missing); llm_judge PASS 0.9 |
| u-steer-midrun | error | error | fail | fail | steer write result.txt (contains 7) APPROVED w/ evidence IN THE WORKTREE; file_contains FAIL (checker wants result2.txt -- the steer was never delivered, see Step 4); terminal_delivery PASS 2/2 (primary failed + turn[0] completed); llm_judge PASS |
| u-route-research-write | fail | fail | fail | fail | unchanged: model refuses raw-IP fetch (wall 5.1s); fetched.txt absent; terminal_delivery PASS 1/1 (completed, non-parked); llm_judge FAIL (reply judged empty) |
| u-fail-nonexistent-read | error | fail | fail | error (REGRESSED) | honest missing-file reply path hit the evidence gate this seed: rev step "zero file evidence after retry" -> task failed; terminal_delivery would PASS (failed terminal at+10s) but harness graded the gate rejection as ERROR before checks ran |
| u-plan-mode-request | error | error | error | error | unchanged park: 9-step plan vs task_approval_step_threshold=5 (default, graded as-is); 302s no terminal |
| u-direct-explicit | error | error | fail | fail | file_write DENIED by fence this run: the coder wrote to /private/tmp/usability-u-direct-explicit-a1/direct.txt (NONEXISTENT dir -- bench harness cleaned the worktree dir layout differently this run); "Path does not match any allowed path pattern"; gate correctly rejected zero-evidence step; terminal_delivery PASS 1/1; llm_judge 0.2 (empty reply judged) |

- SUITE: 0 pass / 4 fail / 2 error (run 5: 0/4/2 -- same counts,
  different composition). llm_judge checker PARSE ERROR IS GONE (the
  <think> patch works: scores now parse; u-context judge PASS 0.9,
  u-direct 0.2, u-route empty-reply fail -- all parsed).

## R6 Step 4 - terminal_delivery FLIP VERIFIED (the point of this run) -- CONFIRMED

Prediction held. Bench transcripts now carry turn.terminal rows:

1. run-6 transcript u-direct-explicit: 2 turn.terminal rows (parked
   + completed) -- run-5 SAME transcript: **0 rows** (grep -c = 0).
2. Per-task terminal_delivery: u-steer 2/2 PASS, u-route 1/1 PASS,
   u-direct 1/1 PASS, u-context 1/2 (primary PASS, steer-turn
   terminal missing), u-fail n/a (harness ERROR pre-checks),
   u-plan n/a (park, no terminal = correct fail signal).
3. The fab82fb one-line subscription fix is proven: the daemon fired
   turn.terminal all along (runs 3-5); the runner just was not
   listening. The checker now sees non-parked terminals in-window on
   every task whose turn actually terminalizes.
4. REMAINING terminal_delivery gap is now only the PARK class:
   u-plan (complexity gate) and late-steer turns still have no
   terminal to deliver -- a daemon-side behavior, not a harness bug.

## R6 Step 5 - p5 steering probe (done) -- STILL BROKEN (4th run)

- chat.steer/chat.followup: "queue not found" (-32603) on raw conv AND
  derived thread conv -- unchanged across runs 3/4/5/6. queue_status
  RPC responds (is_active=false, depths 0) so queues EXIST as state
  but are not registered for the primary conversation.
- Primary turn DID complete with all 5 files landing in project/
  (step1-5.txt); the steer never arrived, so final.txt absent and the
  reply lists all 5 files (steer would have skipped 4/5).
- Independent of every fix tested so far; remains a standalone defect.

## R6 Step 6 - Teardown (done)

- Rig daemon killed (SIGTERM; llama-server + prompt-router sidecar
  confirmed gone; 0 meept-daemon/llama-server processes).
- Provenance: provenance-run6.json + provenance-run6.sha256 written.
- Rig dir preserved: bench-results-run6/, daemon-run6.log,
  bench-usability-run6.log, probes/p5_steering_run6.log,
  meept-bench-run6 binary. NO COMMIT made (bench worktree carries the
  judge-local.py <think> patch as an UNCOMMITTED working-tree edit).

## R6 VERDICT SUMMARY (runs 3 -> 4 -> 5 -> 6)

See 02-REPORT.md section 13 for the final table.

# RUN 7 (FINAL VALIDATION) - 2026-11-22, post steering-wiring + auto-seal + auto-approver fixes

## R7 Step 0 - Recon (done)

- Rig daemons: NONE running at start (no meept-daemon process on the rig
  socket; llama /health and router /health both 000 -- no llama-server
  or prompt-router processes).
- An UNRELATED live e2e sweep was running in the main meept worktree
  (another session's `scripts/e2e-affected.sh` + its temp-bin daemons in
  $TMPDIR). To keep provenance exact, run 7 again built from
  `git archive <HEAD>` into /tmp/meept-build-acc7 (clean tree), same
  protocol as run 5. The dirty working tree was NOT built.
- meept main worktree HEAD: **77fa2184** (past 11eabc94; contains ALL
  claimed fixes: steering wiring ab72e851 + residual wave, auto-seal
  bacb4352, executor evidence 68478b69, bus wiring 4f934dfb). Worktree
  dirty with sibling-session edits; excluded from the build by archive.
- Bench worktree usability-acceptance HEAD: **a85b074** (auto-approver),
  CLEAN working tree -- the run-6 judge-local.py <think>-strip patch is
  now COMMITTED (21dd263). 5 commits past run-6 bench d334cc7.
- Suite state: suites/usability-probe.json already carries
  `extra.auto_approve_plans: true` on u-plan-mode-request (line 69) --
  no config edit needed; the runner-side plan auto-approver is opt-in
  per task (internal/suite/suite.go:86, internal/runner/runner.go:346).

## R7 Step 1 - Provenance + build (done)

- Daemon: /tmp/usability-rig-acc/meept-daemon REBUILT from 77fa2184
  archive, sha256 f1a7a97532299c62fb8657f577dddea842a227a15164d414bc2caa95224895b6
- CLI: /tmp/usability-rig-acc/meept REBUILT, sha256
  6e369d755dddaf76cc6b26d308c07f86ae8c9cf2772ea5abcd43f309118b1cc3
- Bench: /tmp/usability-rig-acc/meept-bench-run7 REBUILT from a85b074,
  sha256 f7eac0b7af04fa239951e1277620c7440bec7fdcc17475c4dc8dcda261e65e91
- Config: meept.json5 4507535d... + models.json5 53b2c162... --
  byte-identical to the graded run-3/4/5/6 state (provenance.sha256
  lines match exactly). Quotas off, thresholds default,
  approval.require_approval=false, /private/tmp allowlist, JUDGE_URL
  bare base. Judge harness: committed <think>-strip (21dd263), no
  working-tree patch this run.

## R7 Step 2 - Boot (done, after one operator error -- disclosed)

- FIRST boot attempt was MISCONFIGURED BY THE OPERATOR: daemon launched
  without MEEPT_HOME, so it loaded the LIVE ~/.meept home and spawned
  llama-server children on the live ports 8080/8084. The daemon was
  killed within ~40s of spawn and its two llama-server children
  (confirmed parented by the misconfigured daemon via
  --supervise-parent) were killed as well. No live service was touched:
  recon had already shown zero pre-existing llama-server processes, and
  post-kill verification shows ports 8080/8084 free, 0 llama-server /
  prompt-router processes remaining. Incident logged here per honesty
  rules.
- CORRECT boot: MEEPT_HOME=/tmp/usability-rig-acc/home, daemon pid and
  socket below; config path resolved inside the rig (see log line).
- Boot attempt 2 (MEEPT_HOME=rig/home) failed: daemon FATAL
  "models.json5 not found" -- correct MEEPT_HOME for this rig is the
  .meept dir itself (matches run-6 log: config loaded from
  home/.meept/models.json5).
- Boot attempt 3 stalled at the classifier boot gate: rig-router
  runtime (prompt-router sidecar) never came up. ROOT CAUSE (rig
  drift, not a daemon defect): models.json5 lifecycle spawn_command
  points at
  .hermes/worktrees/usability-remediation/scripts/prompt_router_sidecar.py
  -- that worktree was DELETED Nov 21 22:14 after its fixes were merged
  to main. Runs 3-6 booted fine because the worktree still existed
  then; the "byte-identical config" is no longer runnable in the
  current environment. Minimal repair: single-line repoint of the
  spawn_command to the same script in the main worktree
  (/Users/caimlas/git/meept/scripts/prompt_router_sidecar.py, sha256
  44a0ed56...). models.json5 sha256 changes 53b2c162 -> d6f53039;
  disclosed here as a required environment repair. No other config
  change. MEEPT_HOME recorded for reproducibility:
  MEEPT_HOME=/tmp/usability-rig-acc/home/.meept.

## R7 Step 2b - Boot (done)

- pid 75491, socket /tmp/usability-rig-acc/home/.meept/meept.sock;
  daemon-run7.log: 2x "Runtime started and healthy" (llm 55096 +
  router 55097); llama /health 200; router /health 200; classifier
  boot gate signaled; "daemon: running". Orphan sweep reaped=0 (rig
  DBs carried no stale tasks this time -- the runs-4/6 stale-task
  recovery line does not appear).

## R7 Step 3a - Graded suite, ATTEMPT 1: ABORTED BY OPERATOR TIMEOUT (disclosed)

- The suite was launched via a foreground shell call with a 420s cap;
  the call TIMED OUT at 420s and killed the bench process mid-run.
  State at kill: 5/6 tasks complete (u-context FAIL, u-steer ERROR,
  u-route FAIL, u-fail FAIL, u-plan TIMEOUT-at-240s -- the task-level
  deadline, not the rig), u-direct-explicit never started.
  results written: bench-results-run7/ (5 transcripts). This attempt is
  NOT the graded run; a clean re-run follows (attempt 2). Notable
  observations from the aborted attempt, all re-verified in attempt 2:
  - u-steer-midrun: steer turn accepted via chat.steer (steering
    wiring works), but the steer turn errored with
    "task_create: name is missing" x3 -- the 8B model misused the
    task_create schema on the steered continuation.
  - u-plan-mode-request: plan parked at "Plan awaiting user approval"
    (23:41:52.928, task ...0005, steps=10) but NO
    "plan auto-approve" line ever appeared in the bench log before the
    kill -- the 240s task deadline (u-plan declares timeout_seconds
    420 but the runner's chat budget for this task was 240s + ext;
    verdict "timeout ... context canceled") fired BEFORE the park was
    detected/approved, OR the approver did not detect this park.
    Attempt 2 verdict decides this prediction.

## R7 Step 3 - Graded usability suite, ATTEMPT 2 = GRADED RUN (done) -- 0 pass / 4 fail / 1 error / 1 timeout... see verdict line

- Command: MEEPT_BENCH_SOCKET=rig socket, JUDGE_URL=http://127.0.0.1:55096
  (bare base), HOME=/Users/caimlas, /tmp/usability-rig-acc/meept-bench-run7
  run --suite suites/usability-probe.json --repo project --scratch
  worktrees --auto-approved --judge-cmd scripts/judge-local.py --out
  bench-results-run7-final. Ran to completion (SUITE_EXIT=1, "run: 5/6
  tasks failed"). FIRST USABILITY PASS EVER: u-direct-explicit PASS.
- Per-task verdicts (R3 -> R4 -> R5 -> R6 -> R7):

| Task | R3 | R4 | R5 | R6 | R7 | Detail |
|---|---|---|---|---|---|---|
| u-context-two-step | error | error | error | fail | fail | file_contains FAIL (counter.txt absent -- the steer/chat-fallback turn dispatched a PLAN that parked, task ...0007 awaiting_approval; the "add 4" instruction never executed as a direct write); terminal_delivery 1/2 (primary completed; steer turn missing); llm_judge PASS 0.9 |
| u-steer-midrun | error | error | fail | fail | error | chat.steer still "queue not found" (-32603) on all 3 candidates at 23:48:18 (runner logged accepted:true via chat-fallback semantics but the transcript carries the steer error; the steered correction NEVER reached the running turn); the daemon instead ran the PRIMARY task's own single-step pipeline: rev step wrote result.txt (not result2.txt) with REAL file_write evidence, approved; then the task STALLED -- no Task finalized, step scheduling went silent at 23:48:28, turn watchdog reaped the primary turn at 23:58:44 ("no progress for 10m"); bench errored on liveness stall at wall 321.8s BEFORE the reap terminal |
| u-route-research-write | fail | fail | fail | fail | fail(2/3 IMPROVED) | fetched.txt NOW PRESENT with matching pattern (file_contains PASS, first time ever); terminal_delivery PASS 1/1 at+4.4s; llm_judge FAIL (empty reply judged -- the reply-shape residual) |
| u-fail-nonexistent-read | error | fail | fail | error | error | agent execution failed via the evidence envelope path ("job ... completed by agent coder: Based on the tool execution results..."); harness graded ERROR before checks; terminal_delivery would have passed (failed terminal in-window) |
| u-plan-mode-request | error | error | error | error | timeout (AUTO-APPROVER DID NOT FIRE) | plan parked at "Plan awaiting user approval" 23:54:57 (task ...0010, steps=10, 6 steps variant in attempt 1); bench stayed silent for the whole 540.7s wall (420s task budget + 120s AutoApproveLivenessExtension), "context deadline exceeded"; task still awaiting_approval NOW; manual task.approve via RPC returns {"status":"approved"} so the DAEMON approval surface works |
| u-direct-explicit | error | error | fail | fail | **PASS** | file_contains PASS (direct.txt); terminal_delivery PASS 1/1 at+5.5s; llm_judge PASS 0.9; the dispatched 1-step task wrote the file with real evidence (evidence_count=2), heuristic-approved, completed in 6s wall |

- SUITE: 1 pass / 4 fail / 1 error + 1 timeout counted inside the fails
  (u-plan timeout verdict; runner prints 5/6 failed). Composition:
  u-direct-explicit FLIPPED GREEN; u-route file_contains + terminal
  now PASS (only judge shape fails); terminal_delivery green on every
  task with an in-window terminal.

## R7 Step 4 - p5 steering probe (done) -- STILL "queue not found" (5th consecutive run)

- chat.steer on both candidates (raw conv + derived thread conv):
  {"code": -32603, "message": "queue not found"} -- IDENTICAL to runs
  3/4/5/6. The prediction (p5 steer returns "queued") DID NOT HOLD.
- Primary turn completed normally (all 5 step files created, honest
  list reply); the steer never arrived, final.txt absent. Probe JSON +
  log preserved (probes/p5-steering.json, probes/p5_steering_run7.log).
- Note: the ab72e851 wiring fix + b16b6082 canonical-identity wave are
  both IN the graded binary (built from 77fa2184), and the step-job
  alias-arming seam (components.go ArmQueueAliases) still shows ZERO
  "Step job queue aliases armed" lines in the daemon log across the
  whole run -- every steer attempt hit resolveQueue -> ErrQueueNotFound.
  This is now a RUN-7 ROOT-CAUSE CANDIDATE, not just a symptom: either
  the alias-arming path is not reached on this dispatch shape
  (isStepJob linked_sessions empty?) or the queue registration window
  is shorter than the steer delay in ALL probe shapes.

## R7 Step 5 - u-plan auto-approver root-cause (done, from evidence)

- The bench-side auto-approver (a85b074) NEVER FIRED in either attempt:
  zero "plan auto-approve" lines in both bench logs, while BOTH
  attempts' plans parked ("Plan awaiting user approval", tasks
  ...0005 / ...0007 / ...0010, still awaiting_approval in
  task.list_extended right now).
- ROOT CAUSE (bench<->daemon contract mismatch, new finding F-BENCH-R7):
  daemonclient.go:963 gates the approver on
  strings.Contains(p.Error, "Plan awaiting user approval") -- but the
  REAL daemon parked turn.terminal payload carries error="" and the
  marker text only inside the ack REPLY ("## starting task..." ack
  prose does not even contain the marker; the marker exists only in
  the daemon log line and task.pending_approval bus event). The
  a85b074 unit test drove a FAKE whose parked payload carried the
  marker in the error field -- the fake does not match the real
  daemon shape, so the seam was never exercised end-to-end.
- The daemon approval surface itself works: a manual
  task.approve {task_id: task-...0010} from the probe client returns
  {"status": "approved", ...} over the same socket.
- VERDICT for the u-plan prediction: infra path (park detection ->
  approve RPC -> resume) is UNPROVEN end-to-end; the graded outcome is
  a timeout (graded fail), but the root cause is the bench seam, not
  the daemon park.

## R7 Step 6 - Teardown (done)

- Rig daemon killed (SIGTERM via kill of the tracked pid; llama-server
  + prompt-router confirmed gone; 0 meept-daemon / llama-server /
  prompt-router processes; ports 55096/55097/55098 free).
- Provenance: provenance-run7.json + provenance-run7.sha256 written.
- Rig dir preserved: bench-results-run7/ (aborted attempt),
  bench-results-run7-final/ (GRADED), daemon-run7.log,
  bench-usability-run7.log (aborted), bench-usability-run7-final.log,
  probes/p5_steering_run7.log + p5-steering.json (run-7),
  meept-bench-run7 binary. NO COMMIT made.
- The models.json5 repair (d6f53039) and the manual task.approve of the
  parked u-plan task are rig-side evidence actions, both logged above.

## R7 VERDICT SUMMARY

See 02-REPORT.md section 14 for the runs 3-7 table and final verdict.
PREDICTION SCORECARD (run 7):
1. p5 steer returns queued + correction lands: REFUTED (5th consecutive
   "queue not found"; correction never landed).
2. u-plan park -> auto-approve -> executes -> terminal: REFUTED as
   built (approver never fired -- marker-seam mismatch F-BENCH-R7);
   the daemon-side park + manual task.approve path IS proven working.
3. File-write tasks pass evidence review with real tool_evidence:
   HELD (u-direct-explicit PASS end-to-end; u-steer rev step approved
   with real file_write evidence; u-route wrote fetched.txt with
   evidence; u-fail executed real tools).
