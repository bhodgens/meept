# Session Affinity + Upstream Teardown — Transfer / Next-Steps

Status: COMPLETE 2026-10-18. All 7 leaves done. Both e2e suites run
UNSKIPPED and green. Work is UNCOMMITTED in the working tree (per the
plan's commit rule the orchestrator owns commits; commit by explicit path
— the tree carries unrelated sibling WIP).

Plan tree: `docs/plans/20261016-session-affinity/` (master.md + 7 leaves).
User-facing doc: `docs/workflows/session-affinity.md`.

---

## 1. What the plan delivers

1. Scope guarantee — every LLM call carries a non-empty session scope
   (`WithTaskScope`), including secondary call sites.
2. Default-on affinity headers — OpenAI-compatible + Anthropic clients send
   the configured session header on every request; Codex keeps its
   wire-mandated `session_id` header.
3. Sticky rotation — Resolver/ProviderManager prefer a session's last
   healthy endpoint before rotating.
4. Upstream teardown — session end/archive/delete/idle/daemon-stop triggers
   best-effort release of upstream resources (llama-server KV erase).

## 2. Completion status

| Leaf | Status | Commit |
|------|--------|--------|
| 01-scope-threading | COMPLETE | ce62a53d |
| 02-affinity-header | COMPLETE | 8177a715 |
| 03-anthropic-header | COMPLETE | b38d772e |
| 04-resolver-sticky | COMPLETE | 07da9d2c |
| 05-session-lifecycle | COMPLETE | 1eedc867 |
| 06-provider-teardown | COMPLETE | ed7178e9 (+ b1af5ce6 integration) |
| 07-e2e-docs | COMPLETE | UNCOMMITTED (this tree; see master.md tracking row for the full defect list) |

## 3. Verification (all green, 2026-10-18)

- `make e2e-fast-area AREA=session-affinity` — ok (195s; includes the
  rotation proof: A fails -> B serves; B fails -> recovered A serves).
- `make e2e-fast-area AREA=session-teardown` — ok (32s; archive
  exact-once erase, stop sweep one erase per session, cloud fires none).
- `make graphs` + `make graphs-check` — green (stale sibling line
  offsets regenerated; audit F66 behavior, no topology change).
- `make test` — 10540 results, 0 failures (test-history recorded).
- New unit pins: `internal/llm/session_affinity_fixes_internal_test.go`
  (5 tests: 5xx arms endpoint block + sticky yields; cooldown cursor
  stays on the healthy member; 5xx not misrouted to the rate-limit lane;
  teardown Handles accepts snapshot keys; block clears on expiry+success).

## 4. The defect classes found while greening leaf 07 (for future waves)

1. GAP A (resolver): a plain 5xx alias failure armed NO endpoint block —
   the streaming client wraps the 5xx in a ClientError and the old
   `VerdictForFailure` only unwrapped a TOP-LEVEL *APIError. Fix: unwrap
   any *APIError in the chain (errors.As) and arm the endpoint cooldown
   for FailureServerError too (resolver.go). The sticky hook then
   disqualifies the dead member via the standard block check.
2. GAP B (teardown key shape, three layers):
   - `LlamaServerTeardown.Handles` now accepts the resolver-snapshot
     `host|credential` shape when the manager resolves the host to a live
     llama runtime (teardown.go isLlamaSnapshotKey).
   - `TeardownBaseURL`'s host fallback compares `endpointHost`
     (host:PORT — the EndpointKey shape) on both sides; the old compare
     used the BARE hostname and never matched a port-bearing key.
   - The erase route strips the provider baseURL's `/v1` prefix —
     llama-server's slot route lives at the server root.
3. WIRING (live-gate class, invisible to unit tests):
   - `RuntimeManager` is now constructed BEFORE the teardown adapters —
     the llama adapter captures the manager POINTER at construction and
     the old order left it nil for the daemon's lifetime.
   - `RegistryConfig.SharedChatter` (internal/agent/registry.go +
     components.go): registry-built loops (dispatched chat turns) ran on
     the SINGLE default client, whose `requestModelOverride` REJECTS a
     cross-provider `WithResolvedModel` — alias rotation AND the
     reporting seam were dead for every dispatched turn even though the
     daemon's primary loop had the PM. With the PM wired, loops take the
     chatter and everything engages.
4. session_id/conversation_id duality: lifecycle events carry the PRIMARY
   session id; the sticky/teardown snapshot keys on the ACCOUNTING
   (conversation) id. `TeardownCoordinator.SetIdentityResolver`
   (components.go wiring: store.Get(id).ConversationID) retries the
   snapshot under the alternate identity AND dispatches with the SNAPSHOT
   id — otherwise the adapter's reference count saw the session's own row
   as "another session" and skipped the erase.
5. Shutdown sweep semantics: `TeardownAdapter.SweepTeardown` erases
   WITHOUT the live-session reference count — at daemon stop every
   session ends together, so per-session refcounting skipped every erase.
   Live-event `Teardown` keeps the refcount (never erase under a live
   session).

## 5. Repo hygiene notes (still true)

- Working tree carries unrelated sibling WIP (bughunt audit waves, GUI,
  mcp, memory, …). Commit leaf-07 by EXPLICIT path only.
- Pre-commit runs 17 gates; the affected e2e suites pass directly (see §3),
  so a hook build broken by sibling mid-edit is the only --no-verify case,
  with the blocked gate + scoped result recorded in the commit body.
- Do NOT commit code directly per plan rules — orchestrator handles
  commits.
