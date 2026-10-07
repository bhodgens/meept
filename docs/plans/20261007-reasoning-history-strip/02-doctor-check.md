# Leaf 02: doctor reasoning-wire-form check

## Meta

- **Role:** Leaf (Wave 1, parallel with Leaf 01 — disjoint files)
- **Parent:** master.md in this directory
- **Owns:** `cmd/meept/doctor_reasoning.go` (new), `cmd/meept/doctor_reasoning_test.go` (new), one appending hunk in `cmd/meept/doctor.go` (call the new check in `runDoctor`), optionally `docs/reference/` doctor page touch.
- **Does NOT touch:** anything under `internal/agent/`, `internal/llm/`.

## Goal (from master, frozen)

Add a report-only `meept doctor` check that probes each local
reasoning-capable endpoint once and classifies its reasoning wire form:
separate channel (ok), inline `<think>` tags (warn), untagged leak (warn),
unreachable (skip). Doctor is report-only by repo convention — never
auto-fix, never restart, never mutate config.

## Current state (drift-audited 2026-10-07)

- `cmd/meept/doctor.go:33` — `type doctorCheck struct { name string; ok bool;
  warn bool; detail string }` (verify exact fields by reading the struct
  before coding).
- `cmd/meept/doctor.go:99` — `runDoctor` accumulates `checks []doctorCheck`;
  the models checks append there (see doctor_models.go usage).
- `cmd/meept/doctor_models.go:24` — `checkModelsDoctor()` is the pattern to
  copy: loads config via `llm.LoadProvidersConfigDefault()`, iterates
  providers deterministically (sorted), returns `[]doctorCheck`.
- Provider config: `llm.LoadProvidersConfigDefault()` gives
  `cfg.Providers[pid]` with `Lifecycle` (nil for cloud providers — skip)
  and model entries with capability tags. Verify the exact field names by
  reading the type (grep `type ProviderConfig` / `type ModelConfig` in
  internal/llm) — do NOT trust this doc over the source.
- Known local endpoints on this machine: :8080 (local-gguf, llama.cpp
  --jinja), :8081 (local-mlx), :8083 (local mlx_lm), :8084 (local-extract,
  completion-only — not reasoning-capable, must be skipped).

## Tasks

1. TDD: write `doctor_reasoning_test.go` FIRST against an
   `httptest.Server` fake endpoint (the package already has tests faking
   HTTP — grep `httptest` in cmd/meept for the house pattern). RED evidence
   before implementing.
2. `cmd/meept/doctor_reasoning.go`: `func checkReasoningDoctor(cfg
   *llm.ProvidersConfig, probe probeFunc) []doctorCheck` — take the HTTP
   prober as a function param (or package-level seam) so tests inject a
   fake. Classification per master Contract 3:
   - POST one chat completion to `<baseURL>/chat/completions`, body:
     model = the reasoning-capable model's serving name, messages =
     `[{"role":"user","content":"Reply with the word: ready"}]`,
     max_tokens 32, 10s client timeout.
   - `reasoning_content` or `reasoning` non-empty AND content has no
     `<think>` → ok line: "separate reasoning channel".
   - `<think>` in content → warn: "inline reasoning tags — meept strips them
     from history, but the endpoint burns output tokens on reasoning;
     enable --jinja with a reasoning parser (llama.cpp) or reasoning_format
     to split the channel".
   - Reasoning-style prose in content, no tags, no separate field → warn:
     "reasoning may be leaking into content untagged".
   - Connection error / timeout → ok=true, warn=true (skip semantics):
     "endpoint unreachable — check skipped".
   - Lifecycle nil or no reasoning-capable model → no line at all (same as
     checkModelsDoctor skipping non-lifecycle providers).
   - Cloud providers (baseURL host not loopback) → skip entirely (no
     network calls to remote APIs from doctor).
3. Wire into `runDoctor` (doctor.go) right after the models checks:
   `checks = append(checks, checkReasoningDoctor(...)...)`.
4. Classification matrix tests: separate-channel, inline-tags, untagged-leak,
   unreachable — one httptest fake per shape + one no-reasoning-model shape
   asserting ZERO lines.

## Guardrails

- Report-only: no restarts, no config writes, no `--fix` behavior.
- One probe per endpoint, never a loop of retries (doctor must stay fast).
- Pre-commit hooks: no ignored errors — handle every err explicitly.
- Commit explicit paths only; siblings work in this worktree.
- Go style: match doctor_models.go (sorted deterministic iteration, single
  responsibility funcs).

## Acceptance

- `go test ./cmd/meept/ -run Reasoning -v` green (matrix covered).
- `go build ./cmd/...` green.
- On this machine with the live daemon: `./bin/meept doctor 2>/dev/null |
  grep reasoning` prints one line per local reasoning endpoint (live smoke;
  acceptable to note "endpoint down — skipped" if a runtime is stopped).
- `docs/reference/` doctor documentation (grep for where existing doctor
  checks are listed) gains the new check name.

## Report back

RED/GREEN evidence, files changed, the live-smoke doctor output lines, any
deviation (with reason).
