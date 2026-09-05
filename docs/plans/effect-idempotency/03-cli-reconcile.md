# CLI Reconcile Surface + Docs - Implementation Leaf

## DISPATCH INSTRUCTION

> You are the implementer for this leaf. Work ONLY from this document.
> Implement ALL tasks below using TDD (test first, watch it fail, implement,
> watch it pass). Do NOT commit. Do NOT run git add. The orchestrator handles
> all git operations after review.
> Do NOT use read_file on existing source files — explore with search_files
> or terminal cat. Never pipe read_file output into write_file
> (line-number corruption rule: `NN|` prefixes must never reach a source
> file). After writing a file, do NOT read it back to verify — write once
> and stop. After completing, report what you built, what files you touched,
> test results, and any deviations from the spec.

## Scope

The human-reconciliation surface: RPC methods `effects.list` /
`effects.reconcile` on the existing daemon RPC server, CLI commands
`meept effects list` / `meept effects reconcile <key>` in cmd/meept, the
`docs/workflows/effects.md` feature doc, and the AGENTS.md Key Components
table row. No new business logic beyond thin transport over the ledger.

**Files:**
- Create: `internal/rpc/effects.go` (handler registration)
- Create: `internal/rpc/effects_test.go`
- Create: `cmd/meept/effects.go` (cobra commands)
- Create: `cmd/meept/effects_test.go`
- Modify: `cmd/meept/main.go` (one AddCommand line)
- Create: `docs/workflows/effects.md`
- Modify: `AGENTS.md` (Key Components table row)

**Test command:** `go test -p 2 ./internal/rpc/... ./cmd/meept/...`
(macOS ephemeral-port rule: always `-p 2`).

## Dependencies

- 02-tool-wiring.md must be REVIEWED first (the reconciler this CLI drives
  exists; the ledger is constructed in the daemon).

## Estimated context

~60K

## Interface Contract (From Parent)

From master.md Contract 6, verbatim:

```
// RPC (internal/rpc/effects.go, registered from internal/daemon/daemon.go
// next to the plan handler registration (~lines 785-796), DIRECT
// RegisterHandler closures — NOT a bus proxy. AGENTS.md: "A proxy with no
// responder blocks the caller for the full timeout" — never proxy this).
type EffectsRPCHandler struct{ /* ledger effects.Ledger */ }

func (h *EffectsRPCHandler) RegisterEffectsMethods(server *rpc.Server) {
    server.RegisterHandler("effects.list", h.handleList)
    server.RegisterHandler("effects.reconcile", h.handleReconcile)
}

// effects.list
//   params: {"state"?: "claimed"|"receipted"|"completed"|"abandoned",
//            "key"?: string}
//   result: {"effects": [ <effects.EffectRecord as JSON> ... ]}
//           ordered oldest claimed_at first when state omitted/pending;
//           explicit state filter uses the same ordering.
//   No ledger wired => {"effects": []} (empty, NOT an error — parity with
//   the ParkStore-degradation posture).
//
// effects.reconcile
//   params: {"key": string,              // required
//            "action": "complete"|"abandon",  // required
//            "receipt"?: json,       // complete only: optional receipt
//                                    // overwrite recorded before Complete
//            "reason"?: string}      // abandon only (required, non-empty)
//   result: {"record": <updated EffectRecord>}
//   unknown key => error "effects: unknown effect key" (ErrUnknownKey).
//   action missing/invalid => "invalid action".
//   abandon without reason => "reason required for abandon".
```

```go
// CLI (cmd/meept/effects.go, registered in cmd/meept/main.go's AddCommand
// block between newChangesCmd() and newRoutingCmd() — ~line 187-188):
//
//   meept effects list [--state=...] [--json]
//   meept effects reconcile <key> --complete [--receipt '<json>']
//   meept effects reconcile <key> --abandon --reason '<text>'
//   meept effects reconcile <key>    # no flags: print the record + hint
```

Registration style to copy: `cmd/meept/plans.go` — `newPlansCmd()` returns a
cobra command, subcommands added via `cmd.AddCommand(...)`, leaf commands
connect to the daemon with `connectDaemon()` + `client.Call("<method>",
params)`, table output via `tabwriter`, `--json` via `json.MarshalIndent`,
errors via `fmt.Errorf("...: %w", err)`. Registration in
`cmd/meept/main.go`: `rootCmd.AddCommand(newEffectsCmd())` — one line, same
block as `newPlansCmd()` (~line 171). If cmd/meept has a `getStringOr`-style
helper (plans.go uses one), reuse it; do not duplicate.

Docs contract (master.md Contract 7):
- `docs/workflows/effects.md` — new; the `internal/effects` →
  `docs/workflows/<pkg>.md` mapping. Content: what the ledger is, the
  Claim→execute→receipt→complete protocol, the EffectKey rule (one
  sentence + example), which tools are wired, startup/resume reconcile
  behavior (auto-retry only for provider-idempotent tools; everything else
  waits for `meept effects reconcile`), CLI usage examples.
