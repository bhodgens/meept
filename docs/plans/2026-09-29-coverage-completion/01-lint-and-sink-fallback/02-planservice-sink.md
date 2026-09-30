# Leaf 02 — PlanService evolver sink fallback

**Objective:** Give `internal/services/plan_service.go` PlanService the
same evolver-sink fallback the RPC layer has (rpc/plan.go, commit
d9688804), so Approve/Reject/Confirm/Revise/Get work for evolver sink
plans whose ids are absent from the shared store. Land the untracked
stray test (`internal/services/plan_service_sink_test.go`) which was
written for exactly this API.

**Files:**
- Modify: `internal/services/plan_service.go`
- Land as-is (fix compile errors): `internal/services/plan_service_sink_test.go` (untracked)

**Contract (mirror rpc/plan.go PlanHandler exactly):**

```go
type PlanService struct {
    manager         *plan.PlanManager
    store           plan.PlanStore
    fallbackManager *plan.PlanManager // evolver sink; either may be nil
    fallbackStore   plan.PlanStore
}

// SetEvolverSink wires the evolver sink manager + store. Either argument
// may be nil; nil values are ignored (setter nil-guard convention).
func (s *PlanService) SetEvolverSink(m *plan.PlanManager, store plan.PlanStore)
```

**Method changes (all five follow the usedFallback pattern from
rpc/plan.go handleApprove — commit d9688804):**

- `Approve` (line ~123), `Reject` (~141), `Confirm` (~159),
  `Revise` (~219): after `s.manager.<Transition>Plan(...)` errors AND
  `s.fallbackManager != nil`, try `s.fallbackManager.<Transition>Plan(...)`;
  on success set `usedFallback = true` and read the plan back from
  `s.fallbackStore.GetPlan` (NOT the shared store — that is the bug this
  leaf fixes). Join errors when both fail, mirroring
  `errors.Join(fmt.Errorf("shared: %w", err), fmt.Errorf("sink: %w", err2))`.
- `Get` (line ~82): if `s.store.GetPlan` errors and fallbackStore is set,
  try `s.fallbackStore.GetPlan`.

**Test fix:** the untracked test's only compile error is the missing
SetEvolverSink method. After implementing, run
`go test -count=1 ./internal/services/ -run TestPlan` — the test's
expectations (sink plan approve succeeds via service, read-back carries
the plan) should pass; adjust only if its stubs need the same
TaskCreator interface the rpc test uses (see
`internal/rpc/plan_sink_fallback_test.go` for the working stub shape).

**Verify:**
```
go build ./...
go test -count=1 ./internal/services/
go test -count=1 ./internal/rpc/          # unchanged behavior
go test -count=1 ./internal/plan/
```

**Commit:** `fix(services): PlanService evolver sink fallback — parity with RPC layer`

**Self-check:** the stray test file must be TRACKED after this leaf
(git status shows it committed, not untracked).
