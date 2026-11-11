# Fuzzy Module + Utterance Normalization — Implementation Leaf (03)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks using TDD. The failing gate tests ALREADY EXIST — make
> them pass; do NOT delete, skip, weaken, or rewrite any gate test. Do NOT
> commit. Do NOT use read_file on existing source files — explore with
> search_files or terminal cat. After writing a file, do NOT read it back.

## Meta

- **Parent:** ./master.md
- **Scope:** C5 internal/util/fuzzy package + C6 NormalizeUtterance in internal/voicechild, wired into the STT-final path.
- **Dependencies:** 00T only
- **Estimated Context:** 75K
- **Concurrency Group:** A (file-disjoint with 01, 08)

## Goal

Port the STT-garble tolerance layer from ~/git/uptractor-jobfile branch
fix/speech-robustness: src/lib/agent/fuzzy.ts -> Go (stdlib only), and the
normalizeUtterance() behavior from src/lib/agent/turn.ts -> Go. Utility: the
ratified dispatch-board design (docs/research/2026-11-10, Q1) requires fuzzy
"do you mean X?" resolution against registries instead of silently minting
keys; voice turns need filler collapse before the LLM sees them.

## Context

- Source: cat src/lib/agent/fuzzy.ts (editDistance, similarity, fuzzyFind) and the normalizeUtterance section of src/lib/agent/turn.ts + docs/SPEECH-ROBUSTNESS.md
- internal/voicechild/pipeline.go — STT final -> LLM turn; Normalize hooks here
- Go generic FuzzyFind; epsilon float comparisons and sorted keys before accumulation (Pitfall 26: Go float determinism)

## Verified Anchors

- internal/voicechild/pipeline.go — final-transcript emit site: grep -n "tr.Final" internal/voicechild/pipeline.go (Final bool field at :35, branch at :458, dedupe lastFinalText at :498, emitPrompt at :523)
- docs/research/2026-11-10-dispatch-board-design-proposal.md:139 — Q1 fuzzy ratification (why this exists)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/util/fuzzy (stdlib only, no deps)
func EditDistance(a, b string) int
func Similarity(a, b string) float64 // token-aware, per-token edit-distance weighted, 0..1
func FuzzyFind[T any](query string, candidates []T, getName func(T) string, threshold float64) []T // sorted desc

// internal/voicechild
func NormalizeUtterance(text string, preserve func(token string) bool) string // idempotent
```

Caller thresholds (frozen): registries/subjects 0.60; short descriptions 0.55.
Normalize: collapse repeated words, collapse triple+ filler ("yeah yeah yeah"->"yeah"), collapse STT filler runs ("um um um"->"um"), drop noise tokens UNLESS preserve(token) is true or token is an action word or numeric.

### What This Leaf Consumes

- C5, C6 (master.md). Gate tests: internal/util/fuzzy/fuzzy_test.go, internal/voicechild/normalize_test.go.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---|---|---|---|---|---|---|
| T1 | fuzzy package: EditDistance, Similarity, FuzzyFind | Create internal/util/fuzzy/fuzzy.go | `go test ./internal/util/fuzzy` | gate RED | canonical table green | Given "Meadow Rich" + candidate "Meadow Ridge"; When FuzzyFind(0.60); Then match returned |
| T2 | NormalizeUtterance | Create internal/voicechild/normalize.go | `go test ./internal/voicechild -run Normalize` | gate RED | green incl. idempotence | Given "yeah yeah yeah um um"; When Normalize; Then "yeah um" |
| T3 | Normalize wiring: normalize ONLY the emitPrompt argument at pipeline.go:523 (dedupe lastFinalText :498 keeps comparing RAW text so barge-in/dedupe behavior is unchanged); transcript persistence in internal/voice/binding.go:297-316 (AppendTranscript) must keep storing RAW — add a raw-capture path in consumeTranscripts so binding gets raw while the LLM gets normalized | Modify internal/voicechild/pipeline.go | `go test ./internal/voicechild` full suite | n/a | green; binding test proves persisted text is raw | Given garbled final; When turn starts; Then binding persists raw, LLM receives normalized |

## Self-Verification Checklist

- [ ] Canonical SPEECH-ROBUSTNESS.md table passes verbatim ("summer planes" => NO match)
- [ ] fuzzy package imports stdlib only
- [ ] Full voicechild suite green (`go test -p 2 ./internal/voicechild ./internal/util/fuzzy`)
- [ ] No scope creep (no intake/registry callers — leaf 05 consumes fuzzy)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** 2. **Verify command + output:** last 20 lines 3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] Similarity formula matches the TS source semantics (token-set weighted edit distance)
- [ ] FuzzyFind sort deterministic for equal scores (stable, key-ordered)
- [ ] Normalize never drops preserve-listed tokens; idempotence test real (not tautological)
- [ ] Pipeline wiring preserves existing transcript persistence (binding.go AppendTranscript stores RAW final; normalized feeds only the LLM turn; dedupe compares raw)

Output: APPROVED or specific gaps with file + line references.

## Notes

- Storing BOTH raw and normalized transcripts matters for eval; check the
  pipeline's transcript store call and keep raw there.
