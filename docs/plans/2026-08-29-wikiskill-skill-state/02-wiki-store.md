# Wiki Store — Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing a
> file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ./master.md
- **Scope:** Persistent wiki layer (`patterns/`, `index.md`, `logs.md`,
  `skill-impact.md`) under `~/.meept/wiki/` with LearningPipeline restart
  survival (arXiv:2608.27454 Wiki Layer).
- **Dependencies:** none (Wave A; independent of 01)
- **Estimated Context:** ~55K
- **Concurrency Group:** A
- **Research reference:** arXiv:2608.27454 (WikiSkill) §3.1 Wiki Layer +
  §3.2.2 maintainer behavior. Commit message MUST cite the arXiv id.

## Goal

`LearningPipeline.StorePattern` is RAM-only — `patterns.json` writes are
deprecated (learning.go:557-697) — so evolver Pass B loses all learned
patterns on daemon restart and nothing records rejected proposals. This leaf
adds `WikiStore`: pattern pages as markdown, an index the evolver can paste
into prompts, an append-only evolution log, and a skill-impact ledger. It also
wires LearningPipeline to write-through on StorePattern and reload on
Initialize.

Binding constraint from the paper (master §Notes): NOTHING here may feed
ContextInjector or any inference prompt. The wiki serves the evolver only.

## Context

Key files to understand before implementing:

- internal/selfimprove/learning.go — `LearnedPattern` (line 44: ID, Type,
  Status, Domain, Description, Pattern, Examples, Confidence, ContentHash,
  Tags), `StorePattern` (561), `Initialize` (176), `loadPatterns` (936),
  deprecated `savePatternsFromSnapshot` (927). Same package; tests INTERNAL —
  grep learning_test.go for fixtures first.
- internal/skills/lifecycle/writer.go — atomic `.tmp`+`os.Rename` pattern to copy.
- docs/plans/2026-08-29-wikiskill-skill-state/master.md — wiki layout (frozen).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/selfimprove/wiki.go
package selfimprove

type SkillImpactEntry struct {
    Time      time.Time `json:"time"`
    Action    string    `json:"action"`
    SkillName string    `json:"skill_name"`
    Diff      string    `json:"diff,omitempty"`
    Score     float64   `json:"score"`
    Accepted  bool      `json:"accepted"`
    Reason    string    `json:"reason,omitempty"`
}

type WikiStore struct { /* dir + logger */ }

func NewWikiStore(dir string, logger *slog.Logger) *WikiStore
func (w *WikiStore) UpsertPattern(p *LearnedPattern) (string, error)
func (w *WikiStore) RebuildIndex() error
func (w *WikiStore) AppendLog(entry string) error
func (w *WikiStore) AppendSkillImpact(e SkillImpactEntry) error
func (w *WikiStore) ReadSkillImpact() (string, error)
func (w *WikiStore) LoadPatterns() ([]*LearnedPattern, error)

// Same-file additions to LearningPipeline:
func (lp *LearningPipeline) SetWikiStore(ws *WikiStore) // nil guard
```

Frozen file formats (exact shapes matter for leaf 03's prompt glue):

```
patterns/<slug>.md   — slug := domain + "-" + first 12 hex of ContentHash
                       (empty domain → "general"); YAML frontmatter:
                       ---
                       id: <pattern ID>
                       type: <strategy|tactic|anti_pattern|heuristic>
                       status: <pending|active|deprecated|rejected>
                       confidence: <float>
                       use_count: <int>
                       created_at: <RFC3339>
                       updated_at: <RFC3339>
                       ---
                       # <description first line>
                       ## Pattern
                       <pattern text>
                       ## Examples
                       - <example> (zero or more)

index.md             — one bullet per pattern, paper's index format:
                       - [slug](patterns/slug.md): PROBLEM + ROOT CAUSE + FIX
                       (v1 renders description as the one-liner; log a WARN
                       when a pattern page has no description)

logs.md              — append-only: "<RFC3339> <free-text entry>\n"

skill-impact.md      — append-only ledger, one JSON object per line
                       (JSONL) using SkillImpactEntry field order.
