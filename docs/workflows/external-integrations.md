# External Integrations

## Overview
Meept supports external integrations including Telegram bot communication, web API access, Google Calendar management, and an MCP server for AI agent platforms. These integrations enable multi-channel interaction and external service connectivity.

## Problem
Single-channel interaction limits accessibility. External integrations provide:
- Multi-platform communication
- External service connectivity
- Flexible interaction modes
- Extended functionality
- AI agent interoperability via MCP

## Behavior

### MCP Chat Server

The MCP (Model Context Protocol) chat server exposes meept sessions to external AI agent platforms (Claude Code, GPT, etc.). It communicates via JSON-RPC over stdin/stdout and connects to the meept daemon via Unix socket RPC.

**Key features:**
- **Session management**: List, create, or attach to chat sessions
- **Message sending**: Send messages with client identity attribution (`source_client`)
- **Event polling**: Subscribe to agent progress, other participants' messages, and responses
- **Status monitoring**: Query daemon health, active agents, and queue depth
- **History access**: Retrieve recent session messages for context

**MCP tools exposed:**

| Tool | Description |
|------|-------------|
| `meept_sessions` | List, create, or attach to chat sessions |
| `meept_send` | Send a message to a session (with `source_client`) |
| `meept_events` | Poll events since last call |
| `meept_status` | Get daemon status |
| `meept_session_history` | Get recent messages from a session |

**Starting the server:**
```bash
meept mcp-chat-server
```

**Registering with Claude Code** (`~/.claude/settings.json`):
```json
{
  "mcpServers": {
    "meept": {
      "command": "meept",
      "args": ["mcp-chat-server"]
    }
  }
}
```

See [Agent Lateral Interrogation Howto](agent-lateral-interrogation-howto.md) for detailed usage patterns.

### Telegram Bot Integration
- **Two-Way Communication**: Send/receive messages via Telegram
- **Bot Interface**: Standard Telegram bot API
- **Session Management**: User session tracking
- **Security**: Authentication and authorization

### Cua-Driver Computer-Use Integration

`cua-driver` (open source, [trycua/cua](https://github.com/trycua/cua)) is a background desktop computer-use driver for macOS, Windows, and Linux. Agents drive the host desktop **without stealing the cursor**: screen/window captures come back with numbered element overlays plus an accessibility-tree index, and input is injected by element index — clicks, typing, scrolling, hotkeys all land on the target app while the user keeps working. It ships in the MCP default catalog (`config/mcp_servers.json5`) as `cua-driver`, **disabled by default**.

**Install the driver** (per OS; no administrator access required):

```bash
# macOS 14+ (installs CuaDriver.app + ~/.local/bin/cua-driver symlink)
/bin/bash -c "$(curl -fsSL https://cua.ai/driver/install.sh)"
```

```powershell
# Windows 10/11 (PowerShell; registers cua-driver-serve autostart task)
irm https://cua.ai/driver/install.ps1 | iex
cua-driver autostart kick
```

```bash
# Linux x86_64 with an X11 / XWayland desktop session (headless servers need
# a desktop session first, e.g. xfce4 under Xvfb)
/bin/bash -c "$(curl -fsSL https://cua.ai/driver/install.sh)"
```

Verify with `cua-driver --version` and `cua-driver doctor`. On macOS, grant Accessibility and Screen Recording permissions: start the daemon once (`open -n -g -a CuaDriver --args serve`), then run `cua-driver permissions grant`.

**Enable in meept** (any of the three catalog surfaces):

1. Edit `~/.meept/mcp_servers.json5`: set `enabled: true` on the `cua-driver` entry.
2. TUI: press `ctl-x o` (mcp menu), select `cua-driver`, press `e`.
3. Menubar app: settings → tools tab, toggle the switch.

Tools register under the server-name prefix — `cua-driver.capture`, `cua-driver.click`, etc. (see [MCP default catalog](tool-routing.md#mcp-default-catalog) for how namespacing works).

**Risk behavior:** every cua-driver tool call passes through the SecurityEngine ([security](security.md)) before execution:

| Tool class | Examples | Risk | Behavior |
|------------|----------|------|----------|
| Observation | `capture`, `screenshot`, `list_apps`, `list_windows`, `get_window_state` | LOW | runs without confirmation |
| Input injection | `click`, `type_text`, `hotkey`, `key`, `scroll`, `drag`, `move_*`, `wait`, `set_value` | HIGH | requires user confirmation (`require_confirmation_high`) |
| Unknown action | any unrecognized `cua-driver.*` name | HIGH | fail-closed: confirmation-gated |

The classification is prefix-matched on the registered name (`pkg/security.ComputerUseRule`); DB-seeded rules keep precedence for operator overrides. The HIGH gate means an agent cannot type or click anywhere until you approve each action unless confirmation is disabled in `[tools.security]`.

See the bundled computer-use skill for the recommended capture → act → verify loop (leaf 09 of the containment-and-computer-use plan).

### Web API Integration
- **HTTP/JSON API**: RESTful interface for external clients
- **Authentication**: API key or token-based access
- **Rate Limiting**: Request throttling
- **Documentation**: API specification available

### Google Calendar Integration
- **Event Management**: Create, read, update, delete events
- **Synchronization**: Bidirectional calendar sync
- **Reminders**: Event-based notifications
- **Permissions**: OAuth2 authentication

### Integration Architecture
- **Modular Design**: Each integration independently configurable
- **Error Handling**: Graceful degradation on service unavailability
- **Security Layers**: Authentication, authorization, input validation
- **Monitoring**: Health checks and performance metrics

## Configuration

```json5
// MCP chat server (meept as MCP server for AI agents)
"mcp_chat_server": {
  "enabled": true,
  "socket_path": "~/.meept/meept.sock",
},

// Telegram bot
"telegram": {
  "enabled": false,
  "bot_token": "",
  "webhook_url": "",
  "allowed_users": [],
},

// Web API
"web": {
  "enabled": false,
  "port": 8080,
  "api_key": "",
  "rate_limit_rpm": 60,
},

// Google Calendar
"calendar": {
  "enabled": false,
  "credentials_file": "~/.meept/calendar-credentials.json",
  "scopes": ["https://www.googleapis.com/auth/calendar"],
},

// General integration settings
"integrations": {
  "timeout_seconds": 30,
  "retry_attempts": 3,
  "health_check_interval": 60,
},
```

## Observability

### Logging
- Integration connection events
- Message send/receive operations
- Authentication attempts
- Error conditions

### Metrics
- Message processing latency
- API response times
- Connection success rate
- Resource utilization

### Debug Info
- Integration status
- Active connections
- Error rates
- Configuration settings

## Edge Cases

### Service Unavailable
- Graceful degradation
- Queued operation retry
- User notification of issues

### Authentication Failure
- Re-authentication attempts
- Clear error messages
- Security event logging

### Rate Limit Exceeded
- Request throttling
- Backoff retry logic
- User notification of limits

### Data Synchronization Conflict
- Conflict resolution strategies
- User notification of issues
- Manual resolution options

### MCP Server — Daemon Not Running
- Clear error message with remediation instructions
- Suggestion to run `meept daemon start`

### MCP Server — Unknown Tool
- Returns JSON-RPC error code `-32601` (method not found)
- Includes tool name in error message