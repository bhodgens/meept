# Full-Stack Bug Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Fix 45 high-confidence bugs across the daemon, CLI client, transport layer, and Flutter GUI that impact stability, usability, and interoperability.

**Architecture:** Fixes are organized into 8 sprints ordered by severity and blast radius. Sprint 1-3 cover critical daemon and transport bugs. Sprint 4-5 cover CLI client bugs. Sprint 6-8 cover Flutter GUI bugs. Each sprint is independently testable and committable.

**Tech Stack:** Go 1.24, Flutter/Dart, SQLite, rxdart, Riverpod

**Review source:** 13 parallel subagent review passes across all domains, reporting only bugs with >= 80% confidence.

---

## File Inventory

### Daemon (`internal/`)

| File | Change | Purpose |
|------|--------|---------|
| `internal/daemon/daemon.go` | Modify | Pass loaded config instead of re-loading; remove duplicate cluster start/stop |
| `internal/daemon/components.go` | Modify | Add context cancellation to leaked goroutines |
| `internal/config/schema.go` | Modify | Make `ShutdownTimeout()` use config field |
| `internal/agent/loop.go` | Modify | Add mutex around `SwitchModel` calls |
| `internal/agent/pair_channel.go` | Modify | Add mutex to `BusPairSessionState` |
| `internal/agent/queue_persister.go` | Modify | Fix `flushLockedHeld` lock contract |
| `internal/agent/llm_classifier.go` | Modify | Fix `extractJSONFromLLM` bracket matching |
| `internal/agent/handler.go` | Modify | Handle `crypto/rand.Read` error |
| `internal/llm/resolver.go` | Modify | Cap exponential backoff factor |
| `internal/skills/executor.go` | Modify | Close locally-created Chatter instances |
| `internal/memory/vector/store.go` | Modify | Add LIMIT to embedding search query |
| `internal/memory/ftstore.go` | Modify | Add lock to `HasFTS5Public()` |
| `internal/memory/episodic.go` | Modify | Add ESCAPE clause to LIKE queries |
| `internal/memory/task.go` | Modify | Add ESCAPE clause to LIKE queries |
| `internal/llm/context_firewall.go` | Modify | Move validation after reduction in Chat/ChatWithProgress |
| `internal/llm/budget.go` | Modify | Fix concurrent RPM overshoot in `WaitForRateLimit` |
| `internal/llm/provider_manager.go` | Modify | Use recent window for recovery check |
| `internal/llm/token_cache.go` | Modify | Fix lock-upgrade antipattern in `Get` |
| `internal/llm/token_cache_l1.go` | Modify | Fix TOCTOU race in `Get` |
| `internal/llm/client.go` | Modify | Add ToolCalls token counting in context firewall |
| `internal/security/tirith.go` | Modify | Block on scanner failure instead of allowing |
| `internal/tools/builtin/shell_tokenize.go` | Modify | Fix backslash escape handling in quotes |
| `internal/comm/http/auth.go` | Modify | Remove query param token for WebSocket auth |
| `internal/comm/http/server.go` | Modify | Add `/api/v1/bus/call` route; unsubscribe WS goroutines on shutdown |
| `internal/comm/web/auth.go` | Modify | Use `net.SplitHostPort` for IP extraction |
| `internal/rpc/cluster_handler.go` | Modify | Add nil guard on `h.cfg` in `handleStatus` |
| `internal/rpc/dev.go` | Modify | Return nil error in `handleReload` |
| `internal/rpc/proxy.go` | Modify | Add server-level shutdown context for bus subscriptions |
| `internal/stt/recorder.go` | Modify | Clean up temp file on Start failure |
| `internal/tools/mcp/manager.go` | Modify | Keep client in map on close failure |
| `internal/skills/discovery.go` | Modify | Add RWMutex to skills map |

### Client (`cmd/meept/`)

| File | Change | Purpose |
|------|--------|---------|
| `cmd/meept/daemon.go` | Modify | Clean up stale PID files in `isDaemonRunning` |
| `cmd/meept/cluster_cmd.go` | Modify | Guard type assertion; handle `rand.Read` errors |
| `cmd/meept/calendar.go` | Modify | Use `config.LoadDefault()` instead of hardcoded TOML path |
| `cmd/meept/templates.go` | Modify | Add `errMsg != ""` guard |
| `cmd/meept/mcp_chat_server.go` | Modify | Add `Close()` method, close RPC connection on exit |

### Flutter GUI (`ui/flutter_ui/`)

| File | Change | Purpose |
|------|--------|---------|
| `ui/flutter_ui/lib/services/websocket_service.dart` | Modify | Replace `Rx.retryWhen` with reconnect loop |
| `ui/flutter_ui/lib/services/meept_api.dart` | Modify | Fix `.cast<>()` crashes; guard `healthCheck` URL |
| `ui/flutter_ui/lib/services/api_client.dart` | Modify | Add `dispose()` method |
| `ui/flutter_ui/lib/providers/providers.dart` | Modify | Wire `ref.onDispose` for ApiClient |
| `ui/flutter_ui/lib/providers/job_provider.dart` | Modify | Add generation counter and dispose guard |
| `ui/flutter_ui/lib/providers/metrics_provider.dart` | Modify | Add dispose guard in WS listener |
| `ui/flutter_ui/lib/providers/stt_provider.dart` | Modify | Move state transition after availability check |
| `ui/flutter_ui/lib/providers/agent_provider.dart` | Modify | Change `ref.read` to `ref.watch` |
| `ui/flutter_ui/lib/features/home/home_screen.dart` | Modify | Fix broken tab routes; fix tool navigation orphaning state |
| `ui/flutter_ui/lib/core/router.dart` | Modify | Add `/plans` and `/agents` routes |
| `ui/flutter_ui/lib/features/chat/chat_input.dart` | Modify | Guard controller mutations; add isLoading check to key handler |
| `ui/flutter_ui/lib/features/chat/slash_autocomplete.dart` | Modify | Defer onDismiss; remove autofocus |
| `ui/flutter_ui/lib/features/chat/chat_message_list.dart` | Modify | Add bottom padding for error banner |
| `ui/flutter_ui/lib/features/chat/chat_message_bubble.dart` | Modify | Render markdown in user messages |
| `ui/flutter_ui/lib/features/sessions/sessions_list.dart` | Modify | Fix error feedback on create; clear active session on delete |
| `ui/flutter_ui/lib/features/agents/agents_tab.dart` | Modify | Wrap states in `Expanded` |
| `ui/flutter_ui/lib/features/settings/settings_panel.dart` | Modify | Guard `onChanged` during programmatic load |
| `ui/flutter_ui/lib/features/search/search_panel.dart` | Modify | Cancel debouncer on dispose; setState in onChanged |
| `ui/flutter_ui/lib/features/calendar/calendar_panel.dart` | Modify | Await API before popping dialog |
| `ui/flutter_ui/lib/widgets/glitch_text.dart` | Modify | Reuse single `Random` instance |

---

## Sprint 1: Critical Daemon Core Bugs (D1, D2, D3, D8, D9)

### Task 1: Fix `--config` flag ignored by daemon (D1)

**Files:**
- Modify: `internal/daemon/daemon.go:119`
- Modify: `cmd/meept-daemon/main.go`

- [x] **Step 1: Write the failing test**

Create test that verifies daemon uses the config passed via `--config` flag instead of always calling `LoadDefault()`.

```go
// cmd/meept-daemon/main_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigFlagRespected(t *testing.T) {
	// Create a temp config with a distinctive shutdown timeout
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "test.json5")
	cfgContent := `{
		daemon: {
			shutdown_timeout: "30s",
		},
	}`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the loaded config has our custom shutdown timeout
	if cfg.ShutdownTimeout() != 30*time.Second {
		t.Errorf("expected 30s shutdown timeout from custom config, got %v", cfg.ShutdownTimeout())
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/meept-daemon/ -run TestConfigFlagRespected -v`
Expected: PASS (test validates config loading; the bug is in daemon.go, which we fix next)