```

Upsert + reload semantics: `UpsertPattern` overwrites the page for the same
slug (same ContentHash ⇒ same slug) with bumped `updated_at`; LoadPatterns
parses every page back into a LearnedPattern (ID from frontmatter; missing ID
⇒ regenerate via pkg/id and log Warn). `LearningPipeline.Initialize` calls
LoadPatterns when wiki store is set and inserts only IDs absent from the
in-memory map (restart survival without clobbering live state). Wiki I/O
errors inside StorePattern log at Warn and NEVER fail StorePattern.

### What This Leaf Consumes

- `LearnedPattern` (same package).
- `pkg/id.Generate()` for regenerated IDs on parse fallback.

## Tasks

### Task 1: WikiStore.UpsertPattern + slug stability

**Objective:** Persist one pattern page per ContentHash, stable across restarts.

**Files:**
- Create: `internal/selfimprove/wiki.go`
- Test: `internal/selfimprove/wiki_test.go`

**Step 1: Write failing test**

```go
func TestUpsertPattern_StableSlugOverwrite(t *testing.T) {
    dir := t.TempDir()
    ws := NewWikiStore(dir, testLogger())
    p := &LearnedPattern{
        ID: "pat-1", Type: PatternTypeStrategy, Status: PatternStatusActive,
        Domain: "code", Description: "prefer table-driven tests",
        Pattern: "write table tests", Confidence: 0.8, UseCount: 5,
        CreatedAt: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
        UpdatedAt: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
        ContentHash: "abc123def456abc123def456",
    }
    p1, err := ws.UpsertPattern(p)
    if err != nil { t.Fatalf("upsert1: %v", err) }
    p.UseCount = 6
    p2, err := ws.UpsertPattern(p)
    if err != nil { t.Fatalf("upsert2: %v", err) }
    if p1 != p2 { t.Fatalf("slug unstable: %s vs %s", p1, p2) }
    if !strings.Contains(filepath.Base(p2), "code-") {
        t.Fatalf("slug missing domain: %s", p2)
    }
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/selfimprove/ -run TestUpsertPattern -v`
Expected: FAIL — undefined: NewWikiStore

**Step 3: Write minimal implementation**

`wiki.go`: slug per frozen format (hex lowercase of first 12 ContentHash
chars; hash may already be lowercase hex — do not re-hash). Frontmatter via
hand-rolled writer in frozen field order (match repo's hujson/markdown
utility only if it already has a YAML frontmatter writer — grep
internal/util/markdown first; do not add a yaml dep). Atomic tmp+rename.

**Step 4: Run test to verify pass**

Run: `go test ./internal/selfimprove/ -run TestUpsertPattern -v`
Expected: PASS

### Task 2: RebuildIndex + AppendLog + AppendSkillImpact/ReadSkillImpact

**Objective:** The three flat files the evolver consumes.

**Files:**
- Modify: `internal/selfimprove/wiki.go`
- Test: `internal/selfimprove/wiki_test.go`

**Step 1: Write failing test**

```go
func TestAppendSkillImpact_JSONLAndRead(t *testing.T) {
    dir := t.TempDir()
    ws := NewWikiStore(dir, testLogger())
    e := SkillImpactEntry{
        Time: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
        Action: "refine", SkillName: "reviewer", Score: 0.42, Accepted: false,
        Reason: "dims below floor",
    }
    if err := ws.AppendSkillImpact(e); err != nil { t.Fatal(err) }
    if err := ws.AppendSkillImpact(e); err != nil { t.Fatal(err) } // two rows
    got, err := ws.ReadSkillImpact()
    if err != nil { t.Fatal(err) }
    lines := strings.Split(strings.TrimSpace(got), "\n")
    if len(lines) != 2 { t.Fatalf("want 2 rows, got %d: %q", len(lines), got) }
    var back SkillImpactEntry
    if err := json.Unmarshal([]byte(lines[0]), &back); err != nil {
        t.Fatalf("row not JSONL: %v", err)
    }
    if back.Accepted { t.Fatal("accepted flag lost") }
}
```

RebuildIndex test: write 2 patterns via UpsertPattern, RebuildIndex, read
index.md, assert both slugs present with `patterns/` links.

**Step 2: Run test to verify failure**

Run: `go test ./internal/selfimprove/ -run 'TestAppendSkillImpact|TestRebuildIndex' -v`
Expected: FAIL — undefined methods

**Step 3: Write minimal implementation**

AppendLog/AppendSkillImpact: `os.OpenFile(..., O_APPEND|O_CREATE|O_WRONLY, 0o600)`
— append-only files are intentionally NOT rewritten. RebuildIndex walks
patterns/*.md, reads frontmatter (reuse Task 1's parser), writes index.md
atomically.

**Step 4: Run test to verify pass**

Run: `go test ./internal/selfimprove/ -run 'TestAppendSkillImpact|TestRebuildIndex' -v`
Expected: PASS

### Task 3: LoadPatterns round-trip + Initialize repopulation

**Objective:** Restart survival — Initialize repopulates the in-memory map
from wiki pages.

**Files:**
- Modify: `internal/selfimprove/wiki.go`, `internal/selfimprove/learning.go`
- Test: `internal/selfimprove/wiki_test.go`, `internal/selfimprove/learning_test.go`

**Step 1: Write failing test**

```go
func TestInitialize_ReloadsPatternsFromWiki(t *testing.T) {
    dir := t.TempDir()
    ws := NewWikiStore(dir, testLogger())
    p := &LearnedPattern{
        ID: "pat-9", Domain: "debug", Description: "grep before read",
        Pattern: "grep first", Confidence: 0.9, UseCount: 7,
        ContentHash: "cafebabecafebabecafebabe",
        CreatedAt: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
        UpdatedAt: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
    }
    if _, err := ws.UpsertPattern(p); err != nil { t.Fatal(err) }
    if err := ws.RebuildIndex(); err != nil { t.Fatal(err) }

    lp := NewLearningPipeline(DefaultLearningConfig(), nil, t.TempDir(), testLogger())
    if err := lp.Initialize(context.Background()); err != nil { t.Fatal(err) }
    lp.SetWikiStore(ws)
    // Second Initialize path: simulate restart by a fresh pipeline with the
    // same wiki dir.
    lp2 := NewLearningPipeline(DefaultLearningConfig(), nil, t.TempDir(), testLogger())
    lp2.SetWikiStore(ws)
    if err := lp2.Initialize(context.Background()); err != nil { t.Fatal(err) }
    got, err := lp2.Retrieve(context.Background(), "grep before read", "", 5)
    if err != nil { t.Fatal(err) }
    if len(got) == 0 { t.Fatal("pattern not reloaded from wiki after restart") }
}
```

Note: check Retrieve's query semantics in learning.go first (query matching
against description/pattern/tags) and adapt the assertion to however existing
learning tests assert Retrieve; do not fight the matcher — reuse the
existing test conventions in learning_test.go.

**Step 2: Run test to verify failure**

Run: `go test ./internal/selfimprove/ -run TestInitialize_Reloads -v`
Expected: FAIL — SetWikiStore undefined

**Step 3: Write minimal implementation**

- `SetWikiStore`: nil-guarded setter.
- `LoadPatterns`: parse every patterns/*.md frontmatter into LearnedPattern
  (confidence via strconv, times via RFC3339, examples from `## Examples`
  list). Corrupt page ⇒ Warn + skip.
