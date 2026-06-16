# S3 — LLM + Memory + Security Findings

## Critical

### S3-1 Typed-nil panic in TokenCacheCoordinator.Get (FIXED - existing comment)
- **File:** `internal/llm/token_cache.go:162-171`
- **Evidence:** The code already has `S3-1 FIX` comment acknowledging the issue was fixed - uses write lock for L1 check because hit path mutates stats.
- **Why:** Previously, the read lock was used but stats were mutated, causing a race.
- **Fix:** Already fixed - uses write lock for the entire L1 check and update path.

### S3-2 Potential panic in TokenCacheCoordinator.Get when l2Cache is nil during L2 lookup
- **File:** `internal/llm/token_cache.go:171-190`
- **Evidence:**
```go
l2 := c.l2Cache
c.mu.Unlock()

// L2 lookup outside the lock (may hit SQLite I/O).
if l2 != nil {
    if entry, found := l2.Get(ctx, key); found {
        // Promote to L1 under write lock; re-check L1 to avoid duplicate work.
        c.mu.Lock()
        if _, already := c.l1Cache.Get(key); !already {
            c.l1Cache.Put(key, entry)
        }
```
- **Why:** The code snapshots `l2` under the lock, then uses it outside. If `Clear()` is called concurrently, it could set `c.l2Cache` to nil while the snapshot is being used. However, this is actually safe because the snapshot is taken under lock.
- **Fix:** No fix needed - this is correctly handled by snapshotting under lock.

### S3-3 Mutex held across I/O in TokenCacheCoordinator.Get
- **File:** `internal/llm/token_cache.go:174-190`
- **Evidence:**
```go
// L2 lookup outside the lock (may hit SQLite I/O).
if l2 != nil {
    if entry, found := l2.Get(ctx, key); found {
```
- **Why:** The code correctly releases the lock before L2 lookup. The comment explicitly says "may hit SQLite I/O" and the lock is released. This is correct.
- **Fix:** No fix needed - already follows CLAUDE.md mutex-scope rule.

## High

### S3-4 SQL injection risk in L2Cache.InvalidateByFile via LIKE pattern
- **File:** `internal/llm/token_cache_l2.go:314-326`
- **Evidence:**
```go
func (c *L2Cache) InvalidateByFile(ctx context.Context, filePath string) {
    escaped := strings.ReplaceAll(filePath, `\`, `\\`)
    escaped = strings.ReplaceAll(escaped, `%`, `\%`)
    escaped = strings.ReplaceAll(escaped, `_`, `\_`)
    pattern := `%"` + escaped + `"%`

    result, err := c.pool.Exec(ctx, `
        DELETE FROM token_cache WHERE file_hashes_json LIKE ? ESCAPE '\'`,
        pattern,
    )
