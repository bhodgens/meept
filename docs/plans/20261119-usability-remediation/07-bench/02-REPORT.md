# 07-bench/02 ACCEPTANCE REPORT - Integration Run (2026-11-21)

Plan: 20261119-usability-remediation. Leaf: 07-bench/02-integration-run.md.
Verdict author: acceptance integration agent. **This is an honest FAIL report.**
Incremental evidence log: 02-RUN-LOG.md (same directory).

## 1. Rig provenance

- Rig dir: /tmp/usability-rig-acc (fresh; setup mirrored /tmp/usability-rig/setup_rig.py)
- Daemon binary: /tmp/usability-rig-acc/meept-daemon, sha256 4952dfc314ad5eaf2ffb76c293ab9231d47ac904f50a54febe45d8d21b209d44
- CLI binary: /tmp/usability-rig-acc/meept, sha256 3f481ecb23e09c53155b4106c1d8f8a5552e6a4bfa8b6020dcffd48e1936dbfe
- Built from: /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation @ 6bdb7d295b9efdca54ba3965e8b2111fdd29b9a3 (ALL 16 impl leaves committed GREEN per orchestrator)
- Bench: /Users/caimlas/git/meept-bench/.hermes/worktrees/usability-acceptance @ b92ee71 (hardened harness, 07-bench/01), bench binary sha256 in /tmp/usability-rig-acc/provenance.sha256 alongside
- Model: LFM2.5-8B-A1B-Q4_K_M.gguf via llama-server (deps build), ports llm 55096 / router 55097 / http 55098 (OS-assigned, none in the avoided live set)
- Socket: /tmp/usability-rig-acc/home/.meept/meept.sock -- no live production daemon existed at run start (verified: no meept-daemon processes, no ~/.meept listeners)
- Quotas: DISABLED, proven in written config: llm.budget.enabled=false, quota_retry.enabled=false; only local providers (rig 8B + rig-router sidecar built from the remediated worktree source); no cloud providers
- Boot health: "daemon: running socket=... pid=47882" + 2x "Runtime started and healthy" in daemon.log
- Full provenance: /tmp/usability-rig-acc/provenance.json + provenance.sha256 (post-final-config)

## 2. Isolation guard proven

`meept-bench run` with HOME=/Users/caimlas and no MEEPT_BENCH_SOCKET override:
exit code 2, "run: refusing to talk to the user's live meept daemon: socket
/Users/caimlas/.meept/meept.sock resolves inside the user's state dir (~/.meept)".
Evidence: /tmp/usability-rig-acc/guard-test.log. (T2 requirement met.)

## 3. Battery verdict per task (T2/T3) -- overall: FAIL

Suites run serially through the hardened harness (--auto-approved,
--judge-cmd scripts/judge-local.py, JUDGE_URL=rig llm port,
MEEPT_BENCH_TRANSCRIPT_DIR pointed at the run transcript dir).

### 3a. Hardened usability-probe suite (final config, run 3) -- 0/6 PASS

Baseline = /tmp/usability-rig/bench-results-clean/results.jsonl (clean run, HEAD 1cee89d7: 3 pass / 3 fail / 1 error).

| Task | Baseline | This run | Checker detail |
|---|---|---|---|
| u-context-two-step | fail | ERROR | step rejected by file-evidence gate ("no file operation was executed for this step, even after a retry") although counter.txt writes were attempted; terminal_delivery not graded |
| u-steer-midrun | pass | ERROR | same file-evidence-gate rejection |
| u-route-research-write | fail | fail (IMPROVED) | file_contains PASS (fetched.txt landed; baseline: pattern not found), llm_judge PASS; terminal_delivery FAIL 0/1 (no non-parked turn.terminal within 300s) |
| u-fail-nonexistent-read | pass | ERROR | REGRESSED: file-read step rejected by the same gate (baseline passed with the honest "does not exist" reply) |
| u-plan-mode-request | error | ERROR | turn stalled: no terminal event; daemon planned 8 steps then "Plan awaiting user approval" with require_approval=false and never terminalized (plan_artifact + terminal_delivery not graded) |
| u-direct-explicit | pass | ERROR | REGRESSED: file_write executed successfully TWICE (transcript tool.execution.complete success=true) yet the step was rejected for "zero file evidence" |

### 3b. Probes p1-p6 (serial, /tmp/usability-rig-acc/probes/) -- 0/6 clean

