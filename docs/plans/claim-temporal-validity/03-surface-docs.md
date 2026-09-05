# Leaf 03 — Surface: Tool Params, Expired Listing, Docs

<!-- DISPATCH INSTRUCTION
Dispatch as ONE implementation-agent session AFTER leaves 01 and 02 are in
the working tree (this leaf's tool + CLI call StoreClaim with new fields and
Manager.ListExpiredClaims). Go only for code; docs edits per Scope. The agent
reads this leaf + master.md contracts + the source files in Dependencies
first.
-->

## Scope

Exactly three changed files:

1. `internal/tools/builtin/retain_typed.go` — `RetainClaimTool` gains
   `valid_from` / `valid_to` / `observed_at` params; new
   `ListExpiredClaimsTool` in the same file.
2. `cmd/meept/memory.go` — new `expired` subcommand (minimal CLI surface,
   per the AGENTS.md wiring requirement: internal-only features are
   incomplete).
3. `docs/workflows/memory.md` — new "### Claim Temporal Validity" section.

Plus the regeneration step (NOT hand-editing):
`docs/reference/generated/memory.md` is produced by gomarkdoc via
`make docs-generate` (→ `mage -d magefiles docsGenerate`; package map in
`magefiles/docs.go:51` maps `internal/memory` → `memory.md`). Task 6 runs the
make target and commits the regenerated output as part of this leaf. **Never
hand-edit files under `docs/reference/generated/`.**

**No other files.** Note the RPC surface (`internal/rpc/epistemic.go`
`retainClaimParams`) is deliberately OUT of scope this leaf — it has no
temporal params yet and touching it would break the 3-file bound; recorded in
OPEN-QUESTIONS Q4 as a small follow-up.

## Dependencies

Read before coding:

- `docs/plans/claim-temporal-validity/master.md` — contract C6.
- `internal/tools/builtin/retain_typed.go` (whole file, 294 lines) —
  `RetainClaimTool` :40-122, `asStringArg` :15, `toStringSlice` :24, the
  sibling tool pattern (`RetainDecisionTool` :126+) to copy for the new
  listing tool.
- `internal/tools/builtin/memory.go` — confirm (do not modify) that it only
  handles episodic/task types (:77-80); the claim surface lives in
  retain_typed.go, not here. Read its schema constant conventions
  (`schemaProp*`, `schemaType*`) since retain_typed.go shares them.
- `internal/memory/epistemic.go` — `StoreClaim` (new fields), `Claim` struct
  (leaf 01 version in working tree), `ListExpiredClaims` (leaf 02).
- `cmd/meept/memory.go` — subcommand structure: `review` :361, `supersede`
  :391, `promote` :439, `reject` :463. Copy the wiring pattern (RPC
  invocation + output formatting) from the smallest sibling.
- `internal/rpc/epistemic.go` — handler registration block (~:40-50) and
  `handleListAutoClaims` as the read-side RPC pattern the new
  `expired` CLI path will ride (or confirms a direct-manager path is used —
  check what `cmd/meept/memory.go` siblings actually call and mirror THAT).
- `docs/workflows/memory.md` — heading structure (## Overview / Behavior /
  Configuration / Observability / Edge Cases) to place the new section
  consistently.
- Existing tool tests for retain tools (search
  `internal/tools/builtin/*_test.go` for `retain` — reuse the harness).

## Estimated context

~70K tokens (leaf doc + master + retain_typed.go + memory.go CLI + RPC +
docs + ~250 lines written).

## Interface Contract

Implements master contract **C6**:

- **Tool params (all optional strings, RFC3339):** `valid_from`, `valid_to`,
  `observed_at` on `retain_claim`. Parse with
  `time.Parse(time.RFC3339, v)`; an invalid value returns an error
  (`fmt.Errorf("invalid %s (want RFC3339): %w", name, err)`) — never silently
  dropped. Empty/absent = not set.
