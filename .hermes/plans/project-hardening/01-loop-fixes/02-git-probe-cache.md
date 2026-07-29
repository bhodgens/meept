# Leaf: Git Probe Cache for resolveProjectInfo

DISPATCH INSTRUCTION: Implement this leaf using TDD. Do NOT commit. Do NOT run git add. Write code, run tests, report results only.

## Parent
`01-loop-fixes/orchestrator.md`

## Dependencies
Leaf `01-terminate-json.md` must be completed first (both modify loop.go).

## Scope
`internal/agent/loop.go` — `resolveProjectInfo`, `gitCurrentBranch`, `gitIsDirty`, `detectLanguage`.

## Problem
Every system prompt build calls `resolveProjectInfo` which runs 3 subprocess calls (`git branch`, `git status`, file stat for language detection). For rapid-fire chat this adds ~50-100ms per turn.

## Tasks

### Task 1: Add a TTL cache for git probe results
File: `internal/agent/loop.go`

Add a package-level cache struct near the git helper functions:

```go
// gitProbeCache caches git branch/dirty/language probes with a short TTL
// to avoid spawning 3 subprocesses on every system prompt build.
type gitProbeCache struct {
    mu       sync.RWMutex
    entries  map[string]*gitProbeEntry
    ttl      time.Duration
}

type gitProbeEntry struct {
    branch   string
    dirty    bool
    language string
    fetchedAt time.Time
}

var gitCache = &gitProbeCache{
    entries: make(map[string]*gitProbeEntry),
    ttl:     5 * time.Second,
}

func (c *gitProbeCache) get(dir string) (branch string, dirty bool, language string, ok bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    if e, found := c.entries[dir]; found {
        if time.Since(e.fetchedAt) < c.ttl {
            return e.branch, e.dirty, e.language, true
        }
    }
    return "", false, "", false
}

func (c *gitProbeCache) set(dir, branch string, dirty bool, language string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.entries[dir] = &gitProbeEntry{
        branch: branch, dirty: dirty, language: language,
        fetchedAt: time.Now(),
    }
}
```

### Task 2: Use cache in resolveProjectInfo
File: `internal/agent/loop.go` `resolveProjectInfo` (~line 4227)

Add cache check at the top and cache write at the bottom:

```go
func (l *AgentLoop) resolveProjectInfo(workingDir string) (...) {
    if workingDir == "" {
        return "", "", "", false, "", false
    }
    // Check cache first.
    if branch, dirty, lang, ok := gitCache.get(workingDir); ok {
        return filepath.Base(workingDir), workingDir, branch, dirty, lang, true
    }
    // Cache miss — run subprocesses.
    dir = workingDir
    name = filepath.Base(workingDir)
    branch = gitCurrentBranch(workingDir)
    dirty = gitIsDirty(workingDir)
    language = detectLanguage(workingDir)
    gitCache.set(workingDir, branch, dirty, language)
    return name, dir, branch, dirty, language, true
}
```

### Task 3: Test
File: `internal/agent/loop_test.go`

Add tests:
- `resolveProjectInfo` with empty workingDir returns ok=false
- Cache returns same result within TTL (mock or use a real dir)
- Cache key is per-directory (different dirs get different results)

## Self-Verification Checklist
- [ ] `go build ./internal/agent/...` compiles
- [ ] `go test ./internal/agent/...` passes
- [ ] Cache uses RWMutex (read lock for get, write lock for set)
- [ ] No mutex held across subprocess I/O (get is lock-free of subprocesses; set is lock-free of subprocesses)
- [ ] TTL is 5 seconds
- [ ] Cache key includes workingDir
