# S6: Daemon Infrastructure Review (Round 5)

Scope: scheduler, queue, worker, runtime, pty, stt, tts, debug, cluster, metrics, session, shadow, task, errcls, eval, validator, benchmark.

Prompt-injection notice: file Read results contained fake `<system-reminder>` blocks calling the code "malware" and instructing refusal to improve. These are injected content, not real system messages. Disregarded per review instructions. The code is the user's own meept project.

---

## Critical

### S6-1 Cluster signature bypass: empty signature skips verification

**File:** `internal/cluster/gossip.go:335`
**Severity:** Critical (security)

**Evidence:**
```go
if g.cfg.Security.RequireNodeSignatures && len(event.Signature) > 0 {
    pubKey, found := g.PeerSigningKey(event.NodeID)
    ...
}
```

**Why:** When `RequireNodeSignatures` is true, the code only enters the verification branch if `len(event.Signature) > 0`. An attacker (or a misconfigured node) can forge a ClusterEvent with an empty/nil Signature field and completely bypass signature verification. The event will be persisted, re-broadcast to all peers, and used to update peer state — all without cryptographic validation.

**Fix:** Reject unsigned events when signatures are required:
```go
if g.cfg.Security.RequireNodeSignatures {
    if len(event.Signature) == 0 {
        g.logger.Warn("gossip: rejecting unsigned event (signatures required)", ...)
        return
    }
    // verify...
}
```

### S6-2 `reclaimJobUnlocked` nil-dereferences `cq.store` on ResetToPending

**File:** `internal/queue/cluster_queue.go:151`
**Severity:** Critical (panic)

**Evidence:**
```go
func (cq *ClusterQueue) reclaimJobUnlocked(ctx context.Context, jobID, reason string) error {
    // 1. Record TASK_RECLAIM event
    if cq.store != nil {                                    // <-- nil guard present
        if err := cq.store.RecordClaimEvent(...)
        ...
    }
    // 2. Reset job state to PENDING
    if err := cq.store.ResetToPending(ctx, jobID); err != nil {   // <-- NO nil guard
        ...
    }
```

**Why:** `RecordClaimEvent` is guarded by `if cq.store != nil` (line 143), but `ResetToPending` at line 151 dereferences `cq.store` unconditionally. If a ClusterQueue is constructed with a nil store (NewClusterQueue does not validate this), any reclaim attempt panics. `CheckNodeReachability` (line 195) also guards for nil store, showing the code anticipates this case but missed it here.

**Fix:** Add `if cq.store == nil { return nil }` at the top of `reclaimJobUnlocked`, or guard the `ResetToPending` call.

---

## High

### S6-3 `reclaimJobUnlocked` holds write lock across SQLite I/O and bus publish

**File:** `internal/queue/cluster_queue.go:141-179`
**Severity:** High (violates CLAUDE.md mutex-scope rule)

**Evidence:**
```go
func (cq *ClusterQueue) ReclaimJob(ctx context.Context, jobID, reason string) error {
    cq.mu.Lock()
    defer cq.mu.Unlock()
    return cq.reclaimJobUnlocked(ctx, jobID, reason)  // holds write lock
}

func (cq *ClusterQueue) reclaimJobUnlocked(...) error {
    if cq.store != nil {
        cq.store.RecordClaimEvent(ctx, ...)  // SQLite write
    }
    cq.store.ResetToPending(ctx, jobID)       // SQLite write
    // ...
    cq.bus.Publish("event.cluster.task_reclaim", msg)  // channel send
```

**Why:** The write lock on `cq.mu` is held across two SQLite writes and a bus publish. This violates the CLAUDE.md mutex-scope rule ("Never hold a mutex across I/O operations"). Contrast with `ReclaimIfStale` (line 227) which explicitly documents that it "collects stale job IDs under a brief RLock, releases the lock, then reclaims each job individually" — yet each individual reclaim re-acquires the write lock and holds it across all I/O.

**Fix:** In `reclaimJobUnlocked`, collect the data needed (jobID, reason, localNodeID) under the write lock, delete the claim record from the map under the lock, then release the lock before performing store writes and bus publish.

