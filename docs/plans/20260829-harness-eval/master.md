# Harness Eval & Isolation - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Dispatch implementation agents, review their work in-session, re-dispatch if
> incomplete, track completion. Do NOT implement code yourself.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 18 leaf documents
- **Scope:** Close the AI-Agents-in-Depth gaps: eval harness, environment verify, judged trajectories, trusted root, strong context isolation, harness-routed speak, typed user memory, status bar, event rewake, docs honesty, client parity.

## Goal

Meept already is a harness. This tree does not port the book. It adds measurement, isolation, and honest learning so existing loops can fail closed instead of guessing.

User-visible outcome: `meept eval` can tell model-weak from harness-bug; coding agents cannot claim success without `go test ./...` when they mutated the workspace; employees notify instead of swallowing text; specialists no longer inherit the parent transcript; evolver cannot edit security or oracles.

## Architecture

One tree. Flat leaves under this directory. Extend existing packages. Do not add RAPTOR, robotics, voice duplex, or in-process RL.

Speak is harness-routed (Q11=A): the model always ends a turn with final text plus optional `reply_to_user`. The harness classifies the run and delivers to a chat bubble, `notify_user`, or a parent report.

Context isolation defaults to ArtifactOnly for dispatcher handoff, subagent, and pair. SharedTranscript is an explicit flag only.

Quality gates stay globally off (`employees.defaults.gate.enabled=false`). Roster coding agents get `gate.command` in AGENT.md. Gate runs only after a workspace-mutating turn.

Shadow stays export-only. Training is a sidecar. Auto-train dead code is quarantined.

File ownership is exclusive per concurrency group. `internal/agent/loop.go` is serial: leaves 05, then 06, then 11.

## Locked decisions

- One tree, not a forest.
- `internal/eval` in this repo; public benches stay in meept-bench; same RunRecord JSON.
- Global gate kill switch stays false. coder + debugger AGENT.md get `gate.command: go test ./...`.
- Speak: harness routes. Isolated children never speak to the user.
- Preemption: iteration-boundary rewake only.
- MemoryFact store beside personality + episodic. Per daemon owner; `owner_id` when multiuser is on.
- Wave 1 daemon+CLI+HTTP. Wave 2 TUI+Flutter+menubar (leaves 16-18).
- Shadow: capture + export. No in-daemon train loop.
- Trusted root deny-list in the applier (code, not a skill).
- Isolation default ArtifactOnly including pairs.

## Interface Contracts

### C1: Eval RunRecord (01, 02, 03)

```go
// Package eval. File: internal/eval/record.go
package eval

type Kind string // "pass_k" | "model_swap" | "ablation"

type RunRecord struct {
    ID          string            `json:"id"`
    CreatedAt   time.Time         `json:"created_at"`
    Kind        Kind              `json:"kind"`
    TaskID      string            `json:"task_id"`
    HarnessHash string            `json:"harness_hash"` // sha256 of prompts+tool list+gate
    ModelID     string            `json:"model_id"`
    K           int               `json:"k"`            // consecutive attempts for Pass^k
    Attempts    []Attempt         `json:"attempts"`
    Passed      bool              `json:"passed"`       // Pass^k: all K consecutive oracle-pass
    OracleName  string            `json:"oracle_name"`
}

type Attempt struct {
    Index     int           `json:"index"`
    ModelID   string        `json:"model_id"`
    Passed    bool          `json:"passed"`
    Oracle    OracleResult  `json:"oracle"`
    TrajectoryID string     `json:"trajectory_id,omitempty"`
}

type OracleResult struct {
    Passed bool   `json:"passed"`
    Output string `json:"output"` // truncated 4KB
    Err    string `json:"error,omitempty"`
}

type Oracle interface {
    Name() string
    Check(ctx context.Context, workdir string) (OracleResult, error)
}

// ShellOracle runs Command in workdir. Exit 0 = pass. Never uses os.Getwd().
type ShellOracle struct{ Name_, Command string; Timeout time.Duration }
```

Owner: 01. Consumers: 02, 03, 04, 08. meept-bench must accept this JSON as-is.

### C2: AGENT.md gate (04)

```go
// internal/agents/models.go addition on AgentMetadata:
Gate *GateMetadata `yaml:"gate,omitempty" json:"gate,omitempty"`

type GateMetadata struct {
    Command           string `yaml:"command,omitempty" json:"command,omitempty"`
    TimeoutSeconds    int    `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"` // default 300
    SkipWhenUnchanged bool   `yaml:"skip_when_unchanged"`                                    // default true
}

