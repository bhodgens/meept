# Child open handler - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Do
> NOT commit — the orchestrator handles git. Explore with search_files or
> terminal cat, never read_file. Do NOT read back files you just wrote.
> RECONCILE FIRST: tierA leaf 02 (ProviderConfig on applyKeys) must be
> committed; the factories keep their tierA signatures - this leaf changes
> NO factory.

## Meta

- **Parent:** master.md
- **Scope:** Child consumes the C1 `open` frame: create the pipeline, dial
  the C2 pair, store identity + local + bargeInRms, answer with the setup
  frame. (master CL1/CL2 timing.)
- **Dependencies:** leaf 01 (EventOpen/Local/BargeInRms), tierA 02
  (ProviderConfig storage path intact)
- **Estimated Context:** 50K
- **Concurrency Group:** B
- **Files this leaf owns:** internal/voicechild/child.go,
  internal/voicechild/pipeline.go (add local/bargeInRms fields + identity
  store ONLY), internal/voicechild/open_test.go (new)

## Goal

Production currently has NO path that makes the child create a pipeline:
newPipeline exists but nothing calls it outside tests (the setup frame's
producer is the child, and the child only sends setup from a pipeline it
never got). `open` is the trigger: daemon sends it right after the audio
pair exists; child creates pipeline + dials pair + stores profile fields +
sends setup, which arms the talk loop (existing routeFrame) and then tierA
keys arrive to build providers. F4 closes because every call now reaches
the open->setup->keys chain.

## Context

Key files:
- internal/voicechild/child.go - handleLine switch, handleSetup pipeline
  create + idempotency, pipelines map.
- internal/voicechild/pipeline.go - newPipeline (dials C2 pair), the
  playGen machinery. Add three fields, no logic.
- internal/voiceframe - Frame now carries EventOpen/Local/BargeInRms
  (leaf 01); direction/from/to/codec/sample_rate already exist.

## Interface Contracts (From Parent)

CL1 open shape (verbatim):
`{"call_id":"cc1","event":"open","from":"alice","to":"local:host","direction":"local","codec":"L16","sample_rate":16000,"local":true,"barge_in_rms":3000}`
CL2 timing (verbatim): open stores identity + local + bargeInRms; setup
frame is the child's ANSWER to open; constructProviders stays on the keys
frame (tierA); a setup without preceding open keeps working (stub
pipeline + warn log) so the phone leg cannot break mid-rollout.

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS |
|---|---|---|---|---|---|
| T1 | Test: open frame -> one pipeline, pair dialed (test dials into the listeners), setup frame emitted with the open frame's identity fields byte-exact | open_test.go | go test -p 2 ./internal/voicechild/ -run TestOpenCreatesPipeline | no EventOpen case | PASS |
| T2 | Test: duplicate open for live ccid -> idempotent (no second pipeline, no second setup, warn logged) | same | -run TestOpenIdempotent | - | PASS |
| T3 | Test: setup-without-open still creates a stub pipeline + warn (compat pin) | same | -run TestSetupWithoutOpenCompat | - | PASS |
| T4 | Implement handleOpen; store local/bargeInRms/codec/sample_rate on pipeline | child.go, pipeline.go | T1-T3 | - | PASS |
| T5 | gofmt/vet/race + full suite | - | go test -p 2 -race ./internal/voicechild/... | - | clean |

## Interface Contract (What This Leaf Exposes)

pipeline fields `local bool`, `bargeInRms int` (leaf 02 reads), and the
guarantee: daemon-sent open always precedes a child-sent setup in
production.

## Self-Verification Checklist

- [ ] Existing setup/keys/concurrency tests pass unchanged
- [ ] Pair dial respects codec chunk size from the open frame
- [ ] No key material involved (none exists at open time)

## Review Checklist

- [ ] Idempotency mirrors handleSetup's existing guard (bughunt F-6 spirit)
- [ ] Setup answer frame byte-matches the old hand-written shape
- [ ] No factory/provider changes sneaked in; ASCII only
