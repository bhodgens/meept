# Meept: Self-Executing Autonomous Bot - Implementation Plan

## Assumptions
- **Language**: Python 3.12+ (best ecosystem fit for memU, memvid, APScheduler, Telegram, Google Calendar)
- **OpenClaw**: Inspiration only for plugin compatibility; not a dependency or fork
- **LLM Endpoint**: Configurable - any OpenAI-compatible API (Ollama, OpenRouter, vLLM, LiteLLM)
- **Platform**: Core cross-platform (Linux/macOS), menubar macOS-specific
- **Repo**: Empty git repo, fresh start

---

## Architecture Overview

```
                         ┌─────────────┐
                         │  Menubar    │  (Tauri, macOS)
                         │  (status)   │
                         └──────┬──────┘
                                │ Unix socket
┌──────────┐  ┌──────────┐     │     ┌──────────┐
│ CLI/TUI  │  │ Telegram │     │     │ Web UI   │
│ (textual)│  │ (creator)│     │     │ (FastAPI) │
└────┬─────┘  └────┬─────┘     │     └────┬─────┘
     │             │           │          │
     └─────────────┴───────┬───┴──────────┘
                           │ JSON-RPC / Unix socket
                    ┌──────┴──────┐
                    │  CommServer │
                    └──────┬──────┘
                           │
                    ┌──────┴──────┐
                    │  MessageBus │  (async pub/sub)
                    └──────┬──────┘
                           │
          ┌────────────────┼────────────────┐
          │                │                │
   ┌──────┴─────┐  ┌──────┴──────┐  ┌──────┴──────┐
   │   Agent    │  │  Scheduler  │  │  Security   │
   │ (front/    │  │ (APScheduler│  │ (sanitize/  │
   │  orch/     │  │  +pipelines)│  │  guard/perm)│
   │  workers)  │  └─────────────┘  └─────────────┘
   └──────┬─────┘
          │
   ┌──────┴──────┐
   │  LLM Client │ ← OpenAI-compatible, JSON5 config, capability matching
   └──────┬──────┘
          │
   ┌──────┴──────────────────┐
   │    Memory Manager       │
   │  ┌──────────┐ ┌───────┐│
   │  │ Episodic │ │ Task  ││
   │  │ (memU)   │ │(memvid││
   │  └──────────┘ └───────┘│
   │  ┌──────────┐ ┌───────┐│
   │  │Personality│ │Export ││
   │  └──────────┘ └───────┘│
   └─────────────────────────┘
          │
   ┌──────┴──────┐
   │ Tool/Plugin │
   │   Registry  │
   │ ┌─────────┐ │
   │ │Built-in │ │
   │ │Plugins  │ │
   │ │MCP svrs │ │
   │ └─────────┘ │
   └─────────────┘
```

---

## Project Structure

