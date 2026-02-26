# Meept Architecture Diagram

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

    subgraph Orchestration["Multi-Agent Orchestration"]
        Queue["Job Queue<br/>SQLite-backed<br/>internal/queue"]
        TaskReg["Task Registry<br/>internal/task"]
        WorkerPool["Worker Pool<br/>internal/worker"]
        Session["Session Store<br/>internal/session"]
    end

    subgraph LLM["LLM Layer"]
        LLMClient["LLM Client<br/>internal/llm"]
        Resolver["Model Resolver<br/>internal/llm"]
        Budget["Budget Tracker<br/>internal/llm"]
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
        PermCheck["Permission Checker<br/>pkg/security"]
    end

    subgraph Skills["Skills System"]
        SkillDisc["Skill Discovery<br/>internal/skills"]
        SkillReg["Skill Registry<br/>internal/skills"]
        SkillExec["Skill Executor<br/>internal/skills"]
        ClawSkills["ClawSkills<br/>Third-party registry"]
    end

    subgraph Memory["Memory System"]
        MemMgr["Memory Manager<br/>internal/memory"]
        Episodic["Episodic Memory<br/>SQLite+FTS5"]
        TaskMem["Task Memory<br/>internal/memory"]
        Consolidation["Memory Consolidation<br/>internal/memory"]
    end

    subgraph External["External Integrations"]
        Calendar["Google Calendar<br/>internal/calendar"]
        Scheduler["Job Scheduler<br/>internal/scheduler"]
        SelfImprove["Self-Improvement<br/>internal/selfimprove"]
    end

    %% Client connections
    CLI --> RPC
    TUI --> RPC
    Telegram --> Bus
    WebUI --> Bus

    %% Daemon initialization
    DaemonMgr --> Config
    DaemonMgr --> Registry
    DaemonMgr --> RPC
    DaemonMgr --> Bus

    %% RPC to Bus
    RPC <--> Bus

    %% Bus message flow
    Bus --> AgentLoop
    Bus --> Queue
    Bus --> TaskReg
    Bus --> Session

    %% Agent system
    AgentLoop --> Executor
    AgentLoop --> Conversation
    AgentLoop --> LLMClient
    Executor --> ToolReg
    Executor --> PermCheck
    Planner --> AgentLoop
    Workspace --> AgentLoop

    %% Orchestration
    Queue --> WorkerPool
    WorkerPool --> AgentLoop
    TaskReg --> WorkerPool

    %% LLM
    LLMClient --> Resolver
    LLMClient --> Budget
    Resolver --> Providers

    %% Tools
    ToolReg --> BuiltinTools
    ToolReg --> MCPClient
    ToolReg --> SkillExec

    %% Security
    Executor --> SecEngine
    SecEngine --> Sanitizer
    SecEngine --> Tirith
    BuiltinTools --> PermCheck

    %% Skills
    SkillDisc --> SkillReg
    SkillReg --> SkillExec
    ClawSkills --> SkillReg

    %% Memory
    MemMgr --> Episodic
    MemMgr --> TaskMem
    MemMgr --> Consolidation
    AgentLoop -.-> MemMgr

    %% External
    Scheduler --> Queue
    Calendar -.-> AgentLoop
    SelfImprove --> SkillReg
```

## Request Flow

```mermaid
sequenceDiagram
    participant C as CLI/TUI
    participant R as RPC Server
    participant B as Message Bus
    participant H as Chat Handler
    participant A as Agent Loop
    participant L as LLM Client
    participant E as Executor
    participant T as Tool

    C->>R: JSON-RPC Request (chat.send)
    R->>B: Publish(chat.request)
    B->>H: Deliver message
    H->>A: RunOnce(message, session)

    loop Reasoning Cycle
        A->>L: Chat(messages, tools)
        L-->>A: Response + ToolCalls

        alt Has Tool Calls
            A->>E: ExecuteAll(toolCalls)
            E->>T: Execute(args)
            T-->>E: Result
            E-->>A: ExecutionResults
            A->>B: Publish(agent.action)
        else Final Response
            A-->>H: Response text
        end
    end

    H->>B: Publish(chat.response)
    B->>R: Deliver response
    R-->>C: JSON-RPC Response
