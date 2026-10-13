# Tree 04 — Knowledge Assets (orchestrator)

Parent: `../master.md`. Conventions: `../../SHARED-CONVENTIONS.md` (§2.5 Page
contract, §5 waves).

**Goal:** Port TAM's Wiki-as-memory-asset in meept's idiom: an opt-in
knowledge store of link-graphed markdown pages, agent-callable retrieval
tools, and directory ingestion. v1 scope per OPEN-QUESTIONS Q7: watched-dir
markdown + explicit `knowledge_ingest` tool. NO crawler, NO CodeGraph —
meept already has RepoMap for code; duplicating TAM's CodeGraph is out of
scope.

**Architecture:** New subpackage `internal/memory/knowledge`. SQLite store
(FTS5 over page bodies, links table for the graph). Tools
`knowledge_search` / `knowledge_read` / `knowledge_ingest` registered only
when `[memory.knowledge] enabled` (default **false** — opt-in-defaults rule;
wiki→inference reachability is a product decision, and the existing evolver
wiki store (`internal/selfimprove/wiki.go`) stays evolver-only and untouched).

Relationship to evolver wiki: separate store, separate schema, no shared
code path. The opt-in gate keeps the standing "wiki/trace stores are
evolver-only, never reachable from inference-path prompt builders" rule
intact — this tree adds a NEW asset type with its own explicit user
consent flag; it does not flip any evolver default.

## Interface contracts

- Page type: frozen in SHARED-CONVENTIONS §2.5.
- Tool schemas (JSON schema for the agent tool registry):
  - `knowledge_search`: `{query: string, limit?: int (default 5, cap 20)}`
    → `{results: [{page_id, title, snippet, score}]}` (FTS5 BM25).
  - `knowledge_read`: `{page_id: string}` → `{page_id, title, body, links,
    source, ingested_at}`.
  - `knowledge_ingest`: `{title: string, body: string, source?: string}`
    → `{page_id}`. Re-ingest of the same `title+source` upserts (id by
    content hash of title+source, matching dual-store identity patterns).
- Link extraction: markdown links `[text](knowledge:<page_id>)` and
  `[[Page Title]]` (resolved to page_id at ingest when the target exists or
  later exists — stored unresolved, resolved at read time; no dangling-link
  errors).
- Config: `[memory.knowledge] enabled=false, ingest_dir="", max_pages=1000`.

## Child Index (inline leaves)

| Leaf | Wave | Scope | Est. |
|------|------|-------|------|
| L1 knowledge-store | 1 | store pkg: schema, upsert, FTS search, link graph | 70K |
| L2 knowledge-tools | 2 | tool wiring + watched-dir ingestion | 70K |
| L3 knowledge-e2e | 3 | e2e suite + config templates + docs | 50K |

## Dispatch Protocol

Standard (root master). L1 brief inlines the §2.5 Page struct and the three
tool schemas verbatim. L2 registers tools — coordinate with the ToolRegistry
single-file risk: `internal/tools/builtin` registration is file-per-tool
(add `knowledge.go`), so no shared-file contention expected; verify at
dispatch.

## Review Checklist

Root checklist plus:
- [ ] Tools absent from registry when `enabled=false` (assert by test)
- [ ] Nothing from this store enters prompts except via explicit tool call
      (no ambient injection in v1 — TAM parity is "called on demand")
- [ ] Upsert by title+source hash; re-ingest updates body + ingested_at
- [ ] `max_pages` enforced at ingest (oldest-evicted, log the eviction)
- [ ] evolver wiki (`internal/selfimprove/`) untouched — verify by diff

## Coding Conventions

Per `../../SHARED-CONVENTIONS.md` §3: Go stdlib-first, sentinel errors with
`%w` wrap, `pkg/id.Generate()` IDs, JSON5 config opt-in defaults, no mutex
across I/O, lowercase UI text, no `os.Getwd()`, GPL/AGPL dep license stop.

## Integration Test Plan

L3 e2e suite `memory-knowledge-assets`: ingest 3 linked pages →
`knowledge_search` returns ranked hits → `knowledge_read` resolves a
`[[wikilink]]` → upsert same title updates in place → disabled-config run
shows zero knowledge tools in the tool list.

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| L1 knowledge-store | PENDING | |
| L2 knowledge-tools | PENDING | |
| L3 knowledge-e2e | PENDING | |

## Leaf definitions

### Leaf L1 — knowledge-store (Wave 1)

**Scope:** Create `internal/memory/knowledge/`:
- `store.go` — SQLite schema (pages: id TEXT PK, title, body, source,
  ingested_at, content_hash UNIQUE(title,source); links: from_id, to_id,
  unresolved_title). FTS5 virtual table over body+title with triggers
  (follow `internal/memory/ftstore.go` schema/trigger patterns).
- `ingest.go` — `Ingest(page Page) (id string, err error)` with upsert
  semantics per contract; link extraction (`knowledge:` URI + `[[title]]`);
  `max_pages` eviction.
- `search.go` — `Search(query string, limit int) ([]Hit, error)` — FTS5
  BM25 + snippet(); `Read(id) (Page, error)` with links resolved to titles
  where resolvable.
- `knowledge_test.go` — roundtrip, upsert, link resolution, eviction,
  search ranking. (New-package bootstrap tier.)

**Verify:** `go build ./internal/memory/... && go test ./internal/memory/knowledge/ -count=1`.

**Do NOT commit.**

### Leaf L2 — knowledge-tools (Wave 2)

**Scope:**
- `internal/tools/builtin/knowledge.go` — the three tools per contract
  schemas, wrapping the store via a narrow interface (no import of
  internal/memory from tools — define the interface in the tools package
  and satisfy it in an adapter in `internal/memory/knowledge/adapter.go`,
  matching existing VectorSearcher seam style).
- Registration gated on `memory.knowledge.enabled` at manager/tool wiring
  point (same construction site as existing memory tools — single touch).
- Watched-dir ingestion: on consolidation tick (reuse existing timer, no new
  ticker), ingest new/changed `*.md` files from `ingest_dir` (source =
  "dir:<relpath>"; hash-diff to skip unchanged).
- Tests: tool schema roundtrip, disabled-gate registration test,
  dir-ingest change detection.

**Verify:** `go build ./... && go test ./internal/tools/builtin/ ./internal/memory/knowledge/ -run Knowledge -count=1`.

**Do NOT commit.**

### Leaf L3 — knowledge-e2e (Wave 3)

**Scope:**
- e2e suite `e2e/suites/memory-knowledge-assets/` per Integration Test Plan
  + `e2e/manifest.json` entries.
- Config template comments in `config/` for the `[memory.knowledge]` block.
- Docs: `docs/workflows/memory.md` — "Knowledge Assets (opt-in)" section:
  contract, tool list, ingestion, evolver-wiki distinction, why default-off.
- CLI (thin): `meept knowledge list|stats` for operator visibility.

**Verify:** `make e2e-fast-area AREA=memory-knowledge-assets && go vet ./internal/memory/... ./internal/tools/...`.

**Do NOT commit.**