| Probe | Result |
|---|---|
| p1 naive continuity | T1 create completed AND hello.txt landed ("hello meept" after T2 edit attempt); T2 edit FAILED via the same evidence-gate rejection despite the file actually being modified; T3/T4 completed but reply is "Prior task: ... status: failed" prose (context continuity BROKEN: session history not surfaced into the reply) |
| p2 isolation | FAILED: secret-file turn rejected by evidence gate; both recall turns returned the "turn finished without a final summary and without any recorded actions" label -- cross-turn memory recall not demonstrated |
| p3 failures | PARTIAL: missing-file task honestly failed with user-shaped failure copy (good); exit-3 shell task rejected by the evidence gate instead of reporting exit code 3 |
| p4 plan delineation | FAILED: D1 direct-write rejected by gate; D2 explicit plan request produced NO plan artifact (plans count 0); D3/D4 failed "maximum iterations reached"; no plan.md artifact anywhere |
| p5 steering | FAILED: chat.steer/chat.followup return "queue not found" on both the session conversation and the resolved thread conversation; steering path unusable on this rig |
| p6 positive control | FAILED: same tool-named wording as p1 T1/T2 also rejected by the evidence gate (baseline p6 vs p1 contrast is now moot; the gate rejects regardless of wording) |

### 3c. Regression suite (dev gate) -- 2/9 PASS

PASS: transcript-completeness, memory-recall-marker (the xfail-flagged memory-recall-marker task PASSED this run).
FAIL: session-workdir-bound, harness-cost-row-present (file not produced).
ERROR (file-evidence gate): file-write-routes-coder, tool-hint-coder-maps-coder, pending-write-direct, memory-store-marker, bus-trace-arrives.

### 3d. Smoke suite -- 0/3 PASS

write-answer-file ERROR (gate), repo-inventory FAIL (routing mismatch expect coder, observed orchestrator; file itself landed), open-ended-judge ERROR (gate).

## 4. Terminal-delivery (R6) summary

Green for exactly one submitted turn across the whole battery:
u-route-research-write's file/judge checks passed but its terminal_delivery
checker FAILED (0/1 terminals within the deadline window). No task in any
suite achieved a delivered terminal + passing checks combination. The
file-evidence-gate rejection relay (task_completed_relay, status=failed)
DOES arrive as a terminal for many tasks but lands after the harness
liveness/timeout window in the usability suite (they grade as ERROR, not
failed-terminal deliveries).

## 5. CRITICAL finding matrix (F-ORCH-1..29 disposition)

The run-3 failure mode is dominated by ONE new systemic defect that
supersedes per-finding grading for the file-write tasks; the full 29-row
matrix is NOT claimable from this run and is NOT fabricated here.
Findings that are demonstrably still FIXED on this rig:

- Failure-path surfacing (F-NEW-13 shape): p3 missing-file task produced an
  honest failed terminal with user-shaped copy (fixed as designed).
- Fabricated-success guard: p1/ev5/ev6 probes where the model hallucinated a
  write with zero tool calls produced the honest "turn finished without ...
  any recorded actions" label instead of a fake success (fixed as designed).
- Quota/quota-retry paths: quotas disabled by config; no quota noise observed
  (correct absence).

Findings rendered UNVERIFIABLE (blocked by the new defect): all
file-artifact-producing checks (context two-step, steering, direct-explicit,
plan artifacts, smoke write, regression write tasks).

## 6. NEW residual defect (root cause identified) -- re-open owner required

**Defect: file-tool Evidence is dropped on the live executor path, so the new
06-evidence-review fileEvidenceGate fail-closes every file-shaped step even
when the write succeeded on disk.**

- internal/tools/builtin/filesystem.go returns `tools.ToolResult` BY VALUE
  (lines 395, 646, 733, 860, 1032).
- internal/agent/executor.go:1608 asserts ONLY `*tools.ToolResult` (pointer):
  `if tr, ok := toolResult.(*tools.ToolResult); ok` -- the value type misses
  the assertion, `tr.Evidence` is dropped, `ExecutionResult.Evidence` stays
  empty, and `publishToolComplete` ships `tool.execution.complete` with no
  evidence payload (verified in run-3 transcripts: file_write success=true,
  evidence=None).
- internal/tools/registry.go:381-409 handles BOTH shapes; the executor does
  not. The daemon job-processor toolEvidence collector therefore records zero
  file-tool executions; review_evidence_gate.go (06-evidence-review, correct
  per its spec) sees zero file-tool evidence and rejects: "Rejected: no file
  operation was executed for this step, even after a retry."