- `AGENTS.md` Key Components table — add exactly one row after the
  **Session** row:
  `| **Effects** | ` + "`internal/effects` (external-effect idempotency ledger)" + ` |`

## Context

This leaf is the "human reconciliation" half of the pinned protocol: when
reconcile surfaces a stuck claimed/receipted effect for a tool that is NOT
provider-idempotent (e.g. push notification), the ONLY sanctioned fix paths
are (a) `--complete` with a hand-verified receipt (the human confirmed the
effect actually landed), or (b) `--abandon` (the effect is dead; the record
stays for audit). The CLI makes both possible without a daemon restart and
without ever prompting the agent.

Verified anchors:
- RPC registration: `internal/rpc/server.go` `RegisterHandler` (~line 175);
  handler-signature `func(ctx context.Context, params json.RawMessage)
  (any, error)`; the plan-handler pattern `internal/rpc/plan.go`
  `RegisterPlanMethods` (~line 45) is the style to copy, including its
  `avail()` availability guard (here: ledger nil → "effects service not
  available").
- Daemon-side registration point: `internal/daemon/daemon.go` `New` (~line
  118), the block registering `planRPCHandler` (~lines 785-796). Leaf 02
  put the ledger on Components (`EffectsLedger`); construct
  `EffectsRPCHandler` there and register the two methods. If the ledger is
  nil (open failed), register nothing — the CLI then gets
  "method not found" and prints a "effects ledger unavailable" hint; note
  this in deviations.
- CLI daemon connection: `connectDaemon()` + `client.Call(...)` as in
  `cmd/meept/plans.go` lines 38-54; output helpers `getStringOr` etc. live
  in the same package (verify names with search_files: `func getStringOr`).
- AGENTS.md table: the Key Components table at ~lines 88-107; Session row
  is line 99. Docs mapping rule at AGENTS.md ~line 651.

## Tasks

### Task 1: RPC handler (TDD)

**Objective:** `effects.list` + `effects.reconcile` registered and correct.

**Files:**
- Test: `internal/rpc/effects_test.go`
- Create: `internal/rpc/effects.go`

**Steps:**

1. Write tests first against an in-memory ledger (leaf 01) + a real
   `rpc.NewServer(...)` instance (copy construction from an existing rpc
   handler test — search_files `internal/rpc/*_test.go` for the fixture
   pattern; if none exists, call the unexported `handleList` /
   `handleReconcile` methods directly with `json.RawMessage` params and
   skip the server fixture, stating that choice).

```go
func TestEffectsRPC(t *testing.T) {
    // subtests:
    // - "list empty": no ledger activity -> {"effects": []}
    // - "list filters by state": seed claimed + completed; state=claimed
    //   returns only the claimed one, oldest first
    // - "list all": state omitted -> pending (claimed+receipted) only
    // - "reconcile complete": seeded claimed -> completed, record echoed
    // - "reconcile complete with receipt": prior receipt overwritten
    // - "reconcile abandon": reason stored (record state abandoned)
    // - "reconcile abandon no reason": error "reason required for abandon"
    // - "reconcile bad action": error "invalid action"
    // - "reconcile unknown key": ErrUnknownKey error text
    // - "no ledger": avail() error
}
```

2. Implement `EffectsRPCHandler` exactly per the contract. Receipt
   overwrite on `complete`: when `receipt` is present, call
   `RecordReceipt` first (works from claimed; for receipted priors it is a
   no-op overwrite ONLY if the state machine allows — if not, document the
   exact behavior chosen), then `Complete`. Never re-execute anything here.

3. Wire registration in `internal/daemon/daemon.go` next to the plan
   handler (~line 794) — one small block, ledger from Components, nil-guarded.

4. `go test -p 2 ./internal/rpc/... ./internal/daemon/...` green.

### Task 2: CLI commands (TDD)

**Objective:** `meept effects list` and `meept effects reconcile`.

**Files:**
- Test: `cmd/meept/effects_test.go`
- Create: `cmd/meept/effects.go`
- Modify: `cmd/meept/main.go`

**Steps:**

1. Test style: copy from `cmd/meept/plans`-adjacent tests (e.g.
   `cmd/meept/backup_cmd_test.go`) — if existing tests exercise cobra
   commands directly with a fake RPC client, follow that; otherwise test
   the pure formatting funcs (row rendering, state filter parsing) and
   exercise the cobra tree with `cmd.SetArgs` + `SetOut`, stubbing
   `connectDaemon` if it is a package var, else document that RPC-level
   coverage comes from Task 1 and the CLI layer is thin transport (state
   it in your report).

2. Implement per the contract. Details that matter:
   - `list`: default output = tabwriter table
     `KEY\tSTATE\tTOOL\tCLAIMED\tAGE`; `--json` = the raw RPC result
     indented. KEY column truncates to 16 chars + "…"? — NO: effect keys
     are identities; print them in full (copy-paste target for reconcile).
     AGE is humanized (e.g. `3h12m`) from claimed_at; lowercase text.
   - `reconcile` requires exactly one of `--complete` / `--abandon`;
     neither flag → print the current record (via `effects.list` with
     `"key"`) + the hint line:
     `confirm the effect's real outcome, then re-run with --complete [--receipt '<json>'] or --abandon --reason '<text>'`
     (lowercase), exit 0.
   - `--abandon` without `--reason` → cobra error (exit non-zero).
   - Errors from the daemon surface verbatim (`fmt.Errorf("effects: %w", err)`).

3. Register in main.go: `rootCmd.AddCommand(newEffectsCmd())` in the
   AddCommand block (~line 187).

4. `go test -p 2 ./cmd/meept/...` green; `go build ./cmd/meept` green.

### Task 3: docs/workflows/effects.md

**Objective:** the feature doc per the docs-mapping rule.

**Files:** Create `docs/workflows/effects.md`

**Required sections** (match the shape of `docs/workflows/change-journal.md`
— read it with terminal cat for tone/structure): overview, why (crash +
resume gap), the protocol (Claim → execute → verify → RecordReceipt →
Complete), effect keys (rule + one example:
`EffectKey("backup.git_push", "/repo", "abc123")`), wired tools table
(push notify: not provider-idempotent; backup push: provider-idempotent),
reconcile on startup and parked-turn resume (auto-retry policy, surfacing
policy, NO new bus topics — visibility is this CLI + logs), CLI usage
(`meept effects list` / `reconcile` examples), states table
(claimed/receipted/completed/abandoned), storage (`<data_dir>/effects.db`,
schema summary), and a "adding a new effect tool" checklist (derive key →
`effects.Run` → `Complete` → register executor only if provider-idempotent →
update docs).

### Task 4: AGENTS.md row + verification

**Objective:** the convention-file update the AGENTS.md maintenance rule
requires for a new internal package.

**Files:** Modify `AGENTS.md`

**Steps:**

1. Add the one Key Components row (Contract 7) after Session (~line 99).
   Nothing else in AGENTS.md changes — no new invariant section is warranted
   (no bus topics, no WS classification, no cross-boundary contract beyond
   what docs/workflows/effects.md covers).

2. Full verification:
   - `go build ./...` green.
   - `go test -p 2 ./internal/effects/... ./internal/rpc/... ./cmd/meept/...
     ./internal/daemon/... -count=1` green.
   - `gofmt -l` on touched dirs: empty.
   - Docs cross-check: `docs/workflows/effects.md` exists; AGENTS.md row
     present; `make graphs` NOT needed (no new bus topics).
   - Line-number corruption check:
     `grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/rpc cmd/meept` → zero.

## Self-Verification Checklist

- [ ] RPC: direct RegisterHandler closures (NOT a bus proxy) — no orphan
      proxy risk
- [ ] `effects.list` never errors on unwired ledger; returns empty list
- [ ] `effects.reconcile --complete` and `--abandon` map to
      `Ledger.Complete` / `Ledger.Abandon` — no re-execution path exists in
      this leaf
- [ ] CLI: full keys printed (no truncation), lowercase output text,
      `--json` supported on list, neither-flag reconcile shows record + hint
- [ ] `--abandon` requires non-empty `--reason`
- [ ] main.go registration is one line in the existing AddCommand block
- [ ] docs/workflows/effects.md covers protocol, keys, tools, reconcile
      policy, CLI, states, storage, new-tool checklist
- [ ] AGENTS.md has exactly the one new Key Components row
- [ ] `go test -p 2` everywhere (always `-p 2`)
- [ ] No debug artifacts: no print debugging, no TODOs, no placeholder values
- [ ] No line-number corruption (`NN|` prefixes) in any file

## Review Checklist

- [ ] All tasks implemented; scope respected (no business-logic changes)
- [ ] Interface contract from master.md Contract 6-7 satisfied exactly
- [ ] Tests written first and passing (TDD followed)
- [ ] CLI style matches cmd/meept conventions (cobra + connectDaemon +
      tabwriter + --json)
- [ ] No obvious bugs or security issues (reconcile is owner-trusted RPC —
      correct per the AGENTS.md RPC trust boundary; no auth added to the
      socket path)
- [ ] No debug artifacts: no print debugging, no TODOs, no placeholder
      values, no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes baked into source
- [ ] Do NOT commit. Do NOT run git add. — confirmed

Report: what you built, files touched, test output summary, deviations
(especially: the receipt-overwrite-on-receipted-state decision, and the CLI
test-fixture approach).