```
meept/
├── Makefile
├── pyproject.toml
├── .gitignore
├── .env.example
├── config/
│   ├── constitution.md          # Guiding principles
│   ├── restrictions.md          # Safety restrictions
│   ├── purpose.md               # Technical task principles
│   ├── meept.toml               # Runtime config (TOML)
│   ├── models.json5             # Model/provider config (JSON5)
│   └── mcp_servers.json         # MCP server definitions
├── src/meept/
│   ├── __init__.py
│   ├── __main__.py              # Entry: meept-daemon / python -m meept
│   ├── core/
│   │   ├── daemon.py            # Daemon lifecycle, asyncio event loop
│   │   ├── bus.py               # In-process async pub/sub message bus
│   │   ├── config.py            # TOML + .md config loader
│   │   └── registry.py          # Component registry + dependency injection
│   ├── llm/
│   │   ├── client.py            # Unified OpenAI-compatible client
│   │   ├── models.py            # ChatMessage, LLMResponse, ModelConfig, TokenUsage
│   │   ├── budget.py            # Token budget (hourly/daily limits, rate limiting)
│   │   ├── providers.py         # JSON5 config loading, ModelsConfig, provider definitions
│   │   └── resolver.py          # ModelResolver: capability-based model selection
│   ├── memory/
│   │   ├── manager.py           # Orchestrates episodic + task subsystems
│   │   ├── episodic.py          # memU integration (conversation, instructions, self)
│   │   ├── task_memory.py       # memvid integration (.mv2, sub-ms search)
│   │   ├── personality.py       # Self-model evolution
│   │   ├── consolidation.py     # Periodic summarization & optimization
│   │   └── export.py            # Human-reviewable Markdown/JSON export
│   ├── scheduler/
│   │   ├── scheduler.py         # APScheduler (AsyncIOScheduler) wrapper
│   │   ├── jobs.py              # Job definitions
│   │   └── pipelines.py         # Multi-step DAG pipeline execution
│   ├── calendar/
│   │   ├── gcal.py              # Google Calendar API (read/write events)
│   │   └── auth.py              # Google OAuth 2.0 credential management
│   ├── security/
│   │   ├── sanitizer.py         # Input sanitization (pattern + optional LLM filter)
│   │   ├── prompt_guard.py      # Prompt structuring with boundary markers
│   │   ├── output_monitor.py    # Output validation
│   │   ├── permissions.py       # Risk-level action gating (SAFE→CRITICAL)
│   │   └── tls.py               # Self-signed TLS cert generation
│   ├── tools/
│   │   ├── interface.py         # Tool ABC, ToolDefinition, ToolRegistry
│   │   ├── loader.py            # Plugin discovery from ~/.meept/plugins/
│   │   ├── mcp_manager.py       # MCP server lifecycle (disabled by default)
│   │   ├── mcp_client.py        # MCP tool call routing
│   │   └── builtin/
│   │       ├── shell.py         # Sandboxed shell execution
│   │       ├── filesystem.py    # Permission-gated file R/W
│   │       ├── web_search.py    # Web search
│   │       ├── web_fetch.py     # URL content fetching
│   │       ├── schedule_tool.py # Agent-invocable scheduling
│   │       └── skill_tools.py   # skill_find, skill_use, skill_resource tools
│   ├── skills/
│   │   ├── models.py            # SkillDefinition dataclass (requires, from_parsed)
│   │   ├── registry.py          # SkillRegistry with capability queries
│   │   ├── discovery.py         # 3-tier SKILL.md filesystem discovery (SkillIndex)
│   │   ├── parser.py            # YAML frontmatter + Markdown parser
│   │   └── tool_filter.py       # FilteredToolRegistry for skill-scoped tools
│   ├── agent/
│   │   ├── loop.py              # Main reasoning/action loop
│   │   ├── planner.py           # Task decomposition & planning
│   │   ├── executor.py          # Action execution with safety checks
│   │   ├── front.py             # FrontAgent: entry point, routes chat requests
│   │   ├── orchestrator.py      # Bridges task plans to PipelineExecutor
│   │   ├── worker_factory.py    # Creates workers with ModelResolver
│   │   ├── collaborative_planner.py  # Plan-review-approve workflow
│   │   └── workspace.py         # Per-task git workspaces
│   ├── comm/
│   │   ├── server.py            # Unix socket server (JSON-RPC 2.0)
│   │   ├── protocol.py          # JsonRpcRequest/Response wire format
│   │   ├── telegram_bot.py      # python-telegram-bot (creator-only auth)
│   │   └── web/
│   │       ├── app.py           # FastAPI (disabled by default)
│   │       ├── auth.py          # OAuth2 + JWT
│   │       └── routes.py        # API routes
│   └── models/
│       ├── messages.py          # MessageType enum, BusMessage
│       ├── tasks.py             # Task/Job data models
│       ├── memory_types.py      # MemoryItem, MemoryResult, MemoryQuery
│       └── config_schema.py     # Pydantic/dataclass config schemas
├── cli/
│   ├── __main__.py              # Entry: meept
│   ├── app.py                   # Textual TUI app
│   ├── screens/
│   │   ├── dashboard.py         # Metrics, recent tasks, status panels
│   │   ├── chat.py              # Chat interaction
│   │   ├── memory_browser.py    # Memory inspection
│   │   └── tasks.py             # Job/task monitoring
│   └── widgets/
│       ├── metrics.py, task_list.py, status_bar.py
├── menubar/                         # Tauri macOS menubar app
│   ├── src-tauri/
│   │   ├── Cargo.toml           # Tauri Rust backend
│   │   ├── src/main.rs          # Tauri app entry + Unix socket IPC to daemon
│   │   ├── tauri.conf.json      # Tauri config (system tray, no main window)
│   │   └── icons/               # Tray icons (idle/working/green/orange)
│   ├── src/                     # Web frontend (HTML/CSS/JS)
│   │   ├── index.html           # Menubar popover UI
│   │   ├── main.js              # Status polling, chat, metrics display
│   │   └── style.css
│   └── package.json             # Frontend build deps
├── plugins/
│   └── example_plugin/
│       ├── meept.plugin.json    # Plugin manifest
│       └── __init__.py          # register(registry) entry point
├── service/
│   ├── com.meept.daemon.plist   # macOS launchd
│   └── meept.service            # Linux systemd
└── tests/
    ├── conftest.py
    ├── test_core/, test_llm/, test_memory/, test_scheduler/
    ├── test_security/, test_tools/, test_comm/, test_agent/
    ├── test_skills/
```

