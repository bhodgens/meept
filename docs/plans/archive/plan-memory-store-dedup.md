# Plan: Memory Store Deduplication

## Overview

`EpisodicMemory` and `TaskMemory` have ~80% code duplication: SQLite + FTS5 initialization, schema definitions, FTS triggers, and core CRUD methods (`Store()`, `Search()`, `GetRecent()`, `Delete()`).

**Source:** `plan-missing-implementation.md` §3.1

## Current State

### Files
- `internal/memory/episodic.go` - Conversation history with `category` field
- `internal/memory/task.go` - Task-specific memory with `domain` field

### Duplicated Components
| Component | Similarity | Key Differences |
|-----------|------------|----------------|
| SQLite initialization | ~100% | `category` vs `domain` field |
| FTS5 schema | ~100% | Same virtual table structure |
| FTS triggers | ~100% | Identical insert/update/delete triggers |
| `Store()` method | ~95% | Field name for metadata |
| `Search()` method | ~95% | BM25 ranking logic identical |
| `GetRecent()` method | ~95% | Timestamp-based ordering identical |
| `Delete()` method | ~95% | Simple DELETE by ID identical |

### Unique Features
- **EpisodicMemory**: Uses `category` field, no special methods beyond CRUD
- **TaskMemory**: Uses `domain` field, has `FindDuplicates()` method

## Solution: Generic `SQLiteFTSStore[T]`

Extract a generics-based base type that both `EpisodicMemory` and `TaskMemory` embed.

### Type Design

```go
// internal/memory/ftstore.go (NEW)

type FTSConfig struct {
    TableName    string      // SQLite table name
    FTS5Table    string      // FTS5 virtual table name
    CategoryField string     // "category" or "domain"
    DBPath       string      // SQLite file path
    Schema       string      // Full CREATE TABLE schema (including FTS)
    Triggers     []string    // FTS trigger definitions
}

// SQLiteFTSStore provides shared SQLite + FTS5 functionality
type SQLiteFTSStore[T any] struct {
    db     *sql.DB
    config FTSConfig
    logger *slog.Logger
}

// FTSOperations defines the minimal interface T must implement
type FTSOperations interface {
    GetID() string
    GetContent() string
    GetCategoryOrDomain() string
    SetTimestamps(now time.Time)
}

func NewSQLiteFTSStore[T FTSOperations](config FTSConfig, logger *slog.Logger) (*SQLiteFTSStore[T], error)
func (s *SQLiteFTSStore[T]) Initialize() error
func (s *SQLiteFTSStore[T]) Store(item T) error
func (s *SQLiteFTSStore[T]) Search(query string, limit int) ([]T, error)
func (s *SQLiteFTSStore[T]) GetRecent(limit int) ([]T, error)
func (s *SQLiteFTSStore[T]) Delete(id string) error
```

### Migration Steps

#### Phase 1: Create Generic Base
1. Create `internal/memory/ftstore.go` with generic implementation
2. Extract FTS5 initialization logic (schema, triggers)
3. Implement generic CRUD with BM25 ranking
4. Add `FTSOperations` interface requirements

**Effort:** ~150 lines for generic type + 100 lines for FTS logic

#### Phase 2: Refactor EpisodicMemory
```go
// Before (internal/memory/episodic.go)
type EpisodicMemory struct {
    db     *sql.DB
    logger *slog.Logger
    // ... FTS5 logic embedded
}

// After
type EpisodicMemory struct {
    store  *SQLiteFTSStore[*EpisodicItem]
    logger *slog.Logger
}

type episodicFTSAdapter struct {
    *EpisodicMemory
}

func (a *episodicFTSAdapter) GetID() string { return a.ID }
func (a *episodicFTSAdapter) GetContent() string { return a.Content }
func (a *episodicFTSAdapter) GetCategoryOrDomain() string { return a.Category }
func (a *episodicFTSAdapter) SetTimestamps(now time.Time) {
    a.CreatedAt = now
    a.UpdatedAt = now
}
```

**Effort:** ~50 lines to wrap generic store