// Registry converts to employee.GateConfig (reuse internal/employee/gate.go RunGate).
// Do NOT duplicate RunGate.
```

Bundled files that MUST gain `gate.command: "go test ./..."`: `config/agents/coder/AGENT.md`, `config/agents/debugger/AGENT.md`. Other executors with `capabilities: code` get the same line. Reviewers do not.

Gate runs at end of a roster-agent turn only when: gate.command is set AND the turn called a mutating tool (`file_write`, `file_edit`, `file_delete`, or `shell_execute` that is not a pure read). Read-only turns skip the gate.

### C3: Speak routing (11)

```go
// File: internal/agent/speak.go
package agent

type SpeakKind int
const (
    SpeakSession SpeakKind = iota // session-attached: existing chat bubble
    SpeakNotify                   // session-detached: notify_user / push
    SpeakParent                   // isolated child: report to parent only
)

func ClassifyRun(sessionAttached, isolatedChild bool) SpeakKind

// Deliver sends text according to kind. Empty text is a no-op (no notify).
// Detached + non-empty final text MUST notify even if the model did not call a tool.
// Isolated child MUST NOT notify or bubble; write parent report only.
// If the model also called notify_user, drop the duplicate final-text notify.
func (r *SpeakRouter) Deliver(ctx context.Context, kind SpeakKind, text, sessionID, conversationID string) error
```

`reply_to_user` is a builtin tool. Mid-turn. Same ClassifyRun. Isolated child: tool returns an error "isolated child cannot speak to user".

### C4: Context isolation (10)

```go
// File: internal/agent/isolation.go
package agent

type ContextIsolation string
const (
    IsolationArtifactOnly     ContextIsolation = "artifact_only"      // default
    IsolationSharedTranscript ContextIsolation = "shared_transcript"  // explicit opt-in
    IsolationBusMessage       ContextIsolation = "bus_message"
)

type SpawnContext struct {
    Isolation ContextIsolation
    Brief     string
    Artifacts []ArtifactRef // paths + hashes, not file bodies
    MemoryIDs []string
    // Transcript is populated ONLY when Isolation == SharedTranscript
    Transcript []llm.ChatMessage
}

func BuildSpawnContext(iso ContextIsolation, brief string, artifacts []ArtifactRef, memoryIDs []string, parent []llm.ChatMessage) SpawnContext
```

Defaults: dispatcher handoff, subagent, pair = ArtifactOnly. Pair SharedTranscript requires an explicit flag on pair create. Parent tool dumps and chain-of-thought never copy under ArtifactOnly.

### C5: Trusted root (09)

```go
// internal/selfimprove/applier.go
var ErrTrustedRoot = errors.New("trusted root path denied")

// Deny if cleaned path matches any prefix (slash-terminated) or exact file:
//   internal/security/
//   pkg/security/
//   internal/eval/
//   internal/selfimprove/applier.go
//   internal/selfimprove/validator.go
//   fence / tirith config under ~/.meept that the engine loads
// Also deny edits to employees.defaults.gate and eval oracle fixtures.
func (a *ChangeApplier) denyTrustedRoot(relPath string) error
```

Skills and instruction files remain writable after verify. AGENT.md prompt body is propose-only (existing). Gate commands are CLI-only (`meept agents set-gate`); the applier cannot change them.

Same leaf also fixes path traversal (`..`, absolute, symlink escape) in applier backups.

### C6: MemoryFact (12)

```go
// File: internal/memory/fact.go
package memory

type FactKind string // "preference" | "restriction" | "account" | "temporal"

type MemoryFact struct {
    ID            string     `json:"id"`
    OwnerID       string     `json:"owner_id"` // empty = daemon owner; set when multiuser on
    Kind          FactKind   `json:"kind"`
    Key           string     `json:"key"`
    Value         string     `json:"value"`
    ValidFrom     *time.Time `json:"valid_from,omitempty"`
    ValidUntil    *time.Time `json:"valid_until,omitempty"`
    SourceSession string     `json:"source_session,omitempty"`
    UpdatedAt     time.Time  `json:"updated_at"`
}

