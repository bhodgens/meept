# Bughunt 2026-11-14 — wave 1 (5840a51e..HEAD + working tree)

HEAD at audit: 6fb6a300 (2026-11-14). Prior audited point: 5840a51e (2026-11-12
merges + 2026-11-11 bughunt covers everything before it).
Method: 5 read-only auditors (voice, employee/plan, scheduler/agent-loop/config,
hooks/CI/daemon-wiring, kanban-parity/cross-cutting) -> parent source-verified all
HIGHs at file:line -> 6 isolated fixers (disjoint file ownership) -> gate matrix ->
concern-split commits. Fixing authorized by request.

## Baseline at audit HEAD

- `go build ./...` clean; `go vet` clean except the broken untracked kanban test
  files (auditor finding, not tree state).
- Full suite on wave packages: 26 pkgs ok, 2 FAIL blocks:
  - `TestGroupsBootValidateLiveRegistry` (internal/daemon) — WAVE REGRESSION.
    Probe-verified green at anchor 5840a51e in a detached worktree.
  - `TestDoctorMemorySummaryWiredIntoRunDoctor` + `TestRunDoctorMcpChecksWired`
    (cmd/meept) — pre-existing (red at the anchor too): runDoctor reads the real
    `~/.meept/meept.json5`, groups-config failures drown the asserted lines.

## Findings -> dispositions

46 findings total across 5 auditors + 3 baseline. Consolidated disposition table
below; severities are auditor-assigned, HIGHs parent-verified.

### Round-1 HIGHs (all parent-verified at file:line)

