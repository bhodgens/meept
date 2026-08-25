# Change Journal (Revert) - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Do NOT commit.
> Do NOT use read_file on existing source — search_files/terminal cat only.

## Meta

- **Parent:** ../master.md
- **Scope:** SQLite-backed journal of applied file changes with checksum-guarded revert; CLI `meept changes list/revert`.
- **Dependencies:** 05-write-staging.md (accept path records entries)
- **Estimated Context:** 70K
- **Concurrency Group:** C
- **Audit references:** parity-audit gap #4c/#4d; FrontierAgent WorkspaceJournal + duckagent checksum-rewind patterns

## Goal

Once a staged change is accepted, there is no undo short of git checkpoints. This leaf adds a Journal: every accept records the pre-image bytes (capped), and `meept changes revert <id>` restores them — refusing when the current file no longer matches what was applied (drift guard). Session-wide revert supported via listing.

## Context

Session store uses SQLite at internal/session/store_sqlite.go — mirror its open/migrate pattern (database/sql + modernc or mattn driver — match whatever go.mod already carries; do NOT add new drivers). CLI commands live under cmd/meept/ subpackages wired in main command tree; follow existing `meept branch` command shape for flags/output style. IDs via pkg/id.Generate().

Key files:
- internal/session/store_sqlite.go - sqlite conventions to copy
- cmd/meept/ - command registration pattern (find branch or memory command)
- internal/tools/builtin/pending_changes.go + resolve.go from leaf 05

## Interface Contracts (From Parent)

### Exposes

```go
// File: internal/tools/builtin/change_journal.go
package builtin

type JournalEntry struct {
    ID        string
    SessionID string
    FilePath  string
    PreImage  []byte // content BEFORE apply; nil if skipped (> maxEntryBytes)
    PostSHA   string // sha256 hex of applied Modified content
    AppliedAt time.Time
    ChangeIDs []string
}

type JournalConfig struct {
    DBPath         string `json:"db_path"          toml:"db_path"`          // default ~/.meept/changes.db
    MaxEntryBytes  int64  `json:"max_entry_bytes"  toml:"max_entry_bytes"`  // default 1MiB
}

func NewJournal(cfg JournalConfig, logger *slog.Logger) (*Journal, error)
func (j *Journal) Record(entry *JournalEntry) error
func (j *Journal) List(sessionID string, limit int) ([]JournalEntry, error) // newest first
func (j *Journal) Revert(id string, fence FenceChecker) (path string, err error)
```

Revert semantics:
- entry.PreImage nil -> error "pre-image not journaled (size cap)"
- current file sha != PostSHA AND current sha != sha(PreImage) -> refuse drift ("file changed since apply")
- current == PreImage hash -> already reverted, idempotent success
- else write PreImage bytes atomically (temp+rename), fence.CheckPath first
- record nothing on failure; return restored path

Wiring: ResolveTool accept success calls j.Record with pre-image it already holds (Original). Registry gets SetJournal(*Journal) setter w/ typed-nil guard. Daemon constructs Journal when config enabled (default ON).

### Consumes

Leaf 05 types; FenceChecker; existing sqlite driver.

## Tasks

### Task 1: Store CRUD + migrations

**Files:** Create internal/tools/builtin/change_journal.go (+ _test.go).
Failing tests: Record+List roundtrip ordering; PreImage nil for oversize; Revert happy path restores bytes in tempdir; drift refusal message contains "changed since apply"; idempotent re-revert succeeds; fence refusal honored. Use t.TempDir for DB. Standard cycle per behavior group.

### Task 2: Accept-path recording

**Files:** Modify resolve.go (SetJournal wiring + Record call post-write); Test extension.
Failing test: staged change accepted -> journal row exists w/ matching ChangeIDs and PreImage equal to original bytes; legacy empty-hash accepts still record (PostSHA from written bytes). Standard cycle.

### Task 3: CLI commands

**Files:** Create cmd/meept/changes.go following existing command pattern; register in command tree.
Behavior: `meept changes list [--session S] [--limit N]` table output lowercase columns (id,file,applied,size,revertable); `meept changes revert <id>` prints result line; --json flag both.
Failing test: command-level test if harness exists for other cmds (check); else thin main-test asserting cobra-style registration compiles + help lists changes.
Docs: extend docs/workflows page created by leaf 05's sibling? NO — docs here: add section to nearest existing filesystem-tools doc page (search_files "file_edit" under docs/) describing journal + revert examples.

## Self-Verification Checklist

- [ ] -race green; tempdir-isolated tests
- [ ] Atomic write (tmp+rename) in Revert
- [ ] No new sqlite driver dependency
- [ ] CLI strings lowercase; IDs from pkg/id

**DO NOT COMMIT.**
**Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Drift guard logic exactly as contract (three-way hash cases)
- [ ] Size-cap skip path tested
- [ ] SQL parameterized; no fmt-built queries
- [ ] Errors wrapped %w

Output: APPROVED or gaps.

## Notes

- Keep Journal in builtin package beside registry to avoid import cycles; it is storage, not tool.
- Multi-entry revert (session-wide) = loop of single Revert by caller (leaf 07 surfaces it); no transactional batch this tree.