#### Phase 3: Refactor TaskMemory
Same pattern as EpisodicMemory but with `domain` field instead of `category`. Keep `FindDuplicates()` as TaskMemory-specific method.

**Effort:** ~60 lines to wrap generic store + keep `FindDuplicates()`

#### Phase 4: Update Manager Integration
```go
// internal/memory/manager.go (MODIFY)
func NewManager(...) *Manager {
    episodic := NewEpisodicMemory(...)  // Still works, internal uses generic
    task := NewTaskMemory(...)          // Still works, internal uses generic
    // ...
}
```

No changes needed to public API - only internal implementation changes.

#### Phase 5: Tests
1. Unit tests for `SQLiteFTSStore[T]` generic type
2. Verify `EpisodicMemory` tests still pass
3. Verify `TaskMemory` tests still pass
4. Test `FindDuplicates()` still works for TaskMemory

**Files to create:**
- `internal/memory/ftstore_test.go` - Generic store tests

## Implementation Phases

### Phase 1: Generic Base (50%)
1. Create `internal/memory/ftstore.go`
2. Implement `SQLiteFTSStore[T]` with all shared logic
3. FTS5 schema and trigger generation
4. BM25 ranking for search

### Phase 2: Migrate EpisodicMemory (15%)
1. Wrap generic store in `EpisodicMemory`
2. Create `episodicFTSAdapter` to satisfy `FTSOperations`
3. Remove duplicated FTS logic from EpisodicMemory

### Phase 3: Migrate TaskMemory (15%)
1. Wrap generic store in `TaskMemory`
2. Create `taskFTSAdapter` to satisfy `FTSOperations`
3. Remove duplicated FTS logic from TaskMemory
4. Keep `FindDuplicates()` method intact

### Phase 4: Integration (10%)
1. Verify Manager still constructs EpisodicMemory/TaskMemory correctly
2. Build passes
3. Existing tests pass

### Phase 5: Polish (10%)
1. Add docs/comments to generic type
2. Update CLAUDE.md to mention generic architecture
3. Verify no API changes for callers

## Files to Create

| File | Purpose |
|------|---------|
| `internal/memory/ftstore.go` | Generic `SQLiteFTSStore[T]` with FTS5 support |
| `internal/memory/ftstore_test.go` | Unit tests for generic store |

## Files to Modify

| File | Change |
|------|--------|
| `internal/memory/episodic.go` | Replace embedded FTS logic with generic store wrapper |
| `internal/memory/task.go` | Replace embedded FTS logic with generic store wrapper |
| `CLAUDE.md` | Document generic architecture in memory section |

## Security Considerations

- SQL injection prevention: Use parameterized queries (already in place)
- Path validation: Keep existing SQLite path checks
- No new permissions needed

## Verification

```bash
# Build
go build ./internal/memory/...

# Unit tests
go test ./internal/memory/... -v

# Integration tests (full suite)
go test ./... -v
```

## Risks & Mitigation

| Risk | Severity | Mitigation |
|------|----------|------------|
| Generic type complexity | Medium | Keep interface minimal, document clearly |
| `FindDuplicates()` breaks | Low | Keep as TaskMemory-specific method |
| FTS5 behavior changes | High | Copy BM25 logic exactly, test search results |
| Performance regression | Low | Same SQLite operations, just refactored |

## Success Criteria

- ✅ `EpisodicMemory` and `TaskMemory` both use `SQLiteFTSStore[T]` internally
- ✅ No public API changes (Manager still constructs them the same way)
- ✅ All existing tests pass
- ✅ `FindDuplicates()` still works for TaskMemory
- ✅ Build passes cleanly

## Open Questions

1. **Should `FindDuplicates()` be generic?** Could add optional `DuplicatesFinder` interface, but overkill. Keep as TaskMemory-specific.

2. **Should schema be configurable?** Yes - pass schema as part of `FTSConfig` to support both `category` and `domain` fields.

3. **What about FTS field names?** Abstract as `CategoryField` in config: "category" for EpisodicMemory, "domain" for TaskMemory.