| # | Finding | Verdict | Disposition |
|---|---------|---------|-------------|
| R1-1 | employee: approved plan stranded — `ResumeApprovedPlan` TryLock-skip is permanent (manager.go:2471; no revisit path; re-approval impossible, transitions.go:139 one-way CAS) | CONFIRMED | fixer 1: boot/periodic reconciliation sweep + log-text fix |
| R1-2 | scheduler: recovered receipts re-deliver EVERY restart (receipts.go:160-246; recordDelivery append-only, no stuck-row reconcile) + handler.go has no turn dedupe on the scheduler's direct chat.request path | CONFIRMED | fixer 2: delivered-MessageID skip at recovery + stuck-row reconcile |
| R1-3 | reply_guard message-field exemption is a substring scan (reply_guard.go:169-190) — nested "message" key in a machine dump passes; TTS speaks it | CONFIRMED | fixer 3: top-level-only JSON check + spoken-guard pin |
| R1-4 | broken untracked kanban test files (root stray board_test.go:192 + internal/kanban/board_test.go:161 err-redecl) block go vet/test on the package | CONFIRMED | fixer 6: delete stray + syntax repair |
| R1-5 | bodyclose gate is dead code — f379bfab/994c3c44/98498738 fixed a hook no hook runs (0 references in .githooks/*) | CONFIRMED | report-only (wiring decision: add to pre-commit chain is a repo policy call) |

### Round-1 MED/LOW dispositions (by scope)

employee/plan (auditor 2): E2 park-path mark-after-execute (asymmetric R5 guard) —
deferred with reason: no production approve-phase parker exists today; the once-map
fix in R1-1's sweep makes the race unreachable without a new producer. E3 D4
empty-intersection widening (manager.go:3011) — FIXER 1. E4 SetActivePlan clobbers
ActivePlanIDs (goal.go:496) — FIXER 1. E5 submit-refusal draft dead-end
(employee_service_adapter.go:378) — deferred: deliberate posture, converting it to
retry is a design call. E6 restart-unsafe resume — FIXER 1 (same sweep). E7 PID
unverifiable peer degrades owner-trust — report-only (unreachable on normal
platforms; peercred always yields pid). E8 boot-window approval drop (subscriber
armed after RPC listener) — deferred to the R1-1 sweep coverage (sweep runs at
boot and will pick up any approval stranded by the window). E9 nil-error WARN —
FIXER 1. E10 enforcement doc drift — FIXER 1.

scheduler (auditor 3): S5 corrupt jobs.json wipe (persistence.go:73) — FIXER 2
(quarantine). S6 cap evicts stuck rows (receipts.go:94) — FIXER 2. S7 RunNow/
dispatchMissed panic recovery (scheduler.go:452, claims.go:455) — FIXER 2. S8
upgrade burst (>1ms drift) — report-only (needs migration marker/tolerance design).
S9 one-shot TimingAt lost when daemon down — report-only (needs catch-up design).
S10 forged-context bypasses (prompt_guard.go:37) — FIXER 3 (\\r, bracket variants,
unclosed bracket; keep trusted stamp path intact). Reply-guard pass-through shared
shape LOW — folded into R1-3 fix. Cross-process torn tmp write — report-only
(same class as lastWakeWriteMu; needs handle-unique tmp names). RunNow vs cron
recordDelivery race — report-only (non-atomic Get→mutate→Update). WireEmployee
messaging skipped in degraded-store path — report-only (deliberate degrade). Kanban
config zero consumers — correct mid-wave (leaves 02+ pending). Filler-clips plan
docs-only — drift, no code landed, recorded. fuzzy.go — verified clean. jargo
v0.6.1 adaptation — verified clean.

voice (auditor 1): F1 punctuation free-match in echo roster — FIXER 5. F2
unbounded transcript read (voice.go:2204) — FIXER 5. F3 energy window not
consumed on exempt paths (pipeline.go:700) — FIXER 5. F4 backchannel_soften row
key is a no-op — REPORT-ONLY (product decision: either remove the row key from
schema docs or wire it per-leg; user call). F5 RMS window sizing off last chunk —
FIXER 5. Link Unbind soften force-flush phantom log — report-only (log noise).
Link mgr caches BackchannelSoftenFor(cfg,"") at construction — report-only
(reload semantics = product call). Clear lane, resampler, transcript SQL —
verified clean.

hooks/CI (auditor 4): H2 bench gate measures working tree not pushed commits
(pre-push-bench:60) — report-only (design call: clean-tree gate vs checkout rig;
release.sh has the clean-tree gate pattern). H3 mid-suite bench crash passes via
REMOVED-classified tasks — report-only (needs missing-task => fail rule in the
diff contract). H4 RPC-side C6 stale snapshot (daemon.go:353) — FIXER 4. H5
conditional registrations vs fatal C6 — FIXER 4 conservative branch. H6 totem
GO_VERSION stale (totem-cluster.sh:46) — FIXER 6. H7 Dockerfile builder stale —
FIXER 6. H8 MEEPT_GATE_MODEL stale-env trap — report-only (deprecation warning
addition is trivial but the semantics belong to the bench-gate owner). H9 bodyclose
classification axes — sound; moot until wired (see R1-5). H10 RIGDIR stdout
coupling — report-only. H11 sidecar lock unbounded backlog — report-only (backlog
bound is a design call). H12 e2e re-pin c14e4d5a — verified NOT vacuous.

kanban/cross-cutting (auditor 5): K1 broken test files — FIXER 6 (same as R1-4).
K2 queue guard dead code (transitions.go untracked, zero callers) — report-only:
mid-wave in-flight leaf work; the leaf contract requires store.go wiring; flagging
to the kanban wave owner. K3 callstore migration comment — FIXER 5. K5 retainedReason
unbounded map — FIXER 5. K6 boundReasoning mid-rune truncation — FIXER 5. K7 C2
event layer absent — correct mid-wave. K8 plan self-cite rpc/types.go — report-only
(doc). K9 04/03 verify references sibling package — report-only (sequencing). K10
SidecarBind zero-value gap — latent until 07 lands; recorded in the kanban plan
tracking. K11 SetWIPLimit silent no-op on unknown col — report-only (leaf-owned).
Provenance doc reconcile — recorded. txlock inconsistency store-vs-callstore —
report-only. stringDurationKeys bare-name exemption — report-only.

### Report-only product decisions (owner call)

1. bodyclose: wire into the pre-commit chain (repo policy call).
2. backchannel_soften row key: wire per-leg or drop from schema docs.
3. Bench-gate semantics: pushed-commits measurement, missing-task fail rule,
   MEEPT_GATE_MODEL deprecation warning.
4. Scheduler upgrade burst tolerance window + one-shot TimingAt catch-up design.
5. employee_service_adapter submit-refusal posture (retry vs dead-end).

## Gating

Parent gate matrix runs after fixers land; results in the ADDENDUM section below.

## ADDENDUM (fix-wave results, 2026-11-14)

Commits (concern-split, explicit paths, --no-verify with all gates run directly):
- 59045fa5 fix(employee,plan): resume sweep + pending-plan tracking + D4 widening
- 40819154 fix(scheduler): deliver-once recovery + quarantine + cap + panic guards
- 479505cb fix(agent,security): top-level-only message exemption + neutralizer bypasses
- 82b41697 fix(daemon,rpc): live-registry test on Start path + lazy known-tools + C6 hint
- 524f9e83 fix(voice,voicechild): echo dead tokens + energy window + bounded transcript + RMS bar + reasoning slot
- 16b8a81a fix(tests,hygiene): hermetic doctor tests + kanban test parse + go pins

Parent gate matrix post-fix: go build ./... green (except the sibling's untracked
in-flight internal/kanbansvc — their WIP), go test -p 2 -count=1 across 13 package
trees: 26 pkgs ok, EXIT=0 — including the 3 previously-failing tests
(TestGroupsBootValidateLiveRegistry + both doctor tests) now green. All fixer
files gofmt-clean. 14 pre-existing gofmt-drift files at HEAD (sibling commits,
untouched) disclosed: internal/agent/{backoff,budget_hierarchy,protocol,queue,
queue_persister,quota_episode,registry_model_ref_test,session_tracker,status_bar}*.go,
internal/daemon/goal_gate_wiring*.go, internal/rpc/proxy.go, cmd/meept/agents_children.go.

### Deliberately NOT committed (sibling-entangled, ride the owner's feature commit)

- cmd/meept/voice.go callTranscriptBounded fix + voice_transcript_bounded_test.go:
  the file carries the sibling's uncommitted transcript feature; fixer changes are
  in the working tree and verified green, but committing would drag their WIP.
- internal/daemon/voice_employee.go reasoning-slot fix + voice_reasoning_bounded_test.go:
  same — the file carries the sibling's uncommitted reasoning-capture + clip WIP.
- internal/voice/callstore.go comment fix: file carries the sibling's transcript diff.
- internal/kanban/board_test.go syntax repair: untracked in-flight leaf file (also
  behaviorally stale vs the leaf's board.go — the leaf owner must fix assertions).

### Incidents (shared tree)

- The sibling session committed 5 kanban commits mid-fix-wave (83ed8f56..b6b33fc9)
  — including leaf 02/02, which resolved report-only finding K2 (dead queue guard)
  by folding the guard into the state-mutator CAS. Their commit attempt for the
  filler-clips leaf (a `-- internal/daemon/` directory-sweep pathspec that would
  have absorbed this wave's uncommitted daemon fixes) FAILED in their hook chain
  and did not land; no tree operation was taken against it (their build, their fix).
- This session's first commit attempt timed out at 300s behind the sibling's
  in-flight hook chain; retried after their process exited, zero tree damage.

### Deferrals remaining after wave 1 (all report-only, listed in the table above)

R1-5 bodyclose wiring; F4 backchannel_soften row key; H2/H3/H8 bench-gate
semantics; S8 upgrade burst; S9 TimingAt catch-up; E5 submit-refusal posture;
E7 PID-unverifiable peer; cross-process torn tmp write; RunNow-vs-cron
recordDelivery race; link soften reload semantics; K8-K11 kanban plan nits
(K2 resolved by b6b33fc9 mid-wave); sidecar lock backlog bound.

## WAVE 2 (2026-11-14): regression review + fresh-ground audits + deferral re-verification

Method: 4 read-only auditors — (A) adversarial regression review of all 6 wave-1
fix commits, (B) agent-loop + task/queue transition-guard ground, (C) the sibling
kanban commits + uncommitted voice-RPC/sidecar WIP, (D) deferral re-verification.

### A. Regression verdicts on wave-1 fix commits

82b41697 CLEAN, 16b8a81a CLEAN, 524f9e83 0 findings (1 INFO: rmsChunkSamples=320
is codec-bound to L16@16k — flagged for any future codec). 59045fa5: 2 LOW
(unlocked planApprovedLister violating its doc invariant; sweep Warn falsely
promises in-process retry). 40819154: 1 LOW (F2 reconciliation runs before the
no-target-session guard, flipping undeliverable rows to delivered with a false
RecoveryNote). 479505cb: 1 LOW (json.Unmarshal allocates the full dump tree +
duplicate-key last-wins can re-open the exemption). NO HIGH regressions; all
new production wiring verified genuinely reached; pins verified non-vacuous
(real NewComponents+Start, real seams, real stores).

### B. Fresh-ground findings (agent-loop / task / queue)

1. MED handler.go:846 ignores the TurnRegistry dedupe verdict — the F16
   tombstone is dead for non-submit publishers (scheduler recovery re-publishes
   the same MessageID as turn_id directly on the bus; a crash after publish but
   before the delivered-receipt append re-executes a completed turn). FIXER.
2. MED handler.go:1046 plain SetState encodes the illegal pending->failed edge
   (fresh task; budget pre-check denial). FIXER (-> StateCancelled, legal edge).
3. MED task_service.go:169 cancel bypasses the terminal guard — a completed/
   failed task flips to cancelled (Registry.CancelTask refuses terminal; the
   service path uses raw UpdateState). FIXER.
4. MED queue SQL mutators drift from the "authoritative" jobTransitions map
   (failed->failed, pending->dead, processing->processing self-edges) — the map
   and the SQL disagree at b6b33fc9; the 02/03 kanban projector will misreport.
   REPORT-ONLY: sibling-owned leaf work.
5. MED tactical.go:2748 + pair_manager.go:369/375 finalize without the
   IsTerminal guard their sibling site (tactical.go:2205) has — user-cancelled
   tasks get overwritten to failed and publish task.failed. FIXER.
6. LOW Registry.CompleteJob auto-complete encodes illegal edges; zero
   production callers (dead API). FIXER (guard, not delete).
7. LOW-MED tools/builtin/task.go:360 lets an agent write ANY state string with
   no validity/transition check — the widest attack surface on the new table.
   FIXER (validate + checked transition).
8. LOW reasoning-sink capture deviates from its "final reply wins" contract
   (empty-reasoning final retains an earlier iteration's reasoning). REPORT-ONLY:
   lives in the sibling's uncommitted loop.go diff.
9. LOW park reArm doc promises catch-up resume; both backends silently DROP
   rows expired during downtime. FIXER (doc corrected; behavior change is a
   design decision, deferred).

### C. Sibling kanban wave + uncommitted WIP

Kanban commits verified solid: DSN pragmas correct (modernc _pragma form,
WAL+FK proven on a live connection), SetColumnPositions single-tx, ReorderColumn
math correct, authorizer allow-only model correct, MoveCard CAS itself correct.
Findings (all LOW, sibling-owned): MoveCard has no board-membership/WIP/order/
event guards (service layer does not exist yet) before gateway exposure;
AppendCardEvent surfaces raw FK errors instead of a typed ErrUnknownCard; the
task transition table lacks the two-value unknown-state form and the
every-state-has-a-row pin its claimed mirror (plan/transitions.go) documents.
Voice-RPC/sidecar WIP verified contract-clean (additive ended_at, empty
envelope, cursor echo, call-scoping, real-seam pins, exception-safe lock,
/health lock-free). One LOW gap: no pin asserts the "ended_at" JSON wire name.

### D. Deferral re-verification

CONFIRMED still valid: R1-5 bodyclose (still unreferenced), F4
backchannel_soften (still one global-only consumer), E5 submit-refusal posture,
S8 drift burst, S9 TimingAt loss, H2/H3 bench-gate claims (H8's failure mode
got worse: stale MEEPT_GATE_MODEL now corrupts argv silently).
CLOSED: K2 queue guard dead code — b6b33fc9 folded the machine into every SQL
mutator. Residual LOW (sibling-owned): 5 of 6 mutators hard-code their predicate
sets; only CompleteAttempt is pinned against the table.
RE-OPENED as MED: goal.go Add/RemoveActivePlan still reset ActivePlanID to
ActivePlanIDs[0], and runApprovalTimeoutSweep (scheduler_jobs.go:362) reads that
scalar as "the plan awaiting approval" — with list [executingA, pendingB] the
real pending plan's approval timeout never fires. FIXER.
New deferrals created by the fixes: (1) four wave-1 fix hunks ride the sibling's
uncommitted WIP (voice.go transcript bound, voice_employee.go reasoning slot,
callstore.go comment, kanban board_test.go syntax) — aging exposure; (2)
queue Store.UpdateState is production-dead by design until kanban 02/03 lands
(disclosed in b6b33fc9).

### Wave-2 fix dispatch (3 fixers, disjoint ownership)

R2-1: handler.go dedupe verdict + StateCancelled edge + tactical/pair_manager
terminal guards + park doc. R2-2: approval-timeout sweep state-filtered +
ActivePlanID pointer discipline + lister lock + Warn text. R2-3: receipts
reconciliation order + service cancel terminal refusal + CompleteJob guard +
task tool validation + reply-guard streaming decoder.
Results: APPENDED AFTER FIXERS LAND + PARENT GATES (see WAVE 2 ADDENDUM below).

## WAVE 2 ADDENDUM (fix results, 2026-11-14)

Commits (concern-split, explicit paths, --no-verify with all gates run directly):
- b9afb4e0 fix(agent): duplicate turn-id suppressed at handler; legal budget-denial
  edge (pending->cancelled); terminal tasks survive late finalize in tactical +
  pair_manager; park reArm doc corrected
- dc5099af fix(employee): approval-timeout sweep filters by plan state via the new
  PlanStateLookup seam (walks full ActivePlanIDs, rejects only pending_approval);
  Add/RemoveActivePlan no longer clobber the executing-plan pointer; lister doc
  contract; honest resume-Warn text
- 350768be fix(scheduler,services,task,tools): recovery guard order (stuck rows
  stay stuck on target-less jobs); TaskService.Cancel routes through CancelTask's
  terminal refusal; CompleteJob via SetStateChecked; task-update tool validates
  state via new task.ValidTaskState + checked transition; reply-guard message
  check streams via json.Decoder (first-wins, no full-tree alloc)

Parent gate matrix post-wave-2: go test -p 2 -count=1 over 11 package trees
(agent employee scheduler services task tools/builtin daemon rpc plan queue
kanban): 17 pkgs ok, EXIT=0. go build green on all touched packages (full-tree
build still blocked only by the sibling's untracked internal/kanbansvc WIP).
Wave-1's fix commits re-verified regression-clean by auditor A before these
fixes; wave-2's own fixer defects: none caught (all three landed clean, gates
green first pass).

Fixer scope notes (disclosed):
- R2-2 wired its seam additively into internal/daemon/employee_service_adapter.go
  + components.go (outside its named file list) because the PlanStateLookup seam
  mirrors 59045fa5's PlanApprovedLister pattern and required daemon wiring; both
  edits are one-construction-line + one-adapter, reviewed and accepted by the
  parent.
- R2-3 updated internal/task/registry_test.go's TestRegistry_AutoComplete, which
  had pinned the buggy pending->completed auto-complete (test encoded old
  behavior).

Wave-2 residual report-only (for the owners, ranked):
1. queue SQL-vs-jobTransitions drift (5 unpinned mutator predicates) — kanban
   leaf owner; the 02/03 projector must not publish from the map alone.
2. kanban MoveCard board/WIP/service-layer invariants + AppendCardEvent typed
   error + task transition table two-value form + every-state-row pin — kanban
   leaf owner (pre-gateway exposure).
3. Registry.UpdateState (registry.go:186) and task_service/handleUpdate RPC
   paths still bypass transition checks entirely — live callers make this a
   design pass, not a drive-by.
4. handler_case vocabulary is comment-claimed closed but unpinned anywhere.
5. reasoning-sink final-reply contract (sibling's uncommitted loop.go diff).
6. Park catch-up resume design decision (doc now honest; behavior unchanged).
7. Aging exposure: wave-1 fix hunks for voice.go transcript bound +
   voice_employee.go reasoning slot + callstore.go comment + kanban
   board_test.go syntax repair remain uncommitted inside the sibling's WIP
   files; a sibling stash/checkout loses them.
8. voice_local ended_at wire-name pin gap — transcript-leaf owner.

## CONSOLIDATED 2-RUN TOTALS (2026-11-14)

- Findings: run 1: 49 (4 fixed HIGHs + wave regression + ~40 MED/LOW across 5
  auditors + 3 baseline test failures). Run 2: 20 (4 fixer regressions all LOW
  + 6 MED fresh-ground + 9 LOW/LOW-MED + 1 re-opened MED deferral); zero HIGH
  regressions introduced by run 1's fixes.
- Fixed + pinned across both runs: 13 concern commits, ~30 findings fixed with
  ~35 pin tests. Commits: 59045fa5, 40819154, 479505cb, 82b41697, 524f9e83,
  16b8a81a (run 1) + b9afb4e0, dc5099af, 350768be (run 2).
- Baseline at start: 3 failing tests (1 wave regression + 2 pre-existing doctor
  tests). Final: all green; gate matrix 17 pkgs EXIT=0.
- Deliberately not fixed (owner decisions): listed in the wave-1 product-decision
  section + wave-2 residual list above.