// Conflict: same OwnerID+Kind+Key -> last-write-wins after extract, previous row kept with ValidUntil=now.
func (s *FactStore) Upsert(ctx context.Context, f MemoryFact) error
func (s *FactStore) GetActive(ctx context.Context, ownerID string, at time.Time) ([]MemoryFact, error)
```

Do not add a new vector engine. Extract is a dedicated LLM call after session close (existing reflection path may enqueue). Retrieval is a tool (`memory_fact_search`) plus optional inject of the active set (capped). Always-inject of raw transcripts is out of scope.

### C7: Trajectory (06, 08)

Shadow `CaptureToolInteraction` already exists in `internal/agent/loop.go`. Do not invent a second capture API.

Remaining: registry-created specialist loops must receive the same `shadowMgr` as the primary loop. Capture must persist tool turns, not only final prose. Cost uses real token counts. Dedup uses `DedupSimilarityThreshold`. Auto-train ticker does not start; export CLI stays.

Judge (08): given a stored trajectory + oracle result, emit `TrajectoryJudgment{Passed bool; FirstErrorStep int; Summary string}`. Feed evolver/selfimprove ONLY judged rows. Unjudged rows are not lessons.

### C8: Status bar (13)

```go
// Deterministic, code-maintained block. Unstable prompt section (cache: false).
// Fields: turn index, tools-this-turn count, isolation mode, speak kind, gate skipped|passed|failed|n/a.
// Must not include model prose. Errors in the bar must fail closed to "unknown", not invent success.
func StatusBar(s TurnStatus) string
```

Assemble via existing `PromptSection{Name:"status", Stable:false}`. Indexed tool schemas stay in the stable prefix when `schema_mode=indexed` (fold cache-stable tool list into this leaf).

### C9: Client parity (16, 17, 18)

Wave 2. All UI text lowercase. Surfaces:

- eval run list + pass/fail
- isolation badge on pair/handoff
- notify inbox / last employee notify
- memory facts list (read-only in v1)

HTTP JSON from leaf 03 is the contract. Do not invent a second shape.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-eval-core.md | leaf | none | 55K | A |
| 02 | 02-eval-cli.md | leaf | 01 | 50K | B |
| 03 | 03-eval-http.md | leaf | 01 | 50K | B |
| 04 | 04-coder-gates.md | leaf | 01 | 60K | B |
| 05 | 05-verify-breaker.md | leaf | none | 55K | A |
| 06 | 06-shadow-capture.md | leaf | 05 | 55K | C |
| 07 | 07-shadow-honesty.md | leaf | none | 50K | A |
| 08 | 08-trajectory-judge.md | leaf | 01, 06 | 55K | D |
| 09 | 09-trusted-root.md | leaf | none | 45K | A |
| 10 | 10-isolation.md | leaf | none | 60K | A |
| 11 | 11-speak-router.md | leaf | 05, 10 | 65K | D |
| 12 | 12-memory-facts.md | leaf | none | 60K | A |
| 13 | 13-status-bar.md | leaf | 10, 11 | 50K | E |
| 14 | 14-event-rewake.md | leaf | 11 | 50K | E |
| 15 | 15-docs-flags-metrics.md | leaf | 01-14 | 45K | F |
| 16 | 16-tui-parity.md | leaf | 03, 11, 12 | 55K | F |
| 17 | 17-flutter-parity.md | leaf | 03, 11, 12 | 60K | F |
| 18 | 18-menubar-parity.md | leaf | 03, 11 | 45K | F |

**Concurrency groups:** A parallel (max 3 per batch, so A splits into A1 then A2). B after 01. C after 05. D after C+B. E after D. F after E (docs + clients). `loop.go` owners: 05 then 06 then 11, never parallel.

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 1: Dispatch Concurrency Group [A]

Dispatch at most 3 at a time. Split A into A1 (01, 05, 07) then A2 (09, 10, 12).

1. **Read** the leaf and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from [leaf]"
   - Context: Full leaf text + interface contracts from this orchestrator + coding conventions + relevant existing source INLINED
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
   - Include: "Do NOT use read_file on existing source files — explore with search_files or terminal cat instead. If you read a file, never feed its output into write_file."
   - Agent follows TDD per the leaf

### Phase 2: Review and Commit Each Child

After each implementation agent returns, the orchestrator reviews in-session (main model, NOT a delegated subagent):

1. Read the changed files from the implementer's file list.
2. Check against leaf spec + contracts + Review Checklist.
3. Run `go test ./internal/<pkg>/... -race -count=1`.

If review finds gaps: re-dispatch with findings. Max 3 cycles; then escalate.

If review passes: `git add <exact paths> && git commit -m "feat(harness-eval): implement [leaf name]"`. Update tracking table to REVIEWED.

### Phase 3: Integration Review

After ALL children reach REVIEWED:

1. `go test ./... -race` in a pristine worktree if siblings are dirty.
2. `make lint-ci` scoped; do not reformat untouched files.
3. Smokes in Integration Test Plan.
4. Normalize gofmt on touched Go files only.
5. Verify no line-number corruption: no `     N|` prefixes in source.
6. Commit integration fixes separately if any.
7. Mark children COMPLETE.

## Review Checklist

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (see Coding Conventions below)
- [ ] No scope creep (nothing beyond spec)
- [ ] No obvious bugs or security issues
- [ ] No debug artifacts: no print/stdout debugging, no TODOs, no placeholder values, no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes baked into source files
- [ ] Production callers exist for every new exported function (grep SetX/RegisterX)
- [ ] UI strings lowercase
- [ ] No `os.Getwd()` in daemon code
- [ ] IDs from `pkg/id.Generate`, never `time.Now().UnixNano`

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language/Framework:** Go 1.24+, existing meept modules. Flutter only in leaves 17. Swift only in leaf 18.
- **Naming:** exported PascalCase, unexported camelCase
- **Imports:** stdlib / third-party / local groups. No new deps without this orchestrator's note.
- **Error handling:** wrap with `%w`, return early, no panic in libs, no `_ = err`
- **Testing:** table-driven, alongside `_test.go`, `-race`
- **Formatting tool:** gofmt on touched files only
- **Config:** schema.go json+toml tags + DefaultConfig(); fail-fast Validate for new enums
- **AGENTS.md:** session_id vs conversation_id; multiuser off-path unchanged; WS chat_message classification

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-eval-core | PENDING | 0 | |
| 02-eval-cli | PENDING | 0 | |
| 03-eval-http | PENDING | 0 | |
| 04-coder-gates | PENDING | 0 | |
| 05-verify-breaker | PENDING | 0 | |
| 06-shadow-capture | PENDING | 0 | |
| 07-shadow-honesty | PENDING | 0 | |
|| 08-trajectory-judge | REVIEWED | 06862e82 |
|| 09-trusted-root | REVIEWED | 46baf6e9 |
|| 10-isolation | REVIEWED | 4088767b |
|| 11-speak-router | REVIEWED | ed9916c0 |
|| 12-memory-facts | REVIEWED | b8441ef6 |
|| 13-status-bar | REVIEWED | 22a3a252 |
|| 14-event-rewake | REVIEWED | 31513939 |
||| 15-docs-flags-metrics | REVIEWED | 659f72ae |
|| 16-tui-parity | REVIEWED | 79a54e48 |
|| 17-flutter-parity | REVIEWED | 79a54e48 |
|| 18-menubar-parity | REVIEWED | 79a54e48 |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go test ./internal/eval/ ./internal/agent/ ./internal/shadow/ ./internal/selfimprove/ ./internal/memory/ ./internal/employee/ -race -count=1`
2. Coder mutating turn: gate runs `go test ./...`; fail blocks completion.
3. Coder read-only turn: gate skipped.
4. Isolated pair spawn: child prompt contains brief+artifacts, not parent tool dumps.
5. Detached GoalLoop final text: notify fired; no chat_message WS event.
6. Session chat: final text still a bubble; no duplicate notify.
7. Applier reject: patch touching `internal/security/` returns ErrTrustedRoot.
8. `meept eval run` with k=2 writes a RunRecord JSON that unmarshals into C1.
9. HTTP GET `/api/v1/eval/runs/{id}` matches CLI record.
10. features.md no longer claims closed-loop shadow training.

## Open Questions

- meept-bench live GAIA is out of this tree; RunRecord JSON is the join.
- Default gate command is Go. Flutter/Python projects set a per-goal gate later.
- Memory eval (LoCoMo) lives in meept-bench, not this tree.
- Voice duplex and robotics stay skipped.

## Notes

- `internal/agent/loop.go` is ~6k lines. Leaves 05/06/11 must patch call sites, not rewrite the file.
- `CaptureToolInteraction` already exists. Leaf 06 wires specialists; do not duplicate the API.
- Employee Goal.Gate already exists. Leaf 04 is roster AGENT.md, not a second GoalLoop gate.
- Daemon CWD is not the project. Oracles and gates take an explicit workdir.
- Sibling sessions may dirty shared files. Scope tests to owned packages.
