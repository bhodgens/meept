# mutexio

A `go vet`-style analyzer that detects `sync.Mutex` / `sync.RWMutex` locks held
across I/O operations, enforcing the project's **CLAUDE.md "Mutex scope" rule**:

> Never hold a mutex across I/O operations (network calls, disk reads/writes,
> LLM calls, channel sends). Use the "collect under lock, release, then
> operate" pattern.

## What it flags

The analyzer reports any function body where a `Lock()`/`RLock()` is followed
(in the same function, before the matching `Unlock()`/`RUnlock()`) by a call
to a method whose name matches a known I/O pattern:

| Category | Method names |
|----------|-------------|
| File I/O | `WriteFile`, `ReadFile`, `Close`, `Save`, `Load`, `Persist`, `PersistSync` |
| HTTP     | `Do`, `Post`, `Get` |
| LLM      | `Chat`, `ChatStream`, `ChatWithProgress` |
| RPC      | `Call` |
| Bus      | `Publish` |
| DB       | `Exec`, `Query`, `QueryRow` |
| Net      | `Connect`, `Dial`, `Send`, `Receive` |
| Codec    | `MarshalIndent`, `Unmarshal` |

Matching is by method name string (not type), which is intentionally
conservative for v1.

## What it does NOT flag

- Calls on the mutex itself (e.g., `mu.Unlock()` won't trigger on `Close`).
- Calls inside `defer` statements (those run at function return, not while
  the lock is held).
- I/O via callbacks, goroutines, or function pointers (intraprocedural only).

## Defer handling

When `defer mu.Unlock()` is used, the effective unlock position is the end of
the function body. Any I/O between `mu.Lock()` and the function's closing
brace is flagged, matching the rule's intent.

## Usage

```bash
# Run via make
make mutexio

# Or directly
go run ./tools/analyzers/mutexio/ ./...

# On a specific package
go run ./tools/analyzers/mutexio/ ./internal/agent/...
```

## Known limitations

1. **Intraprocedural only** — does not follow callbacks, goroutines, or
   interface method calls into other functions.
2. **Method-name based** — flags by the method name string, which may
   produce false positives on unrelated methods that happen to share names
   like `Save`, `Load`, or `Close`. Receiver type checking is planned for v2.
3. **No control-flow graph** — a textual range check is used instead of a
   proper CFG. This means I/O in an unreachable branch is still flagged,
   and I/O inside a nested `if`/`for` is flagged the same as straight-line
   code.
4. **Nested function literals** — calls inside a `FuncLit` (anonymous
   function) within the lock range are included in the scan, even though
   the function may not execute synchronously. This is conservative.
5. **Goroutines** — `go func(){ ... }()` inside a lock range is not specially
   handled; calls inside the literal are treated like any other.

## Structure

```
tools/analyzers/mutexio/
├── main.go                  # entrypoint (package main)
├── README.md                # this file
└── mutexio/
    └── analyzer.go          # analyzer (package mutexio)
```
