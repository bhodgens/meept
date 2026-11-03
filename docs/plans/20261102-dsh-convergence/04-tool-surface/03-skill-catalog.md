# 03 Skill Catalog + Hot-Reload - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. This branch runs a tests-first gate;
> the failing gate tests ALREADY EXIST and are committed (00T-tests.md T3/T3b
> + the tool-surface e2e suite). Your job is to make them pass - do NOT
> delete, skip, weaken, or rewrite any gate test (report suspected-wrong gate
> tests to the orchestrator instead). Do NOT commit - the orchestrator handles
> all git operations after review. Do NOT use read_file on existing source
> files - explore with search_files or terminal cat. After writing a file, do
> NOT read it back - write once and stop. Report what you built, files touched,
> and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** fsnotify hot-reload of skills over the two discovery roots + a
  `skills/change` bus event + a DURABLE per-session skill catalog that survives
  resume, replacing the process-cache-only relist in
  `internal/agent/context_injector.go` (master C9).
- **Dependencies:** B3 (parent orchestrator); gate tests in 00T-tests.md
- **Estimated Context:** 24K
- **Concurrency Group:** A
- **Audit references:** /tmp/dsh-audit/skills-schedule-storage.md (1b watcher, 1c catalog)

## Goal

Skills today are discovered at start and cached lazily; picking up a new or
edited skill requires a restart or `make sync-config` (audit 1b). This leaf
mounts a filesystem watcher on the two discovery roots, hot-invalidates the
existing `LazySkillLoader` cache on add/change/unlink, emits a `skills/change`
bus event so the TUI/GUI can refresh, and replaces the per-prompt relist of
cached skills with a DURABLE per-session catalog — a session message listing
every visible model-invocable skill, digest-deduplicated, so the catalog
reconstructs after resume/restart instead of depending on the live process
cache (audit 1c).

## Context

- `internal/skills/discovery.go:31-53` - `DefaultTiers()`: project `.meept/skills`
  (PriorityProject) and `~/.meept/skills` (PriorityUser), plus a config tier and
  an auto-added Hermes tier. `:136 DiscoverWithContext` rebuilds the whole map
  with priority shadowing. `:194 DiscoverMetadataOnly` is the cheap index rebuild.
- `internal/skills/lazy_loader.go` - `NewLazySkillLoader(index, opts...)` (:56),
  `Load` (:73), `Get` (:214), `Invalidate(name)` (:238), `Clear()` (:260),
  `CachedNames()` (:294), `SetIndex(index)` (:307). This is the cache leaf 03
  controls; fsnotify events map to `Invalidate`/`Clear` + a metadata re-discover
  + `SetIndex`.
- `internal/agent/context_injector.go:112-136` - `visibleSkillLines()` emits one
  C4 line per CACHED visible skill at prompt build; the listing depends on the
  live cache, so a skill first needs a fuzzy hit to appear and disappears on
  restart. This is the function the DURABLE catalog replaces/extends.
- Bus: `internal/bus` typed topics (`bus.Topic[T]` / `PublishT` / `SubscribeT`,
  root AGENTS.md). The `skills/change` event follows the typed-topic pattern;
  payload shape decided here (see B3). No `skills/change` topic exists today.
- fsnotify: `github.com/fsnotify/fsnotify` is the standard Go watcher (BSD).
  Confirm it is pinned in go.mod (or vendor it) before importing — see the
  parent's Open Questions #1; never choose an unvetted fork.

## Verified Anchors

