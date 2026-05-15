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