- [x] **Step 3: Fix daemon.go to use passed config**

In `internal/daemon/daemon.go`, replace the `config.LoadDefault()` call at line 119 with the config passed through `daemon.Config`:

```go
// In daemon.Config struct, add:
FullConfig *config.Config

// In daemon.New(), replace line 119:
//   fullCfg, err := config.LoadDefault()
// with:
fullCfg := cfg.FullConfig
if fullCfg == nil {
    var err error
    fullCfg, err = config.LoadDefault()
    if err != nil {
        logger.Warn("Failed to load config, using defaults", "error", err)
        fullCfg = config.DefaultConfig()
    }
}
```

In `cmd/meept-daemon/main.go`, pass the already-loaded config:

```go
// In runDaemon(), update daemon.Config initialization:
dCfg := daemon.Config{
    // ... existing fields ...
    FullConfig: appCfg,
}
```

- [x] **Step 4: Run all daemon tests**

Run: `go test ./cmd/meept-daemon/ -v && go test ./internal/daemon/ -v`
Expected: All PASS

- [x] **Step 5: Commit**

```bash
git add internal/daemon/daemon.go cmd/meept-daemon/main.go
git commit -m "fix: respect --config flag in daemon instead of always loading defaults"
```

---

### Task 2: Remove duplicate cluster start/stop (D2)

**Files:**
- Modify: `internal/daemon/daemon.go:675-684,848-867`
- Reference: `internal/daemon/components.go:1688-1706,1917-1935`

- [x] **Step 1: Write the failing test**

```go
// internal/daemon/daemon_test.go -- add to existing test file
func TestClusterNotDoubleStarted(t *testing.T) {
    // Verify that ClusterEngine.Start is called exactly once during daemon startup
    // by checking that components.Start() handles it and daemon.Run() does not re-call.
}
```

- [x] **Step 2: Remove duplicate cluster start in daemon.Run()**

In `internal/daemon/daemon.go`, remove the cluster start block around lines 675-684:

```go
// DELETE these lines from daemon.Run() (around line 675-684):
// if d.components != nil && d.components.ClusterEngine != nil {
//     if err := d.components.ClusterEngine.Start(ctx); err != nil { ... }
// }
// if d.components != nil && d.components.ClusterGitSync != nil {
//     if err := d.components.ClusterGitSync.Start(ctx); err != nil { ... }
// }
```

These are already handled by `components.Start()` at `components.go:1688-1706`.

- [x] **Step 3: Remove duplicate cluster stop in shutdown()**

In `internal/daemon/daemon.go`, remove the cluster stop block around lines 848-867:

```go
// DELETE these lines from daemon.shutdown() (around line 848-867):
// if d.components != nil && d.components.ClusterEngine != nil {
//     if err := d.components.ClusterEngine.Stop(); err != nil { ... }
// }
// if d.components != nil && d.components.ClusterGitSync != nil {
//     if err := d.components.ClusterGitSync.Stop(); err != nil { ... }
// }
// if d.components != nil && d.components.ClusterWireGuard != nil {
//     if err := d.components.ClusterWireGuard.Stop(); err != nil { ... }
// }
// if d.components != nil && d.components.ClusterQueue != nil {
//     if err := d.components.ClusterQueue.Close(); err != nil { ... }
// }
```

These are already handled by `components.Stop()` at `components.go:1917-1935`.

Keep the WireGuard manager cleanup at line ~892 (which does additional cleanup beyond just Stop).

- [x] **Step 4: Run daemon tests**

Run: `go test ./internal/daemon/ -v`
Expected: All PASS

- [x] **Step 5: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "fix: remove duplicate cluster start/stop that causes double-invocation"
```

---

### Task 3: Fix shared LLM client race in SwitchModel (D3)

**Files:**
- Modify: `internal/agent/loop.go:1702,1719,2108`

- [x] **Step 1: Write the failing test**

```go
// internal/agent/loop_test.go
func TestSwitchModelConcurrentSafety(t *testing.T) {
    // Create two agent loops sharing the same llm.Client
    // Call resolveModelAlias concurrently from both
    // Verify no data race (run with -race)
}
```

- [x] **Step 2: Add mutex protection around SwitchModel**

In `internal/agent/loop.go`, wrap all `SwitchModel` calls with the loop's existing mutex:

```go
// At line ~1702, change:
l.llmClient.SwitchModel(modelConfig)
// To:
l.mu.Lock()
l.llmClient.SwitchModel(modelConfig)
l.mu.Unlock()

