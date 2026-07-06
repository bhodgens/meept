# Concurrency and Field Ownership

This document defines the concurrency patterns and field ownership annotations used throughout the codebase to prevent TOCTOU bugs, race conditions, and missing serialization.

## Annotation Format

All shared struct fields MUST be annotated with their guarding mechanism:

```go
type GitSync struct {
    mu      sync.Mutex  // guards: running, runCtx, runCancel
    gitMu   sync.Mutex  // guards: all git CLI operations
    cfg     *Config     // immutable after construction
    localCfg *Config    // immutable after construction
}
```

### Annotation Types

| Annotation | Meaning |
|------------|---------|
| `// guards: field1, field2` | Access to these fields requires holding this mutex |
| `// immutable` | Field is read-only after construction |
| `// per-key: invokeMuMap` | Field uses per-key serialization via map |

## Access Patterns

### Guarded Fields

Fields annotated with `// guards:` must only be accessed while holding the specified mutex.

```go
// WRONG: accessing guarded field without lock
running := g.running

// RIGHT: snapshot under lock
g.mu.Lock()
running := g.running
g.mu.Unlock()
```

### Snapshot Pattern

When you need to use a guarded value across operations, take a snapshot under lock:

```go
// Collect under lock, release, then operate
g.mu.Lock()
cfg := m.config  // snapshot
running := g.running
g.mu.Unlock()

result, err := doNetworkCall(ctx, cfg)  // I/O outside lock
```

### Per-Entity Serialization

For operations that need per-key serialization (e.g., one operation per employee/job ID):

```go
// Pattern: map[K]*sync.Mutex for per-key serialization
type Manager struct {
    invokeMuMap      map[string]*sync.Mutex
    invokeMuMapGuard sync.Mutex  // guards: invokeMuMap
}

func (m *Manager) getMutex(key string) *sync.Mutex {
    m.invokeMuMapGuard.Lock()
    defer m.invokeMuMapGuard.Unlock()

    if m.invokeMuMap == nil {
        m.invokeMuMap = make(map[string]*sync.Mutex)
    }
    mu, ok := m.invokeMuMap[key]
    if !ok {
        mu = &sync.Mutex{}
        m.invokeMuMap[key] = mu
    }
    return mu
}

func (m *Manager) Invoke(ctx context.Context, key string) error {
    mu := m.getMutex(key)
    mu.Lock()
    defer mu.Unlock()
    // ... do work serialized by key
}
```

## Common Pitfalls

### 1. Typed-nil Interface Assignment

Nil concrete pointer assigned to interface produces non-nil interface that panics:

```go
type Checker interface {
    Check() error
}

type RealChecker struct{}

func (r *RealChecker) Check() error { return nil }

// WRONG:
var c Checker
c = (*RealChecker)(nil)  // c != nil now, but c.Check() panics

// RIGHT:
var c Checker
if real != nil {
    c = real  // only assign non-nil
}
```

### 2. Deferred Unlock with I/O

Deferred `Unlock()` means the lock is held until function return:

```go
func (s *Service) Process() error {
    s.mu.Lock()
    defer s.mu.Unlock()  // held until end of function

    // WRONG: I/O under lock
    return s.db.Exec(...)  // mutexio analyzer will flag this

    // RIGHT: snapshot, then operate
    cfg := s.config
    s.mu.Unlock()  // explicit unlock before I/O
    return s.db.Exec(...)
}
```

### 3. Channel Send Under Mutex

Holding mutex during channel send can deadlock if receiver needs same mutex:

```go
// WRONG: channel send under mutex
g.mu.Lock()
g.results <- result  // may block forever
g.mu.Unlock()

// RIGHT: prepare message, release, then send
g.mu.Lock()
result := g.results
g.mu.Unlock()
g.results <- result
```

## Fieldguard Analyzer

The `fieldguard` analyzer (`tools/analyzers/fieldguard/`) verifies that field access follows ownership annotations. Run with:

```bash
make fieldguard
```

### What it checks:

1. Fields with `// guards:` annotation are only accessed while holding that mutex
2. Fields with `// immutable` are only written in constructors
3. Unannotated shared fields are flagged for review

### Suppressing Findings

Findings can be suppressed with `//nolint:fieldguard // rationale`:

```go
// Initializing in constructor (immutable exception)
m.cfg = cfg  //nolint:fieldguard // initialization in New()
```

## Testing Patterns

### Race Detector

Always run concurrent tests with `-race`:

```bash
go test -race -count=1 ./internal/cluster/ ./internal/queue/
```

### Table-Driven Concurrency Tests

```go
func TestConcurrentAccess(t *testing.T) {
    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(2)
        go func(id int) {
            defer wg.Done()
            // concurrent read/write
        }(i)
        go func(id int) {
            defer wg.Done()
            // concurrent read/write
        }(i)
    }
    wg.Wait()
}
```
