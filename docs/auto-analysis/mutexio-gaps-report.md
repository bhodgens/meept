# Mutexio Analyzer Gap Analysis

**Date:** 2026-06-16
**Analyzer:** `tools/analyzers/mutexio/`
**Objective:** Identify detection gaps in the mutexio analyzer and propose
practical enhancement strategies for each.

## Executive Summary

The current mutexio analyzer catches the most common form of the
CLAUDE.md "Mutex scope" rule violation: calling a known I/O method
(`Close`, `Send`, `Write`, `Query`, etc.) on a receiver between a `Lock`
and `Unlock` pair in the same function body. However, four gap categories
represent real bug classes that escape detection.

## Gap 1: Channel Sends Under Lock (`ast.SendStmt`)

**Problem:** The analyzer only inspects `ast.CallExpr` nodes. Channel
sends (`ch <- value`) are `ast.SendStmt`, not `CallExpr`, so they are
never checked.

**Example violation that escapes detection:**
```go
mu.Lock()
defer mu.Unlock()
resultCh <- computeResult()  // channel send under lock — NOT detected
```

**Detection strategy:** Add `(*ast.SendStmt)(nil)` to the inspector's
node filter. When a `SendStmt` falls within a Lock/Unlock textual range,
report it. This is a straightforward 10-line addition to `checkRange`.

**Implementation difficulty:** Low.
**False-positive risk:** Low — channel sends under lock are almost always
problematic per the CLAUDE.md rule.

## Gap 2: Goroutine Launches Under Lock (`ast.GoStmt`)

**Problem:** `go func(){ ... }()` launches a goroutine that may outlive
the lock scope. The parent goroutine's lock is not held by the child.
Calls inside the function literal are currently scanned as if they
execute synchronously (the analyzer notes this in limitation #5), but
the real issue is the opposite: the goroutine launch itself is an
implicit scheduling operation that shouldn't happen under lock.

**Example:**
```go
mu.Lock()
defer mu.Unlock()
go processAsync(data)  // goroutine launch under lock — NOT detected
```

**Detection strategy:** Add `(*ast.GoStmt)(nil)` to the node filter.
Report any `GoStmt` within a Lock/Unlock range. The launched goroutine
reads captured variables that may be mutated by other lock holders,
creating a data race.

**Implementation difficulty:** Low.
**False-positive risk:** Medium — there are legitimate cases where
launching a goroutine under lock is intentional (e.g., the goroutine
acquires its own lock). Consider allowing a whitelist comment like
`//mutexio:ignore`.

## Gap 3: No Interprocedural Analysis (Function Calls)

**Problem:** The analyzer is strictly intraprocedural. If code calls a
helper function that performs I/O, the call site is not flagged because
the helper's name isn't in `ioMethods`.

**Example:**
```go
func (m *Manager) locked() {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.saveToDisk()  // calls WriteFile internally — NOT detected
}

func (m *Manager) saveToDisk() {
    os.WriteFile(m.path, m.data, 0644)
}
```

**Detection strategies (ranked by practicality):**

1. **Heuristic: flag calls to methods on `this` under lock.** Any
   `m.foo()` call where `m` is the struct holding the mutex is
   suspicious — it may access state that requires the lock, or perform
   I/O. Medium false-positive rate but catches real bugs.

2. **Function-summary approach.** Build a map of function names that
   transitively call I/O methods. This requires a two-pass analysis:
   - Pass 1: scan all functions, build a set of functions that call
     I/O methods directly.
   - Pass 2: scan for functions that call pass-1 functions, transitively.
   - Pass 3: in locked sections, flag calls to functions in the I/O set.
   **Difficulty:** Medium-High. The `go/analysis` framework supports
   multi-package analysis but interprocedural call graph construction
   requires the `callgraph` or `ssa` packages from `golang.org/x/tools`.

3. **Use `cha` (Class Hierarchy Analysis) or `rts` (Rapid Type
   Analysis) call graph.** The `golang.org/x/tools/go/callgraph/cha`
   package provides a whole-program call graph. Combined with the SSA
   representation, we can build a precise "does this function transitively
   perform I/O?" query.
   **Difficulty:** High. Requires building SSA for all packages under
   analysis, which significantly increases analyzer runtime.

**Recommendation:** Start with strategy 1 (flag calls to `this`-methods
under lock) as a low-cost improvement. Consider strategy 2 if the project
grows significantly.

## Gap 4: Lock Held in Helper (Inverted Lock Pattern)

**Problem:** A helper function acquires the lock and performs I/O, but
the caller has no idea the lock is involved.

**Example:**
```go
func (m *Manager) Get(id string) *Item {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.db.Query("SELECT ...", id)  // I/O under lock
}

// Caller has no visibility:
item := manager.Get("foo")
```

**Current behavior:** This IS detected by mutexio — `Query` is in
`ioMethods` and the call is between Lock/Unlock. However, the fix
(restructure to release lock before DB call) may require API changes,
making it harder to address.

**Detection status:** Already detected. No gap here, but the fix pattern
requires careful refactoring:

```go
func (m *Manager) Get(id string) *Item {
    m.mu.RLock()
    snapshot := m.db  // collect under lock
    m.mu.RUnlock()
    return snapshot.Query("SELECT ...", id)  // I/O without lock
}
```

## Gap 5: `sync.Once` and Channel-Based Locking

**Problem:** Some code paths use `sync.Once` or channel-based coordination
instead of `sync.Mutex`. The analyzer only recognizes `Lock`/`RLock`/
`Unlock`/`RUnlock` method names.

**Detection strategies:**
- For `sync.Once.Do()`: flag I/O inside `Do()` since `Do` blocks until
  the function completes, effectively holding a lock.
- For channel-based locks (`<-sem` / `sem <- struct{}{}`): hard to
  distinguish from data flow, high false-positive risk.

**Recommendation:** Add `Once.Do` detection in v2. Skip channel-based
locking patterns — too noisy.

## Summary Table

| Gap | Detection difficulty | False-positive risk | Value | Priority |
|-----|---------------------|--------------------|----|----------|
| Channel sends | Low | Low | High | Implement first |
| Goroutine launches | Low | Medium | High | Implement second |
| Interprocedural (this-method) | Low-Medium | Medium | High | Implement third |
| Interprocedural (full SSA) | High | Low | Medium | Defer |
| sync.Once | Low | Low | Medium | Implement fourth |
| Channel-based locking | High | High | Low | Skip |

## Recommended Implementation Order

1. **Channel sends** (`ast.SendStmt`) — ~10 lines in `checkRange`
2. **Goroutine launches** (`ast.GoStmt`) — ~5 lines + node filter entry
3. **This-method calls under lock** — ~15 lines in `checkRange`
4. **`sync.Once.Do` blocks** — ~20 lines, new node filter entry

Total estimated effort for items 1-4: ~50 lines of new code, all within
the existing analyzer framework.
