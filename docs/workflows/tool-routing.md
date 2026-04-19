# Dynamic Tool Routing

## Overview
Dynamic tool routing enables Meept agents to discover and execute tools based on their capabilities and permissions. Tools are matched to agents dynamically, with caching for performance and MCP integration for external tool support.

## Problem
Without dynamic routing, agents would need hardcoded tool access, limiting flexibility and requiring code changes for new tools. Dynamic routing allows:
- Agents to discover tools at runtime
- Permission-based tool access control
- Integration of external tools via MCP
- Caching for performance optimization

## Behavior

### Tool Discovery
1. **Tool Registration**: Tools register with the system via the tool registry
2. **Agent Capability Matching**: Agents are matched to tools based on declared capabilities
3. **Permission Checking**: Security engine validates tool access permissions
4. **Caching**: Tool metadata is cached for performance

### Tool Execution Flow
```
Agent Request → Tool Registry → Security Check → Tool Execution → Result
```

### MCP Integration
- MCP servers register tools dynamically
- Tools are discovered via MCP protocol
- External tools integrate seamlessly with built-in tools

### Agent-Tool Matching
- Agents declare required capabilities
- Tools declare provided capabilities
- Registry finds optimal tool-agent matches

## Configuration

```toml
[tools]
enabled = true
cache_ttl_seconds = 300
mcp_enabled = true

[tools.mcp]
servers = [
  "~/.meept/mcp_servers.json"
]
auto_discover = true

[tools.security]
require_confirmation_high = true
require_confirmation_critical = true
block_financial = true
```

## Observability

### Logging
- Tool registration events
- Permission denials
- Execution failures
- Cache hits/misses

### Metrics
- Tool execution latency
- Cache hit rate
- Permission check results
- MCP tool discovery status

### Debug Info
- Available tools per agent
- Tool capability mappings
- MCP server connections

## Edge Cases

### Tool Not Found
- Returns clear error message
- Suggests similar tools if available
- Logs missing tool requests

### Permission Denied
- Security engine blocks execution
- Audit log records denial
- Agent receives permission error

### MCP Server Unavailable
- External tools marked as unavailable
- Automatic retry with backoff
- Graceful degradation to built-in tools

### Cache Invalidation
- Cache cleared on tool registration changes
- Manual cache clear via admin tools
- Time-based TTL for freshness