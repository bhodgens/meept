# Evaluation harnesses

## Offline guards versus model measurements

A successful measurement command is not a quality gate. Keep corpus validation,
harness regression tests, and live model scores separate when reporting results.
The commands below assume existing Python/NumPy and Go installations.

| Harness | Purpose and current CI wiring | Offline command |
| --- | --- | --- |
| `tools/classifier-eval/eval_harness.py` | Five-fold routing measurement using cached/server embeddings. Measurement results do not determine its exit status. `m4_gold_acceptance.py --self-test` covers replay disjointness, degenerate vectors, privacy and route-coverage floors. `test_hardening.py` covers extracted sandbox remapping/build-hook behavior and OOD scoring controls. Both targets run in `.github/workflows/code-quality.yml`; the job installs NumPy first. | `make classifier-eval-selftest classifier-eval-hardening-test` |
| `tools/memory-eval` | Ambient extraction and distillation measurement against a JSON corpus and chat endpoint. `--validate-corpus` is offline. Four local HTTP tests cover transport and parse failure accounting in both grading paths. | `GOPROXY=off GOSUMDB=off go run ./tools/memory-eval --validate-corpus` |
| `tools/mutation` | Mutation helpers used by bus and AST rule tests. `RunMutationTest` verifies an original pass followed by a mutated failure. Three package tests cover isolated changed-source execution, missed mutations, and zero applicable mutations. | `GOPROXY=off GOSUMDB=off go test -short -count=1 -p 2 ./tools/mutation` |

The full Go suite is invoked in `.github/workflows/ci.yml`. The separate
`code-quality.yml` full-suite job still lacks the canonical `-p 2` bound;
this review did not change workflow or Makefile ownership.

## Classifier OOD scoring contract

Out-of-distribution (OOD) cases must **never enter training**, but every held-out
case—including OOD—must reach the selected prediction head. Gold labels are used
to grade predictions, not to choose them.

The hardening test uses five synthetic in-domain cases and one OOD case, fixed
in-memory embeddings, and two control heads. It executes the real fold runner
and score pooling without using a model, private replay, disk embedding cache,
or the persisted fold assignment:

- **Always route:** all six queries reach the head, the OOD abstention rate is
  zero, one wrong route is recorded, and the OOD case appears in confusion
  evidence and lowers the composite score.
- **Always abstain:** all six queries still reach the head, the OOD abstention
  rate is one, and there are no wrong routes.
- Both controls assert OOD exclusion from training and the unchanged in-domain
  denominator/chain-credit convention.

Previously, `run_permutation` injected an abstention for OOD cases without
calling the head. Thus OOD-R was mechanically 100%, wrong OOD routes were
invisible, and composite scores omitted their penalty. These regressions now
run through the existing `classifier-eval-hardening-test` CI target. The
in-domain coverage/precision/E2E definitions remain unchanged; the chain
baseline is modeled credit, not a new live measurement. Historical result
artifacts are not rewritten and should not be cited as measured OOD behavior
when produced by the bypassing runner.

## Observation fixes and remaining limits

- Fixed: `Embedder.embed_keys` now caches vectors by response index. The shuffled
  response regression passes. Old disk caches are unchanged and may still contain
  incorrectly associated vectors; use a fresh cache for new measurements.
- Fixed: ambient and distillation grading count transport and parse failures as
  missed gold items. Four local HTTP regressions pass. Historical recall results
  remain unchanged and require a new run before comparison with corrected metrics.
- Fixed: `RunFileMutations` accepts `func(sourcePath string) error`. The callback
  receives the original path for the passing baseline, then each isolated changed
  copy. The original file is never replaced. The regression compiles a temporary
  module and confirms an actual assertion failure, not a compilation failure.
  Three tests pass. Existing in-memory mutation callers are unchanged.
  The callback still owns error classification; any returned error counts as caught.
  Generic callback errors alone do not prove assertion-level mutation coverage.

These are source-inspection findings, not live model measurements. No endpoint,
model service, or daemon is needed to reproduce the fixed OOD regression.

## Tool invocation verification

The sweep reads daemon-owned `tool_invocations` from completed step results.
Artifact evidence is separate: a successful extraction can produce no artifact.
The collector preserves call identity, tool, agent, execution conversation,
success, cache state, and conflicting duplicate status. The grader requires
correct response content plus distinct successful, uncached recorded calls.
Missing records return UNVERIFIED. Failed records return FAIL.

The producer uses best-effort completion events. Counts prove a minimum observed
number, not an exact count. Offline acceptance and rejection tests run through
`make e2e-sweep-selftest`; collector tests run in `internal/daemon`.
