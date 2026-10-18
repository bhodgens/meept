# E2E Suites + Docs + Invariants - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. Do NOT commit — the orchestrator handles all
> git operations after review. Do NOT use read_file on existing source
> files — explore with search_files or terminal cat. After writing a file,
> do NOT read it back to verify — write once and stop. After completing,
> report what you built, what files you touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Hermetic e2e suites proving the whole chain, manifest
  entries, feature docs, AGENTS.md invariant updates.
- **Dependencies:** leaves 01-06 merged (this leaf verifies their
  combined behavior)
- **Estimated Context:** 70K
- **Concurrency Group:** Wave 3 (solo)

## Goal

The repo's e2e policy: all new feature tests live in the hermetic e2e
tier. This leaf turns the master's Integration Test Plan into executable
suites and lands the documentation the repo requires for feature changes
(docs/workflows, AGENTS.md invariants, generated-graphs freshness).

## Context

- e2e harness: `e2e/suites/<name>/` with the `e2e` build tag; manifest +
  path→suite mapping in `e2e/manifest.json`. See
  `docs/workflows/e2e-testing.md` for tiers and manifest format.
  Run with `make e2e-fast-area AREA=<name>`.
- The scratch daemon rig builds meept-daemon/meept once per test process
  into `$TMPDIR/meept-e2e-bin*` — use the existing harness helpers
  (`e2e/harness/daemon.go`), never a hand-rolled `os.MkdirTemp`.
- Stub inference endpoints: hermetic suites must not need a real model.
  Existing suites may already stub OpenAI-compatible endpoints — search
  `e2e/suites/` for an httptest-style stub to mirror before writing a new
  one.
- Docs: `docs/workflows/session-affinity.md` (new, linked in mkdocs.yml
  Workflows nav), AGENTS.md updates per the maintenance rule (root file
  summary + package AGENTS.md if internal/llm or internal/session gain
  invariants).

## Interface Contracts (From Parent)

From master's Integration Test Plan — four executable proofs:

1. `session-affinity` suite: two sessions against a stub OpenAI-compatible
   endpoint that records request headers. Assert: every chat request from
   session A carries the affinity header with A's id (never B's); a
   secondary call (the scope-threaded reflection/classifier path) also
   carries it; a session-less call (no scope) carries no header.
2. `session-teardown` suite: stub llama-server endpoint recording the
   erase route. Assert: archive fires exactly one erase; idle expiry
   (`idle_release` shortened via test config) fires exactly one; daemon
   stop sweep fires once per active session; a cloud-configured provider
   fires none.
3. Sticky rotation: two endpoints behind one alias in the test config;
   fail endpoint A's stub mid-session; assert the session moves to B and,
   when A recovers, preference re-arms only per Contract 4 semantics
   (next success overwrites memory).
4. Manifest: both suites registered in `e2e/manifest.json` path_map in
   the same commit.

### What This Leaf Exposes

Executable suites + docs + invariant text. No production code changes
except manifest/JSON.

## Tasks

1. **Explore first:** read one existing hermetic suite end to end for
   the harness idiom (setup, daemon spawn, config injection, teardown).
   Mirror it exactly. Find an existing header-capturing stub server or
   write one inside the suite.
2. **Suite `session-affinity`:** implement proof 1. Config: affinity
   enabled (default). Shorten nothing; hermetic defaults only.
3. **Suite `session-teardown`:** implement proof 2. Test config sets
   `idle_release` to seconds-scale. The stub records route + count; the
   suite asserts exact counts.
4. **Sticky-rotation proof:** either a third suite or a section inside
   `session-affinity` — whichever matches how existing suites group
   related scenarios. Two stub endpoints, alias config in the test
   daemon's models.json5.
5. **Manifest:** add both suites to `e2e/manifest.json` with correct
   path_map entries.
6. **Run them:** `make e2e-fast-area AREA=session-affinity` and
   `AREA=session-teardown` — both green. Then
   `make e2e-affected` (your diff touches internal/llm + internal/session
   via earlier leaves; the mapping must route to your suites).
7. **Docs:** write `docs/workflows/session-affinity.md` — problem,
   config knobs (`llm.session_affinity`), teardown behavior, cloud no-op
   rationale, limits (affinity is a request; the backend decides).
   Link in mkdocs.yml. Update root AGENTS.md Critical Invariants with a
   3-5 line summary ("Session affinity headers are default-on; empty
   scope sends no header; teardown is best-effort and never fails a
   turn") and the full contract in `internal/llm/AGENTS.md` +
   `internal/session/AGENTS.md` if those files exist (they do).
8. **Graphs freshness:** no new bus/RPC/HTTP/WS surface was added — run
   `make graphs-check` and confirm green (if red, find the accidental
   surface and remove it, do not regenerate blindly).

## Self-Verification Checklist

- [ ] Both suites green: `make e2e-fast-area AREA=session-affinity`,
      `AREA=session-teardown`
- [ ] `make e2e-affected` routes the working diff to the new suites
- [ ] Manifest path_map entries present and correct
- [ ] `make test` still passes (no unit-tier regressions)
- [ ] `make graphs-check` green
- [ ] docs/workflows/session-affinity.md exists, linked in mkdocs.yml
- [ ] AGENTS.md (root + llm + session) invariant summaries updated
- [ ] No line-number corruption, no TODOs, no debug scaffolding left in
      suites
- [ ] Do NOT commit

## Review Checklist (for the orchestrator)

- Suites assert EXACT header values and EXACT erase counts (no
  lenient >= asserts that would pass on a broken teardown).
- Suites are hermetic: no network beyond loopback stubs, no real models.
- Docs use plain language, define affinity and KV cache on first use.
