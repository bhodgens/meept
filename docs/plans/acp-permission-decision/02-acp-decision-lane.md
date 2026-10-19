# Leaf 02 — ACP session decision lane (Contract B)

Parent: `master.md` (Contract B, decision 3). Depends on leaf 01 (mode +
threshold fields exist — commit first, read its API).
Branch: acp.

## Scope

`handlePermission` consults a Decisioner when `permission_mode == "decision"`
and fails CLOSED otherwise. Reuses `llm.DecisionClient` via a local interface.

## Tasks

### Task 1: failing tests (existing files only)

- `internal/acp/session_test.go`: add a stub `Decisioner` (records calls;
  returns scripted result/error). Cases:
  - mode `decision`, stub answers `"allow"`, confidence 1.0 ≥ threshold 0.5 →
    reply outcome `selected`/`allow-once`; event `permission_decision` text
    `allow`.
  - mode `decision`, stub answers `"deny"` → outcome `cancelled`; event
    `deny`.
  - mode `decision`, nil Decisioner → cancelled, no call, Warn logged.
  - mode `decision`, Decisioner.Available()==false → cancelled.
  - mode `decision`, Ask returns error → cancelled (fail-closed — opposite of
    builtin gate; pin with a test comment).
  - mode `decision`, answer `"allow"`, confidence 0.4 < threshold 0.5 →
    cancelled.
  - mode `permissive`/`deny` behavior byte-identical (existing cases must
    still pass untouched).
- Do NOT block the test goroutine on the transport: reply assertions read
  `Transport` written frames per the existing io.Pipe pattern (Reply inside
  handlePermission is synchronous — assert on what the fake stdin/stdout
  harness sees).

### Task 2: decision.go (new file)

Per master Contract B: `Decisioner` interface (shape identical to
builtin.decisionAsker — copy the interface, cite the source; NO import of
internal/tools/builtin). Note in file header: `*llm.DecisionClient`
satisfies it without an adapter.

### Task 3: session.go changes

- `SessionConfig` += `Decision Decisioner`, `DecisionThreshold float64`.
- `const decisionAskTimeout = 15 * time.Second` — bounded, context.Background
  derived (handlePermission has no caller ctx).
- Rewrite `handlePermission` per Contract B dispatch. Keep the
  `approved`/`denied` event text shape for permissive/deny paths unchanged so
  existing consumers see the same stream.
- Question render per Contract B: bounded 300-rune state, fixed prompt,
  options `["allow","deny"]`.

### Task 4: manager.go changes

- Field `decision Decisioner` + `decisionThreshold float64` (threshold copied
  from `cfg.ACP` — Manager already holds the ACPConfig; verify field name).
- `SetDecisionClient(d Decisioner)` nil-safe.
- `GetOrCreate`: stamp `cfg.Decision`, `cfg.DecisionThreshold` into the
  SessionConfig passed to `startFn` — ONLY when `PermissionMode == "decision"`
  (leave nil otherwise; byte-identical non-decision lane).

### Task 5: e2e extension

`e2e/suites/acp-wire/` (build tag `e2e`): add a scenario — testdata fake
agent sends `session/requestPermission`; daemon-side manager configured
`permission_mode: "decision"` with an injected stub Decisioner (allow then
deny); assert wire reply + `permission_decision` event in read verb output.
Manifest already maps `internal/acp/ -> acp-wire` — no new package, no
manifest change.

## Verification

```
go test -p 2 ./internal/acp/ -run 'Permission|Decision' -count=1
make e2e-fast-area AREA=acp-wire
go build ./internal/acp/
```

## Rules

- `grep -n os.Getwd internal/acp/*.go` stays zero.
- Never hold `s.mu` across the Ask call (mutexio): read config fields under
  lock, unlock, then Ask.
- All UI/log text lowercase (repo convention).

## Stop clause

If `handlePermission` cannot get a bounded context without touching the
Transport API, STOP and report — do not widen Transport.
