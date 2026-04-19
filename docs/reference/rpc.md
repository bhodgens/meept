# RPC API Reference

Meept uses JSON-RPC 2.0 over Unix sockets for communication between the CLI and daemon.

## Overview

The RPC server runs on a Unix socket (default: `~/.meept/meept.sock`) and handles requests from CLI clients and other integrations.

## Connection Details

- **Protocol**: JSON-RPC 2.0
- **Transport**: Unix socket
- **Timeout**: 10 minutes per operation
- **Max idle**: 5 minutes
- **Frame format**: Length-prefixed JSON messages

## Authentication

Currently uses Unix socket permissions for authentication:
- Socket file permissions: `0600` (owner read/write only)
- Client must have access to socket file

## Methods

### Built-in Methods

#### `ping` - Health Check

Simple ping/pong for connectivity testing.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "ping",
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "pong"
}
```

#### `status` / `daemon.status` - Daemon Status

Get comprehensive daemon status information.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "status",
  "id": 2
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "status": "running",
    "version": "0.2.0-go",
    "uptime_seconds": 3600.5,
    "model": "claude-opus-4-5-20251101",
    "default_model": "claude-sonnet-4-5-20241022",
    "tokens_used": 15000,
    "tokens_remaining": 985000,
    "budget_used": 0.15,
    "budget_remaining": 9.85,
    "registered_methods": ["ping", "status", "chat.send", "chat.stream"],
    "bus_subscribers": 5
  }
}
```

#### `bus.publish` - Publish Message

Publish a message to the message bus.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "bus.publish",
  "id": 3,
  "params": {
    "topic": "agent.task.completed",
    "payload": {
      "task_id": "task-123",
      "status": "completed",
      "agent_id": "coder"
    }
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "delivered": 3
  }
}
```

#### `bus.stats` - Bus Statistics

Get message bus statistics.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "bus.stats",
  "id": 4
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "_total": 5,
    "agent.task.created": 2,
    "agent.task.completed": 1,
    "memory.stored": 10,
    "tool.executed": 50
  }
}
```

### Chat Methods

#### `chat.send` - Send Message

Send a message and get immediate response.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "chat.send",
  "id": 5,
  "params": {
    "message": "What's the weather like?",
    "session_id": "session-abc123",
    "agent_id": "chat"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "result": {
    "response": "I don't have access to real-time weather data, but I can help you find weather APIs or build a weather application.",
    "session_id": "session-abc123",
    "agent_id": "chat",
    "tokens_used": 45,
    "tool_calls": 0
  }
}
```

#### `chat.stream` - Streaming Response

Send a message and receive streaming responses.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "chat.stream",
  "id": 6,
  "params": {
    "message": "Write a Go function to calculate Fibonacci",
    "session_id": "session-abc123",
    "agent_id": "coder"
  }
}
```

**Response (streaming):**
```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "result": {
    "chunk": "Here's a Go function",
    "complete": false
  }
}

{
  "jsonrpc": "2.0",
  "id": 6,
  "result": {
    "chunk": " to calculate Fibonacci numbers",
    "complete": false
  }
}

{
  "jsonrpc": "2.0",
  "id": 6,
  "result": {
    "chunk": ":",
    "complete": true,
    "tokens_used": 120,
    "tool_calls": 2
  }
}
```

### Session Methods

#### `sessions.list` - List Sessions

Get list of active sessions.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "sessions.list",
  "id": 7
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "result": {
    "sessions": [
      {
        "id": "session-abc123",
        "created_at": "2026-04-18T10:30:00Z",
        "last_activity": "2026-04-18T10:45:00Z",
        "agent_id": "coder",
        "message_count": 15
      },
      {
        "id": "session-def456",
        "created_at": "2026-04-18T09:15:00Z",
        "last_activity": "2026-04-18T09:20:00Z",
        "agent_id": "chat",
        "message_count": 3
      }
    ],
    "count": 2
  }
}
```

#### `sessions.create` - Create Session

Create a new session.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "sessions.create",
  "id": 8,
  "params": {
    "agent_id": "coder"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 8,
  "result": {
    "session_id": "session-ghi789",
    "agent_id": "coder",
    "created_at": "2026-04-18T11:00:00Z"
  }
}
```

#### `sessions.attach` - Attach to Session

Attach to an existing session.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "sessions.attach",
  "id": 9,
  "params": {
    "session_id": "session-abc123"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 9,
  "result": {
    "session_id": "session-abc123",
    "agent_id": "coder",
    "message_count": 15,
    "last_message": "2026-04-18T10:45:00Z"
  }
}
```

### Job Methods

#### `jobs.list` - List Jobs

Get list of scheduled and running jobs.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "jobs.list",
  "id": 10
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "result": {
    "jobs": [
      {
        "id": "job-123",
        "name": "Daily backup",
        "type": "shell",
        "schedule": "0 2 * * *",
        "status": "scheduled",
        "next_run": "2026-04-19T02:00:00Z",
        "last_run": "2026-04-18T02:00:00Z",
        "created_at": "2026-04-15T10:00:00Z"
      }
    ],
    "count": 1
  }
}
```

#### `jobs.create` - Create Job

