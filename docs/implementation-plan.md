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

### Memory (SQLite-Primary with Optional Acceleration)

- **Architecture**: SQLite+FTS5 is the canonical, primary store for all memory subsystems. All content and metadata live in SQLite tables, providing zero-config persistence and full-text keyword search out of the box.
- **Optional acceleration**: memU (episodic) and memvid (task) are optional extras (`pip install meept[memory]`) that layer vector similarity search on top of the SQLite store when installed. Without them, FTS5 keyword search is used.
- **Episodic**: Conversation history, instructions, self-model. Stored in SQLite with FTS5 indexing. When memU is installed, provides LLM-based retrieval with higher recall.
- **Task**: Technical tasks, code, command outputs. Stored in SQLite with FTS5 per domain. When memvid is installed, provides sub-0.1ms vector search.
- **Personality**: Evolving self-model updated via LLM summarization of interactions.
- **Consolidation**: Scheduled job (every 6h) summarizes/compresses old memories.
- **Export**: CLI command to dump memories as Markdown or JSON for human review.
- **Bus integration**: MemoryManager subscribes to `memory.query` and `memory.export` bus topics, enabling JSON-RPC access from frontends.

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
| `memu-py` >=0.1.0 | Episodic memory (memU) -- **optional extra** (`meept[memory]`), falls back to SQLite+FTS5 |
| `memvid-sdk` >=2.0.0 | Task memory (memvid .mv2) -- **optional extra** (`meept[memory]`), falls back to SQLite+FTS5 |
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

## Implementation Status Summary

*Last assessed: 2026-02-04*

| Phase | Feature | Completion | Notes |
|-------|---------|------------|-------|
| 1 | Foundation | 95% | All files implemented, daemon boots |
| 2 | Communication Layer | 85% | Protocol + CLI exist; JSON-RPC bus subscribers wired for memory, security, skills, pipeline |
| 3 | Security Layer | 95% | Exceeds plan scope (added Tirith, SQLite engine) |
| 4 | Agent Loop + Tools + Skills | 90% | All subsystems present; ClawSkills added beyond plan |
| 5 | Memory Systems | 80% | SQLite-primary architecture adopted; bus subscribers wired |
| 6 | Scheduler + Calendar | 90% | Substantially complete as noted originally |
| 7 | Plugin System + MCP | 85% | Multi-transport MCP, OAuth 2.1 implemented |
| 8 | Telegram + Web Interface | 60% | Files exist; integration incomplete |
| 9 | Menubar + CLI Dashboard | 40% | CLI done; Tauri menubar barely scaffolded |
| 10 | Service + Tests + Polish | 65% | 50 test files, service templates exist |
| **Overall** | | **~80%** | |

### Resolved: Memory Architecture (Option B Adopted)

The original plan described memU/memvid as primary backends. The implementation instead uses **SQLite+FTS5 as the canonical primary store** with memU/memvid as optional acceleration layers (`pip install meept[memory]`). This decision has been formally adopted as the design direction (Option B: SQLite-primary) for the following reasons:

- **Zero-config persistence**: SQLite requires no external services and works everywhere.
- **Graceful degradation**: The system functions fully without memU/memvid installed.
- **Optional acceleration**: When memU/memvid are installed, they layer vector similarity search on top of SQLite for higher recall, without changing the storage model.

The Key Design Decisions section above has been updated to reflect this architecture.

### Unplanned Addition: ClawSkills Module

`src/meept/clawskills/` (1,249 lines) was implemented but does not appear in the original plan. Provides skill installation, indexing, and security validation for an external ClawSkills ecosystem.

---

## Implementation Phases

### Phase 1: Foundation -- 95%
Create project scaffolding, daemon lifecycle, message bus, config system, LLM client with token budget.

**Status**: All planned files exist and are substantially implemented. Core daemon lifecycle, async message bus, TOML+Markdown config loading, LLM client with httpx, token budgeting, and JSON5 provider configuration are all functional. ~985 lines in the LLM subsystem alone.

**Files**: pyproject.toml, Makefile, .gitignore, src/meept/{__init__,__main__}.py, core/{daemon,bus,config,registry}.py, llm/{client,models,budget,providers}.py, models/{messages,config_schema}.py, config/{meept.toml,constitution.md,restrictions.md,purpose.md}

**Verify**: `make install && make setup && meept-daemon` boots daemon, connects to configured LLM, responds to test prompt via internal bus.

**Remaining**: End-to-end boot verification; config reload edge cases.

### Phase 2: Communication Layer -- 75%
Unix socket server, JSON-RPC protocol, basic CLI with chat screen.

