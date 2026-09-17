# Corpus and cache provenance - implementation leaf

DISPATCH INSTRUCTION: Execute after leaf 02 passes parent review. Do NOT commit, stage, install, rebuild live models, or replace installed artifacts.

## Meta

Parent: [master.md](master.md).
Scope: align default training inputs and identify evidence origins.
Dependencies: 02-scoring.md reviewed. Concurrency group: B.
Estimated Context: 65K. Effort: 3-6 hours in serial checkpoints.
Audit references: MEAS-02, MEAS-03, MEAS-05, MEAS-06.

## Goal

Make default builders use the evaluated corpus population. Prevent alias-only cache reuse from supporting fresh acceptance claims. Preserve fold evidence and distinguish test fixtures from deployment models.

## Context

`scripts/build_prefilter_centroids.py:39,118,131` defaults to adversarial-only input. The harness loads base plus adversarial data. Parent verification finds 222 default keys versus 361 eligible combined keys.

`internal/agent/testdata/prefilter_tfidf_veto.json` declares 202 training documents. This is fixture metadata, not proof of deployed-model staleness. `scripts/build_tfidf_veto.py` requires corpus arguments; inspect its invocation paths before changing defaults.

`eval_harness.py:399` hashes model alias and instruction for cache identity. Distinct weights under the same alias reuse disk vectors. The API response handling also constructs indexed `got` values at lines 441-445 but stores zipped response-order values at lines 446-459. Treat possible response-order mismatch as a lead to test, not a proven finding.

## Interface Contracts (From Parent)

Default eligible key sets must equal the harness loader's combined population. Explicit subset runs remain supported and labeled. Do not hardcode review counts.

Preserve existing builder output fields consumed by Go. Add provenance metadata only after checking the runtime decoder tolerates additions. Record source hashes, eligible-key hash, count, embedding identity, and preprocessing.

For cache identity, prefer a verified local model content fingerprint or immutable revision. Missing identity must not silently reuse legacy cache evidence for acceptance. Avoid hashing secrets or endpoint credentials. A configurable identity without verification is an assertion, not proof.

## Tasks

### Task 1: unify default corpus selection

Files: `scripts/build_prefilter_centroids.py`, `tools/classifier-eval/eval_harness.py`, proposed `tools/classifier-eval/test_provenance.py`.

1. Add a unittest comparing builder defaults with non-OOD keys from `load_cases`.
2. Run `/opt/homebrew/opt/python@3.12/bin/python3.12 -m unittest discover -s tools/classifier-eval -p test_provenance.py -v`; require the 139-key omission to fail.
3. Share corpus semantics without introducing model-loading side effects. Keep builder help and explicit subset arguments consistent.
4. Test expected_intent overrides, OOD exclusion, duplicate keys, both corpus layouts, and escaped text.
5. Rerun the test without an embedding server. Report exact source sets and metadata.

If a standalone shared loader is required to avoid importing NumPy into a lightweight builder, stop and freeze the file ownership before adding the module. Do not create another drifting parser.

### Task 2: establish artifact provenance and fixture intent

Files: `scripts/build_tfidf_veto.py`, the new provenance test, and the fixture only if regeneration is justified.

1. Trace fixture readers and documented builder invocations. Determine fixture versus deployment purpose.
2. Test metadata against the actual selected population, not a fixed document count.
3. Add provenance emission to both builders in serial checkpoints.
4. Exercise metadata generation with synthetic vectors or existing offline data. Test the real serialization shape against Go loader expectations.
5. Retain a deliberate small fixture with explicit purpose, or propose regeneration with a pinned command and dependencies. No live model request or installation without approval.

### Task 3: make cache identity explicit

Files: eval_harness.py, test_provenance.py, and m4_gold_acceptance.py only after parent assigns that consumer edit.

1. Reproduce identical cache directories for different endpoints sharing the same alias and instruction.
2. Freeze the model-identity input and validation source. Trace every Embedder constructor and spec reader.
3. Test different fingerprints, matching fingerprints, changed instructions, legacy cache contents, and missing identity.
4. Implement versioned cache metadata and a fail-closed acceptance policy. Preserve exploratory legacy use only with explicit unverified labeling.
5. Run provenance tests and classifier self-tests. Do not delete existing caches.

At the same boundary, test an embedding response returned in reversed index order. If the current writer assigns vectors to the wrong keys, repair index-based mapping. Treat duplicate, missing, and out-of-range indexes as errors. Report the lead as refuted if the production test cannot reproduce it.

### Task 4: protect fold evidence

Files: eval_harness.py and test_provenance.py.

1. Set FOLD_CACHE to an absent temporary path and observe current behavior.
2. Preserve deterministic assignments for all existing keys.
3. Make initialization explicit for new experiments; require recorded fold evidence for acceptance.
4. Test missing, malformed, incomplete, and unchanged caches. Corpus growth must not silently reassign existing keys.
5. Run self-tests and report whether MEAS-06 is a policy repair rather than a demonstrated assignment error.

## Self-Verification Checklist

- [ ] Default builder and evaluation key sets match exactly.
- [ ] Runtime artifact readers accept the metadata shape.
- [ ] Fixture purpose and installed artifact status remain distinct.
- [ ] Identity and fold tests cannot overwrite user caches or evidence.
- [ ] Response-order lead receives an executed disposition. Do NOT commit.

## Review Checklist (For Review Agent)

- [ ] Parent verifies set parity through real loaders.
- [ ] Provenance is tied to actual content, not just supplied labels.
- [ ] No dependency installation or model rebuild occurs without approval.
- [ ] New cache policy reaches acceptance consumers, not only constructors.
- [ ] Report APPROVED or precise contract gaps.
