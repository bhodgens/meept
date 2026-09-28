# Fuzzing

Go native fuzzing (`func FuzzXxx(f *testing.F)`) covers the repo's
untrusted-input parsers. Every fuzz target's seed corpus runs automatically in
normal `go test` (CI included), so a broken seed or a regression caught by a
previous fuzz session fails the ordinary test suite; longer discovery sessions
are run locally on demand.

## Fuzz targets

| Target | Package | Invariants checked |
|--------|---------|--------------------|
| `FuzzInputSanitizer` | `internal/security` | Never panics at any strictness level; `CleanText` stays valid UTF-8; raw boundary markers (`<<<USER_INPUT>>>` etc.) and structural tokens (`<\|im_start\|>`, `[INST]`, ...) never survive unescaped. Also `FuzzOutputMonitorRedact`: redaction never grows the text and never modifies text with no detected credentials. |
| `FuzzWSClassString` | `internal/comm/wsclass` | Never panics on out-of-range values; always returns one of the six valid wire strings. |
| `FuzzExtractFencedBlocks`, `FuzzExtractGoBlocks` | `internal/validator` | Never panics on arbitrary markdown; every returned block satisfies `0 <= start <= end <= len(output)` and `code == output[start:end]`. This is exactly where the lint_js prose bug lived. |
| `FuzzErrorClassifiers` | `internal/errcls` | `IsRateLimit` / `IsRetryable` / `IsRateLimitErrorMessage` never panic on arbitrary strings or deep wrapped error trees (pure functions of input). |
| `FuzzParseResponseWithTools`, `FuzzSSEChunkParse` | `internal/llm` | Chat-response parser and SSE-body parse shape never panic on arbitrary bytes (HTTP-body stand-in); a returned error never comes with a response. |

## Running

Seed corpus only (what CI does implicitly):

```bash
go test ./internal/security/ ./internal/comm/wsclass/ ./internal/validator/ ./internal/errcls/ ./internal/llm/
```

A real fuzzing session per target (run locally; seconds to minutes as time
allows — any crash found is written to `testdata/fuzz/` and then replays in
plain `go test` forever after):

```bash
go test -fuzz=FuzzInputSanitizer -fuzztime=30s ./internal/security/
go test -fuzz=FuzzOutputMonitorRedact -fuzztime=30s ./internal/security/
go test -fuzz=FuzzWSClassString -fuzztime=30s ./internal/comm/wsclass/
go test -fuzz=FuzzExtractFencedBlocks -fuzztime=30s ./internal/validator/
go test -fuzz=FuzzExtractGoBlocks -fuzztime=30s ./internal/validator/
go test -fuzz=FuzzErrorClassifiers -fuzztime=30s ./internal/errcls/
go test -fuzz=FuzzParseResponseWithTools -fuzztime=30s ./internal/llm/
go test -fuzz=FuzzSSEChunkParse -fuzztime=30s ./internal/llm/
```

Notes:

- `go test -fuzz` runs one target per package invocation; the other tests in
  that package still run normally first.
- If a crasher is found, minimize it, fix the parser, and move the failing
  input from `testdata/fuzz/FuzzXxx/<hash>` into the seed list in the fuzz
  target so the regression is pinned permanently.
- macOS full-suite runs keep `-p 2` (see root AGENTS.md); fuzzing a single
  package is unaffected.