---

## Key Design Decisions

### Communication
- **Daemon <-> Frontends**: JSON-RPC 2.0 over Unix socket (`~/.meept/meept.sock`), permissions 0600
- Methods: `chat`, `status`, `memory.query`, `memory.export`, `scheduler.list_jobs`, `scheduler.add_job`, `config.reload`
- TLS optional for TCP (web interface); Unix socket handles local security via file permissions

### Memory (Hybrid)
- **Episodic (memU)**: Conversation history, instructions, self-model. Stores as human-readable Markdown. LLM-based retrieval (92% accuracy). **SQLite metadata store** (file-based, zero-config persistence via custom adapter wrapping memU's metadata layer).
- **Task (memvid)**: Technical tasks, code, command outputs. `.mv2` binary format, sub-0.1ms search. Separate files per domain.
- **Personality**: Evolving self-model updated via LLM summarization of interactions.
- **Consolidation**: Scheduled job (every 6h) summarizes/compresses old memories.
- **Export**: CLI command to dump memories as Markdown or JSON for human review.

### Security
- **Layer 1**: Regex pattern detection for known injection patterns (fast, zero cost)
- **Layer 2**: Structural sanitization (strip role markers, escape special tokens)
- **Layer 3**: Optional LLM-based classification for external/untrusted data sources
- **Prompt guard**: All user/tool inputs wrapped in explicit boundary markers
- **Action permissions**: RiskLevel enum (SAFE->CRITICAL). HIGH/CRITICAL always require confirmation. Financial actions always blocked.
- **MCP outputs**: Sanitized before reaching agent context

### Tool/Plugin System
- Tools implement `Tool` ABC with `definition()` -> `ToolDefinition` and `execute(**kwargs) -> dict`
- `ToolDefinition.to_openai_schema()` converts to OpenAI function-calling format
- Plugins: directory with `meept.plugin.json` manifest + Python module exporting `register(registry)`
- MCP servers: OpenCode-style JSON config, disabled by default, started on demand

### LLM Client & Model Configuration
- Single `LLMClient` class using `httpx.AsyncClient` speaking OpenAI `/v1/chat/completions`
- **JSON5 configuration** (`config/models.json5`): Providers define base URLs and auth; models nested under providers with `provider/model-id` reference format
- `ModelConfig` per model: base_url, model_id, api_key, cost estimate, capabilities, context_limit, provider_id
- **Capability matching**: Models declare `capabilities` tags (e.g. `code`, `reasoning`, `vision`, `tool_use`, `long_context`, `fast`, `cheap`); skills declare `requires` tags; `ModelResolver.resolve_for_skill()` picks cheapest capable model
- `TokenBudget`: hourly/daily limits, per-minute rate limiting, configurable aggressiveness (0.0-1.0)
- Model switching at runtime via `client.switch_model(config)` or `create_client_from_resolved()`
- Environment variable expansion (`${VAR}`) in JSON5 config values

### Skills System
- **SKILL.md format**: YAML frontmatter (name, description, requires, allowed-tools, risk-level, max-iterations) + Markdown body (instructions/examples)
- **3-tier discovery** (highest priority wins):
  1. `.meept/skills/` (project-local)
  2. `~/.meept/skills/` (user-global)
  3. `~/.config/meept/skills/` (system-wide)
- **Capability/requires matching**: Skills declare `requires` tags; `ModelResolver` finds cheapest model whose `capabilities` superset covers the requirements
- **LLM-driven discovery**: No triage agent; LLM discovers and activates skills via `skill_find`, `skill_use`, `skill_resource` tool calls
- **SkillIndex**: Lazy-loaded filesystem index with shadowing across tiers
- **SkillRegistry**: In-memory registry with `find_by_capabilities()` and `get_requirements()` queries

---

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `httpx` >=0.27 | HTTP client for LLM APIs |
| `pyyaml` >=6.0 | SKILL.md YAML frontmatter parsing |
| `memu-py` >=0.1.0 | Episodic memory (memU) |
| `memvid-sdk` >=2.0.0 | Task memory (memvid .mv2) |
| `apscheduler` >=3.11 | Job scheduling |
| `google-api-python-client` >=2.100 | Google Calendar |
| `google-auth-oauthlib` >=1.0 | Google OAuth 2.0 |
| `python-telegram-bot` >=22.0 | Telegram integration |
| `fastapi` >=0.115 | Web API framework |
| `uvicorn[standard]` >=0.30 | ASGI server |
| `pyjwt` >=2.8 | JWT tokens |
| `textual` >=7.0 | Terminal UI |
| `mcp` >=1.25 | MCP Python SDK |
| `cryptography` >=42.0 | TLS/crypto |
| Tauri 2.x (Rust/JS) | macOS menubar app (richer UI than rumps, lighter than Electron) |
| `aiosqlite` >=0.20 | Async SQLite for memU metadata persistence |

---

## Implementation Phases

### Phase 1: Foundation
Create project scaffolding, daemon lifecycle, message bus, config system, LLM client with token budget.

**Files**: pyproject.toml, Makefile, .gitignore, src/meept/{__init__,__main__}.py, core/{daemon,bus,config,registry}.py, llm/{client,models,budget,providers}.py, models/{messages,config_schema}.py, config/{meept.toml,constitution.md,restrictions.md,purpose.md}

**Verify**: `make install && make setup && meept-daemon` boots daemon, connects to configured LLM, responds to test prompt via internal bus.

### Phase 2: Communication Layer
Unix socket server, JSON-RPC protocol, basic CLI with chat screen.

**Files**: comm/{server,protocol}.py, cli/{__init__,__main__,app}.py, cli/screens/chat.py

**Verify**: `make cli` opens TUI, type messages, receive LLM responses through daemon.

### Phase 3: Security Layer
Input sanitization pipeline, prompt guard, action permissions, TLS cert generation.

**Files**: security/{sanitizer,prompt_guard,permissions,output_monitor,tls,engine,seed_rules}.py

**Verify**: Injection attempts (`ignore previous instructions...`) detected and blocked. Constitution/restrictions loaded and enforced.

### Phase 4: Agent Loop + Tools + Skills
Reasoning loop (plan->execute->observe), task decomposition, built-in tools (shell, filesystem, web, scheduling, skill discovery). FrontAgent entry point, Orchestrator pipeline execution, WorkerFactory with ModelResolver, CollaborativePlanner with approval workflow, per-task git WorkspaceManager.

**Files**: agent/{loop,planner,executor,front,orchestrator,worker_factory,collaborative_planner,workspace}.py, tools/{interface,loader}.py, tools/builtin/{shell,filesystem,web_search,web_fetch,schedule_tool,skill_tools}.py, skills/{models,registry,discovery,parser,tool_filter}.py, llm/resolver.py, config/models.json5, models/tasks.py

**Verify**: Ask agent to read a file -> plans the action -> checks permissions -> executes -> returns result. Skills discoverable via skill_find tool. Capability matching selects correct model.

### Phase 5: Memory Systems
memU episodic memory, memvid task memory, personality model, consolidation, human export tools.

**Files**: memory/{manager,episodic,task_memory,personality,consolidation,export}.py, models/memory_types.py

**Verify**: Converse, restart daemon, agent recalls prior conversation. Store technical task, search for it. Export as Markdown.

### Phase 6: Scheduler + Calendar *(substantially complete)*
APScheduler integration with fallback scheduler, job definitions, DAG pipeline execution, Google Calendar read/write.

**Components implemented**:
- APScheduler wrapper (`MeeptScheduler`) with fallback for APScheduler-free installs
- Retry with exponential backoff on job handlers
- Bus-based RPC (`scheduler.list_jobs`, `scheduler.add_job` subscribers)
- Agent job publishing (`CHAT_REQUEST` via `add_agent_job`)
- `schedule_tool`: agents can schedule future work via tool calls
- `PipelineExecutor`: DAG execution, parallel steps, per-step timeout/retry, cancellation, context passing, bus progress events
- Built-in maintenance jobs: memory consolidation (6h), personality update (daily), health check (5min), budget reset (midnight UTC)

**Files**: scheduler/{scheduler,jobs,pipelines}.py, calendar/{gcal,auth}.py

**Verify**: Memory consolidation runs on schedule. Calendar events listed/created via agent. Pipeline DAG executes steps in dependency order.

### Phase 7: Plugin System + MCP
Plugin loading from disk, MCP server management with sanitized tool output. Full remote MCP server support.

**Transports**: Local (stdio), Remote Streamable HTTP (SDK), Remote raw HTTP (no SDK fallback), WebSocket (SDK).

**OAuth 2.1**: SDK's `OAuthClientProvider` with `FileTokenStorage` at `~/.meept/mcp-auth/` (0600 permissions). Supports authorization code + PKCE and client credentials (M2M) flows.

**Auto-reconnection**: Exponential backoff with jitter (1s initial, 30s max, 5 retries). Triggered by SSE listener disconnect (raw mode) or connection errors.

**Server-initiated requests**: GET SSE stream in raw mode (background listener task); SDK handles internally for streamable HTTP and WebSocket.

**Config**: opencode convention with `oauth` field (dict for OAuth config, `false` to disable, absent for default). `headers` for static bearer auth.

**Dependencies**: `httpx-sse>=0.4` (core, raw SSE parsing), `websockets>=16.0` (optional with `mcp`).

**Files**: tools/{mcp_manager,mcp_client,mcp_auth}.py, plugins/example_plugin/{meept.plugin.json,__init__}.py, config/mcp_servers.json

**Verify**: Example plugin loads, tool appears in agent's available tools. Local MCP server starts and tools work. Remote HTTP server connects and discovers tools. OAuth flow triggers and tokens persist. WebSocket server connects. Auto-reconnect fires on disconnect. Raw HTTP fallback works without SDK.

### Phase 8: Telegram + Web Interface
Telegram bot (creator-only), FastAPI web UI with OAuth/JWT.

**Files**: comm/telegram_bot.py, comm/web/{app,auth,routes}.py

**Verify**: Telegram message -> response. Web login -> chat via browser.

### Phase 9: Menubar + CLI Dashboard
Tauri menubar app (system tray with popover UI showing status/metrics/chat), full CLI dashboard.

**Files**: menubar/src-tauri/{Cargo.toml,src/main.rs,tauri.conf.json,icons/}, menubar/src/{index.html,main.js,style.css}, menubar/package.json, cli/screens/{dashboard,memory_browser,tasks}.py, cli/widgets/{metrics,task_list,status_bar}.py

**Verify**: Menubar tray icon shows green on task completion, orange when input needed. Popover shows live status. Dashboard displays metrics.

### Phase 10: Service + Tests + Polish
launchd/systemd service files, full test suite, README, .env.example.

**Files**: service/{com.meept.daemon.plist,meept.service}, .env.example, tests/**, README.md

**Verify**: `make install-service` -> meept runs at login. `make test` passes. `make uninstall` cleans up.

---

## Verification (End-to-End)

1. `make install && make setup` - installs deps, creates ~/.meept/ config
2. Edit `~/.meept/models.json5` with LLM endpoint (e.g. Ollama at localhost:11434)
3. `make start` - daemon starts in background
4. `make cli` - open TUI, chat with agent, verify LLM responses
5. Ask agent to remember something, restart daemon, verify recall
6. Test security: try prompt injection, verify it's blocked
7. Test skill discovery: place SKILL.md in `~/.meept/skills/`, verify `skill_find` returns it
8. Test capability matching: configure two models with different capabilities, verify correct model selected
9. `cd menubar && cargo tauri build` (or `make menubar`) - verify tray icon appears and reflects daemon status
10. `make test` - all tests pass