- internal/skills/discovery.go:31-53 - DefaultTiers (two discovery roots + config/Hermes tiers)
- internal/skills/discovery.go:136 - DiscoverWithContext (full map rebuild with shadowing)
- internal/skills/discovery.go:194 - DiscoverMetadataOnly (cheap index rebuild)
- internal/skills/lazy_loader.go:56/73/214/238/260/294/307 - loader seams
- internal/agent/context_injector.go:112-136 - visibleSkillLines (replaced by durable catalog)
- internal/bus - typed Topic/PublishT/SubscribeT pattern (per root AGENTS.md)
- internal/session - durable per-session message store (the catalog's home)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/skills/watcher.go (NEW):
//  type Watcher struct{...}
//  func NewWatcher(loader *LazySkillLoader, discover func(context.Context) error,
//        log *slog.Logger) (*Watcher, error)
//  // Discover those exact roots that EXIST today (project + user); watch
//  // add/change/remove under <root>/<skill>/SKILL.md and <root>/<skill>.md.
//  func (w *Watcher) Start(ctx context.Context) error   // spawns fsnotify loop
//  func (w *Watcher) Close() error
// internal/bus: new typed topic carrying:
//  type SkillChanged struct { Source string `json:"source"`; Names []string `json:"names,omitempty"` }
//  skills/change  // bus.Topic[SkillChanged]  (payload exactly this struct)
// internal/agent/context_injector.go:
//  // REPLACE the cache-only C4 listing source with the durable catalog: a
//  // message rendered at turn start from the fresh discovery, appended to the
//  // session once per digest. digest = sha256 over sorted "name\tdescription".
```

### What This Leaf Exposes

(A) The watcher + `skills/change` event + the durable catalog message. (B) The
`/name` gesture consumes the catalog + loader — 04-skill-invocation.

### What This Leaf Consumes

- B3 (parent) - full contracts above. No signature invented beyond B3. The
  loader seams (`Invalidate`/`Clear`/`SetIndex`) are THE invalidation surface.
- Consumers: 04-skill-invocation (catalog + loader), tool-surface e2e,
  TUI/GUI refresh (skills/change).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|--------------|
| T1 | fsnotify watcher on the two roots | Create internal/skills/watcher.go; Test internal/skills/watcher_test.go (gate 00T T3) | `go test -p 2 ./internal/skills -run Watcher -v` | FAIL: no watcher type (gate test references it) | PASS: watcher monitors the discovery roots' SKILL.md / <name>.md paths | Given an fsnotify event for `<root>/<skill>/SKILL.md` add; When Start handles it; Then the loader cache is invalidated for that skill (or cleared) and a re-discover runs |
| T2 | skills/change bus event | Modify internal/skills/watcher.go; add the typed topic | `go test -p 2 ./internal/skills ./internal/bus -run 'SkillChange|Topic' -v` | FAIL: no skills/change topic/publish | PASS: on a qualifying fsnotify event a `skills/change` typed event is published with Source:"skills" + affected Names | Given a qualifying event; When the watcher finishes invalidation+rediscover; Then skills/change fires after the durable commit (post-change, not before) |
| T3 | Durable per-session catalog | Modify internal/agent/context_injector.go (and its driving caller); Test internal/agent/context_catalog_test.go (gate 00T T3b) | `go test -p 2 ./internal/agent -run Catalog -v` | FAIL: no catalog message/digest/rebuild | PASS: at turn start the injector materializes a catalog message listing every visible model-invocable skill (C4 line), digest-deduplicated; rebuilding from the persisted session record yields the same set | Given a fresh discovery of {a,b}; When the catalog is built at turn start and then rebuilt from the session record; Then both produce the same digest and no duplicate catalog message is appended |
| T4 | Replace the cache-only relist | Modify internal/agent/context_injector.go (:112-136) | `go test -p 2 ./internal/agent -run Catalog -v` | FAIL: still reads live cache only | PASS: `visibleSkillLines` (or its replacement) sources from the durable catalog discovery snapshot, not only `CachedNames()` | Given a skill present in discovery but not yet cache-loaded; When the catalog builds; Then it is listed (skills discoverable independent of fuzzy routing) |
| T5 | Hermetic e2e: hot-reload + durability | e2e/suites/tool-surface/ (e2e tag; gate 00T T5) | `make e2e-fast-area AREA=tool-surface` | FAIL: new skill not picked up without restart | PASS: writing `~/.meept/skills/x/SKILL.md` makes skill x appear in the session catalog without a daemon restart; after a daemon restart the catalog still lists it (durable) | Given a running scratch daemon; When a new skill file is added to the user root; Then the catalog shows it within the watcher threshold; a daemon restart keeps it |

**Worked example - T3:**

T3: Durable catalog with digest dedup.
1. Confirm the committed gate test (internal/agent/context_catalog_test.go) builds a catalog twice and expects equal digests + no duplicate append.
2. Implementation sketch:
```go
// catalogs.go / within context_injector.go
func catalogDigest(entries []catalogEntry) string {
    // sorted by name; h := sha256; for each e: h.Write([]byte(e.Name + "\t" + e.Description + "\n"))
    // return hex
}
// at turn start: entries = every visible model-invocable skill from discovery
// (NOT just CachedNames); if catalogDigest(entries) != lastPersistedDigest,
// append the catalog message (source kind:"skill-catalog") to the session AND
// persist the digest so a resume rebuild produces the same set.
```
   The catalog is a DURABLE session record — rebuild at turn start from the
   session store (like dsh 1c's catalogHistory), not from the live cache. Keep
   the C4 line shape from the current emit (`- name: desc [load: file_read path]`).
3. Verify: `go test -p 2 ./internal/agent -run Catalog -v` -> PASS.

## Self-Verification Checklist

- [ ] All tasks implemented and gate tests pass GREEN (00T T3/T3b + e2e hot-reload/durability case)
- [ ] Interface contract B3 satisfied exactly (Watcher, skills/change payload, durable catalog digest)
- [ ] Discovery remains authoritative; loader invalidation rides existing seams (Invalidate/Clear/SetIndex)
- [ ] skills/change fires AFTER a durable commit, on qualifying paths only (SKILL.md / <name>.md)
- [ ] No gate test deleted/weakened; gate-test diff empty
- [ ] fsnotify dependency resolved (pinned/vendored, never an unvetted fork)
- [ ] No scope creep - no changes to skill parser, nested_agent_route, or requires-tools

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path, incl. go.mod if fsnotify added]
2. **Verify command + output:** [the verify commands in Tasks; last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests passing and untouched
- [ ] B3 matches exactly (signatures, types, paths)
- [ ] Watcher watches ONLY the canonical skill paths (<root>/<skill>/SKILL.md and <root>/<skill>.md) — stray events (e.g. .git, editor locks) must not hammer a full rediscover
- [ ] No full-map cache rebuild on every keystroke; invalidation is scoped where possible
- [ ] Conventions followed; no bugs, no security issues; no debug artifacts or line-number corruption
- [ ] skills/change typed topic is registered and wired (bus topology regenerates via `make graphs`)

Output: APPROVED or specific gaps with file + line references.

## Notes

- dsh watcher semantics to port (packages/skill/skill-filesystem/src/index.ts:294-615): a stability threshold (~200ms) collapses bursts, poll fallback for inotify on some backends, and `isRelevantWatchEvent` fires ONLY on `<root>/<skill>/SKILL.md` / `<root>/<skill>.md` (:676-693). Port the relevance gate + the debounce; the roots are meept's (project + user), not dsh's.
- Durability: keep the catalog as a session record (source kind "skill-catalog", matching dsh tool-skill/src/index.ts:35-50 SkillCatalogSource). meept's `internal/session` store is the home; a resume replay reads it back and digests the last catalog.
- `skills.state` remains opt-in in meept (root AGENTS.md). The watcher + catalog are surface over that capability; do not flip the default (a product decision per root AGENTS.md).
