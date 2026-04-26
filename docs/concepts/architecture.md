# Architecture

Meept is a Go daemon with a layered architecture: client interfaces connect through an RPC layer to a message bus, which routes messages to agent loops that use LLM inference and tool execution.

## System Overview

```mermaid
flowchart TB
    subgraph Clients["Client Layer"]
        CLI["CLI<br/>cmd/meept"]
        TUI["TUI Mode<br/>internal/tui"]
        Telegram["Telegram Bot<br/>internal/comm/telegram"]
        WebUI["Web API<br/>internal/comm/web"]
    end

    subgraph Daemon["Daemon Core"]
        DaemonMgr["Daemon Manager<br/>internal/daemon"]
        Config["Config Loader<br/>internal/config"]
        Registry["Component Registry<br/>internal/registry"]
    end

    subgraph Communication["Communication Layer"]
        RPC["RPC Server<br/>Unix Socket JSON-RPC<br/>internal/rpc"]
        Bus["Message Bus<br/>Pub/Sub<br/>internal/bus"]
    end

    subgraph Agent["Agent System"]
        AgentLoop["Agent Loop<br/>internal/agent"]
        Executor["Tool Executor<br/>internal/agent"]
        Conversation["Conversation Store<br/>internal/agent"]
        Planner["Collaborative Planner<br/>internal/agent"]
        Workspace["Workspace Manager<br/>internal/agent"]
    end

    subgraph LLM["LLM Layer"]
        LLMClient["LLM Client<br/>internal/llm"]
        Resolver["Model Resolver<br/>internal/llm"]
        Budget["Budget Tracker<br/>internal/llm"]
        TokenCache["Token Cache<br/>internal/llm (L1+L2)"]
        Providers["Provider Adapters<br/>OpenAI-compatible"]
    end

    subgraph Tools["Tool System"]
        ToolReg["Tool Registry<br/>internal/tools"]
        BuiltinTools["Builtin Tools<br/>filesystem, shell, web"]
        MCPClient["MCP Client<br/>stdio/http transport"]
    end

    subgraph Security["Security Layer"]
        SecEngine["Security Engine<br/>internal/security"]
        Sanitizer["Input Sanitizer<br/>internal/security"]
        Tirith["Tirith Scanner<br/>Shell command analysis"]
    end

    subgraph Memory["Memory System"]
        MemMgr["Memory Manager<br/>internal/memory"]
        Episodic["Episodic Memory<br/>SQLite+FTS5"]
        TaskMem["Task Memory<br/>internal/memory"]
        Consolidation["Memory Consolidation<br/>internal/memory"]
    end

    CLI --> RPC
    TUI --> RPC
    Telegram --> Bus
    WebUI --> Bus

    DaemonMgr --> Config
    DaemonMgr --> Registry
    DaemonMgr --> RPC
    DaemonMgr --> Bus

    RPC <--> Bus
    Bus --> AgentLoop

    AgentLoop --> Executor
    AgentLoop --> Conversation
    AgentLoop --> LLMClient
    Executor --> ToolReg
    Executor --> SecEngine
    ToolReg --> BuiltinTools
    ToolReg --> MCPClient

    LLMClient --> Resolver
    LLMClient --> Budget
    Resolver --> Providers

    MemMgr --> Episodic
    MemMgr --> TaskMem
    MemMgr --> Consolidation
    AgentLoop -.-> MemMgr

    SecEngine --> Sanitizer
    SecEngine --> Tirith
```

## Component Layers

### Entry Points

| Component | Package | Description |
|-----------|---------|-------------|
| CLI | `cmd/meept` | Command-line client |
| Daemon | `cmd/meept-daemon` | Background daemon process |
| TUI | `internal/tui` | Bubble Tea interactive terminal UI |

### Communication

| Component | Package | Description |
|-----------|---------|-------------|
| RPC Server | `internal/rpc` | Unix socket JSON-RPC server |
| Message Bus | `internal/bus` | Pub/sub message routing |

### Agent System

| Component | Package | Description |
|-----------|---------|-------------|
| Agent Loop | `internal/agent` | Core reasoning loop with tool use |
| Tool Executor | `internal/agent` | Permission-checked tool execution |
| Conversation | `internal/agent` | Persistent session management |
| Planner | `internal/agent` | Task decomposition and collaborative review |
| Workspace | `internal/agent` | Git-backed task tracking |

### Orchestration

| Component | Package | Description |
|-----------|---------|-------------|
| Job Queue | `internal/queue` | SQLite-backed job queue |
| Worker Pool | `internal/worker` | Multi-agent worker management |
| Session Store | `internal/session` | Persistent session state |

### Support Systems

| Component | Package | Description |
|-----------|---------|-------------|
| LLM Client | `internal/llm` | Multi-provider LLM integration |
| Token Cache | `internal/llm` | L1+L2 response caching with file-aware invalidation |
| Tool Registry | `internal/tools` | Tool registration and dispatch |
| Security | `internal/security` | Sanitization, taint tracking, permissions |
| Memory | `internal/memory` | Multi-tier memory with FTS5 |
| Skills | `internal/skills` | Skill discovery and execution |
| Scheduler | `internal/scheduler` | Cron-based job scheduling |

## Data Flow

```mermaid
flowchart LR
    subgraph Input
        User[User Input]
        Scheduled[Scheduled Jobs]
        External[External Events]
    end

    subgraph Processing
        Parse[Parse & Validate]
        Queue[Job Queue]
        Route[Route to Worker]
    end

    subgraph Execution
        Agent[Agent Loop]
        LLM[LLM Inference]
        Tools[Tool Execution]
    end

    subgraph Output
        Response[Response]
        SideEffects[Side Effects]
        Memory[Memory Update]
    end

    User --> Parse
    Scheduled --> Queue
    External --> Parse

    Parse --> Queue
    Queue --> Route
    Route --> Agent

    Agent <--> LLM
    Agent <--> Tools

    Agent --> Response
    Tools --> SideEffects
    Agent --> Memory
```

## Key Design Decisions

1. **Daemon model** — Meept runs as a persistent process, not a per-session CLI. This enables job scheduling, persistent memory, and multi-session state.

2. **Message bus** — All communication between components goes through a pub/sub bus. This decouples components and enables easy extension.

3. **Multi-agent routing** — Rather than one agent doing everything, specialist agents handle different task types. The dispatcher classifies and routes.

4. **SQLite backbone** — Job queue, memory, audit logs, and metrics all use SQLite for zero-dependency persistence.

5. **OpenAI-compatible API** — LLM providers all use the OpenAI chat completion format, making it easy to add new providers.
