# Evaluation (Eval) & Harness

The `internal/eval` package provides two related but distinct capabilities:

1. **Trace analysis evaluation** — classification benchmarks, failure mode
   analysis, and conversational analysis sessions for interactive trace review
   (the HALO trace workflow).
2. **Agent run evaluation** — harness-driven evaluation of agent turns with
   oracle-gated verdicts, Pass^k scoring, and trajectory judgment.

---

## Agent Run Evaluation

The harness-eval leaves (01–18 from `docs/plans/20260829-harness-eval/master.md`)
added measurement, isolation, and honest learning to the daemon. The user-visible
outcome is the `meept eval` CLI plus the HTTP `/api/v1/eval/runs` endpoint.

### RunRecord shape (C1)

```go
type RunRecord struct {
    ID          string    `json:"id"`
    CreatedAt   time.Time `json:"created_at"`
    Kind        Kind      `json:"kind"`       // "pass_k" | "model_swap" | "ablation"
    TaskID      string    `json:"task_id"`
    HarnessHash string    `json:"harness_hash"` // sha256 of prompts+tool list+gate
    ModelID     string    `json:"model_id"`
    K           int       `json:"k"`            // consecutive attempts for Pass^k
    Attempts    []Attempt `json:"attempts"`
    Passed      bool      `json:"passed"`       // Pass^k: all K consecutive oracle-pass
    OracleName  string    `json:"oracle_name"`
}

type Attempt struct {
    Index        int          `json:"index"`
    ModelID      string       `json:"model_id"`
    Passed       bool         `json:"passed"`
    Oracle       OracleResult `json:"oracle"`
    TrajectoryID string       `json:"trajectory_id,omitempty"`
}

type OracleResult struct {
    Passed bool   `json:"passed"`
    Output string `json:"output"` // truncated 4KB
    Err    string `json:"error,omitempty"`
}
```

### ShellOracle

```go
type ShellOracle struct {
    Name_   string
    Command string
    Timeout time.Duration
}
```

Exit 0 = pass. Never uses `os.Getwd()` — callers pass an explicit workdir.

### CLI verbs

| Verb | Command |
|------|---------|
| run | `meept eval run --task=<id> --kind=pass_k --k=2` |
| list | `meept eval list [--task=<id>] [--limit=20]` |
| show | `meept eval show <run-id>` |

### HTTP endpoint

```
GET /api/v1/eval/runs
GET /api/v1/eval/runs/<id>
```

Returns `RunRecord` JSON. The same JSON shape is accepted by meept-bench as the
join point.

### Pass^k

A run with `k=2` records two attempts. The run is `Passed=true` only when both
attempts pass the oracle. This guards against flaky single-shot scores.

### Trajectory judgment (leaf 08)

After a run completes, `Judge(ctx, steps, oracle, workdir)` produces a
`TrajectoryJudgment{Passed, FirstErrorStep, Summary}`. The evolver's
`OnlyJudged` gate ensures only judged trajectories feed self-improvement.

### Status bar (leaf 13)

Every turn injects a stable-prefix section showing:

- turn index
- tools-this-turn count
- isolation mode (`artifact_only` / `shared_transcript`)
- speak kind (`session` / `notify` / `parent`)
- gate status (`skipped` / `passed` / `failed` / `n/a`)

Errors in the bar fail-closed to `"unknown"`, never invent success.

### Isolation defaults (leaf 10)

- Dispatcher handoff → `ArtifactOnly`
- Subagent delegate → `ArtifactOnly`
- Pair → `ArtifactOnly` (opt-in `SharedTranscript` via explicit flag)
- Parent tool dumps and chain-of-thought never copy under `ArtifactOnly`

### Speak routing (leaf 11)

- Session-attached turns → chat bubble (`SpeakSession`)
- Detached goal loops → `notify_user` / push (`SpeakNotify`)
- Isolated children → parent report only (`SpeakParent`)

Isolated children cannot call `reply_to_user`; the builtin tool returns
`ErrIsolatedSpeak`.

### Trusted root (leaf 09)

The change applier denies writes to:

- `internal/security/`
- `pkg/security/`
- `internal/eval/`
- `internal/selfimprove/applier.go`
- `internal/selfimprove/validator.go`
- gate config paths

Path traversal (`..`, absolute paths, symlink escape) is also denied.

### Memory facts (leaf 12)

`MemoryFact` is a typed, time-bounded preference/restriction/account/temporal
store. Retrieval is via the `memory_fact_search` tool plus optional injection
(capped). Extraction runs after session close via the reflection path.

---

## Trace Analysis Evaluation (HALO)

The `AnalysisSession` type supports multi-turn human-in-the-loop review of trace
analysis results. Sessions have a lifecycle: **active** → **paused** →
**completed**. Each turn records a user query, analyst response, and references
to trace/span IDs.

### State machine

```
active ──Pause()──> paused ──Resume()──> active
  │                        │
  │  Close()               │  Close()
  └────────────────────────┴──> completed (read-only)
```

### Usage

```go
mgr := NewAnalysisSessionManager(basePath)
session := mgr.CreateSession(traceIDs, failureModes)
session.AddTurn("What traces are affected?", "3 traces show the issue.", nil, nil)
session.Close()
data, _ := session.ExportJSON()
```

### Edge cases

- Adding turns after `Close()` returns `nil` — caller must check
- `Resume()` and `Pause()` are no-ops on incompatible states (no panic)
- `GetFollowUpSuggestions()` returns `nil` for sessions with no turns
- Follow-up suggestions are generated at turn-commit time, not lazily
