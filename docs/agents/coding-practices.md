# Agent Invariants: Coding Practices

Cross-cutting coding invariants for the meept repo. Referenced from the root
AGENTS.md; the full rule prose moved here from the root file to keep the root
context file under the auto-load truncation cap. Update both in the same
commit per the root maintenance rule.

## Typed-nil interface guard

Nil `*ConcreteType` assigned to an interface produces a non-nil interface that
panics on method calls. Guard at call sites and in `With*` functions:

```go
if tokenCache != nil {
    opts = append(opts, WithTokenCache(tokenCache))
}
```

## Setter methods

Every `Set*` method MUST include a nil guard. Verified by
`internal/tools/builtin/setters_test.go`:

```go
func (t *SomeTool) SetFenceChecker(fc FenceChecker) {
    if fc != nil {
        t.fenceChecker = fc
    }
}
```

## Mutex scope

Never hold a mutex across I/O operations. Use "collect under lock, release,
then operate":

```go
mu.Lock()
cfg := m.config  // snapshot
mu.Unlock()
result, err := doNetworkCall(ctx, cfg)  // I/O outside lock
```

When the collect-then-operate pattern spans an IIFE or closure boundary, the
`mutexio` static analyzer cannot see the scope separation and will flag it as
a false positive. Suppress with a `//nolint:mutexio` directive that explains
why the call is outside the lock scope:

```go
var stale *Resource
func() {
    mu.Lock()
    defer mu.Unlock()
    stale = m.resource  // collect under lock
    delete(m.resources, id)
}()

// Lock released by IIFE above; safe to do I/O here.
if stale != nil {
    stale.Close() //nolint:mutexio // collected outside IIFE lock scope
}
```

## Error handling

Pre-commit hooks block commits that introduce new `_ = someFunc()` ignored-error
sites or bare `panic(err)`. Always handle errors:

```go
if err != nil {
    return fmt.Errorf("context: %w", err)
}
```

Type assertions on `map[string]any` values (common in bus payloads) must use
the two-value form:

```go
if convID, ok := payload["conversation_id"].(string); ok {
    // use convID
}
```

## Prefer Clean Architectural Fixes

**Always prefer clean architectural fixes over hacky workarounds, even when the
clean fix requires more work.** Hacky workarounds accumulate technical debt and
create fragile systems that are hard to reason about.

Examples of hacky patterns to avoid:
- **Content comparison for deduplication** — comparing serialized content
  instead of using proper identity keys (`id`, `session_id`, hash of source).
- **Suppressing symptoms** — catching/swallowing an error to make a test pass
  without understanding why the error occurs.
- **Patching around a root cause** — adding a special case downstream instead
  of fixing the upstream producer of bad data.

**When fixing a bug:**

1. **Trace the root cause** through the full data flow — from the observed
   symptom back to its origin. Do not stop at the first place you *could* patch.
2. **Fix it at the source** — change the code that produces the incorrect
   behavior, not the code that merely reacts to it.
3. **If a workaround is temporarily necessary**, add a `TODO` comment with the
   ticket/reference and a concrete plan for the proper fix:

   ```go
   // TODO(subagent-1234): Temporary dedup by content string. Replace with
   // proper id-based dedup once Dispatcher emits stable task IDs.
   if seen[task.Payload] { continue }
   ```

**Never ship a workaround as the final solution without explicit user
approval.** If the clean fix is too large for the current change, say so, get
approval for the temporary measure, and record the follow-up work as an issue
or TODO.
