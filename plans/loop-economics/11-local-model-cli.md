# Local Model Lifecycle CLI - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** meept model pull/list/test; RuntimeManager registers pulled GGUF models as provider endpoints.
- **Deps:** none | **Context:** 60K | **Group:** C

## Goal

Meept manages local RUNTIME processes (llama.cpp/MLX) but not model FILES — users must obtain GGUFs out-of-band. Add: `model pull <repo-id>` (HuggingFace HTTP, resumable, quant filter), `model list`, `model test <name>` (one-token probe through the runtime). Pulled models become selectable by existing capability resolver.

## Context

internal/llm/runtime_process.go + RuntimeManager lifecycle exist. HF hub download = plain HTTPS resolve endpoints (no new SDK): https://huggingface.co/api/models/<id> lists files; resolve/<rev>/<file> downloads w/ Range resume. Auth via HF_TOKEN env when set.

Key files: internal/llm/runtime*.go, cmd/meept command tree, config [llm] models_dir default ~/.meept/models.

## Interface Contracts (From Parent)

```go
// internal/llm/modelstore.go:
type ModelRecord struct{ Name, RepoID, File string; Bytes int64; SHA256 string; AddedAt time.Time }
func OpenModelStore(dir string) (*ModelStore, error) // index.json in dir
func (s *ModelStore) Pull(ctx context.Context, repoID, quant string, progress func(done, total int64)) (*ModelRecord, error)
// picks single .gguf matching quant substring (e.g., "q4_k_m"); ambiguous -> list candidates error.
// Resumable via .part file + Range header; sha256 computed streaming.
func (s *ModelStore) List() []ModelRecord
```

RuntimeManager addition:
```go
func (m *RuntimeManager) RegisterLocalModel(rec ModelRecord) error
// wires llama.cpp-style launch args pointing at file under a "local-models" provider alias
// compatible with existing provider/alias resolution.
```

CLI:
- meept model pull <repo> [--quant q4_k_m] [--force]
- meept model list [--json]
- meept model test <name>  -> starts runtime if needed, 1-token completion, reports latency, stops (or keeps warm per runtime policy)

## Tasks
1. Failing tests store against httptest fake hub: manifest parse, quant selection incl. ambiguity error, resume (interrupted body then Range completes), sha verify, corrupt-file rejection.
2. Failing tests RegisterLocalModel wiring (fake process manager pattern from existing runtime tests).
3. CLI commands w/ injectable client for tests; progress output lowercase, bytes humanized.
4. Docs: docs/workflows/llm-management.md section + config line for models_dir.

## Self-Verification Checklist
- [ ] -race green internal/llm cmd/meept
- [ ] No blocking UI on progress (callback-driven)
- [ ] Disk-space pre-check w/ clear error

## Review Checklist
- [ ] Resume logic handles server-without-Range gracefully (restart)
- [ ] No token ever logged
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: GBNF leaf consumes this only for availability; keep APIs clean.