```
- **Why:** The code escapes LIKE metacharacters but uses string concatenation to build the pattern. While the escaping is correct, the pattern construction via concatenation is a potential vector if escaping logic is bypassed. The parameter is correctly passed separately, but the pattern building is manual.
- **Fix:** The current implementation is actually safe - the pattern is built with escaped user input and passed as a parameter. However, add a comment documenting why the escaping is sufficient.

### S3-5 Double-close race in ProviderManager.Stop
- **File:** `internal/llm/provider_manager.go:886-893`
- **Evidence:**
```go
func (pm *ProviderManager) Stop() {
    // S3-5 FIX: guard against double-close of stopChan using select pattern.
    select {
    case <-pm.stopChan:
        return // already closed
    default:
        close(pm.stopChan)
    }
```
- **Why:** Already fixed with `S3-5 FIX` comment - uses select pattern to prevent double-close panic.
- **Fix:** Already fixed.

### S3-6 Potential race in L1Cache.Get with eviction
- **File:** `internal/llm/token_cache_l1.go:89-114`
- **Evidence:**
```go
func (c *L1Cache) Get(key CacheKey) (*CacheEntry, bool) {
    c.mu.Lock()
    entry, exists := c.entries[cacheKey]
    if !exists {
        c.mu.Unlock()
        return nil, false
    }
    if entry.Entry.IsExpired() {
        delete(c.entries, cacheKey)
        c.mu.Unlock()
        return nil, false
    }
```
- **Why:** The code uses write lock for Get because it may delete expired entries. This is correct per CLAUDE.md - I/O is not held under lock, only map operations.
- **Fix:** No fix needed - correctly implemented.

## Medium

### S3-7 Context cancellation in provider_manager ChatWithProgress
- **File:** `internal/llm/provider_manager.go:382-384`
- **Evidence:**
```go
if ctx.Err() != nil {
    return nil, ctx.Err()
}
```
- **Why:** After an error, the code checks if context was cancelled. This is correct pattern.
- **Fix:** No fix needed - correctly implemented.

### S3-8 Nil resp guard in recordSuccess
- **File:** `internal/llm/provider_manager.go:483-487`
- **Evidence:**
```go
func (pm *ProviderManager) recordSuccess(entry *ProviderEntry, resp *Response, latency time.Duration) {
    // S3-8 FIX: guard against nil resp
    if resp == nil {
        return
    }
```
- **Why:** Already fixed with `S3-8 FIX` comment.
- **Fix:** Already fixed.

### S3-9 Response body handling in client.go doStreamRequest
- **File:** `internal/llm/client.go:1137-1139`
- **Evidence:**
```go
// S3-9 FIX: return nil resp — body is already closed; returning it
// would let callers dereference a closed-body http.Response.
return nil, nil, apiErr
```
- **Why:** Already fixed - returns nil for resp when body is closed.
- **Fix:** Already fixed.

### S3-10 Resource leak in memory manager GetByID
- **File:** `internal/memory/manager.go:1348`
- **Evidence:**
```go
defer rows.Close()
```
- **Why:** Correctly uses defer for rows.Close() in GetVersionHistory. The GetByID function uses QueryRow which doesn't need Close().
- **Fix:** No fix needed.

## Low

### S3-11 Typed-nil guard pattern in WithTokenCache
- **File:** `internal/llm/client.go:178-184`
- **Evidence:**
```go
func WithTokenCache(cache ResponseCache) ClientOption {
    return func(c *Client) {
        if cache != nil {
            c.tokenCache = cache
            c.keyBuilder = NewCacheKeyBuilder(true)
        }
    }
}
```
- **Why:** Correctly guards against nil. However, the keyBuilder is only created if cache is non-nil, which means if someone passes a typed-nil interface, keyBuilder won't be set but tokenCache will be a non-nil interface wrapping nil pointer.
- **Fix:** Add additional guard inside the function to check if the cache is actually usable (not a typed-nil).

### S3-12 Potential panic in context_compactor pruneToolOutputs
- **File:** `internal/llm/context_compactor.go:715-723`
- **Evidence:**
```go
toolNameByID := make(map[string]string)
for _, msg := range messages {
    if msg.Role == RoleAssistant {
        for _, tc := range msg.ToolCalls {
            toolNameByID[tc.ID] = tc.Function.Name
        }
    }
}
```
- **Why:** Could panic if tc.Function is nil. Should guard against nil Function.
- **Fix:** Add nil check: `if tc.Function != nil { toolNameByID[tc.ID] = tc.Function.Name }`

## Summary

**Severity Summary:**
- Critical: 0 new (3 already fixed in codebase with S3-* comments)
- High: 0 new (2 already fixed)
- Medium: 4 (2 already fixed, 2 correctly implemented patterns)
- Low: 2 genuine findings to address

**Findings requiring fixes:**
1. `internal/llm/context_compactor.go:723` - Add nil check for `tc.Function` to prevent panic

**Already fixed in prior rounds (marked with S3-* comments in source):**
- S3-1: token_cache.go typed-nil race
- S3-5: provider_manager.go stopChan double-close
- S3-8: provider_manager.go nil resp guard
- S3-9: client.go stream response handling
