# Meept Go Refactoring Plan

## Executive Summary

This plan outlines a selective refactoring strategy to rewrite Meept's server/core infrastructure in Go while keeping AI/business logic in Python. The architecture naturally supports this split—the daemon, message bus, and RPC server are well-isolated from agent/LLM logic.

**Scope:**
- **Rewrite in Go:** daemon lifecycle, message bus, CommServer, models
- **Keep in Python:** agent loop, LLM clients, memory, skills, CLI
- **Rust stays:** Tauri menubar app (already Rust, minimal)

**Benefits:**
- 5-10x faster daemon startup (<100ms vs ~500ms)
- 20-50x RPC throughput improvement
- Single binary deployment for core daemon
- Better resource utilization (goroutines vs asyncio)

**Timeline:** 12-16 weeks across 4 phases

---

## Architecture: Current vs Target

### Current (Pure Python)

```
┌─────────────────────────────────────────────────┐
│              ALL PYTHON                          │
├─────────────────────────────────────────────────┤
│ Daemon → Bus → Registry → Scheduler            │
│    ↓                                            │
│ CommServer (Unix socket JSON-RPC)              │
│    ↓                                            │
│ Agent Loop → LLM → Memory → Skills             │
│    ↓                                            │
│ Tools → MCP → Security                         │
└─────────────────────────────────────────────────┘
```

### Target (Go Core + Python Logic)

```
┌─────────────────────────────────────────────────┐
│ Clients: CLI (Python) | Web (FastAPI) | Telegram │
└──────────────────┬──────────────────────────────┘
                   │ JSON-RPC 2.0 / HTTP
┌──────────────────▼──────────────────────────────┐
│  ┌────────────────────────────────────────────┐ │
│  │ GO DAEMON (meept-daemon)                  │ │
│  ├────────────────────────────────────────────┤ │
│  │ - CommServer (Unix socket)                │ │
│  │ - MessageBus (channel-based pub/sub)      │ │
│  │ - Registry (component lifecycle)          │ │
│  │ - gRPC/IPC bridge to Python subsystems    │ │
│  └────────────────────────────────────────────┘ │
│          │ gRPC / Subprocess IPC               │
│  ┌────────▼──────────────────────────────────┐ │
│  │ PYTHON AGENTS                             │ │
│  ├────────────────────────────────────────────┤ │
│  │ - FrontAgent / Orchestrator               │ │
│  │ - AgentLoop / WorkerFactory               │ │
│  │ - LLM clients / Model resolution          │ │
│  │ - MemoryManager (episodic/task/personal)  │ │
│  │ - Scheduler (APScheduler)                 │ │
│  │ - Skills registry                         │ │
│  └────────────────────────────────────────────┘ │
│          │ Tool calls                           │
│  ┌────────▼──────────────────────────────────┐ │
│  │ TOOL LAYER (Mixed)                        │ │
│  ├────────────────────────────────────────────┤ │
│  │ - Shell/Filesystem (Python)               │ │
│  │ - Permission checker (Go, optional)       │ │
│  │ - Web tools (Go, optional)                │ │
│  │ - MCP client (Python)                     │ │
│  └────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────┘
```

---

## Module Classification

### Tier 1: Rewrite in Go (HIGH PRIORITY)

| Module | Files | Current LOC | Why Go |
|--------|-------|-------------|--------|
| `core/daemon.py` | 1 | 300 | Unix daemon best practices, signals |
| `core/bus.py` | 1 | 200 | Channels >> asyncio for pub/sub |
| `core/registry.py` | 1 | 150 | Simple factory pattern |
| `comm/server.py` | 1 | 350 | Stateless RPC, high concurrency |
| `comm/protocol.py` | 1 | 150 | Frame parsing |
| `models/*.py` | 5 | 300 | Pure data structures |

**Total: ~1,450 LOC Python → ~1,200 LOC Go**

### Tier 2: Keep in Python (AI/Business Logic)

| Module | Files | Why Python |
|--------|-------|------------|
| `agent/*` | 9 | LLM prompt engineering, complex state |
| `llm/*` | 6 | Model API integration, capability matching |
| `memory/*` | 7 | AI-driven consolidation, complex SQL |
| `skills/*` | 6 | Business logic, agent coupling |
| `clawskills/*` | 7 | Third-party integration, versioning |
| `selfimprove/*` | 15 | Experimental, heavy business logic |
| `cli/*` | 12 | Textual TUI (Python-only) |
| `tests/*` | 56 | pytest ecosystem |

### Tier 3: Optional Go Refactor (MEDIUM PRIORITY)

| Module | Files | Rationale |
|--------|-------|-----------|
| `security/permissions.py` | 1 | Hot path, pure function logic |
| `tools/builtin/web_fetch.py` | 1 | net/http is superior |
| `tools/builtin/web_search.py` | 1 | HTTP client operations |
| `scheduler/scheduler.py` | 1 | After daemon stable in Go |

### Tier 4: Leave As-Is

| Module | Language | Notes |
|--------|----------|-------|
| `menubar/*` | Rust/Tauri | Already optimal |
| `config/*` | TOML/JSON5 | Data files |