**Status**: JSON-RPC 2.0 wire format (protocol.py) is implemented. Unix socket server exists. CLI is implemented (~1,950 lines) with Textual TUI, chat screen, and widget framework. However, not all planned JSON-RPC methods (`chat`, `status`, `memory.query`, `memory.export`, `scheduler.list_jobs`, `scheduler.add_job`, `config.reload`) appear to be fully wired through the socket server to their respective subsystem handlers.

**Files**: comm/{server,protocol}.py, cli/{__init__,__main__,app}.py, cli/screens/chat.py

**Verify**: `make cli` opens TUI, type messages, receive LLM responses through daemon.

**Remaining**: Full JSON-RPC method wiring; connection stability testing; reconnect handling.

### Phase 3: Security Layer -- 95%
Input sanitization pipeline, prompt guard, action permissions, TLS cert generation.

**Status**: All planned files exist plus significant additions. 2,384 lines of security code. Notable additions beyond plan: `tirith.py` (pre-execution shell command security scanning), `engine.py` (SQLite-backed permission engine with audit logging), `seed_rules.py` (pre-populated risk rules for tools, paths, commands, financial patterns). The security system exceeds the original plan's scope.

**Files**: security/{sanitizer,prompt_guard,permissions,output_monitor,tls,engine,seed_rules,tirith}.py

**Verify**: Injection attempts (`ignore previous instructions...`) detected and blocked. Constitution/restrictions loaded and enforced.

**Remaining**: Comprehensive adversarial testing; edge case coverage for novel injection patterns.

### Phase 4: Agent Loop + Tools + Skills -- 90%
Reasoning loop (plan->execute->observe), task decomposition, built-in tools (shell, filesystem, web, scheduling, skill discovery). FrontAgent entry point, Orchestrator pipeline execution, WorkerFactory with ModelResolver, CollaborativePlanner with approval workflow, per-task git WorkspaceManager.

**Status**: All planned files exist. Agent subsystem: 2,506 lines across 8 files. Tool subsystem: 2,222 lines including all 6 built-in tools. Skills subsystem: 531 lines plus additional executor.py and dispatcher.py (not in original plan). The agent loop injects memory context before each LLM turn via `_inject_memory_context()`. DAG-based task execution, skill resolution, and context management are implemented.

**Files**: agent/{loop,planner,executor,front,orchestrator,worker_factory,collaborative_planner,workspace}.py, tools/{interface,loader}.py, tools/builtin/{shell,filesystem,web_search,web_fetch,schedule_tool,skill_tools}.py, skills/{models,registry,discovery,parser,tool_filter,executor,dispatcher}.py, llm/resolver.py, config/models.json5, models/tasks.py

**Verify**: Ask agent to read a file -> plans the action -> checks permissions -> executes -> returns result. Skills discoverable via skill_find tool. Capability matching selects correct model.

**Remaining**: Full end-to-end integration testing of plan->execute->observe cycle; multi-step task chaining verification.

### Phase 5: Memory Systems -- 70%
memU episodic memory, memvid task memory, personality model, consolidation, human export tools.

**Status**: All planned files exist and are substantial (2,303 lines total): manager.py (399), episodic.py (490), task_memory.py (526), personality.py (252), consolidation.py (255), export.py (251), memory_types.py (130). Personality evolution, consolidation scheduling, and Markdown/JSON export all work.

**However, the architecture deviates significantly from the plan** (see Critical Note above). memU and memvid are optional extras, not core dependencies. SQLite+FTS5 is the primary store and search engine. The planned "human-readable Markdown" runtime storage and "LLM-based retrieval (92% accuracy)" via memU are not active in a default installation. The Memory (Hybrid) design in the Key Design Decisions section does not accurately describe what was built.

**Files**: memory/{manager,episodic,task_memory,personality,consolidation,export}.py, models/memory_types.py

**Verify**: Converse, restart daemon, agent recalls prior conversation. Store technical task, search for it. Export as Markdown.

**Remaining**: Test LLM-based retrieval quality when memU IS installed. Verify cross-restart memory persistence end-to-end.

### Phase 6: Scheduler + Calendar -- 90% *(substantially complete)*
APScheduler integration with fallback scheduler, job definitions, DAG pipeline execution, Google Calendar read/write.

**Status**: All planned components implemented (1,076 lines for scheduler). The phase was already marked as substantially complete in the original plan.

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

**Remaining**: Google Calendar end-to-end OAuth flow testing; pipeline error recovery edge cases.