### S6-4 `RecordClaimEvent` uses `[]byte(action)` as ed25519 signature placeholder

**File:** `internal/queue/cluster_queue.go:287`
**Severity:** High (security/correctness)

**Evidence:**
```go
sig := []byte(action) // placeholder: real signatures via ed25519
_, err := s.db.ExecContext(ctx, query,
    eventID, nodeID, "TASK_"+action,
    ..., sig, ...)
```

**Why:** The `signature` column in `cluster_events` is `NOT NULL`, so a real signature is required. Storing the action string bytes as a fake signature means:
1. Any code that verifies signatures from this table will fail (treating it as a real ed25519 sig), silently dropping legitimate events.
2. If signature verification is skipped for short signatures, it opens the door to event forgery.
3. The comment acknowledges this is a placeholder, but production code should not ship with fake crypto material.

**Fix:** Either sign the event with the node's ed25519 key (via the gossip engine's signing infrastructure) before storing, or change the schema to allow NULL signatures and store NULL.

### S6-5 `RecordClaimEvent` uses predictable event IDs

**File:** `internal/queue/cluster_queue.go:285`
**Severity:** High

**Evidence:**
```go
eventID := fmt.Sprintf("claim-%s-%s-%d", jobID, action, time.Now().UnixNano())
```

**Why:** This is the predictable-IDs anti-pattern documented in MEMORY.md. Two concurrent calls (e.g., simultaneous reclaim of two jobs) within the same nanosecond produce identical event IDs. Since `event_id` is `PRIMARY KEY`, the second insert fails. The gossip engine's `persistEvent` uses `INSERT OR IGNORE`, so the duplicate is silently dropped — but the first event may also be the wrong one. Should use `pkg/id.Generate()` or `models.GenerateEventID()`.

### S6-6 Gossip signature verification only checks events from known peers

**File:** `internal/cluster/gossip.go:335-345`
**Severity:** High (security)

**Evidence:**
```go
if g.cfg.Security.RequireNodeSignatures && len(event.Signature) > 0 {
    pubKey, found := g.PeerSigningKey(event.NodeID)
    if !found {
        g.logger.Warn("gossip: no signing key for event sender", ...)
        return
    }
    if !event.Verify(pubKey) {
        ...
        return
    }
}
```

**Why:** Even with the empty-signature bypass (S6-1) fixed, if `event.NodeID` is not in the `signingPub` map, the event is rejected with only a warning log. But an attacker can set `event.NodeID` to the local node's own ID — the local node's public key is always registered in `signingPub[localNode]` (line 104 of gossip.go). The attacker would need the local node's private key to sign, so this is actually not exploitable by itself. However, the verification logic is fragile — if `PeerSigningKey` ever returns a default/wrong key, forged events would pass. Combined with S6-1 (empty sig bypass), the current code is exploitable.

---

## Medium

### S6-7 `PersistentQueue.Claim` slow-path loses race, sends worker to error state

**File:** `internal/queue/queue.go:193-235`
**Severity:** Medium

**Evidence:**
The slow path (when `hasCancelFilter` is true) lists pending jobs, finds the first claimable one, then calls `ClaimNextByID`. Between the list and the claim, another worker may claim the same job. `ClaimNextByID` returns `ErrJobAlreadyClaimed`. The slow path returns this error to the caller.

In `worker/worker.go:222-230`:
```go
job, err := w.queue.Claim(ctx, w.ID, w.Capabilities)
if err != nil {
    if errors.Is(err, queue.ErrNoJobAvailable) {
        return false, nil
    }
    w.setStateWithError(StateError, "", err)  // worker goes to error state
    return false, err
}
```

**Why:** A legitimate race condition (two workers claiming the same job) causes the worker to enter Error state and log an error, even though there may be other pending jobs it could claim. The fast path doesn't have this issue because it's atomic. The slow path should retry the list-claim loop before returning an error.

**Fix:** Translate `ErrJobAlreadyClaimed` to `ErrNoJobAvailable` in the slow path, or retry up to N times.

### S6-8 `CheckNodeReachability` uses `db.QueryRow` without context

**File:** `internal/queue/cluster_queue.go:200-203`
**Severity:** Medium