---

## Implementation Phases

### Phase 1: Foundations (4-6 weeks)

**Goal:** Extract server/core, validate API contract

#### 1.1 Go Project Setup
```
meept/
├── cmd/
│   └── meept-daemon/
│       └── main.go          # Entry point
├── internal/
│   ├── bus/
│   │   └── bus.go           # MessageBus with channels
│   ├── daemon/
│   │   └── daemon.go        # Lifecycle management
│   ├── registry/
│   │   └── registry.go      # Component registry
│   └── rpc/
│       ├── server.go        # CommServer
│       └── protocol.go      # Frame parsing
├── pkg/
│   └── models/
│       └── types.go         # Shared data types
├── go.mod
└── go.sum
```

#### 1.2 MessageBus Implementation
```go
// internal/bus/bus.go
type MessageBus struct {
    subscribers map[string][]chan *BusMessage
    mu          sync.RWMutex
}

func (b *MessageBus) Publish(topic string, msg *BusMessage) {
    b.mu.RLock()
    defer b.mu.RUnlock()
    for _, ch := range b.subscribers[topic] {
        select {
        case ch <- msg:
        default:
            // Non-blocking send; drop if full
        }
    }
}

func (b *MessageBus) Subscribe(topic string) <-chan *BusMessage {
    ch := make(chan *BusMessage, 100)
    b.mu.Lock()
    b.subscribers[topic] = append(b.subscribers[topic], ch)
    b.mu.Unlock()
    return ch
}
```

#### 1.3 CommServer Implementation
```go
// internal/rpc/server.go
func (s *Server) Serve(socketPath string) error {
    listener, err := net.Listen("unix", socketPath)
    if err != nil {
        return err
    }
    defer listener.Close()

    for {
        conn, err := listener.Accept()
        if err != nil {
            continue
        }
        go s.handleConnection(conn)
    }
}

func (s *Server) handleConnection(conn net.Conn) {
    defer conn.Close()
    reader := bufio.NewReader(conn)

    for {
        // Read length-prefixed frame
        lengthLine, _ := reader.ReadString('\n')
        length, _ := strconv.Atoi(strings.TrimSpace(lengthLine))

        payload := make([]byte, length)
        io.ReadFull(reader, payload)

        // Dispatch JSON-RPC
        var req JSONRPCRequest
        json.Unmarshal(payload, &req)

        result := s.dispatch(req.Method, req.Params)

        // Write response
        response := JSONRPCResponse{ID: req.ID, Result: result}
        respBytes, _ := json.Marshal(response)
        fmt.Fprintf(conn, "%d\n%s", len(respBytes), respBytes)
    }
}
```

#### 1.4 Deliverables
- [ ] `meept-daemon` Go binary compiles and starts
- [ ] MessageBus handles pub/sub between goroutines
- [ ] CommServer accepts JSON-RPC over Unix socket
- [ ] Python agent connects to Go daemon successfully
- [ ] All existing tests pass (Python agent unchanged)

### Phase 2: Performance Optimization (6-8 weeks)

**Goal:** Accelerate hot paths

#### 2.1 Go Permission Checker
```go
// pkg/security/permissions.go
func CheckFileAccess(path, user, action string) (bool, error) {
    absPath, err := filepath.Abs(path)
    if err != nil {
        return false, err
    }

    // Check against blocked paths
    for _, blocked := range BlockedPaths {
        if strings.HasPrefix(absPath, blocked) {
            return false, nil
        }
    }

    // Check action-specific permissions
    switch action {
    case "read":
        return checkReadPermission(absPath, user)
    case "write":
        return checkWritePermission(absPath, user)
    default:
        return false, fmt.Errorf("unknown action: %s", action)
    }
}
```

#### 2.2 Python Integration (gRPC or subprocess)
```python
# src/meept/security/go_permissions.py
import subprocess
import json

def check_file_access_go(path: str, user: str, action: str) -> bool:
    result = subprocess.run(
        ["meept-perms", "check", "--path", path, "--user", user, "--action", action],
        capture_output=True,
        text=True,
    )
    return json.loads(result.stdout).get("allowed", False)
```

#### 2.3 Deliverables
- [ ] Permission checker in Go: 10k checks/sec
- [ ] Python binding works with existing security layer
- [ ] Daemon startup <100ms (vs ~500ms baseline)
- [ ] Load test: 1,000 concurrent RPC requests

### Phase 3: Integration (4-6 weeks)

**Goal:** Scheduler and optional subsystems

#### 3.1 Go Scheduler (Optional)
```go
// internal/scheduler/scheduler.go
import "github.com/robfig/cron/v3"

type Scheduler struct {
    cron *cron.Cron
    bus  *bus.MessageBus
}

func (s *Scheduler) AddJob(spec string, handler func()) (cron.EntryID, error) {
    return s.cron.AddFunc(spec, handler)
}

func (s *Scheduler) DispatchToBus(topic string, payload interface{}) {
    msg := &BusMessage{Topic: topic, Payload: payload}
    s.bus.Publish(topic, msg)
}
```

