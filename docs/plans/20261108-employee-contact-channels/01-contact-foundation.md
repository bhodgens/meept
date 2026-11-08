# Contact foundation (registry + chain + ended-state prereq) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The failing tests ALREADY EXIST and
> are committed (test gate 00T) - your job is to make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test (report suspected-wrong
> gate tests to the orchestrator instead). Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/contact package (Record/Store/ChainRunner) + the phone-leg ended-state prereq fix (GH #123) + config surface.
- **Dependencies:** 00T gate tests (committed, RED)
- **Estimated Context:** 55K
- **Concurrency Group:** A

## Goal

Deliver contracts C1 and C2 from ../master.md: the per-person contact registry (phone E.164 + nullable signal UUID + ordered channel policy) and the fallback ChainRunner (one employee action, ordered channel attempts, per-attempt failure detection, shared chain execution id, no double-notification). Also close GH #123: register the call hold on BOTH phone legs so calls.db rows reach the ended state, upgrading the voice-local e2e logged gap to a hard assertion.

## Context

Meept is a Go agent daemon at /Users/caimlas/git/meept. Channels implemented elsewhere in this tree implement the C2 ChannelAttempt seam; this leaf owns the seam itself and the registry the chain resolves people from. The store mirrors the SQLite patterns in internal/voice/callstore.go (table + CRUD + migrations). The ended-state fix: internal/voice/AGENTS.md documents the gap precisely ("Goal-hold feeding" section); AcquireCallHold at internal/daemon/voice_employee.go:267 is today called ONLY by the local leg's Hold seam - the phone inbound and outbound paths never register the hold, so ReleaseHook -> heldCalls never sees phone ccids and Binding.OnCallEnd never fires for them.

## Verified Anchors

- internal/voice/callstore.go:37 - calls.db schema pattern (`employee_id TEXT NOT NULL DEFAULT ''` etc.)
- internal/daemon/voice_employee.go:267 - `func (w *VoiceEmployeeWiring) AcquireCallHold(ccid, employeeID string) func()` - the hold seam
- internal/voice/AGENTS.md "Goal-hold feeding" - the gap contract
- internal/voice/dial.go:624-633 - the NormalizeE164 leg-conditional rule (pattern for E.164 normalization in the contact store)
- internal/config/schema.go:5330 - voice config block pattern (`default_leg` etc.) for adding the `[contact]` section
- e2e/suites/voice-local/voice_local_test.go:858-866 - the logged known gap to upgrade to a hard assertion

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
package contact // internal/contact/types.go, store.go, chain.go

type Channel string // "telnyx_voice" | "signal_voice" | "signal_msg" | "telnyx_sms"

type Record struct {
    ID          string
    DisplayName string
    Phone       string    // E.164 normalized at write
    SignalUUID  string    // empty until observed
    Policy      []Channel // empty = caller passes employee default at chain time
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type Store interface {
    Put(ctx context.Context, rec Record) error
    Get(ctx context.Context, id string) (Record, error) // ErrUnknownContact
    GetByPhone(ctx context.Context, e164 string) (Record, error)
    GetBySignalUUID(ctx context.Context, uuid string) (Record, error)
    List(ctx context.Context) ([]Record, error)
    Delete(ctx context.Context, id string) error
}
func NewSQLiteStore(db *sql.DB, log *slog.Logger) (*SQLiteStore, error) // creates `contacts` table via migration

type ChannelAttempt interface {
    Channel() Channel
    Attempt(ctx context.Context, rec Record, message string) (outcome string, err error)
}

type ChainRunner struct{}
func NewChainRunner(attempts map[Channel]ChannelAttempt, delays map[Channel]time.Duration) *ChainRunner
func (c *ChainRunner) Run(ctx context.Context, rec Record, message string, override []Channel) ChainReport
type ChainReport struct {
    ChainID  string
    Attempts []AttemptOutcome // {Channel, Outcome, Err, DurationMS}
}
```

Config (internal/config/schema.go): `[contact]` section - `default_channels []string` (employee default chain), `hop_delay_ms int` (default 5000). Wire into DefaultConfig().

### What This Leaf Consumes

- Effects ledger + audit log seams (internal/effects, internal/auditlog) for chain/attempt rows - inspect their existing call pattern in the phone dial path (internal/voice) and reuse it; the chain writes ONE execution row + one attempt row per attempt, keyed by ChainID.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T0 | Phone-leg ended-state fix (GH #123) | Modify: internal/daemon/voice_wiring.go (register hold on inbound answer + outbound dial paths, same pattern as LocalDeps.Hold); Modify: e2e/suites/voice-local/voice_local_test.go:858-866 (upgrade logged gap to hard assertion) | `go test ./internal/daemon/ -run VoiceEmployee; go test -tags e2e ./e2e/suites/voice-local/` | hold nil on phone paths -> rows stay 'active' | phone inbound+outbound rows reach 'ended'; local leg unchanged; double-end idempotent | Given a completed outbound phone call; When release fan-out runs; Then calls.db row state == 'ended' |
| T1 | types.go + store.go | Create: internal/contact/types.go, internal/contact/store.go; Test: internal/contact/store_test.go (exists, RED) | `go test ./internal/contact/ -run Store` | gate tests RED | CRUD + normalize + GetByPhone/GetBySignalUUID + ErrUnknownContact green | Given Put with Phone "13055551234"; When GetByPhone("+13055551234"); Then record returned (normalized at write) |
| T2 | SQLite migration + table | Modify: internal/contact/store.go (contacts table: id, display_name, phone UNIQUE, signal_uuid, policy JSON, timestamps) | `go test ./internal/contact/ -run Store` | table absent | concurrent Put on same phone -> second wins (upsert) + no constraint crash | Given two Puts same phone; Then GetByPhone returns latest |
| T3 | chain.go ChainRunner | Create: internal/contact/chain.go; Test: internal/contact/chain_test.go (exists, RED) | `go test ./internal/contact/ -run Chain` | ChainRunner undefined | ordered attempts; stop on delivered/answered; busy/unavailable/no_answer/failed fall through; per-attempt delay honored via injected clock; ONE ChainID across attempts; override policy wins over record policy | Given attempts [telnyx_voice:no_answer, signal_voice:busy, signal_msg:delivered]; When Run; Then 3 attempts in order, ChainID shared, report final outcome delivered |
| T4 | chain effects/audit rows | Modify: internal/contact/chain.go (execution row before first attempt; attempt row after each; keyed ChainID) | `go test ./internal/contact/ -run Chain` | no ledger rows | chain execution row + N attempt rows visible in fake ledger | Given 2-attempt chain; Then ledger has 1 execution + 2 attempt rows, all with ChainID |
| T5 | config section | Modify: internal/config/schema.go ([contact] struct + DefaultConfig defaults), config/meept.json5 template comment | `go build ./... && go test ./internal/config/` | section absent | defaults present: default_channels ["telnyx_voice","signal_voice","signal_msg","telnyx_sms"], hop_delay_ms 5000 | Given DefaultConfig(); Then cfg.Contact.DefaultChannels has 4 entries |

## Self-Verification Checklist

- [ ] All tasks implemented; gate tests passing (store_test, chain_test GREEN)
- [ ] Interface contracts satisfied exactly (C1, C2 signatures)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] Do NOT COMMIT.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests passing, gate tests untouched
- [ ] Interface contracts match exactly (signatures, types, paths)
- [ ] Conventions followed (named errors, pkg/id, no mutex-across-I/O, json tags); no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption
- [ ] GH #123 fix verified by e2e assertion (not log-only)

Output: APPROVED or specific gaps with file + line references.

## Notes

- Channel constants are ALSO implemented by leaves 11 and 12 - they own the Attempt implementations ("signal_voice"/"signal_msg" via the signal client; "telnyx_sms" via the messaging client; "telnyx_voice" adapts the EXISTING voice_call dial path). This leaf owns the SEAM and REGISTRY only.
- Voice semantics: a CONNECTED voice call is a chain STOP (outcome "answered"), not a failure - the call proceeds on its own session; the chain does not wait for the conversation to finish. Only no-answer/busy/unavailable/failed advance the chain.
- Keep the contacts table schema additive; no destructive migration.