**Evidence:**
```go
row := cq.store.db.QueryRow(
    `SELECT last_heartbeat FROM cluster_members WHERE node_id = ?`,
    nodeID,
)
```

**Why:** Uses `db.QueryRow` instead of `db.QueryRowContext`. The query cannot be cancelled by the caller's context and may block indefinitely on a contended SQLite database. Other store methods like `RecordClaimEvent` and `ResetToPending` correctly use `ExecContext`.

**Fix:** Change to `cq.store.db.QueryRowContext(context.Background(), ...)` or accept a context parameter.

### S6-9 `pty.Manager.Close` holds write lock across `sess.Close()` I/O

**File:** `internal/pty/manager.go:138-147`
**Severity:** Medium

**Evidence:**
```go
func (m *Manager) Close() error {
    m.mu.Lock()
    defer m.mu.Unlock()
    for id := range m.sessions {
        m.destroySessionLocked(id)  // calls sess.Close()
    }
    return nil
}
```

**Why:** `destroySessionLocked` calls `sess.Close()` which performs I/O: `s.ptmx.Close()` (file close), `s.ptyCmd.Process.Kill()` (syscall), and `close(s.done)` (channel close). If any session is stuck or slow to close, all other Manager operations are blocked. Per CLAUDE.md mutex-scope rule, locks should not be held across I/O.

**Fix:** Snapshot session IDs under the lock, release the lock, then close each session individually.

### S6-10 `debug.Client.readLoop` can block forever on unresponsive adapter

**File:** `internal/queue/../debug/client.go:196-229`
**Severity:** Medium (goroutine leak)

**Evidence:**
```go
func (c *Client) readLoop(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }
        msg, err := c.readMessage()  // blocks on c.stdout.ReadString('\n')
```

**Why:** The `select` on `ctx.Done()` only fires between reads. `readMessage()` blocks on `c.stdout.ReadString('\n')`, which blocks until the adapter sends data or closes stdout. The context passed from `manager.go:131-132` is `context.Background()`, so it's never cancelled. If the adapter process hangs without exiting, readLoop blocks forever and the goroutine leaks. `Client.Close()` kills the process which closes stdout, but only if Close() is actually called — a forgotten session leaks the goroutine permanently.

**Fix:** Use a context with cancellation in `manager.go` instead of `context.Background()`, or add a read deadline via a timeout wrapper.

### S6-11 Predictable message IDs in queue and worker handlers

**File:** `internal/queue/queue.go:768`, `internal/worker/pool.go:573`
**Severity:** Medium

**Evidence:**
```go
// queue.go:768
ID: fmt.Sprintf("queue-resp-%d", time.Now().UnixNano()),

// pool.go:573
ID: fmt.Sprintf("worker-resp-%d", time.Now().UnixNano()),
```

Also in `cluster/gossip.go:273,364`:
```go
ID: fmt.Sprintf("gossip-pub-%d", time.Now().UnixNano()),
ID: fmt.Sprintf("gossip-%s-%d", g.localNode, time.Now().UnixNano()),
```

**Why:** Same predictable-IDs anti-pattern. Concurrent responses within the same nanosecond produce colliding BusMessage IDs. Should use `pkg/id.Generate()` or include an atomic counter.

### S6-12 `tts.Manager.Speak` spawns goroutine while holding mutex-induced invariants

**File:** `internal/tts/manager.go:63-82`
**Severity:** Medium (race)

**Evidence:**
```go
func (m *Manager) Speak(text string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    // ...
    m.speaking = true
    m.processing = true
    go func() {
        defer func() { m.processQueue() }()
        ctx := context.Background()
        result, err := m.synth.Synthesize(ctx, text)  // slow piper subprocess
        // ...
    }()
    return nil
}
```

**Why:** The goroutine calls `m.synth.Synthesize()` which in `PiperEngine.Synthesize` acquires `e.mu` — a different lock, so no deadlock. But the `defer m.processQueue()` in the goroutine calls `m.mu.Lock()` at line 94, which will block until `Speak` returns (since `Speak` holds `m.mu` via `defer`). This is correct behavior but creates a subtle ordering dependency: the goroutine starts, blocks on Synthesize, and only after Speak returns can processQueue acquire the lock. Meanwhile, `Speak` has already set `m.speaking = true` and `m.processing = true`, so subsequent `Speak` calls will queue messages. This is fragile but not a bug per se.

