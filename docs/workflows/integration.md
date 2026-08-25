# Integration Tests

`internal/integration/` holds cross-leaf integration suites that guard the
seams individual unit tests cannot see. Plain package (no build tag), so the
suites run under the normal `go test ./... -race`.

## Containment Suite (`containment_test.go`)

Proves the four containment workstreams compose end-to-end
(plans/containment-and-computer-use):

| Test | Guards |
|------|--------|
| `TestEnvStrippedThroughBackendExecution` | Env allowlist strips daemon secrets from real child processes ([runtime env policy](../concepts/runtime.md)) |
| `TestSecretPlaceholderRoundTrip` | `MEEPT_SECRET:` placeholders resolve to real credentials only toward declared hosts via the egress proxy ([secrets](secrets.md)) |
| `TestSandboxRefusalFailsClosed` | `require_sandbox=true` with no qualifying backend refuses execution instead of degrading ([runtime](../concepts/runtime.md)) |
| `TestStageAcceptJournalRevertChain` | Stage → drift-refusal → accept → journal → revert chain ([change journal](change-journal.md)) |
| `TestSurfacesSeeSameState` | HTTP/TUI/agent surfaces observe the same pending-change state ([change review](change-review.md)) |

## Conventions

- Use `t.TempDir()` for all scratch state; no shared fixtures.
- Deterministic: no sleeps except bounded readiness polls with deadlines.
- Consume leaf packages' public APIs only — no internals.
- Run with `-race`: these tests exercise concurrent seams.
