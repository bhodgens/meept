# Canary Probe — Implementation Leaf

> **For the implementing agent:** The gate tests ALREADY EXIST and are committed
> (internal/llm/runtime_config_canary_test.go, health_checker_canary_internal_test.go).
> Your job: make them pass. Do NOT delete, skip, weaken, or rewrite any gate
> test. If a gate test appears wrong, STOP and report — the orchestrator runs
> the delta protocol. Do NOT commit. Do NOT git add. No read_file→write_file;
> inspect with terminal cat/sed; write once and stop.

## Meta

- **Parent:** ./master.md
- **Scope:** CanaryConfig schema + resolution (Contract 1) and the canary probe inside HealthChecker (Contract 5), including the cold-load guard and ledger bypass.
- **Dependencies:** 00T gate TESTS_VALIDATED
- **Estimated Context:** 85K
- **Concurrency Group:** B

## Goal

Add the inference-canary lane to `internal/llm`: config parses and resolves per Contract 1; the checker, once /health has answered 200 at least once since (re-)arm, probes `POST /v1/chat/completions` every CanaryInterval with a dedicated http.Client (CanaryTimeout). Success = HTTP 200 + parseable non-empty `choices`. Failures accumulate in a counter separate from the /health unhealthyCount.

## Interface Contracts (From Parent)

See master.md Contracts 1 and 5 — inlined in your dispatch context. Key symbols you introduce:

```go
// runtime_config.go
type CanaryConfig struct { Enabled *bool `json:"enabled"`; IntervalSeconds int `json:"interval_seconds"`; TimeoutSeconds int `json:"timeout_seconds"`; Threshold int `json:"threshold"`; Prompt string `json:"prompt"` }
// HealthCheckConfig gains Canary CanaryConfig `json:"canary"`
// RuntimeConfig gains: CanaryEnabled bool, CanaryInterval time.Duration,
//   CanaryTimeout time.Duration, CanaryThreshold int, CanaryPrompt string
// Resolution: 0 → defaults (60s/15s/3/"ping"); Enabled nil → true iff the
//   endpoint baseURL host is loopback (reuse the existing loopback check the
//   F-D8 classifier gate uses; do not write a new one) AND a lifecycle block exists.

// health_checker.go (this leaf adds fields + probe only; verdicts are leaf 02)
// canaryArmed bool (set on first /health 200 after Start), canaryFails int,
// canaryClient *http.Client, runCanary() issuing Contract-5 request shape.
```

Canary request model id: read the endpoint's resolved model id from the same snapshot source the classifier boot gate uses (master Open Question 1 default). If the seam is not reachable from HealthChecker without new plumbing, pass the model id into NewHealthChecker via RuntimeConfig (it already carries the model path/id resolution) — pick the smaller diff and note it in Deviations.

## Tasks

### Task 1: Config schema + resolution (gate: runtime_config_canary_test.go)

**Files:** Modify `internal/llm/runtime_config.go`. Add CanaryConfig to HealthCheckConfig; resolve into RuntimeConfig fields with the defaults and loopback rule above. Keep existing field order/behavior byte-identical when `canary` is absent.

### Task 2: Canary lane in HealthChecker (gate: health_checker_canary_internal_test.go)

**Files:** Modify `internal/llm/health_checker.go`.

1. New fields per contract; canaryClient built in NewHealthChecker with Timeout=CanaryTimeout (separate from the health client).
2. In `checkOnce`: on /health 200 → set canaryArmed=true (guarded: only matters when CanaryEnabled). Existing verdict logic UNCHANGED in this leaf.
3. Canary cadence: run canaries from the same run loop using a time accumulator (tick every HealthInterval; fire canary when accumulated >= CanaryInterval) — do NOT spawn a second goroutine (keeps the generation guard and Stop semantics intact).
4. `runCanary(gen)`: build Contract-5 request against the server root (same root derivation checkOnce uses); POST with canaryClient; classify per Contract 5; on failure increment canaryFails, on success reset to 0. HTTP + body parse OUTSIDE h.mu (mutexio rule); counter updates under h.mu with the generation guard.
5. Ledger bypass: canaryClient is a plain http.Client — no Recorder, no Resolver, no slot gate. Add a comment pinning this (gate F9).

### Task 3: Start() re-arm resets canary state

In `Start`: canaryArmed=false, canaryFails=0 alongside the existing healthy/unhealthyCount reset (a re-armed checker must re-prove /health before canaries resume — cold-load guard across restarts).

## Self-Verification Checklist

- [ ] `go test -count=1 ./internal/llm/ -run 'Canary'` GREEN (all gate canary tests)
- [ ] `go test -count=1 -p 2 ./internal/llm/` GREEN (no regression to existing checker tests)
- [ ] `go vet ./internal/llm/` clean; gofmt clean
- [ ] No gate test file modified (`git diff --stat` shows only runtime_config.go, health_checker.go, plus verdict tests still RED for leaf 02 symbols — that is expected and fine)

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Canary failure counter is SEPARATE from unhealthyCount (a canary failure must not flip the existing /health verdict in this leaf)
- [ ] Single run goroutine (no new goroutines; generation guard covers canary)
- [ ] Loopback detection reuses the existing helper, not a new implementation
- [ ] No I/O under h.mu
- [ ] Absent `canary` config = byte-identical existing behavior

Output: APPROVED or gaps with file:line.