- **Listing tool:** `list_expired_claims` tool, category "memory", single
  optional integer param `limit` (default 20), returns
  `{"success": true, "claims": [...]}` where each entry carries
  `memory_id`, `text`, `valid_to`, `rev`, `status`, and `reason: "expired"`.
- **CLI:** `meept memory expired [--limit N]` prints one claim per line:
  `<id>  rev=<n>  valid_to=<RFC3339>  <first-80-chars-of-text>`, or
  "no expired claims" when the list is empty. Exit code 0 either way.
- **Docs:** `docs/workflows/memory.md` gains `### Claim Temporal Validity`
  under Behavior, documenting: the three new params, the rev semantics
  (monotonic, bumped on supersede), valid_to stamping on supersede,
  hard-exclusion from detection/canonical with the explicit "expired"
  reason, and `meept memory expired`.
- Backward compat: a `retain_claim` call without the new params produces
  byte-identical behavior to today (same metadata keys as before).

## Tasks

### Task 1 — retain_claim temporal params (TDD)

1. Tests first (reuse the existing retain-tool test harness; search for it
   before writing — do not invent):

```go
func TestRetainClaimTemporalParams(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		args    map[string]any
		wantErr bool
		wantVF  bool // valid_from metadata present after store
		wantVT  bool
	}{
		{
			name: "no temporal args = legacy behavior",
			args: map[string]any{"text": "plain claim"},
		},
		{
			name: "valid window",
			args: map[string]any{
				"text":       "bounded",
				"valid_from": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
				"valid_to":   time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
			},
			wantVF: true, wantVT: true,
		},
		{
			name:    "invalid valid_to errors",
			args:    map[string]any{"text": "x", "valid_to": "next tuesday"},
			wantErr: true,
		},
		{
			name:    "invalid observed_at errors",
			args:    map[string]any{"text": "x", "observed_at": "2026-13-45"},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// tool := NewRetainClaimTool(manager) using the existing test
			// manager harness; Execute(tc.args); assert err/wantErr and, on
			// success, read the stored memory and check valid_from/valid_to
			// metadata presence matches wantVF/wantVT.
		})
	}
}
```

2. Implementation in `RetainClaimTool.Parameters`: add three string
   properties with descriptions naming RFC3339 explicitly. In `Execute`,
   after the existing arg mapping:

```go
	claim := memory.Claim{ /* existing mapping unchanged */ }

	for name, dst := range map[string]**time.Time{
		"valid_from":  &claim.ValidFrom,
		"valid_to":    &claim.ValidTo,
	} {
		raw, ok := args[name]
		if !ok || raw == "" {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("invalid %s (want RFC3339 string)", name)
		}
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fmt.Errorf("invalid %s (want RFC3339): %w", name, err)
		}
		t := parsed.UTC()
		*dst = &t
	}
	if raw, ok := args["observed_at"]; ok && raw != "" {
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("invalid observed_at (want RFC3339 string)")
		}
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fmt.Errorf("invalid observed_at (want RFC3339): %w", err)
		}
		claim.ObservedAt = parsed.UTC()
	}
```

   (Prefer the explicit three-block form over the map-loop if the map-loop's
   double pointer indirection reads poorly — house style favors plain code;
   either is acceptable, tests decide.)

3. Also map `args["observed_at"]` → `claim.ObservedAt` (shown above).
4. Run: `go test -p 2 -run 'TestRetainClaim' ./internal/tools/builtin/`

### Task 2 — list_expired_claims tool (TDD)

1. Test: store one expired + one live claim through the manager harness,
   execute the tool, assert exactly the expired one returns with
   `reason: "expired"` and a parsable `valid_to`.
2. Implementation in `internal/tools/builtin/retain_typed.go`, below
   `RetainClaimTool` (copy the sibling-tool shape):

