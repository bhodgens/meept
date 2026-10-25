# Tree 04 — Knowledge Assets (orchestrator)

Parent: `../master.md`. Conventions: `../../SHARED-CONVENTIONS.md` (§2.5 Page
contract, §5 waves).

**Goal:** Port TAM's Wiki-as-memory-asset in meept's idiom: an opt-in
knowledge store of link-graphed markdown pages, agent-callable retrieval
tools, and directory ingestion — PLUS CodeGraph-style Go code indexing
(resolved Q7b). v1 ingestion scope: watched-dir markdown, explicit
`knowledge_ingest` tool, and a `go/ast` code indexer for Go repositories.
NO web crawlers.

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
- Config: `[memory.knowledge] enabled=false, ingest_dir="", max_pages=1000,
  code_dirs=[]`.

## Child Index (inline leaves)

| Leaf | Wave | Scope | Est. |
|------|------|-------|------|
| L1 knowledge-store | 1 | store pkg: schema, upsert, FTS search, link graph | 70K |
| L2 knowledge-tools | 2 | tool wiring + watched-dir ingestion | 70K |
| L3 code-index | 2 | go/ast symbol+call-graph indexer → code pages | 80K |
| L4 knowledge-e2e | 3 | e2e suite + config templates + docs | 50K |

## Dispatch Protocol

Standard (root master). L1 brief inlines the §2.5 Page struct and the three
tool schemas verbatim. L2 and L3 both run in wave 2 but touch disjoint
packages (`internal/tools/builtin`+`knowledge/adapter` vs
`internal/memory/knowledge/codeindex`) — no shared-file contention. L4 is
wave 3 and covers both features in one suite.

## Review Checklist

Root checklist plus:
- [ ] Tools absent from registry when `enabled=false` (assert by test)
- [ ] Nothing from this store enters prompts except via explicit tool call
      (no ambient injection in v1 — TAM parity is "called on demand")
- [ ] Upsert by title+source hash; re-ingest updates body + ingested_at
- [ ] `max_pages` enforced at ingest (oldest-evicted, log the eviction)
- [ ] Code index skips `vendor/`, `testdata/`, `_test.go`; call edges only
      resolve intra-repo (no phantom external callers)
- [ ] evolver wiki (`internal/selfimprove/`) untouched — verify by diff

## Coding Conventions

Per `../../SHARED-CONVENTIONS.md` §3: Go stdlib-first, sentinel errors with
`%w` wrap, `pkg/id.Generate()` IDs, JSON5 config opt-in defaults, no mutex
across I/O, lowercase UI text, no `os.Getwd()`, GPL/AGPL dep license stop.

## Integration Test Plan

L4 e2e suite `memory-knowledge-assets`: ingest 3 linked pages →
`knowledge_search` returns ranked hits → `knowledge_read` resolves a
`[[wikilink]]` → upsert same title updates in place → code-index a fixture
repo → `knowledge_search` finds a symbol → impact query returns its caller
→ disabled-config run shows zero knowledge tools in the tool list.

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| L1 knowledge-store | PENDING | |
| L2 knowledge-tools | PENDING | |
| L3 code-index | PENDING | parallel with L2, disjoint packages |
| L4 knowledge-e2e | PENDING | |

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

### Leaf L3 — code-index (Wave 2, parallel with L2)

**Scope (resolved Q7b — CodeGraph-style indexing for Go code):**
- `internal/memory/knowledge/codeindex/indexer.go` — walk `code_dirs`
  (default empty = disabled), parse each `*.go` file with `go/ast` +
  `go/parser` (stdlib, no new deps; skip `vendor/`, `testdata/`,
  `_test.go` by default):
  - One CODE page per file: `{title: "code:<pkg-path>", body: package,
    imports, top-level decl signatures}`.
  - Symbol table rows: func/method/type/var/const names with file:line.
  - Call edges: intra-repo calls between indexed functions (from
    `ast.CallExpr` resolved against the symbol table; unresolved →
    external, stored as unresolved links).
- Ingest into the SAME knowledge store as CODE-kind pages
  (`source: "code:<relpath>"`) so `knowledge_search` covers code and docs
  in one query; re-index upserts by path (changed-file detection by
  content hash, mirroring L2's dir-ingest diff).
- Impact-analysis helper: `ImpactedBy(symbol string) []string` — one-hop
  callers from the call-edge table (TAM's "changing this might affect
  those" v1: single hop, no transitive closure).
- Retrigger: index runs on consolidation tick alongside L2's dir ingest
  (same timer, no new ticker); full re-index on `code_dirs` change.
- Tests: fixture repo under `internal/memory/knowledge/codeindex/testdata/`
  — index it, assert symbol count, one known call edge, one impacted-by
  result, upsert-idempotency.
- License note: `go/ast`/`go/parser` are stdlib — no new dependency, no
  license gate needed.

**Verify:** `go build ./... && go test ./internal/memory/knowledge/... -count=1`.

**Do NOT commit.**

### Leaf L4 — knowledge-e2e (Wave 3)

**Scope:**
- e2e suite `e2e/suites/memory-knowledge-assets/` per Integration Test Plan
  + `e2e/manifest.json` entries.
- Config template comments in `config/` for the `[memory.knowledge]` block.
- Docs: `docs/workflows/memory.md` — "Knowledge Assets (opt-in)" section:
  contract, tool list, ingestion, evolver-wiki distinction, why default-off.
- CLI (thin): `meept knowledge list|stats` for operator visibility.

**Verify:** `make e2e-fast-area AREA=memory-knowledge-assets && go vet ./internal/memory/... ./internal/tools/...`.

**Do NOT commit.**

## Test Gate

<!-- Tests-first gate (hierarchical-planning skill v1.3.0, patched 2026-10-24).
     Coverage matrix derived mechanically from leaf task headers; the control
     agent (orchestrator, in-session) refines rows and fills test names at
     Phase 0 dispatch. No implementation leaf dispatches until every row is
     VALIDATED. Delta protocol: lesser of plan/tests updates to the greater.
     Recipe: skill hierarchical-planning -> references/tests-first-gate.md -->

N/A — no leaf documents found alongside this orchestrator. If this tree
gains leaves, build the coverage matrix before dispatching them.