- Net effect: every successful write is followed by a false-negative gate
  rejection; tasks terminalize as failed; the chat user hears "the task
  failed" for work that actually happened. This ALSO regresses
  u-fail-nonexistent-read and p3-shell (evidence gate misclassifies reads and
  non-write steps whose descriptions mention a file).
- Suggested fix (one-line-shaped): at executor.go:1608 also assert the value
  type (`toolResult.(tools.ToolResult)`) -- mirror registry.go's dual-shape
  handling -- and add a regression test pinning a VALUE-returned ToolResult
  with Evidence through ExecuteTool -> publishToolComplete.
- Suggested follow-up leaves:
  1. Fix the executor dual-shape assertion + pinning test (owner: 01-safety
     or 03-context, max 3 cycles per plan rules).
  2. u-plan-mode-request stall: plan mode with plans.approval.require_approval=false
     still parked the task at "Plan awaiting user approval" and never
     terminalized (planner phase-parse retries -> single-phase fallback ->
     approval wait with no auto-approval). Needs a terminal or auto-seal path
     for non-interactive approval-off runs.
  3. chat.steer/chat.followup "queue not found" (p5): steering queue
     registration never happens for chat.submit turns on this config; p5 is
     unrunnable until queues register under the resolved thread conversation.
  4. session-history recall in replies (p1 T3/T4 answered with stale "Prior
     task" prose, not current session state): 03-context/01 recall fidelity
     did not survive the live model path.

## 7. Comparison vs clean baseline (honest)

- Baseline (1cee89d7, clean run): 3 pass / 3 fail / 1 error.
- This run (6bdb7d29): 0 pass / 1 fail (improved) / 5 error.
- The single improvement (u-route-research-write file content) is REAL: the
  fetch-and-save pipeline works and the reply-shape judge passes.
- The regression is REAL and systemic: the remediation's own evidence gate,
  starved by the executor evidence-dropping bug, converts previously passing
  file tasks into failures. Zero CRITICAL reproductions were observed in the
  baseline sense (fabricated success, quota masking), but the tree does NOT
  pass its own usability battery: acceptance FAILS.

## 8. Pre-existing flagged failures

- TestGuardRetry_RetryParksSkipsPostTurnPipeline,
  TestTacticalScheduler_VacuousValidatorPassNotValidated, unpinned
  03-context T4, F-NEW-14: NOT re-verified in this leaf (out of scope; this
  run exercised the black-box battery, not the go unit suites). They remain
  flagged as pre-existing per the orchestrator's list; no new evidence here.

## 9. Limitations

- GUI pixel-pass verification remains operator-pending (master Q3/I4); only
  the API/TUI-level surfaces were exercised.
- Runs 1-2 were invalidated by MY rig provisioning config (plan compiler on,
  approval key missing, /private/tmp allowlist gap) -- kept as evidence
  (bench-results-run1-configdefect, bench-results-run2-pre_fencefix); run 3
  is the only graded run, on the corrected config matching the prior rig.
- The judge (scripts/judge-local.py) called the rig llama-server with
  temperature 0; judge verdicts are as deterministic as that server allows.
- No commit made, per leaf instructions. Rig daemons killed at end of run.

## 10. VERDICT

- T1 (build + boot): PASS -- provenance recorded, rig healthy, quotas off,
  isolation guard proven.
- T2 (run battery to verdict): PASS on execution (every task/probe ran and
  produced verdicts or honest errors; no harness-error rows fabricated),
  with the steering queue RPC as the only probe that could not exercise its
  target path (infrastructure "queue not found").
- T3 (compare + report): PASS (this report, honest FAIL content).
- OVERALL ACCEPTANCE: **FAIL** -- the remediated tree does not pass the
  hardened usability battery (0/6 usability, 0/6 probes clean, 2/9
  regression, 0/3 smoke). Primary residual: executor evidence-dropping bug
  (section 6) starving the new evidence gate. Root-cause analysis and a
  suggested one-line fix are included; per plan rules the owning branch
  should be re-opened (max 3 cycles).

## 11. Post-fix re-run (run 4, 2026-11-21) -- still FAIL

Re-run scope: rebuild with the fix commit, re-run the FAILED tasks (the
file-write gate tasks + p5 steering + u-plan-mode-request), capture
verdicts. Full per-step evidence: 02-RUN-LOG.md "RUN 4" section.

### 11a. Rig provenance (run 4)

- Fix commit: 68478b69 ("executor accepts value-type ToolResult so
  file-tool evidence reaches the gate", internal/agent/executor.go,
  +14/-1) at HEAD of worktree usability-remediation.
- Build: from `git archive 68478b69` (clean tree; the worktree carried
  sibling in-progress edits -- plan_draft.go draft-status event,
  review_manager.go failure copy -- which are NOT in the graded binary).
- Daemon sha256 af03953d92d7644b6d6f413e36d94b18c100caf122d4e2d643abcd73381558d8,
  CLI c67d5c9275f3c38233946f8556b73ed120575107c63f76bfa0aa710ce78b5032,
  bench binary (rebuilt at bench HEAD b92ee71)
  888bc5a19e241db6b9b43941be028df6bfccb2e49506ce2c3777fd0d751c77c1.
  Run-3 binaries preserved as *-run3. Provenance: provenance-run4.json +
  provenance-run4.sha256.
- Config: byte-identical to run 3 final state (config sha256 matches
  provenance.sha256 line for meept.json5). Same rig dir, ports 55096-55098,
  quotas off, socket rig-only. Boot healthy (2x "Runtime started and
  healthy"); one run-3 leftover task recovered from tasks.db at boot.
- Full suite re-run (all 6 usability tasks had failed in run 3, so the
  targeted set = the whole suite) + p5 probe re-run + one u-plan config
  experiment (outside the graded config, below).

### 11b. Per-task verdicts (run 4) -- 0 pass / 2 fail / 4 error

| Task | Run 3 | Run 4 | Detail |
|---|---|---|---|
| u-context-two-step | error | error | unchanged: "no file operation was executed" gate rejection |
| u-steer-midrun | error | error | unchanged gate rejection; NOTE the tactical steer/followup RPC was ACCEPTED this run (run 3: queue-not-found at this layer too) |
| u-route-research-write | fail | fail | file_contains FAIL (fetched.txt did NOT land this run -- model refused the raw-IP fetch; run-3 model variance ran the other way); judge invalid in graded run (11d), re-run FAIL confirmed with working judge |
| u-fail-nonexistent-read | error | fail (IMPROVED) | honest missing-file failure now terminalizes in-window: exit_zero PASS; reply-shape judged 0.0 in re-run; terminal_delivery 0/1 |
| u-plan-mode-request | error | error | unchanged park: plan parsed (8-9 steps) -> "Plan awaiting user approval" -> no terminal in 300s |
| u-direct-explicit | error | error | same verdict, NEW evidence: tool.execution.complete NOW carries file evidence (file_exists size=2 + file_hash) -- the 68478b69 executor fix WORKS |

p5 steering re-run: chat.steer/chat.followup still "queue not found" on
raw conv and derived thread conv -- steering remains unusable. Secondary
observation: the primary turn replied listing all 5 files created while
the writes were only STAGED pending-changes and 4/5 never hit disk.

### 11c. NEW root cause (second defect) -- the fix is necessary, not sufficient

68478b69 repaired the executor half of the pipeline: run-4 transcripts
show evidence payloads on the bus where run 3 had `evidence: None`. The
gate still fail-closes because the evidence never reaches it:

- internal/daemon/components.go NewComponents: line 5107 runs
  `jobProc.WithBus(c.msgBus)` behind `if c.msgBus != nil`, but the field
  is only assigned at line 5483 (`c.msgBus = msgBus`) -- 376 lines LATER.
  At wiring time the field is nil, so the toolEvidenceCollector is never
  subscribed in the production daemon.
- Consequence chain (all verified on the live rig): step-job envelopes
  carry no tool_evidence; tactical.go's "Extracted tool-issued evidence"
  log fires 0 times in the entire run-4 daemon log; task_steps.evidence
  stays NULL; review_evidence_gate.go (correct per its spec) sees zero
  file-tool evidence and rejects every file-shaped step.
- Unit tests do not catch it: they call WithBus(bus) directly and never
  exercise NewComponents' assignment ordering.
- Suggested fix (one-line-shaped): assign c.msgBus at the top of
  NewComponents (or move the jobProc wiring below the assignment), plus a
  regression test constructing via NewComponents with a bus and asserting
  tool_evidence arrives on a step envelope. Owner: 02-routing or 01-safety
  (same max-3-cycles rule).

### 11d. Judge URL disclosure (honest)

The graded run's llm_judge checks all errored: I passed
JUDGE_URL=http://127.0.0.1:55096/v1 but judge-local.py appends /v1
itself (-> /v1/v1/... 404). Run 3 used the bare base URL. Re-runs of the
two judge-bearing tasks with the corrected URL confirmed both FAIL
verdicts stand with a working judge (u-route: file still absent;
u-fail-nonexistent-read: exit_zero PASS, reply-shape 0.0). No task
verdict flips because of the URL; only the judge SCORES from the graded
run are invalid.

### 11e. u-plan-mode-request approval dependency (answered, config experiment)

The park is NOT caused by plans.approval.require_approval (already false
in the graded config). It is the COMPLEXITY GATE:
orchestrator.task_approval_step_threshold (default 5) parks any plan
with >= 5 steps for approval regardless of require_approval. Adding
`"orchestrator": {"task_approval_step_threshold": 0}` (gate disabled) to
the rig config removed the park entirely: the 8-step plan went straight
to execution ("Promoted root steps to ready", steps completing). So YES
-- enabling execution past the approval gate via this one key lets the
turn terminalize instead of parking. Caveat: with threshold=0 the task
still failed its 300s harness window (8-step plan too slow for the 8B)
and steps still hit the collector defect -- threshold=0 removes the
park, not the failures. Experiment config reverted; graded config
unchanged on disk.

### 11f. Updated overall verdict (run 4)

- T1 (rebuild + boot with fix): PASS (provenance exact, config identical).
- T2 (re-run failed tasks): PASS on execution; every targeted task and
  probe produced a verdict; two known infra notes honestly disclosed
  (judge URL; u-plan experiment run outside graded config, reverted).
- T3 (report): PASS (this section + RUN-LOG run 4).
- OVERALL ACCEPTANCE: **STILL FAIL** -- usability suite 0/6 PASS
  (composition improved: 4 error -> 2 error / 2 fail / 0 pass, one task
  net-improved). The 68478b69 fix is verified working at the executor
  layer, but acceptance cannot pass until the second defect
  (components.go msgBus assignment order starving the evidence
  collector) is fixed; p5 steering and the u-plan complexity-gate park
  remain independently broken.

### 11g. Limitations (run 4)

- Single-seed, single-run per task (same as run 3); the 8B model's
  behavior is nondeterministic (u-route fetched.txt landed in run 3,
  refused in run 4).
- One run-3 leftover task recovered at boot (tasks.db not fresh); graded
  rows were all fresh task IDs (timestamped 10:42+).
- Regression + smoke suites were NOT re-run (out of the targeted scope);
  their run-3 verdicts stand as recorded.
- No commit made, per instructions. Rig daemons killed at end of run;
  rig dir preserved with run-4 artifacts (bench-results-run4*,
  daemon-run4.log, provenance-run4.*, probes/p5_steering_run4.log).

## 12. Post-fix re-run (run 5, 2026-11-21) -- collector wiring fixed; STILL FAIL, composition improved

Re-run scope: rebuild with the second fix commit, re-run the failed
tasks + p5 steering, and PROVE the collector now subscribes. Full
per-step evidence: 02-RUN-LOG.md "RUN 5" section.

### 12a. Rig provenance (run 5)

- Fix commit: 4f934dfb ("job processor bus wiring used nil c.msgBus
  field - toolEvidenceCollector never subscribed",
  internal/daemon/components.go, +9/-2) at HEAD of worktree
  usability-remediation. The fix passes the msgBus PARAMETER to
  jobProc.WithBus instead of the c.msgBus field (assigned at
  Start-time, always nil at the wiring site).
- Build: from `git archive 4f934dfb` (clean tree; the worktree still
  carries the same sibling in-progress edits as run 4, NOT in the
  graded binary). Daemon sha256
  0ff5db3d3ccec68f4721111e111b59e8567de5288ce65e7725196a9d238d8e29,
  CLI 03ff26bef2e463a0aac1aa001bd659e048f2c817ddc147ff9aa5799fafb9e0a5,
  bench binary reused from run 4 (bench HEAD unchanged at b92ee71).
  Run-3 binaries preserved as *-run3. Provenance: provenance-run5.json
  + .sha256 (+ .sha256.bins for binary hashes).
- Config: byte-identical to the run-3/4 graded state -- meept.json5 and
  models.json5 sha256 match the provenance.sha256 lines exactly;
  task_approval_step_threshold left at DEFAULT (park graded as-is);
  JUDGE_URL passed in the CORRECT bare form (no /v1 doubling).
- Boot healthy (2x "Runtime started and healthy"); 1 stale task
  recovered from tasks.db (same as run 4).

### 12b. Collector-subscription proof (the point of this run) -- CONFIRMED

Four independent signals, all absent in run 4:

1. "Extracted tool-issued evidence from execution result" fired 7x in
   the run-5 daemon log (run 4: 0x in the entire run).
2. tool.execution.complete envelopes in bench transcripts carry real
   evidence payloads (file_exists size=2 + file_hash on
   u-direct-explicit).
3. tasks.db task_steps.evidence is populated and those steps
   terminalize APPROVED (run 4: NULL evidence, blanket rejection).
4. End-to-end: file_write steps pass evidence review and land their
   files (u-direct-explicit file_contains PASS; u-steer write
   approved). Prediction held: file-write tasks now pass evidence
   review -- their residual failures are a different failure class.

With run 4's executor-layer proof, the pipeline executor -> bus ->
collector -> envelope -> gate is verified end-to-end. Defect 2 is
CLOSED.

### 12c. Per-task verdicts (run 5) -- 0 pass / 4 fail / 2 error

| Task | Run 3 | Run 4 | Run 5 | Detail |
|---|---|---|---|---|
| u-context-two-step | error | error | error | write steps now approve WITH evidence; residual rejection is the verify step -- model never executed file_read (gate correctly fail-closed on zero evidence) |
| u-steer-midrun | error | error | fail (IMPROVED) | steer write lands + approves w/ evidence; fails on wrong-dir write (rig root, not repo root) + terminal_delivery 0/2 |
| u-route-research-write | fail | fail | fail | unchanged: model refuses the raw-IP fetch; fetched.txt absent |
| u-fail-nonexistent-read | error | fail | fail | exit_zero PASS; step approved w/ evidence (list_directory+shell+tool_view); judge scored reply shape 0.9 in raw output; checker errored parsing the <think>-prefixed score; terminal_delivery 0/1 |
| u-plan-mode-request | error | error | error | unchanged park (10-step plan vs task_approval_step_threshold=5, graded as-is) |
| u-direct-explicit | error | error | fail (IMPROVED) | file_contains PASS, hash-backed evidence, step approved, "task completed" reply produced; residual FAIL = terminal_delivery 0/1 only |

p5 steering re-run: chat.steer/chat.followup STILL "queue not found"
(-32603) on raw and derived-thread conversations -- unchanged across
runs 3/4/5, independent of both fixes. Positive note: the primary turn
landed all 5 files on disk this run (run 4's staged-write + claimed-
success shape did not recur).

### 12d. Judge checker parse disclosure (honest)

The llm_judge checker errored on 3 tasks despite the judge server
being reached and scoring: judge-local.py could not parse scores when
this seed's judge output was prefixed with an empty <think></think>
block ("cannot find score in judge output: '<think></think> 0.9 ...'").
No run-5 verdict relies on an llm_judge pass; all fails stand on
file_contains / terminal_delivery / exit_zero alone. JUDGE_URL was
correct this run (bare base).

### 12e. Updated overall verdict (runs 3 -> 4 -> 5)

- T1 (rebuild + boot with fix): PASS (provenance exact, config
  byte-identical).
- T2 (re-run failed tasks + probes): PASS on execution; every targeted
  task and probe produced a verdict; judge-parse issue disclosed.
- T3 (report): PASS (this section + RUN-LOG RUN 5).
- OVERALL ACCEPTANCE: **STILL FAIL (0/6 usability PASS)** -- but the
  failure CLASS changed. Both infrastructure defects found in runs 3/4
  are now fixed and PROVEN fixed (executor value-type drop: 68478b69;
  collector bus wiring: 4f934dfb). Remaining blockers, in rough
  priority order:
  1. terminal_delivery 0/1 on every task that actually completed its
     work -- completed turns still do not produce a non-parked
     turn.terminal in-window (now the top residual).
  2. p5 steering queue registration ("queue not found"), broken across
     all three runs and untouched by both fixes.
  3. u-plan complexity-gate park (task_approval_step_threshold default
     vs a plan request; root-caused in run 4, config experiment
     documented there).
  4. 8B-model behavior variance (raw-IP fetch refusal, skipped verify
     tool, wrong-dir writes) -- now the difference between fail and
     pass on tasks whose plumbing works.
- No commit made, per instructions. Rig daemons killed; rig dir
  preserved with run-5 artifacts (bench-results-run5, daemon-run5.log,
  bench-usability-run5.log, probes/p5_steering_run5.log,
  provenance-run5.*).

## 13. Final re-run (run 6, 2026-11-21) -- terminal_delivery fix verified; OVERALL STILL FAIL (0/6 usability PASS)

Re-run scope: full usability suite (all 6) + p5 steering, with the
bench turn.terminal-subscription fix (fab82fb, bench HEAD d334cc7).
Daemon source unchanged since run 5 (meept HEAD 4f934dfb). Full
per-step evidence: 02-RUN-LOG.md "RUN 6" section.

### 13a. Rig provenance (run 6)

- Bench binary rebuilt at d334cc7 (sha256 e3cdc217...); daemon/CLI
  binaries byte-identical to run 5 provenance. Config byte-identical
  to the graded run-3/4/5 state (sha256 match); quotas off; thresholds
  default; JUDGE_URL bare base. Boot healthy; 1 stale task recovered.
- PLUS one working-tree (uncommitted) bench judge fix: judge-local.py
  strips the empty `<think></think>` prefix that broke score parsing
  in run 5; selftest + regex check PASS. This is why run-6 llm_judge
  checks produce real scores instead of checker errors.

### 13b. THE POINT OF RUN 6: terminal_delivery flip -- CONFIRMED

Prediction held. The fab82fb one-line fix (runner subscribes
turn.terminal) flipped the checker from structurally-blind to green on
every completed task:

| Task | terminal_delivery R5 | R6 |
|---|---|---|
| u-context-two-step | 0/2 FAIL | 1/2 (primary delivered; steer-turn terminal absent) |
| u-steer-midrun | 0/2 FAIL | **2/2 PASS** |
| u-route-research-write | 0/1 FAIL | **1/1 PASS** |
| u-fail-nonexistent-read | 0/1 FAIL | n/a (harness ERROR before checks) |
| u-plan-mode-request | n/a (park) | n/a (park -- correct) |
| u-direct-explicit | 0/1 FAIL | **1/1 PASS** |

Structural proof: run-6 transcript u-direct-explicit contains 2
turn.terminal rows; the run-5 SAME transcript contains 0. The daemon
had been firing terminals all along (runs 3-5); only the runner was
not listening. Residual terminal_delivery failures are the PARK class
(u-plan complexity gate; steer turns on u-context) -- daemon-side
behavior, not harness bugs.

### 13c. Per-task verdicts (run 6) -- 0 pass / 4 fail / 2 error

| Task | R3 | R4 | R5 | R6 | R6 detail |
|---|---|---|---|---|---|
| u-context-two-step | error | error | error | fail (IMPROVED) | first time the primary chain fully works: write approved w/ evidence, counter.txt written, terminal completed in-window, judge PASS 0.9. FAIL because the steer-turn revision claimed a write it never executed ("[unverified: no tools...]") and hash-validation failed it |
| u-steer-midrun | error | error | fail | fail | steer-turn write (result.txt=7) approved w/ evidence in the worktree; FAIL: checker wants result2.txt and the steer never delivered (p5 defect, 13e); terminal_delivery PASS |
| u-route-research-write | fail | fail | fail | fail | unchanged: 8B refuses the raw-IP fetch (5.1s); judge now scores the (empty) reply honestly |
| u-fail-nonexistent-read | error | fail | fail | error (REGRESSED) | seed variance: rev step claimed no file op, gate fail-closed ("zero file evidence after retry") -> task failed as ERROR; the underlying honest-missing-file behavior was proven good in runs 4/5 |
| u-plan-mode-request | error | error | error | error | unchanged park: 9-step plan vs task_approval_step_threshold=5, graded as-is (root cause + config experiment documented in 11e) |
| u-direct-explicit | error | error | fail | fail | fence DENIED the write: coder targeted /private/tmp/usability-u-direct-explicit-a1/direct.txt (a path that did not exist this run); "Path does not match any allowed path pattern"; gate correctly rejected the zero-evidence step; terminal_delivery PASS |

p5 steering: chat.steer/chat.followup STILL "queue not found"
(-32603) on raw and derived-thread conversations -- 4th consecutive
run (3/4/5/6). queue_status responds (queue state exists, depths 0),
so registration for the primary conversation is the defect. Primary
turn itself completed with all 5 step files landing.

### 13d. Judge-checker parse fix disclosure

The run-5 llm_judge checker errors (empty `<think></think>` prefix
before the score) were fixed in the bench working tree for run 6
(judge-local.py `<think>` strip; NOT committed). Run-6 judge checks
all parsed: u-context PASS 0.9, u-steer PASS, u-direct 0.2 (reply
judged near-empty -- the machine-shaped summary only), u-route FAIL
(empty reply). No verdict in the table relies on a judge pass.

### 13e. FINAL OVERALL VERDICT (runs 3-6)

- T1 (build + boot): PASS all four runs.
- T2 (run battery to verdict): PASS all four runs (every task/probe
  produced a verdict or honest error; one judge-URL defect in run 4
  and one judge-parse defect in run 5, both disclosed and fixed).
- T3 (compare + report): PASS (this report + RUN-LOG).
- OVERALL ACCEPTANCE: **FAIL (final)** -- usability 0/6 PASS in every
  graded run. Composition by run: R3 0p/1f/5e -> R4 0p/2f/4e -> R5
  0p/4f/2e -> R6 0p/4f/2e.

Verdict trajectory across the three infrastructure defects:

| Defect | Run found | Fix | Status after R6 |
|---|---|---|---|
| executor drops value-type ToolResult evidence | R3 | 68478b69 | CLOSED (proven R4) |
| components.go msgBus nil at wiring -> collector never subscribed | R4 | 4f934dfb | CLOSED (proven R5) |
| bench runner never subscribed turn.terminal | R3-R5 | fab82fb | CLOSED (proven R6: checker flipped green) |
| judge-local.py <think> prefix parse failure | R5 | working-tree patch | CLOSED (uncommitted) |

The evidence pipeline and terminal-delivery reporting are now
end-to-end verified. What remains is NOT harness plumbing:

1. p5 steering queue registration ("queue not found", -32603) --
   4 runs, untouched by every fix; the last standing infrastructure
   defect. Blocks u-steer-midrun's second file and the entire
   mid-run-steering UX.
2. u-plan complexity-gate park (task_approval_step_threshold=5
   default vs any real plan request) -- non-interactive runs can
   never terminalize a planned task; needs auto-seal or terminal
   path (11e documents the threshold=0 experiment).
3. 8B model behavior variance -- now the pass/fail delta on tasks
   with working plumbing: raw-IP fetch refusal (u-route, 3 runs),
   unbacked write claims under steering (u-context R6), wrong/
   nonexistent target paths (u-direct R6; u-steer R5), skipped
   verify step (u-context R5), seed-dependent honest-vs-gate-reject
   on missing files (u-fail R6 vs R4/R5).
4. Unbacked-claim + hash-validation interplay: the steer-turn
   "wrote 5 without tools" claim was correctly rejected, but the
   task then lost its (real, approved) primary result -- a task
   should arguably complete on its approved steps. Design question
   for the owning branch, not a bug in the gate itself.

Verdict-grade honesty note: R6's 0/6 headline equals R5's, but the
failure class changed again -- R5 failed on reporting (terminals
invisible to the harness); R6 fails on daemon UX defects (steering
queue, plan park) and model behavior, with all three plumbing layers
proven correct. No further harness-side fix is indicated; remaining
work belongs to the daemon (steering queue registration, plan
auto-seal) and to model quality.

### 13f. Limitations (run 6)

- Single seed per task, single run (same as R3-R5); 8B nondeterminism
  is material (u-fail and u-direct flipped failure modes run-to-run).
- The judge-parse fix is an uncommitted working-tree edit in the
  bench worktree; if the worktree is reset, run-6's llm_judge scores
  are not reproducible without re-applying it.
- R6 re-ran ONLY the usability suite + p5 (per run scope). Regression
  (2/9) and smoke (0/3) verdicts from run 3 stand as recorded; they
  predate all three proven fixes and would likely improve, but were
  not re-measured.
- GUI pixel-pass verification remains operator-pending (section 9).
- No commit made, per instructions. Rig daemons killed at end of run
  (0 meept-daemon/llama-server processes; log ends "daemon: stopped").
  Rig dir preserved with run-6 artifacts (bench-results-run6/,
  daemon-run6.log, bench-usability-run6.log, probes/p5_steering_run6.log,
  provenance-run6.*).