```go
// ListExpiredClaimsTool surfaces claims whose validity window has closed.
// Hard-exclusion removes them from trust-weighted results silently; this
// tool is the visibility half of that contract.
type ListExpiredClaimsTool struct {
	tools.ToolDefaults
	manager *memory.Manager
}

// NewListExpiredClaimsTool constructs the tool bound to the given manager.
func NewListExpiredClaimsTool(manager *memory.Manager) *ListExpiredClaimsTool {
	return &ListExpiredClaimsTool{manager: manager}
}

func (t *ListExpiredClaimsTool) Name() string     { return "list_expired_claims" }
func (t *ListExpiredClaimsTool) Category() string { return "memory" }

func (t *ListExpiredClaimsTool) Description() string {
	return "List claims whose validity window (valid_to) has passed. " +
		"Expired claims are excluded from trust-weighted results; use this " +
		"to see what has aged out and decide on re-verification or rejection."
}

func (t *ListExpiredClaimsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"limit": {
				Type:        schemaTypeInteger,
				Description: "Max claims to return (default 20).",
			},
		},
	}
}

func (t *ListExpiredClaimsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}
	limit := 20
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	results, err := t.manager.ListExpiredClaims(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list expired claims: %w", err)
	}
	claims := make([]map[string]any, 0, len(results))
	for _, r := range results {
		vt, _ := r.Memory.Metadata["valid_to"].(string) // two-value: absent = ""
		rev := int64(0)
		if f, ok := r.Memory.Metadata["rev"].(float64); ok {
			rev = int64(f)
		}
		claims = append(claims, map[string]any{
			"memory_id": r.Memory.ID,
			"text":      r.Memory.Content,
			"valid_to":  vt,
			"rev":       rev,
			"status":    r.Memory.Metadata["status"],
			"reason":    "expired",
		})
	}
	return map[string]any{"success": true, "claims": claims}, nil
}
```

3. **Registration:** find where `NewRetainClaimTool` is constructed and
   registered (search `internal/tools` + `internal/agent` for
   `NewRetainClaimTool`) and add the new tool to the same registration list.
   If registration happens in a file outside the 3-file scope, STOP and
   report — the orchestrator will either extend scope by one registration
   file explicitly or move registration to leaf follow-up. Do NOT edit a
   fourth file on your own authority.
4. Run: `go test -p 2 -run 'TestListExpiredClaimsTool' ./internal/tools/builtin/`

### Task 3 — CLI `meept memory expired` (TDD where practical)

1. Read `cmd/meept/memory.go` subcommand wiring (:361 `review`, :391
   `supersede`, :439 `promote`, :463 `reject`) and mirror the smallest one.
   Add:

```go
	expiredCmd := &cobra.Command{
		Use:   "expired",
		Short: "list claims whose validity window has closed",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Mirror the RPC-or-manager invocation pattern used by the
			// review/supersede siblings (read them; copy exactly).
			limit, _ := cmd.Flags().GetInt("limit")
			_ = limit // wired to the listExpired RPC / manager call below
			// ... call ListExpiredClaims (same path the siblings use),
			// print "<id>  rev=<n>  valid_to=<RFC3339>  <text[:80]>" per
			// claim, or "no expired claims".
			return nil
		},
	}
	expiredCmd.Flags().Int("limit", 20, "max claims to list")
	// attach to the memory command alongside promote/reject.
```

2. If the CLI test convention covers `cmd/meept` (search
   `cmd/meept/*_test.go` for an existing memory-command test), add a table
   test for the flag default + empty-list output. If `cmd/meept` has no test
   harness for these commands, note that in the report (do not build a new
   harness — out of scope).
3. Run: `go build -o bin/meept ./cmd/meept && ./bin/meept memory expired`
   (against the dev config; an empty result / connection error both prove
   wiring compiles and flags parse — report which you saw).

### Task 4 — docs/workflows/memory.md section

