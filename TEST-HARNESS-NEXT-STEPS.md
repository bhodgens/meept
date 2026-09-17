# Testing harness follow-up

## Verified changes

Run `make e2e-sweep-selftest classifier-eval-hardening-test classifier-eval-selftest`.
The first target checks sweep completion, grading, command exits, shell reply checks,
and both embedded tool clients. The classifier target includes fixed-input scoring controls.
No live model measurements accompany this change. Commit `0688a9b0` contains the
harness code, invocation records, tests, and workflow documentation.

## Approved scope

The user approves async-first testing with a separate legacy suite, recorded tool
execution plus a correct result for tool-required cases, and an isolated local-model
run after offline transport and evidence checks pass. Missing tool evidence must
produce UNVERIFIED, never PASS. Implementations are in progress; approval is not
verification. Do not run a second local-model campaign while another scratch run
uses the machine.

## Verified repair and remaining live check

- Git reports no unmerged files. `GOPROXY=off GOSUMDB=off go build -p 2 ./...` passes.
- `toolEvidenceCollector.absorb` records call identities independently of artifact
  evidence. `AgentJobProcessor.Process` persists `tool_invocations` and the execution
  conversation in step results. Six collector tests pass, including a real executor
  completion event. The offline probe is not a model measurement.
- `evidence.py` now returns PASS for correct content plus distinct successful,
  uncached calls with matching agent and execution identities. Missing, failed,
  conflicting, cached, or wrong-identity records cannot pass. The regression first
  failed with UNVERIFIED and now passes. Counts prove a minimum, never an exact count.
- `make e2e-sweep-selftest` passes 48 sweep tests and four shell tests.
- Existing Python `/Users/caimlas/.hermes/hermes-agent/venv/bin/python` supplies
  websockets 15.0.1. No dependency installation is needed.
- The first full Go sweep fails in two integration tests. The dispatch failure
  reports `connect: can't assign requested address`. Both tests pass together
  three times; dispatch passes ten additional focused repetitions. Do not claim
  the first full sweep passed. The final bounded full sweep exits successfully
  with code 0. Output: `/tmp/meept-harness-final-go-tests.log`.

## Remaining work, in priority order

1. Run one isolated async local-model campaign after the other scratch run stops.
   Processes 62701 and 62705 run `/private/tmp/meept-extract-smoke/meept-daemon`
   during the latest check. Do not kill another worker's processes or overlap GPU
   measurements. No live model run completes in this repair.
2. Verify live step result persistence and strict grading together. The producer
   still receives best-effort events; late events can return UNVERIFIED. Offline
   collector and SQLite fixture checks do not prove live delivery ordering.

## Completed observation fixes

1. Embedding cache writes now use response indices. The shuffled-response regression
   and both classifier gates pass. Old caches remain unchanged; use a fresh cache.
2. Ambient and distillation evaluation count failed calls as missed gold items.
   Four HTTP regression tests and the package race check pass. No metric fields changed.
3. `RunFileMutations` now passes the source path to its callback, checks the original
   baseline, and runs each changed copy in a separate temporary directory. Three tests
   pass, including compilation and an actual assertion failure on the changed source.
   The original file remains unchanged. Run `go test -short -count=1 -p 2 ./tools/mutation`.
   The callback still controls error classification: the generic runner cannot distinguish
   a test assertion failure from a compiler or infrastructure failure.

## Other observations

- `.github/workflows/code-quality.yml:209` runs the full Go suite without `-p 2`.
  The package-parallelism rule is mandatory for macOS; check this Linux job separately.
- Shell transcript errors still use broad provider-SKIP matching and retry turns.
  Add protocol-error and side-effect replay controls before trusting those outcomes.
- Historical classifier results from the old runner have forced perfect OOD abstention.
  OOD means inputs outside the supported routing classes. Do not cite those historical
  abstention scores as measured behavior. Recompute from real predictions.

## Scope boundary

All existing unrelated changes remain untouched. The repository has concurrent work.
Review explicit paths before any commit. Do not run tests against the user's live daemon,
production home, or project directory. No software installation is required for the
new standard-library tests.