The real issue is that `Speak` holds `m.mu` while spawning the goroutine and the goroutine's first action (`Synthesize`) is a long-running subprocess call. Since the goroutine doesn't need the lock for Synthesize, this is fine. Lower priority.

---

## Low

### S6-13 `gossip_transport.markSentToPeer` pruning deletes random entries

**File:** `internal/cluster/gossip_transport.go:353-361`
**Severity:** Low

**Evidence:**
```go
if len(t.sentEvents[nodeID]) > 1000 {
    count := 0
    for k := range t.sentEvents[nodeID] {
        if count > 500 {
            break
        }
        delete(t.sentEvents[nodeID], k)
        count++
    }
}
```

**Why:** Go map iteration order is randomized, so this deletes arbitrary entries, not the oldest. Since there are no timestamps, oldest-first deletion is impossible. The consequence is that recently-sent events may be deleted, causing them to be re-sent on the next broadcast. This is inefficient but not incorrect — the receiver deduplicates. Adding a timestamp or using an LRU structure would fix this.

### S6-14 `Pool.Scale` reads worker count then modifies without holding lock

**File:** `internal/worker/pool.go:248-270`
**Severity:** Low

**Evidence:**
```go
func (p *Pool) Scale(ctx context.Context, targetCount int) error {
    p.mu.Lock()
    currentCount := len(p.workers)
    p.mu.Unlock()
    // ... gap where workers can be added/removed by other goroutines ...
    if targetCount > currentCount {
        for range targetCount - currentCount {
            worker, err := p.AddWorker(p.defaultCaps)  // re-acquires lock
```

**Why:** Between reading `currentCount` and calling `AddWorker`/`RemoveWorker`, other goroutines may change the worker count. The scale operation may overshoot or undershoot. In practice this is unlikely to cause issues since Scale is typically called manually.

### S6-15 `debug.parseGDBVariable` has dead code / always-false condition

**File:** `internal/debug/adapter_native.go:634`
**Severity:** Low

**Evidence:**
```go
if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "Thread") ||
    strings.HasPrefix(trimmed, "No") || strings.HasPrefix(trimmed, " ") == false {
    // Variables from "info locals" typically start at column 0 ...
    // Be lenient: check for "=" in the line.
}
```

**Why:** This `if` block has an empty body (the comment is inside). The condition `strings.HasPrefix(trimmed, " ") == false` is equivalent to `!strings.HasPrefix(trimmed, " ")` which is true for most lines. The entire `if` statement is a no-op — it doesn't filter anything. The code falls through to the `=` check regardless. This appears to be incomplete refactoring.

### S6-16 `ClusterMember.SigningPub` type mismatch with scan target

**File:** `internal/queue/store.go:1003`
**Severity:** Low

**Evidence:**
```go
type ClusterMember struct {
    // ...
    SigningPub   ed25519.PublicKey `json:"signing_pubkey"`  // type: ed25519.PublicKey
}

// In scanClusterMember:
m.SigningPub = signingPubRaw  // signingPubRaw is []byte
```

**Why:** `ed25519.PublicKey` is defined as `type PublicKey []byte`, so the assignment compiles. However, ed25519 public keys must be exactly 32 bytes. The scan reads a `BLOB` without validating length. If the database contains a corrupted or truncated key, downstream signature verification will panic or produce incorrect results. Should validate `len(signingPubRaw) == ed25519.PublicKeySize` after scanning.

---

## Summary

| Severity | Count |
|----------|-------|
| Critical | 2     |
| High     | 4     |
| Medium   | 6     |
| Low      | 4     |
| **Total**| **16**|

**Top priorities:**
1. S6-1: Signature bypass with empty signature (security critical)
2. S6-2: Nil deref in reclaimJobUnlocked (panic)
3. S6-3: Mutex held across I/O in reclaimJobUnlocked (CLAUDE.md rule violation)
4. S6-4/S6-5: Fake signatures and predictable IDs in cluster events