Add under `## Behavior` (after the existing epistemic sections; match
surrounding heading level `###`):

```markdown
### Claim Temporal Validity

Claims carry optional temporal bounds and a monotonic revision counter.

| Field | Metadata key | Meaning |
|-------|--------------|---------|
| ObservedAt | `observed_at` | when the claim was observed true (RFC3339; absent = store time) |
| ValidFrom | `valid_from` | earliest instant the claim is in force (RFC3339; absent = unbounded) |
| ValidTo | `valid_to` | latest instant the claim is in force (RFC3339; absent = unbounded) |
| Rev | `rev` | monotonic revision; 0 on create, +1 on each supersede |

Behavior:

- **Supersede** (`meept memory supersede`) stamps the successor with
  `rev = old rev + 1` and closes the superseded claim's window with
  `valid_to = supersede time` (in place; graph edges keep pointing at the
  same IDs).
- **Enforcement:** claims outside their window are hard-excluded from
  relationship detection and canonical-claim selection, with an explicit
  `expired` reason logged — they never silently vanish from auditability.
- **Visibility:** `meept memory expired` and the `list_expired_claims` tool
  list them; `retain_claim` accepts `valid_from` / `valid_to` /
  `observed_at` (RFC3339).
- **Backward compatibility:** claims stored before this feature have none of
  these keys and behave as unbounded, rev 0.
```

### Task 5 — AGENTS.md maintenance check (read-only)

Per the AGENTS.md maintenance rule: this feature adds no package, no build
target, and no new cross-boundary invariant requiring an AGENTS.md edit (the
docs mapping `internal/memory/` → `docs/workflows/memory.md` already covers
it). Confirm and state so in the report. If you find otherwise (e.g., a new
analyzer trip), report instead of editing AGENTS.md (out of scope).

### Task 6 — Regenerate reference docs

```
make docs-generate
git diff --stat docs/reference/generated/
```

Confirm `docs/reference/generated/memory.md` now mentions
`ListExpiredClaims` and the extended `Claim` fields. Do NOT hand-edit the
generated file. Include the regenerated file in the leaf's changed-files
report (it is a build artifact of the make target, not a hand edit).

### Task 7 — Full leaf verification

```
go build ./...
go vet ./internal/tools/builtin/ ./cmd/meept/
go test -p 2 ./internal/tools/builtin/...
go test -p 2 ./internal/memory/...
```

All green before reporting.

## Self-Verification Checklist

- [ ] `go build ./...` passes; both test suites above green.
- [ ] `retain_claim` without new params behaves byte-identically to before
      (backward-compat case proven by test).
- [ ] Invalid RFC3339 values return errors, never silently dropped.
- [ ] `list_expired_claims` output entries carry `reason: "expired"`.
- [ ] Tool registration wired (or STOP-and-report fired per Task 2.3).
- [ ] CLI subcommand mirrors sibling wiring; `--limit` flag works.
- [ ] docs/workflows/memory.md section matches the surrounding structure.
- [ ] `make docs-generate` run; generated file NOT hand-edited.
- [ ] No debug artifacts, no TODOs, no placeholder values.
- [ ] Line-number-corruption rule: never pipe read_file output into
      write_file; locate edit sites with search_files/terminal.
- [ ] Do NOT commit. Do NOT run git add.

## Review Checklist (for the orchestrator reviewing this leaf)

- [ ] Scope respected: retain_typed.go + cmd/meept/memory.go +
      docs/workflows/memory.md (+ generated memory.md via make target; any
      registration-file scope extension was orchestrator-approved).
- [ ] Two-value assertions on all metadata reads in the tool.
- [ ] Error wrapping matches house style; no ignored errors.
- [ ] Tool descriptions lowercase-consistent with existing sibling tools.
- [ ] Docs section factual (matches implemented behavior, not the aspiration).
- [ ] Do NOT commit. Do NOT run git add. (Orchestrator commits after review.)