- `Initialize`: after existing `loadPatterns()`, when `lp.wiki != nil`, call
  LoadPatterns and insert only missing IDs. Keep all existing behavior when
  wiki is nil (zero behavior change for current users).

**Step 4: Run test to verify pass**

Run: `go test ./internal/selfimprove/ -race -count=1 -v`
Expected: PASS (whole package — learning_test.go must stay green; it asserts
patterns.json is NOT written; wiki writes must not break those assertions
because wiki dir ≠ dataDir in those tests — verify, do not assume).

### Task 4: Self-verification sweep

- `go build ./...`
- `go test ./internal/selfimprove/ -race -count=1`
- `gofmt -l` on touched files
- `go run ./tools/analyzers/mutexio/... ./internal/selfimprove/`
- grep `scratch/` exclusion: confirm no prompt builder references WikiStore.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`-race -count=1`)
- [ ] Contracts satisfied exactly (signatures, file formats, slug rule)
- [ ] Files at exact specified paths
- [ ] learning_test.go green (patterns.json still never written)
- [ ] No deviations (or documented below)
- [ ] Nil wiki ⇒ byte-identical LearningPipeline behavior

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contracts match master §Contract 2 exactly (incl. frozen file formats)
- [ ] Append-only files never rewritten; Upsert files atomic
- [ ] SetWikiStore nil guard; StorePattern never fails on wiki I/O
- [ ] No unused exports; no debug artifacts; gofmt clean
- [ ] WikiStore referenced from NO inference prompt builder

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The JSONL ledger (skill-impact.md) is deliberately append+read-whole (leaf 03
  caps it at 20k chars in prompts). Do not build parse/stream abstractions.
- Do NOT wire the store into the daemon or evolver (leaf 05 / leaf 03 own that).
- Frontmatter writer: keep it 20 lines, hand-rolled, deterministic field order.
  If internal/util/markdown already has frontmatter helpers, use them.
