# Leaf 04 — decision-model compaction lane (opt-in)

## Goal

Score-and-drop compaction as an ALTERNATIVE summarizer at
ContextCompressor stage 2, active only when the decision model is
configured AND the opt-in knob is true. Existing summarizer stays the
default.

## Depends on

Leaf 02 (DecisionClient) landed.

## Tasks

1. Read `internal/llm/context_compressor.go` first: the `Chatter` seam
   (`summarizer Chatter` field, NewContextCompressor signature) is the
   injection point. Do NOT change NewContextCompressor's signature.
2. Config: add `decision_enabled` bool (default false) and
   `decision_keep_bar` int (default 2) to the compaction config section
   in schema.go (find the section that carries CompressionConfig
   settings — grep `CompressionConfig` in internal/config) +
   DefaultConfig + models.json5 commented entry.
3. `internal/llm/decision_compactor.go` — `DecisionCompactor` satisfying
   `Chatter` structurally (verify the exact interface shape — Chatter's
   method set — before writing; adapt if it is chat-shaped):
   - `Compact(entries, currentTask)`-shaped method: for each history
     entry, ONE score question `{Type:"score", Prompt:"does this entry
     still matter for the current task? 0-4"}` with the entry text as
     State.
   - Entries scoring >= decision_keep_bar stay VERBATIM; below are
     dropped. Never rewrite survivors.
   - Batch efficiency: if the client supports multi-question per prefill
     later, keep the loop shape so the call site is one Compact call.
   - Any client error on an entry → keep that entry (fail open, keep
     context; log Warn once per Compact run, not per entry).
4. Wire: at the construction site of ContextCompressor (grep
   `NewContextCompressor(` in internal/daemon or internal/agent), when
   `compaction.decision_enabled && decisionClient.Available()` pass the
   DecisionCompactor as the summarizer arg; otherwise the existing
   summarizer (byte-identical legacy path).
5. Tests: decision_compactor_test.go — keep-bar boundary (2 stays, 1
   drops), verbatim preservation (no text mutation), error-fail-open
   keeps entry, disabled/nil client never constructs the compactor.
   Integration guard: with knob false, construction path identical to
   today (assert via existing construction tests still passing).
6. Sweep: `go test -p 2 ./internal/llm/...` green; both binaries build.

## Constraints

- Summarizer lane untouched when knob false or model unset.
- Explicit-path commit.

## Report back

RED/GREEN evidence, Chatter's actual method set (it may not be
Compact-shaped — report what you adapted), construction wiring location
(file:line).