Create a new scheduled job.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "jobs.create",
  "id": 11,
  "params": {
    "name": "Health check",
    "schedule": "*/5 * * * *",
    "type": "agent",
    "prompt": "Check system health and report any issues"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "result": {
    "job_id": "job-456",
    "name": "Health check",
    "status": "scheduled",
    "next_run": "2026-04-18T11:05:00Z"
  }
}
```

#### `jobs.cancel` - Cancel Job

Cancel a scheduled job.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "jobs.cancel",
  "id": 12,
  "params": {
    "job_id": "job-123"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 12,
  "result": {
    "job_id": "job-123",
    "status": "cancelled"
  }
}
```

### Memory Methods

#### `memory.search` - Search Memory

Search long-term memory.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "memory.search",
  "id": 13,
  "params": {
    "query": "authentication patterns",
    "type": "task",
    "limit": 10,
    "min_relevance": 0.3
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 13,
  "result": {
    "results": [
      {
        "id": "mem-abc123",
        "content": "We implemented JWT authentication middleware",
        "type": "task",
        "category": "code",
        "relevance": 0.85,
        "source": "fts5"
      }
    ],
    "count": 1,
    "query": "authentication patterns"
  }
}
```

#### `memory.store` - Store Memory

Store information in long-term memory.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "memory.store",
  "id": 14,
  "params": {
    "content": "User prefers concise responses",
    "type": "episodic",
    "category": "preferences"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 14,
  "result": {
    "success": true,
    "memory_id": "mem-def456",
    "type": "episodic",
    "category": "preferences"
  }
}
```

### Task Methods

#### `tasks.list` - List Tasks

Get list of background tasks.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "tasks.list",
  "id": 15
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 15,
  "result": {
    "tasks": [
      {
        "id": "task-123",
        "name": "Fix authentication bug",
        "description": "JWT validation failing for expired tokens",
        "status": "in_progress",
        "assigned_agent": "coder",
        "created_at": "2026-04-18T09:00:00Z",
        "updated_at": "2026-04-18T10:30:00Z"
      }
    ],
    "count": 1
  }
}
```

#### `tasks.create` - Create Task

Create a new background task.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "tasks.create",
  "id": 16,
  "params": {
    "name": "Implement rate limiting",
    "description": "Add rate limiting middleware to API",
    "agent_id": "coder"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 16,
  "result": {
    "task_id": "task-456",
    "name": "Implement rate limiting",
    "status": "pending",
    "assigned_agent": "coder",
    "created_at": "2026-04-18T11:00:00Z"
  }
}
```

#### `tasks.update` - Update Task

Update task status or properties.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "tasks.update",
  "id": 17,
  "params": {
    "task_id": "task-123",
    "status": "completed",
    "notes": "Fixed by updating token expiration check"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 17,
  "result": {
    "task_id": "task-123",
    "status": "completed",
    "updated_at": "2026-04-18T11:15:00Z"
  }
}
```

#### `tasks.get` - Get Task

Get detailed task information.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "tasks.get",
  "id": 18,
  "params": {
    "task_id": "task-123"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 18,
  "result": {
    "id": "task-123",
    "name": "Fix authentication bug",
    "description": "JWT validation failing for expired tokens",
    "status": "completed",
    "assigned_agent": "coder",
    "created_at": "2026-04-18T09:00:00Z",
    "updated_at": "2026-04-18T11:15:00Z",
    "completed_at": "2026-04-18T11:15:00Z",
    "notes": "Fixed by updating token expiration check"
  }
}
```

## Error Handling

### Error Response Format

```json
{
  "jsonrpc": "2.0",
  "id": 999,
  "error": {
    "code": -32601,
    "message": "Method not found",
    "data": {
      "method": "invalid.method",
      "available_methods": ["ping", "status", "chat.send"]
    }
  }
}
```

### Common Error Codes

- `-32600` - Invalid Request
- `-32601` - Method Not Found
- `-32602` - Invalid Parameters
- `-32603` - Internal Error
- `-32700` - Parse Error

## Client Libraries

### Go Client Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net"
    "encoding/json"
)

type RPCClient struct {
    conn net.Conn
}

func NewRPCClient(socketPath string) (*RPCClient, error) {
    conn, err := net.Dial("unix", socketPath)
    if err != nil {
        return nil, err
    }
    return &RPCClient{conn: conn}, nil
}

func (c *RPCClient) Call(ctx context.Context, method string, params any) (any, error) {
    req := map[string]any{
        "jsonrpc": "2.0",
        "method": method,
        "params": params,
        "id": 1,
    }

    data, err := json.Marshal(req)
    if err != nil {
        return nil, err
    }

    // Send request and read response
    // ... implementation details

    return result, nil
}

func main() {
    client, err := NewRPCClient("/home/user/.meept/meept.sock")
    if err != nil {
        log.Fatal(err)
    }

    status, err := client.Call(context.Background(), "status", nil)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Daemon status: %v\n", status)
}
```

## Security Considerations

- RPC server only accepts connections from local users with socket file access
- All operations are subject to security engine checks
- Sensitive operations require confirmation based on risk level
- Audit logging can be enabled for compliance