#### 3.2 Deliverables
- [ ] Cron jobs dispatch to Python agent via bus
- [ ] Existing APScheduler jobs migrated
- [ ] Integration tests pass

### Phase 4: Cleanup (2-4 weeks)

**Goal:** Decommission Python daemon, documentation

#### 4.1 Tasks
- [ ] Archive `src/meept/core/daemon.py`, `bus.py`, `registry.py`
- [ ] Archive `src/meept/comm/server.py`, `protocol.py`
- [ ] Update architecture diagrams
- [ ] Update README with Go build instructions
- [ ] Release v0.2.0 with Go daemon

#### 4.2 CI/CD Updates
```yaml
# .github/workflows/build.yml
jobs:
  build-go:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      - run: go build -o bin/meept-daemon ./cmd/meept-daemon

  build-python:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-python@v5
        with:
          python-version: '3.14'
      - run: pip install -e .
```

---

## Go Dependencies

```go
// go.mod
module github.com/your-org/meept

go 1.22

require (
    github.com/spf13/cobra v1.8.0      // CLI framework
    github.com/robfig/cron/v3 v3.0.1   // Cron scheduling
    gopkg.in/yaml.v3 v3.0.1            // Config parsing
    github.com/mattn/go-sqlite3 v1.14.22 // SQLite (if needed)
)
```

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Python-Go IPC overhead | Use Unix sockets, not HTTP |
| Breaking existing clients | Keep JSON-RPC schema identical |
| Subtle behavior differences | Run both daemons in parallel for 2-3 weeks |
| Go learning curve | Start with simple modules (models, protocol) |
| Rollback needed | Keep Python daemon code archived, not deleted |

---

## Success Metrics

| Metric | Baseline (Python) | Target (Go) |
|--------|-------------------|-------------|
| Daemon startup | ~500ms | <100ms |
| RPC latency (p50) | ~5ms | <1ms |
| RPC throughput | ~100 req/s | >2,000 req/s |
| Memory (idle) | ~50MB | <20MB |
| Binary size | N/A (interpreted) | <15MB |

---

## Migration Checklist

### Pre-Development
- [ ] Document current JSON-RPC schema (all methods, params, responses)
- [ ] Document MessageBus topics and subscribers
- [ ] Establish Python-Go message contract (JSON schema)
- [ ] Set up Go project structure
- [ ] Baseline performance measurements

### Phase 1 Checklist
- [ ] Initialize Go module (`go mod init`)
- [ ] Implement MessageBus with channels
- [ ] Implement daemon lifecycle (signals, PID file)
- [ ] Implement CommServer with frame parsing
- [ ] Implement Registry for component factory
- [ ] Integration test: Python client → Go server
- [ ] Load test: 1,000 concurrent connections

### Phase 2 Checklist
- [ ] Extract permission checks to Go
- [ ] Create Python subprocess/gRPC binding
- [ ] Benchmark permission checks (10k/sec target)
- [ ] Optional: Go web tools (fetch/search)

### Phase 3 Checklist
- [ ] Integrate cron scheduler into Go daemon
- [ ] Migrate existing APScheduler jobs
- [ ] End-to-end integration tests

### Phase 4 Checklist
- [ ] Archive Python core/comm modules
- [ ] Update CI/CD for Go builds
- [ ] Update documentation
- [ ] Release v0.2.0

---

## Effort Estimate

| Phase | Duration | Risk Level |
|-------|----------|------------|
| Phase 1: Foundations | 4-6 weeks | Medium |
| Phase 2: Performance | 6-8 weeks | Low |
| Phase 3: Integration | 4-6 weeks | Medium |
| Phase 4: Cleanup | 2-4 weeks | Low |
| **Total** | **16-24 weeks** | **Medium** |

---

## What Stays in Python

The following explicitly remains in Python:

1. **Agent Logic** (`agent/*`) - LLM orchestration, task planning
2. **LLM Integration** (`llm/*`) - Model clients, capability matching
3. **Memory System** (`memory/*`) - SQLite+FTS5, consolidation
4. **Skills** (`skills/*`, `clawskills/*`) - Discovery, registry
5. **Self-Improvement** (`selfimprove/*`) - Experimental framework
6. **CLI/TUI** (`cli/*`) - Textual-based terminal UI
7. **Web API** (`comm/web.py`) - FastAPI (adequate as-is)
8. **Telegram Bot** (`comm/telegram.py`) - python-telegram-bot
9. **Tests** (`tests/*`) - pytest ecosystem

---

## Conclusion

This refactoring strategy provides significant performance gains (5-20x startup, 10-50x throughput) with low risk because:

1. **Clean separation exists** - Server infrastructure is already decoupled from AI logic
2. **API contract stays stable** - JSON-RPC schema unchanged
3. **Parallel operation** - Run both daemons during validation
4. **Rollback possible** - Python code archived, not deleted
5. **Incremental adoption** - Each phase delivers value independently

Start with Phase 1 (daemon + CommServer) to validate the approach before committing to further phases.