### Phase 7: Plugin System + MCP -- 85%
Plugin loading from disk, MCP server management with sanitized tool output. Full remote MCP server support.

**Status**: MCP manager, client, and auth files exist. Multiple transport support implemented: Local (stdio), Remote Streamable HTTP, Remote raw HTTP fallback, WebSocket. OAuth 2.1 with PKCE and client credentials flows. Auto-reconnection with exponential backoff. Plugin framework with manifest-based discovery.

**Transports**: Local (stdio), Remote Streamable HTTP (SDK), Remote raw HTTP (no SDK fallback), WebSocket (SDK).

**OAuth 2.1**: SDK's `OAuthClientProvider` with `FileTokenStorage` at `~/.meept/mcp-auth/` (0600 permissions). Supports authorization code + PKCE and client credentials (M2M) flows.

**Auto-reconnection**: Exponential backoff with jitter (1s initial, 30s max, 5 retries). Triggered by SSE listener disconnect (raw mode) or connection errors.

**Server-initiated requests**: GET SSE stream in raw mode (background listener task); SDK handles internally for streamable HTTP and WebSocket.

**Config**: opencode convention with `oauth` field (dict for OAuth config, `false` to disable, absent for default). `headers` for static bearer auth.

**Dependencies**: `httpx-sse>=0.4` (core, raw SSE parsing), `websockets>=16.0` (optional with `mcp`).

**Files**: tools/{mcp_manager,mcp_client,mcp_auth}.py, plugins/example_plugin/{meept.plugin.json,__init__}.py, config/mcp_servers.json

**Verify**: Example plugin loads, tool appears in agent's available tools. Local MCP server starts and tools work. Remote HTTP server connects and discovers tools. OAuth flow triggers and tokens persist. WebSocket server connects. Auto-reconnect fires on disconnect. Raw HTTP fallback works without SDK.

**Remaining**: Example plugin documentation; third-party plugin testing; MCP output sanitization verification.

### Phase 8: Telegram + Web Interface -- 60%
Telegram bot (creator-only), FastAPI web UI with OAuth/JWT.

**Status**: All planned files exist (telegram_bot.py, web/app.py, web/auth.py, web/routes.py). Basic structure is in place but end-to-end integration between frontends and the daemon's JSON-RPC server is incomplete. The Telegram bot and web interface have routing and auth scaffolding but lack full message flow testing through the daemon.

**Files**: comm/telegram_bot.py, comm/web/{app,auth,routes}.py

**Verify**: Telegram message -> response. Web login -> chat via browser.

**Remaining**: Full Telegram message->daemon->LLM->response flow; Web UI OAuth login flow; WebSocket or SSE streaming for real-time chat; frontend polish.

### Phase 9: Menubar + CLI Dashboard -- 40%
Tauri menubar app (system tray with popover UI showing status/metrics/chat), full CLI dashboard.

**Status**: The CLI dashboard portion is largely complete (~1,950 lines) with Textual TUI screens (dashboard, chat, memory browser, tasks) and widgets (metrics, task list, status bar). The Tauri macOS menubar app is **barely scaffolded** -- minimal Python backend exists but the Rust/JS frontend (main.rs, index.html, main.js, style.css) and Tauri configuration (Cargo.toml, tauri.conf.json, tray icons) have not been built.

**Files**: menubar/src-tauri/{Cargo.toml,src/main.rs,tauri.conf.json,icons/}, menubar/src/{index.html,main.js,style.css}, menubar/package.json, cli/screens/{dashboard,memory_browser,tasks}.py, cli/widgets/{metrics,task_list,status_bar}.py

**Verify**: Menubar tray icon shows green on task completion, orange when input needed. Popover shows live status. Dashboard displays metrics.

**Remaining**: Entire Tauri app build (Rust backend, JS frontend, tray icons, IPC to daemon). CLI dashboard integration testing with live daemon.

### Phase 10: Service + Tests + Polish -- 65%
launchd/systemd service files, full test suite, README, .env.example.

**Status**: Service file templates exist (meept.service at 12 lines, com.meept.daemon.plist at 21 lines) but require variable substitution and installation scripting. Test suite has 50 files covering all major subsystems (core, llm, memory, scheduler, security, tools, comm, agent, skills, clawskills). README exists. Overall test pass rate and coverage depth have not been independently verified.

**Files**: service/{com.meept.daemon.plist,meept.service}, .env.example, tests/**, README.md

**Verify**: `make install-service` -> meept runs at login. `make test` passes. `make uninstall` cleans up.

**Remaining**: `make install-service` / `make uninstall` automation; full CI test pass verification; .env.example completeness; README accuracy review.

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