```

## Component Dependencies

```mermaid
graph LR
    subgraph Core["Core Dependencies"]
        config["config"]
        bus["bus"]
        registry["registry"]
    end

    subgraph Runtime["Runtime Components"]
        daemon["daemon"]
        rpc["rpc"]
        agent["agent"]
    end

    subgraph Processing["Processing"]
        llm["llm"]
        tools["tools"]
        skills["skills"]
    end

    subgraph Storage["Storage"]
        memory["memory"]
        session["session"]
        queue["queue"]
        task["task"]
    end

    subgraph Safety["Safety"]
        security["security"]
    end

    daemon --> config
    daemon --> bus
    daemon --> registry
    daemon --> rpc

    rpc --> bus
    rpc --> security

    agent --> llm
    agent --> tools
    agent --> bus
    agent --> security

    tools --> skills
    tools --> security

    llm --> config

    memory --> config
    session --> bus
    queue --> bus
    task --> bus
```

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

## Multi-Agent Orchestration

```mermaid
flowchart TB
    subgraph AgentRegistry["Agent Registry"]
        Dispatcher["Dispatcher<br/>intake & routing"]
        Chat["Chat Agent<br/>general conversation"]
        Coder["Coder Agent<br/>file & shell ops"]
        Debugger["Debugger Agent<br/>troubleshooting"]
        Planner["Planner Agent<br/>task decomposition"]
        Analyst["Analyst Agent<br/>research & analysis"]
        Committer["Committer Agent<br/>git operations"]
        Scheduler["Scheduler Agent<br/>job scheduling"]
    end

    subgraph Tools["Coworker Awareness Tools"]
        PlatformAgents["platform_agents<br/>List available agents"]
        PlatformStatus["platform_status<br/>Platform health"]
        PlatformTools["platform_tools<br/>List tools"]
        DelegateTask["delegate_task<br/>Route to specialist"]
    end

    subgraph Queue["Job Queue"]
        Jobs[(SQLite Jobs)]
        AgentFilter{"agent_id<br/>filter"}
    end

    User([User]) --> Dispatcher
    Dispatcher --> PlatformAgents
    Dispatcher --> DelegateTask
    DelegateTask --> Coder
    DelegateTask --> Debugger
    DelegateTask --> Planner
    DelegateTask --> Analyst
    DelegateTask --> Committer
    DelegateTask --> Scheduler

    Jobs --> AgentFilter
    AgentFilter -->|"agent_id=coder"| Coder
    AgentFilter -->|"agent_id=planner"| Planner
    AgentFilter -->|"unassigned"| Chat
```

### Agent Types

| ID | Role | Purpose | Additional Tools |
|----|------|---------|------------------|
| `dispatcher` | Dispatcher | Intake, classify, route requests | `delegate_task` |
| `chat` | Executor | General conversation | `web_fetch` |
| `coder` | Executor | File ops, shell, coding | `file_*`, `shell_execute` |
| `debugger` | Executor | Troubleshooting, bug fixing | `file_*`, `shell_execute` |
| `planner` | Executor | Task decomposition, planning | - |
| `analyst` | Executor | Research, data analysis | `web_fetch` |
| `committer` | Executor | Git operations | `shell_execute` |
| `scheduler` | Executor | Job scheduling | - |

### Task Pickup Flow

Two paths exist for agents to receive work:

1. **Synchronous (Chat Handler)**: User → RPC → MessageBus → `chat.request` → Agent Loop
2. **Asynchronous (Job Queue)**: Job → SQLite Queue → Worker Pool → Agent by `agent_id`

Jobs specify `agent_id` to target a specific agent. Unassigned jobs can be claimed by any agent matching required capabilities.

## Package Structure

| Layer | Packages | Description |
|-------|----------|-------------|
| **Entry** | `cmd/meept`, `cmd/meept-daemon` | CLI and daemon entry points |
| **Server** | `internal/daemon`, `internal/rpc`, `internal/bus` | Daemon lifecycle, RPC, messaging |
| **Agent** | `internal/agent` | Agent loop, executor, conversation, planner, registry |
| **Orchestration** | `internal/queue`, `internal/task`, `internal/worker`, `internal/session` | Multi-agent job orchestration |
| **LLM** | `internal/llm` | Client, resolver, budget, providers |
| **Tools** | `internal/tools`, `internal/tools/builtin`, `internal/tools/mcp` | Tool registry and implementations |
| **Skills** | `internal/skills`, `internal/clawskills` | Skill discovery, parsing, execution |
| **Security** | `internal/security`, `pkg/security` | Permission checking, sanitization |
| **Memory** | `internal/memory` | Episodic, task, consolidation |
| **External** | `internal/comm/*`, `internal/calendar`, `internal/scheduler` | External integrations |
| **Self-Improve** | `internal/selfimprove` | Autonomous improvement system |