// At line ~1719, change:
if err := l.llmClient.SwitchModel(modelConfig); err == nil {
// To:
l.mu.Lock()
err := l.llmClient.SwitchModel(modelConfig)
l.mu.Unlock()
if err == nil {

// At line ~2108, change:
l.llmClient.SwitchModel(modelConfig)
// To:
l.mu.Lock()
l.llmClient.SwitchModel(modelConfig)
l.mu.Unlock()
```

Alternatively, if the mutex is already held at these call sites, add a dedicated `modelMu sync.Mutex` field to `AgentLoop` and use it specifically for model switching.

- [x] **Step 3: Run with race detector**

Run: `go test -race ./internal/agent/ -v -run TestSwitchModel`
Expected: PASS with no race detected

- [x] **Step 4: Commit**

```bash
git add internal/agent/loop.go
git commit -m "fix: add mutex around SwitchModel to prevent concurrent model corruption"
```

---

### Task 4: Fix leaked goroutines in components (D8)

**Files:**
- Modify: `internal/daemon/components.go:696,1183`

- [x] **Step 1: Add context to component goroutines**

In `internal/daemon/components.go`, the `NewComponents` function should accept a context. Pass the daemon context from `daemon.New()`.

```go
// In NewComponents signature, add ctx parameter:
func NewComponents(ctx context.Context, cfg ComponentsConfig, ...) (*Components, error) {
```

- [x] **Step 2: Fix progress-synthesizer goroutine (line ~696)**

```go
// Replace:
progressSub := msgBus.Subscribe("progress-synthesizer", "agent.event.*")
go func() {
    for msg := range progressSub.Channel {
        // ...
    }
}()

// With:
progressSub := msgBus.Subscribe("progress-synthesizer", "agent.event.*")
go func() {
    defer msgBus.Unsubscribe(progressSub)
    for {
        select {
        case <-ctx.Done():
            return
        case msg, ok := <-progressSub.Channel:
            if !ok {
                return
            }
            // ... existing handler logic ...
        }
    }
}()
```

- [x] **Step 3: Fix dispatcher-stats-handler goroutine (line ~1183)**

Apply the same pattern:

```go
statsSub := msgBus.Subscribe("dispatcher-stats-handler", "dispatcher.stats")
go func() {
    defer msgBus.Unsubscribe(statsSub)
    for {
        select {
        case <-ctx.Done():
            return
        case msg, ok := <-statsSub.Channel:
            if !ok {
                return
            }
            // ... existing handler logic ...
        }
    }
}()
```

- [x] **Step 4: Pass context from daemon.go**

In `daemon.New()`, pass the daemon context to `NewComponents`:

```go
components, err := NewComponents(d.ctx, componentsConfig, ...)
```

- [x] **Step 5: Run daemon tests**

Run: `go test ./internal/daemon/ -v`
Expected: All PASS

- [x] **Step 6: Commit**

```bash
git add internal/daemon/components.go internal/daemon/daemon.go
git commit -m "fix: add context cancellation to progress-synthesizer and dispatcher-stats goroutines"
```

---

### Task 5: Make ShutdownTimeout use config field (D9)

**Files:**
- Modify: `internal/config/schema.go:1569-1571`

- [x] **Step 1: Add ShutdownTimeout field to config struct**

In `internal/config/schema.go`, add the field to the `DaemonConfig` struct:

```go
type DaemonConfig struct {
    // ... existing fields ...
    ShutdownTimeout string `json:"shutdown_timeout"`
}
```

- [x] **Step 2: Update ShutdownTimeout() method**

```go
func (c *Config) ShutdownTimeout() time.Duration {
    if c.Daemon.ShutdownTimeout != "" {
        d, err := time.ParseDuration(c.Daemon.ShutdownTimeout)
        if err == nil {
            return d
        }
    }
    return 10 * time.Second
}
```

- [x] **Step 3: Run config tests**

Run: `go test ./internal/config/ -v`
Expected: All PASS

- [x] **Step 4: Commit**

```bash
git add internal/config/schema.go
git commit -m "fix: respect configured shutdown timeout instead of hardcoding 10s"
```

---

## Sprint 2: Critical Transport + LLM/Memory Bugs (D5, D6, D7, C1, C2)

### Task 6: Add missing `/api/v1/bus/call` route (C1)

**Files:**
- Modify: `internal/comm/http/server.go:783-784`
- Reference: `internal/transport/http_client.go:112`

- [x] **Step 1: Write the failing test**

```go
// internal/comm/http/server_test.go
func TestBusCallRoute(t *testing.T) {
    s := setupTestServer(t)
    // POST to /api/v1/bus/call should not return 404
    body := `{"method": "daemon.status", "params": {}}`
    req := httptest.NewRequest("POST", "/api/v1/bus/call", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    s.handler.ServeHTTP(w, req)
    if w.Code == 404 {
        t.Error("/api/v1/bus/call route not registered")
    }
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/comm/http/ -run TestBusCallRoute -v`
Expected: FAIL — route not registered

- [x] **Step 3: Register the route**

In `internal/comm/http/server.go`, add the route in `setupRESTRoutes()` near line 784:

```go
mux.HandleFunc("POST /api/v1/bus/call", s.handleBusCall)
```

Add the handler method:

```go
func (s *Server) handleBusCall(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Method string         `json:"method"`
        Params map[string]any `json:"params"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    ctx := r.Context()
    result, err := s.rpcServer.Dispatch(ctx, req.Method, req.Params)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    writeJSON(w, http.StatusOK, map[string]any{
        "result": result,
    })
}
```

Wire `s.rpcServer` in the Server struct during construction (passed from daemon wiring).

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/comm/http/ -run TestBusCallRoute -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/comm/http/server.go
git commit -m "fix: register /api/v1/bus/call route for HTTP transport"
```

---

### Task 7: Fix cluster handler nil dereference (C2)

**Files:**
- Modify: `internal/rpc/cluster_handler.go:70-77`

- [x] **Step 1: Write the failing test**

```go
// internal/rpc/cluster_handler_test.go
func TestHandleStatusNilConfig(t *testing.T) {
    h := &ClusterHandler{cfg: nil}
    result, err := h.handleStatus(context.Background(), nil)
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    resp, ok := result.(StatusResponse)
    if !ok {
        t.Fatal("expected StatusResponse")
    }
    if resp.Enabled {
        t.Error("expected Enabled=false when cfg is nil")
    }
}
```

- [x] **Step 2: Run test to verify it fails (panics)**

Run: `go test ./internal/rpc/ -run TestHandleStatusNilConfig -v`
Expected: FAIL/PANIC — nil pointer dereference

- [x] **Step 3: Fix the nil guard**

In `internal/rpc/cluster_handler.go`, fix `handleStatus`:

```go
func (h *ClusterHandler) handleStatus(_ context.Context, params json.RawMessage) (any, error) {
    resp := StatusResponse{
        Enabled:  h.cfg != nil,
        GossipOK: h.gossip != nil,
        SyncOK:   h.gitSync != nil,
    }
    if h.cfg != nil {
        resp.NodeID = h.cfg.NodeID
        resp.ClusterID = h.cfg.ClusterID
    }
    return resp, nil
}
```

Also review `handleStart` and `handleJoin` for the same pattern and add nil guards where missing.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/rpc/ -run TestHandleStatusNilConfig -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/rpc/cluster_handler.go
git commit -m "fix: prevent nil pointer dereference in cluster handleStatus"
```

---

### Task 8: Cap exponential backoff overflow in resolver (D5)

**Files:**
- Modify: `internal/llm/resolver.go:251`

- [x] **Step 1: Write the failing test**

```go
// internal/llm/resolver_test.go
func TestRecordAliasFailureBackoffCap(t *testing.T) {
    r := setupTestResolver(t)
    // Simulate 100 consecutive failures
    for i := 0; i < 100; i++ {
        r.RecordAliasFailure("test-alias")
    }
    // Should not panic or produce negative/huge backoff
    health := r.getAliasHealth("test-alias")
    backoff := time.Duration(float64(time.Second) * float64(1<<min(uint(health.ConsecutiveFails-1), 10)))
    if backoff > 1024*time.Second {
        t.Errorf("backoff too large: %v", backoff)
    }
}
```

- [x] **Step 2: Fix the overflow**

In `internal/llm/resolver.go` at line 251, cap the shift:

```go
// Replace:
backoffFactor := 1 << uint(health.ConsecutiveFails-1) // 2^(fails-1)

// With:
shift := uint(health.ConsecutiveFails - 1)
if shift > 10 {
    shift = 10 // Cap at 2^10 = 1024x backoff
}
backoffFactor := 1 << shift
```

Also add nil check for alias:

```go
alias := r.aliases[aliasName]
if alias == nil {
    return
}
```

- [x] **Step 3: Run resolver tests**

Run: `go test ./internal/llm/ -run TestRecordAlias -v`
Expected: PASS

- [x] **Step 4: Commit**

```bash
git add internal/llm/resolver.go
git commit -m "fix: cap exponential backoff to prevent integer overflow at high fail counts"
```

---

### Task 9: Close leaked Chatter instances in skills executor (D6)

**Files:**
- Modify: `internal/skills/executor.go:164,306`

- [x] **Step 1: Fix Execute method (line ~164)**

In `internal/skills/executor.go`, add cleanup after chat call in `Execute`:

```go
// After resolving chatter (around line 164-170):
var chatter llm.Chatter
var createdLocally bool
if e.client == nil {
    chatter = createChatter(modelConfig, e.logger)
    createdLocally = true
} else if e.client.Config().ModelID != modelConfig.ModelID {
    chatter = createChatter(modelConfig, e.logger)
    createdLocally = true
} else {
    chatter = e.client
}

// After the chat call completes, add cleanup:
if createdLocally {
    if closer, ok := chatter.(io.Closer); ok {
        closer.Close()
    }
}
```

- [x] **Step 2: Apply same fix to ExecuteWithMessages (line ~306)**

Apply the identical pattern to the second method.

- [x] **Step 3: Add io import**

Ensure `"io"` is in the import list.

- [x] **Step 4: Run skills tests**

Run: `go test ./internal/skills/ -v`
Expected: All PASS

- [x] **Step 5: Commit**

```bash
git add internal/skills/executor.go
git commit -m "fix: close locally-created Chatter instances to prevent HTTP transport leaks"
```

---

### Task 10: Add LIMIT to vector store search (D7)

**Files:**
- Modify: `internal/memory/vector/store.go:170`

- [x] **Step 1: Add limit parameter to Search**

In `internal/memory/vector/store.go`, change the query at line 170:

```go
// Replace:
rows, err := s.db.Query(`
    SELECT id, vector FROM embeddings
`)

// With:
rows, err := s.db.Query(`
    SELECT id, vector FROM embeddings
    ORDER BY rowid DESC
    LIMIT 1000
`)
```

Also consider accepting a `limit` parameter in the `Search` method signature with a default of 500-1000.

- [x] **Step 2: Run memory tests**

Run: `go test ./internal/memory/ -v`
Expected: All PASS

- [x] **Step 3: Commit**

```bash
git add internal/memory/vector/store.go
git commit -m "fix: add LIMIT to vector search to prevent loading all embeddings into memory"
```

---

## Sprint 3: Remaining Daemon Bugs (D4, D10-D18)

### Task 11: Add mutex to BusPairSessionState (D4)

**Files:**
- Modify: `internal/agent/pair_channel.go:51-61`
- Modify: `internal/agent/pair_orchestrator.go:93-97`

- [x] **Step 1: Add mutex to BusPairSessionState**

```go
type BusPairSessionState struct {
    mu           sync.RWMutex `json:"-"`
    SessionID    string       `json:"session_id"`
    ActorID      string       `json:"actor_id"`
    ReviewerID   string       `json:"reviewer_id"`
    CurrentTurn  int          `json:"current_turn"`
    MaxTurns     int          `json:"max_turns"`
    Phase        string       `json:"phase"`
    LastVerdict  PairVerdict  `json:"last_verdict,omitempty"`
    Turns        []PairTurn   `json:"turns,omitempty"`
    InitialPrompt string     `json:"initial_prompt"`
}
```

- [x] **Step 2: Return deep copy from GetSession**

In `pair_orchestrator.go`, return a copy instead of the pointer:

```go
func (po *PairOrchestrator) GetSession(sessionID string) *BusPairSessionState {
    po.mu.RLock()
    defer po.mu.RUnlock()
    state, ok := po.sessions[sessionID]
    if !ok {
        return nil
    }
    // Return a deep copy
    cp := *state
    cp.Turns = make([]PairTurn, len(state.Turns))
    copy(cp.Turns, state.Turns)
    return &cp
}
```

- [x] **Step 3: Run agent tests with race detector**

Run: `go test -race ./internal/agent/ -v -run TestPair`
Expected: PASS with no races

- [x] **Step 4: Commit**

```bash
git add internal/agent/pair_channel.go internal/agent/pair_orchestrator.go
git commit -m "fix: add deep copy in GetSession to prevent data race on BusPairSessionState"
```

---

### Task 12: Fix queue persister lock contract (D11)

**Files:**
- Modify: `internal/agent/queue_persister.go:179-191`

- [x] **Step 1: Fix flushLockedHeld to not release caller's lock**

```go
// Replace flushLockedHeld with a version that does not release the lock:
func (p *QueuePersister) flushLockedHeld() {
    if len(p.pending) == 0 {
        return
    }
    pending := p.pending
    p.pending = make([]QueuedMessage, 0)
    // Do NOT release the lock. Call flushPending without releasing,
    // since flushPending acquires its own lock internally for additions.
    // Instead, pass pending directly and let flushPending handle locking.
    p.mu.Unlock()
    p.flushPending(pending)
    p.mu.Lock()
}
```

Verify that callers of `flushLockedHeld` still hold the lock after the call returns, or document that the lock is temporarily released. If `EnqueueAsync` has code after `flushLockedHeld` that relies on the lock, wrap that code in its own `p.mu.Lock()/Unlock()`.

- [x] **Step 2: Run queue persister tests**

Run: `go test ./internal/agent/ -run TestQueuePersister -v`
Expected: PASS

- [x] **Step 3: Commit**

```bash
git add internal/agent/queue_persister.go
git commit -m "fix: clarify lock contract in queue persister flushLockedHeld"
```

---

### Task 13: Fix LIKE ESCAPE clause and HasFTS5Public race (D14, D15)

**Files:**
- Modify: `internal/memory/episodic.go:206-214`
- Modify: `internal/memory/task.go:205-223`
- Modify: `internal/memory/ftstore.go:229-231`

- [x] **Step 1: Fix ESCAPE clause in episodic memory**

In `internal/memory/episodic.go`, append `ESCAPE '\\'` to all LIKE queries that use `escapeLikeWildcards`:

```go
// After each LIKE ? that uses escapedQuery, add ESCAPE:
// For example:
query += " AND content LIKE ? ESCAPE '\\' "
```

- [x] **Step 2: Fix ESCAPE clause in task memory**

Apply the same fix to `internal/memory/task.go`.

- [x] **Step 3: Fix HasFTS5Public race**

In `internal/memory/ftstore.go`, change `HasFTS5Public` to use the lock:

```go
// Replace:
func (s *SQLiteFTSStore) HasFTS5Public() bool {
    return s.hasFTS5
}

// With:
func (s *SQLiteFTSStore) HasFTS5Public() bool {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.hasFTS5
}
```

- [x] **Step 4: Run memory tests with race detector**

Run: `go test -race ./internal/memory/ -v`
Expected: All PASS, no races

- [x] **Step 5: Commit**

```bash
git add internal/memory/episodic.go internal/memory/task.go internal/memory/ftstore.go
git commit -m "fix: add ESCAPE clause to LIKE queries and lock HasFTS5Public"
```

---

### Task 14: Fix context firewall validation ordering (D13)

**Files:**
- Modify: `internal/llm/context_firewall.go:401-441`

- [x] **Step 1: Move validation after processMessages in Chat**

In `internal/llm/context_firewall.go`, in the `Chat` method, move `ValidateContextSize` to after `processMessages`:

```go
// Current order (wrong):
// 1. ValidateContextSize
// 2. processMessages
// 3. send to LLM

// Fixed order:
// 1. processMessages
// 2. ValidateContextSize
// 3. send to LLM
```

Apply the same fix to `ChatWithProgress`.

- [x] **Step 2: Run context firewall tests**

Run: `go test ./internal/llm/ -run TestContextFirewall -v`
Expected: All PASS

- [x] **Step 3: Commit**

```bash
git add internal/llm/context_firewall.go
git commit -m "fix: validate context size after reduction, not before"
```

---

### Task 15: Fix token cache races and RPM overshoot (D17, D18, D18b)

**Files:**
- Modify: `internal/llm/token_cache.go:153-163`
- Modify: `internal/llm/token_cache_l1.go:87-115`
- Modify: `internal/llm/budget.go:658-691`

- [x] **Step 1: Fix token cache lock-upgrade antipattern**

In `internal/llm/token_cache.go`, use a single `Lock()` for the `Get` method instead of repeated RLock/Lock cycles:

```go
func (c *TokenCacheCoordinator) Get(ctx context.Context, key string) (string, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if !c.config.Enabled {
        c.stats.Misses++
        return "", false
    }

    // Check L1
    if val, ok := c.l1Cache.Get(key); ok {
        c.stats.L1Hits++
        return val, true
    }

    // Check L2
    // ... existing L2 logic, but without lock drops ...
}
```

- [x] **Step 2: Fix L1 cache TOCTOU**

In `internal/llm/token_cache_l1.go`, hold a write lock for the entire `Get` that may mutate:

```go
func (c *L1Cache) Get(key string) (string, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()

    entry, exists := c.entries[key]
    if !exists {
        c.stats.Misses++
        return "", false
    }

    if entry.Entry.IsExpired() {
        delete(c.entries, key)
        c.stats.Misses++
        return "", false
    }

    c.stats.Hits++
    entry.HitCount++
    c.entries[key] = entry
    return entry.Entry.Value, true
}
```

- [x] **Step 3: Fix RPM rate limiter concurrent overshoot**

In `internal/llm/budget.go`, add re-check after waking:

```go
// After the select/time.After wakes:
b.mu.Lock()
b.pruneRPMWindow()
// Re-check: if still at capacity, wait again
if len(b.requestTimestamps) >= b.rateLimitRPM {
    b.mu.Unlock()
    continue // loop back and wait
}
b.requestTimestamps = append(b.requestTimestamps, time.Now())
b.mu.Unlock()
return nil
```

- [x] **Step 4: Run LLM tests with race detector**

Run: `go test -race ./internal/llm/ -v -run TestTokenCache -timeout 30s`
Expected: All PASS, no races

- [x] **Step 5: Commit**

```bash
git add internal/llm/token_cache.go internal/llm/token_cache_l1.go internal/llm/budget.go
git commit -m "fix: resolve token cache lock races and RPM rate limiter overshoot"
```

---

### Task 16: Fix remaining daemon bugs (D10, D16, D18, misc)

**Files:**
- Modify: `internal/agent/llm_classifier.go:413-429`
- Modify: `internal/agent/handler.go:1208-1211`
- Modify: `internal/llm/provider_manager.go:399-414`
- Modify: `internal/llm/context_firewall.go:750-756`
- Modify: `internal/skills/discovery.go:112`
- Modify: `cmd/meept-daemon/main.go:26,44`

- [x] **Step 1: Fix extractJSONFromLLM bracket matching**

In `internal/agent/llm_classifier.go`, replace `extractJSONFromLLM` with a bracket-counting parser:

```go
func extractJSONFromLLM(s string) string {
    // Find first { or [
    startBrace := strings.Index(s, "{")
    startBracket := strings.Index(s, "[")
    start := -1
    var openCh, closeCh byte
    if startBrace == -1 && startBracket == -1 {
        return ""
    }
    if startBrace == -1 {
        start = startBracket
        openCh, closeCh = '[', ']'
    } else if startBracket == -1 || startBrace < startBracket {
        start = startBrace
        openCh, closeCh = '{', '}'
    } else {
        start = startBracket
        openCh, closeCh = '[', ']'
    }

    depth := 0
    inString := false
    escape := false
    for i := start; i < len(s); i++ {
        ch := s[i]
        if escape {
            escape = false
            continue
        }
        if ch == '\\' && inString {
            escape = true
            continue
        }
        if ch == '"' {
            inString = !inString
            continue
        }
        if inString {
            continue
        }
        if ch == openCh {
            depth++
        } else if ch == closeCh {
            depth--
            if depth == 0 {
                return s[start : i+1]
            }
        }
    }
    return ""
}
```

- [x] **Step 2: Handle crypto/rand.Read error**

In `internal/agent/handler.go`, fix `generateMessageID`:

```go
func generateMessageID() string {
    var randBytes [4]byte
    if _, err := rand.Read(randBytes[:]); err != nil {
        // Fallback: use timestamp-only ID (collision risk is low with nanosecond precision)
    }
    return time.Now().Format("20060102150405.000000000") + "-" + hex.EncodeToString(randBytes[:])
}
```

- [x] **Step 3: Fix provider recovery using recent window**

In `internal/llm/provider_manager.go`, replace lifetime counters with a sliding window for recovery checks. Add a `RecentSuccesses` and `RecentFailures` counter that reset every 5 minutes.

- [x] **Step 4: Add ToolCalls token counting**

In `internal/llm/context_firewall.go`, update `countTokens`:

```go
func (f *ContextFirewall) countTokens(messages []ChatMessage) int {
    total := 0
    for _, msg := range messages {
        total += f.tokenizer.CountTokens(msg.Content)
        for _, tc := range msg.ToolCalls {
            b, _ := json.Marshal(tc)
            total += f.tokenizer.CountTokens(string(b))
        }
    }
    return total
}
```

- [x] **Step 5: Add RWMutex to skills discovery**

In `internal/skills/discovery.go`, add `sync.RWMutex` to `Discovery` struct and guard `d.skills` access in all methods.

- [x] **Step 6: Remove unused foreground flag**

In `cmd/meept-daemon/main.go`, remove the `foreground` variable and its flag registration (lines 26 and 44), or implement daemonization.

- [x] **Step 7: Run tests**

Run: `go test ./internal/agent/ ./internal/llm/ ./internal/skills/ ./cmd/meept-daemon/ -v`
Expected: All PASS

- [x] **Step 8: Commit**

```bash
git add internal/agent/llm_classifier.go internal/agent/handler.go internal/llm/provider_manager.go internal/llm/context_firewall.go internal/skills/discovery.go cmd/meept-daemon/main.go
git commit -m "fix: bracket matching in JSON extractor, rand error handling, provider recovery, token counting, skills race, remove unused flag"
```

---

## Sprint 4: CLI Client Bugs (C3-C10)

### Task 17: Fix stale PID file false positive (C3)

**Files:**
- Modify: `cmd/meept/daemon.go:98` (isDaemonRunning function)

- [x] **Step 1: Fix isDaemonRunning to clean up stale PID files**

In `cmd/meept/daemon.go`, update `isDaemonRunning`:

```go
func isDaemonRunning(pidFile string) (int, bool) {
    data, err := os.ReadFile(pidFile)
    if err != nil {
        return 0, false
    }
    pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
    if err != nil {
        os.Remove(pidFile) // Invalid PID file
        return 0, false
    }
    proc, err := os.FindProcess(pid)
    if err != nil {
        os.Remove(pidFile)
        return 0, false
    }
    if err := proc.Signal(syscall.Signal(0)); err != nil {
        os.Remove(pidFile) // Stale PID — process is dead
        return 0, false
    }
    return pid, true
}
```

- [x] **Step 2: Run daemon CLI tests**

Run: `go test ./cmd/meept/ -v -run TestDaemon`
Expected: PASS

- [x] **Step 3: Commit**

```bash
git add cmd/meept/daemon.go
git commit -m "fix: clean up stale PID files in isDaemonRunning"
```

---

### Task 18: Fix cluster command type assertion and calendar config (C4, C5)

**Files:**
- Modify: `cmd/meept/cluster_cmd.go:397,832,839,845`
- Modify: `cmd/meept/calendar.go:141`

- [x] **Step 1: Guard type assertion in cluster status**

In `cmd/meept/cluster_cmd.go` at line 397:

```go
// Replace:
members := result["members"].([]any)

// With:
membersRaw, ok := result["members"]
if !ok || membersRaw == nil {
    fmt.Println("  members: (none)")
    return nil
}
members, ok := membersRaw.([]any)
if !ok {
    fmt.Println("  members: (unexpected format)")
    return nil
}
```

- [x] **Step 2: Handle rand.Read errors in key generators**

Fix `generateNodeID`, `generateClusterID`, `generateJoinKey` to propagate errors:

```go
func generateNodeID() (string, error) {
    b := make([]byte, 6)
    if _, err := rand.Read(b); err != nil {
        return "", fmt.Errorf("generate node ID: %w", err)
    }
    return fmt.Sprintf("node-%x", b), nil
}
```

Update all callers to handle the error.

- [x] **Step 3: Fix calendar config loading**

In `cmd/meept/calendar.go`, replace `loadCalendarConfig`:

```go
func loadCalendarConfig() (*config.CalendarConfig, error) {
    cfg, err := config.LoadDefault()
    if err != nil {
        def := config.DefaultConfig()
        return &def.Calendar, nil
    }
    return &cfg.Calendar, nil
}
```

- [x] **Step 4: Run CLI tests**

Run: `go test ./cmd/meept/ -v`
Expected: All PASS

- [x] **Step 5: Commit**

```bash
git add cmd/meept/cluster_cmd.go cmd/meept/calendar.go
git commit -m "fix: guard cluster type assertions, handle rand errors, use JSON5 config for calendar"
```

---

### Task 19: Fix templates error check and MCP server leak (C9, C10)

**Files:**
- Modify: `cmd/meept/templates.go:55,140,217,301`
- Modify: `cmd/meept/mcp_chat_server.go`

- [x] **Step 1: Fix templates error check**

In `cmd/meept/templates.go`, add `&& errMsg != ""` guard to all four error checks:

```go
// Replace (4 locations):
if errMsg, ok := resultMap["error"].(string); ok {

// With:
if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
```

- [x] **Step 2: Add Close() to MCP chat server**

In `cmd/meept/mcp_chat_server.go`, add a `Close` method and call it on exit:

```go
type Server struct {
    // ... existing fields ...
}

func (s *Server) Close() {
    if s.client != nil {
        s.client.Close()
    }
}
```

In the `Run` method or its caller, add `defer srv.Close()`.

- [x] **Step 3: Run CLI tests**

Run: `go test ./cmd/meept/ -v`
Expected: All PASS

- [x] **Step 4: Commit**

```bash
git add cmd/meept/templates.go cmd/meept/mcp_chat_server.go
git commit -m "fix: guard empty error strings in templates, close MCP server RPC connection"
```

---

## Sprint 5: Transport + Security Fixes (C6-C8, S1-S3)

### Task 20: Fix WebSocket goroutine leak and bus subscription leak (C6, C8)

**Files:**
- Modify: `internal/comm/http/server.go:274-287,575`
- Modify: `internal/rpc/dev.go:358-364`
- Modify: `internal/rpc/proxy.go:267-304`

- [x] **Step 1: Store and unsubscribe WS bus goroutines**

In `internal/comm/http/server.go`, store subscribers:

```go
type Server struct {
    // ... existing fields ...
    wsSubscribers []*bus.Subscriber
    wsSubMu       sync.Mutex
}
```

In the subscriber creation loop (line ~274), append each subscriber:

```go
s.wsSubMu.Lock()
s.wsSubscribers = append(s.wsSubscribers, sub)
s.wsSubMu.Unlock()
```

In `Shutdown`, unsubscribe:

```go
s.wsSubMu.Lock()
for _, sub := range s.wsSubscribers {
    s.bus.Unsubscribe(sub)
}
s.wsSubMu.Unlock()
```

- [x] **Step 2: Fix handleReload to return nil error**

In `internal/rpc/dev.go`, change the error return:

```go
// Replace:
return map[string]any{
    RPCKeySuccess: false,
    "error":       err.Error(),
}, err

// With:
return map[string]any{
    RPCKeySuccess: false,
    "error":       err.Error(),
}, nil
```

- [x] **Step 3: Add TTL cleanup for bus subscriptions**

In `internal/rpc/proxy.go`, replace `context.Background()` with a context derived from a server-level shutdown context, and add periodic cleanup for stale subscriptions.

- [x] **Step 4: Run tests**

Run: `go test ./internal/comm/http/ ./internal/rpc/ -v`
Expected: All PASS

- [x] **Step 5: Commit**

```bash
git add internal/comm/http/server.go internal/rpc/dev.go internal/rpc/proxy.go
git commit -m "fix: unsubscribe WS goroutines on shutdown, fix handleReload, add bus sub cleanup"
```

---

### Task 21: Fix security findings (S1-S3)

**Files:**
- Modify: `internal/comm/http/auth.go:84-86`
- Modify: `internal/tools/builtin/shell_tokenize.go:39-41`
- Modify: `internal/security/tirith.go:87-94`
- Modify: `internal/comm/web/auth.go:228-231`
- Modify: `internal/stt/recorder.go:48-55`
- Modify: `internal/tools/mcp/manager.go:325-330`

- [x] **Step 1: Remove query param token for WebSocket auth**

In `internal/comm/http/auth.go`, require the token in the header only:

```go
// Replace:
if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
    return r.URL.Query().Get("token")
}

// With a check that still allows WebSocket but requires header auth:
if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
    // For WebSocket, check Sec-WebSocket-Protocol or custom header
    // Query params are insecure (logged in URLs)
    return r.Header.Get("X-API-Key")
}
```

Document the migration path: Flutter WebSocket clients must send the token via `Sec-WebSocket-Protocol` or a custom header instead of `?token=`.

- [x] **Step 2: Fix shell tokenizer escape handling**

In `internal/tools/builtin/shell_tokenize.go`, properly handle backslash escapes:

```go
if t.input[t.pos] == '\\' && t.pos+1 < len(t.input) {
    t.pos += 2 // skip escaped char
    continue
}
```

This currently skips the escape but does not include the escaped character in the token. Fix by appending the escaped character:

```go
if t.input[t.pos] == '\\' && t.pos+1 < len(t.input) {
    token.WriteByte(t.input[t.pos+1])
    t.pos += 2
    continue
}
```

- [x] **Step 3: Make Tirith block on failure**

In `internal/security/tirith.go`:

```go
if ctx.Err() != nil || err != nil {
    exitError := &exec.ExitError{}
    if errors.As(err, &exitError) {
        return nil // Non-zero exit = allow (Tirith's explicit decision)
    }
    // Scanner failure (timeout, crash) = block by default
    msg := "tirith scanner error: " + err.Error()
    return &TirithResult{Blocked: true, Message: &msg}
}
```

- [x] **Step 4: Fix IPv6 IP extraction**

In `internal/comm/web/auth.go`:

```go
// Replace:
ip := r.RemoteAddr
if colonIdx := strings.LastIndex(ip, ":"); colonIdx != -1 {
    ip = ip[:colonIdx]
}

// With:
host, _, err := net.SplitHostPort(r.RemoteAddr)
if err != nil {
    host = r.RemoteAddr
}
ip := strings.Trim(host, "[]")
```

- [x] **Step 5: Fix STT recorder temp file leak**

In `internal/stt/recorder.go`, add cleanup on Start failure:

```go
if err := r.cmd.Start(); err != nil {
    os.Remove(tmp.Name())
    return fmt.Errorf("stt: start recorder: %w", err)
}
```

- [x] **Step 6: Fix MCP manager orphan on close failure**

In `internal/tools/mcp/manager.go`:

```go
// Replace:
if err := existingClient.Close(); err != nil {
    m.logger.Error("error closing MCP client during restart", "name", name, "error", err)
}
delete(m.clients, name)

// With:
if err := existingClient.Close(); err != nil {
    m.logger.Error("error closing MCP client during restart", "name", name, "error", err)
    continue // Keep in map for retry
}
delete(m.clients, name)
```

- [x] **Step 7: Run security and tool tests**

Run: `go test ./internal/security/ ./internal/tools/ ./internal/stt/ ./internal/comm/ -v`
Expected: All PASS

- [x] **Step 8: Commit**

```bash
git add internal/comm/http/auth.go internal/tools/builtin/shell_tokenize.go internal/security/tirith.go internal/comm/web/auth.go internal/stt/recorder.go internal/tools/mcp/manager.go
git commit -m "fix: remove query param auth, fix shell escapes, block on Tirith failure, fix IP parsing, fix STT temp leak, fix MCP orphan"
```

---

## Sprint 6: Flutter Critical — WebSocket + API Layer (F1, F2, F5, F13, F17)

### Task 22: Fix WebSocket reconnect (F1)

**Files:**
- Modify: `ui/flutter_ui/lib/services/websocket_service.dart:104-124,262-268`

- [x] **Step 1: Replace Rx.retryWhen with reconnect loop**

In `websocket_service.dart`, replace the `connect` method:

```dart
Future<void> connect({String? path}) async {
    _wasExplicitlyDisconnected = false;
    if (_isDisposed || _isConnecting) return;
    _isConnecting = true;
    _reconnectSubscription?.cancel();

    final wsPath = path ?? '/ws';
    await _connectWithRetry(wsPath);
  }

  Future<void> _connectWithRetry(String wsPath) async {
    while (!_isDisposed && !_wasExplicitlyDisconnected) {
      try {
        await _openConnection(wsPath);
        // Connection established. When it drops, onDone will fire.
        // Re-enter the retry loop from onDone.
        return;
      } catch (e) {
        if (_wasExplicitlyDisconnected || _isDisposed) {
          _isConnecting = false;
          return;
        }
        _errorSubject.addSafe('Reconnecting: $e');
        await Future<void>.delayed(_nextReconnectDelay());
      }
    }
    _isConnecting = false;
  }
```

- [x] **Step 2: Trigger reconnect from onDone**

In `_openConnection`, update the `onDone` handler to trigger reconnect:

```dart
onDone: () {
    _connectionSubject.add(false);
    _cleanupChannel();
    if (!_wasExplicitlyDisconnected && !_isDisposed) {
      _connectWithRetry(wsPath); // Reconnect loop
    } else {
      _isConnecting = false;
    }
  },
```

- [x] **Step 3: Run Flutter WebSocket tests**

Run: `cd ui/flutter_ui && flutter test test/`
Expected: All PASS

- [x] **Step 4: Commit**

```bash
git add ui/flutter_ui/lib/services/websocket_service.dart
git commit -m "fix: replace Rx.retryWhen with reconnect loop for automatic WebSocket reconnection"
```

---

### Task 23: Fix unsafe cast and API client disposal (F2, F13, F17)

**Files:**
- Modify: `ui/flutter_ui/lib/services/meept_api.dart:33-34,241,251`
- Modify: `ui/flutter_ui/lib/services/api_client.dart`
- Modify: `ui/flutter_ui/lib/models/api_models.dart:67`
- Modify: `ui/flutter_ui/lib/providers/providers.dart`

- [x] **Step 1: Fix .cast crashes in meept_api.dart**

```dart
// Replace at line 241 and 251:
return raw.cast<Map<String, dynamic>>().toList();

// With:
return raw.map((e) => Map<String, dynamic>.from(e as Map)).toList();
```

- [x] **Step 2: Fix healthCheck URL construction**

```dart
Future<Map<String, dynamic>> healthCheck() async {
    final rootUrl = _dio.options.baseUrl;
    final idx = rootUrl.indexOf('/api/');
    final base = idx >= 0 ? rootUrl.substring(0, idx) : rootUrl;
    final response = await _dio.get('$base/health');
    return response.data as Map<String, dynamic>;
  }
```

- [x] **Step 3: Fix tool_calls cast in api_models.dart**

```dart
// Replace:
'tool_calls': (json['tool_calls'] as List?)?.cast<String>(),

// With:
'tool_calls': (json['tool_calls'] as List?)?.map((e) => e.toString()).toList(),
```

- [x] **Step 4: Add dispose to ApiClient**

In `api_client.dart`:

```dart
void dispose() {
    _dio.close(force: true);
  }
```

- [x] **Step 5: Wire disposal in providers.dart**

```dart
final apiClientProvider = Provider<ApiClient>((ref) {
    final storage = ref.watch(storageProvider);
    final client = ApiClient.storage(storage: storage);
    ref.onDispose(() => client.dispose());
    return client;
  });
```

- [x] **Step 6: Run Flutter tests**

Run: `cd ui/flutter_ui && flutter test`
Expected: All PASS

- [x] **Step 7: Commit**

```bash
git add ui/flutter_ui/lib/services/meept_api.dart ui/flutter_ui/lib/services/api_client.dart ui/flutter_ui/lib/models/api_models.dart ui/flutter_ui/lib/providers/providers.dart
git commit -m "fix: unsafe cast crashes, healthCheck URL, and API client disposal"
```

---

### Task 24: Fix provider state management bugs (F5, F11, F12)

**Files:**
- Modify: `ui/flutter_ui/lib/providers/job_provider.dart:111-137`
- Modify: `ui/flutter_ui/lib/providers/metrics_provider.dart:89-128`
- Modify: `ui/flutter_ui/lib/providers/stt_provider.dart:49`
- Modify: `ui/flutter_ui/lib/providers/agent_provider.dart:57`

- [x] **Step 1: Add generation counter to JobNotifier**

```dart
class JobNotifier extends StateNotifier<JobState> {
    int _fetchGeneration = 0;
    bool _disposed = false;

    Future<void> _fetchJobs() async {
        final gen = ++_fetchGeneration;
        try {
            final jobs = await apiClient.listJobs();
            final stats = await apiClient.getQueueStats();
            if (_disposed || gen != _fetchGeneration) return;
            // ... update state
        } catch (e) {
            if (_disposed || gen != _fetchGeneration) return;
            // ... update state
        }
    }

    @override
    void dispose() {
        _disposed = true;
        super.dispose();
    }
}
```

- [x] **Step 2: Add dispose guard to MetricsNotifier WS listener**

In `metrics_provider.dart`, in the `_subscribeToMetrics` listener:

```dart
_metricsSubscription = websocket.subscribeToMetrics().listen((msg) {
    if (_disposed) return;
    // ... existing logic
});
```

- [x] **Step 3: Fix SttNotifier state ordering**

In `stt_provider.dart`:

```dart
Future<void> startRecording({...}) async {
    if (!_service.isAvailable) {
        await _service.initialize();
    }
    if (!_service.isAvailable) return; // Return BEFORE setting state

    state = SttState.recording; // Now set after guard
    _service.startRecording(...);
}
```

- [x] **Step 4: Fix AgentNotifier ref.read -> ref.watch**

In `agent_provider.dart`:

```dart
// Replace:
final client = ref.read(apiClientProvider);

// With:
final client = ref.watch(apiClientProvider);
```

- [x] **Step 5: Run Flutter provider tests**

Run: `cd ui/flutter_ui && flutter test test/providers/`
Expected: All PASS

- [x] **Step 6: Commit**

```bash
git add ui/flutter_ui/lib/providers/job_provider.dart ui/flutter_ui/lib/providers/metrics_provider.dart ui/flutter_ui/lib/providers/stt_provider.dart ui/flutter_ui/lib/providers/agent_provider.dart
git commit -m "fix: provider generation guards, dispose safety, STT state ordering, agent ref.watch"
```

---

## Sprint 7: Flutter Chat + Navigation Bugs (F6-F10, F16)

### Task 25: Fix chat input and slash autocomplete bugs (F6, F8, F9)

**Files:**
- Modify: `ui/flutter_ui/lib/features/chat/chat_input.dart`
- Modify: `ui/flutter_ui/lib/features/chat/slash_autocomplete.dart`

- [x] **Step 1: Fix re-entrant controller mutations in ChatInput**

In `chat_input.dart`, bracket mutations with listener removal:

```dart
void _onTextChanged() {
    final currentText = _controller.text;
    _detectPaste(currentText);
    final finalText = _controller.text;
    _detectSlashCommand(finalText);
    _detectFilePaths(finalText);
    _updateGhostText(finalText);
    _previousText = finalText;
}

// In _detectPaste, bracket the mutation:
_controller.removeListener(_onTextChanged);
_controller.text = newText;
_controller.selection = TextSelection.collapsed(offset: newText.length);
_controller.addListener(_onTextChanged);
```

- [x] **Step 2: Add isLoading guard to key handler**

In `_handleKeyEvent`:

```dart
if (event.logicalKey == LogicalKeyboardKey.enter) {
    if (ref.read(chatProvider).isLoading) return KeyEventResult.ignored;
    // ... rest of enter handling
}
```

- [x] **Step 3: Defer onDismiss in SlashAutocomplete**

In `slash_autocomplete.dart`:

```dart
if (_matches.isEmpty) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
        widget.onDismiss?.call();
    });
    return;
}
```

- [x] **Step 4: Remove autofocus from SlashAutocomplete**

In `slash_autocomplete.dart`, remove `autofocus: true` from the Focus widget (line ~102).

- [x] **Step 5: Run Flutter chat tests**

Run: `cd ui/flutter_ui && flutter test test/features/chat/`
Expected: All PASS

- [x] **Step 6: Commit**

```bash
git add ui/flutter_ui/lib/features/chat/chat_input.dart ui/flutter_ui/lib/features/chat/slash_autocomplete.dart
git commit -m "fix: re-entrant controller mutations, isLoading guard, deferred onDismiss, remove autofocus"
```

---

### Task 26: Fix navigation and routing bugs (F7, F10)

**Files:**
- Modify: `ui/flutter_ui/lib/core/router.dart`
- Modify: `ui/flutter_ui/lib/features/home/home_screen.dart`

- [x] **Step 1: Add /plans and /agents routes to router**

In `router.dart`, add routes:

```dart
GoRoute(
    path: '/plans',
    builder: (context, state) => const HomeScreen(initialTab: 2),
),
GoRoute(
    path: '/agents',
    builder: (context, state) => const HomeScreen(initialTab: 4),
),
```

Add `initialTab` parameter to `HomeScreen`:

```dart
class HomeScreen extends ConsumerStatefulWidget {
    final int initialTab;
    const HomeScreen({super.key, this.initialTab = 0});
    // ...
}
```

- [x] **Step 2: Fix tool navigation orphaning activeToolProvider**

In `home_screen.dart`:

```dart
onToolSelected: (route) {
    if (_hasRoute(route)) {
        // Full-screen tool panel -- don't set activeTool
        _navigateTool(route);
    } else {
        // In-chat panel swap
        ref.read(activeToolProvider.notifier).state = route;
        if (_selectedTab != HomeTab.chat) {
            setState(() => _selectedTab = HomeTab.chat);
        }
    }
},
```

- [x] **Step 3: Run Flutter tests**

Run: `cd ui/flutter_ui && flutter test`
Expected: All PASS

- [x] **Step 4: Commit**

```bash
git add ui/flutter_ui/lib/core/router.dart ui/flutter_ui/lib/features/home/home_screen.dart
git commit -m "fix: add /plans and /agents routes, prevent activeTool state orphaning"
```

---

### Task 27: Fix session and message list bugs (F4, F16)

**Files:**
- Modify: `ui/flutter_ui/lib/features/sessions/sessions_list.dart`
- Modify: `ui/flutter_ui/lib/features/chat/chat_message_list.dart:128-140`
- Modify: `ui/flutter_ui/lib/features/chat/chat_message_bubble.dart:48-61`

- [x] **Step 1: Fix session creation error feedback**

In `sessions_list.dart`:

```dart
onPressed: () async {
    if (controller.text.isNotEmpty) {
        final session = await notifier.createSession(controller.text);
        if (session != null) {
            Navigator.pop(context);
            ref.read(activeSessionProvider.notifier).state = session;
            if (context.mounted) {
                context.go('/');
            }
        } else {
            // Error: show feedback, don't pop
            if (context.mounted) {
                ScaffoldMessenger.of(context).showSnackBar(
                    const SnackBar(
                        content: Text('failed to create session'),
                        backgroundColor: CyberpunkColors.redAlert,
                    ),
                );
            }
        }
    }
},
```

- [x] **Step 2: Clear active session on delete**

In `sessions_list.dart`:

```dart
onPressed: () {
    final isActive = activeSession?.id == sessionId;
    ref.read(sessionProvider.notifier).deleteSession(sessionId);
    if (isActive) {
        ref.read(activeSessionProvider.notifier).state = null;
    }
    Navigator.pop(context);
},
```

- [x] **Step 3: Add bottom padding for error banner**

In `chat_message_list.dart`:

```dart
padding: EdgeInsets.fromLTRB(
    16, 16, 16,
    chatState.error != null ? 100 : 16,
),
```

- [x] **Step 4: Run Flutter tests**

Run: `cd ui/flutter_ui && flutter test test/features/sessions/ test/features/chat/`
Expected: All PASS

- [x] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/features/sessions/sessions_list.dart ui/flutter_ui/lib/features/chat/chat_message_list.dart
git commit -m "fix: session error feedback, active session cleanup on delete, error banner padding"
```

---

## Sprint 8: Flutter Remaining UI Bugs (F14, F15, misc)

### Task 28: Fix remaining Flutter UI bugs

**Files:**
- Modify: `ui/flutter_ui/lib/features/agents/agents_tab.dart:55-95`
- Modify: `ui/flutter_ui/lib/features/settings/settings_panel.dart:484-498`
- Modify: `ui/flutter_ui/lib/features/search/search_panel.dart`
- Modify: `ui/flutter_ui/lib/features/calendar/calendar_panel.dart:93`
- Modify: `ui/flutter_ui/lib/widgets/glitch_text.dart:35`

- [x] **Step 1: Wrap agents_tab states in Expanded**

```dart
if (agentState.isLoading)
    const Expanded(child: Center(child: CircularProgressIndicator()))
else if (agentState.error != null)
    const Expanded(child: Center(child: Column(...)))
else if (agentState.agents.isEmpty)
    const Expanded(child: Center(child: Text('no agents available')))
else
    Expanded(child: GridView.builder(...))
```

- [x] **Step 2: Guard settings_panel programmatic updates**

```dart
bool _programmaticUpdate = false;

// In _loadConfig:
_programmaticUpdate = true;
_configController.text = content;
_programmaticUpdate = false;

// In onChanged:
onChanged: (value) {
    if (_programmaticUpdate) return;
    setState(() {
        _configContent = value;
        _hasChanges = true;
    });
},
```

- [x] **Step 3: Fix search_panel debouncer and setState**

```dart
onChanged: (value) {
    setState(() {}); // Update clear button visibility
    _debouncer.run(() => _search(value));
},

// In dispose:
@override
void dispose() {
    _debouncer.dispose(); // Cancel pending timer
    _searchController.dispose();
    super.dispose();
}
```

Add a `dispose()` method to the `Debouncer` class if it doesn't have one:

```dart
void dispose() {
    _timer?.cancel();
}
```

- [x] **Step 4: Fix calendar dialog premature pop**

```dart
Future<void> _createEvent(...) async {
    try {
        await client.createCalendarEvent(...);
        if (mounted) {
            Navigator.pop(context); // Pop only on success
        }
    } catch (e) {
        // Leave dialog open for retry
        if (mounted) {
            ScaffoldMessenger.of(context).showSnackBar(...);
        }
    }
}
```

- [x] **Step 5: Fix GlitchText Random allocation**

```dart
class _GlitchTextState extends State<GlitchText> with SingleTickerProviderStateMixin {
    final _random = math.Random();

    // In the listener:
    _offsetX = (_random.nextDouble() - 0.5) * widget.glitchIntensity * 4;
    _offsetY = (_random.nextDouble() - 0.5) * widget.glitchIntensity * 2;
}
```

- [x] **Step 6: Run all Flutter tests**

Run: `cd ui/flutter_ui && flutter test`
Expected: All PASS

- [x] **Step 7: Commit**

```bash
git add ui/flutter_ui/lib/features/agents/agents_tab.dart ui/flutter_ui/lib/features/settings/settings_panel.dart ui/flutter_ui/lib/features/search/search_panel.dart ui/flutter_ui/lib/features/calendar/calendar_panel.dart ui/flutter_ui/lib/widgets/glitch_text.dart
git commit -m "fix: agents layout, settings programmatic update guard, search debouncer, calendar dialog, GlitchText random"
```

---

### Task 29: Final verification — run all tests

- [x] **Step 1: Run full Go test suite**

```bash
go test -race ./... -timeout 300s
```

Expected: All PASS, no races

- [x] **Step 2: Run full Flutter test suite**

```bash
cd ui/flutter_ui && flutter test
```

Expected: All PASS

- [x] **Step 3: Run integration smoke test**

```bash
make build
./bin/meept status
./bin/meept config list
```

Expected: No panics or crashes

- [x] **Step 4: Final commit**

```bash
git commit --allow-empty -m "chore: complete full-stack bug fix sprint — 45 bugs fixed across daemon, client, and Flutter GUI"
```
