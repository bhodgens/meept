# Tool Reference

Meept provides a comprehensive set of built-in tools that agents can use to interact with the system, filesystem, web, memory, and more.

## Overview

Tools are the primary mechanism by which LLM agents interact with the system. Each tool defines its parameters via JSON Schema and implements an Execute method.

## Tool Interface

All tools implement the `Tool` interface:

```go
type Tool interface {
    Name() string                    // Unique identifier (snake_case)
    Description() string            // Human-readable description
    Parameters() llm.FunctionParameters // JSON Schema parameters
    Execute(ctx context.Context, args map[string]any) (any, error)
}
```

## Baseline Tools

These tools are available to all agents:

### Memory Tools

#### `memory_store` - Store Information

Store information in long-term memory for future reference.

**Description:** "Store information in long-term memory for future reference. Use this to save important facts, decisions, learnings, or context that should be remembered across conversations."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "content": {
      "type": "string",
      "description": "The content to store in memory. Should be clear and self-contained."
    },
    "type": {
      "type": "string",
      "description": "Memory type: 'episodic' for conversations/interactions, 'task' for technical knowledge.",
      "enum": ["episodic", "task"]
    },
    "category": {
      "type": "string",
      "description": "Optional category to organize the memory (e.g., 'conversation', 'code', 'decision')."
    }
  },
  "required": ["content", "type"]
}
```

**Example:**
```json
{
  "content": "The user prefers concise technical explanations",
  "type": "episodic",
  "category": "preferences"
}
```

#### `memory_search` - Search Memories

Search memories for relevant past context.

**Description:** "Search memories for relevant past context. Use this to find information that was previously stored, such as past conversations, decisions, or learnings."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Search query to find relevant memories."
    },
    "type": {
      "type": "string",
      "description": "Optional: filter to 'episodic' or 'task' memories only.",
      "enum": ["episodic", "task", ""]
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of results to return (default 10, max 50)."
    },
    "min_relevance": {
      "type": "number",
      "description": "Minimum relevance score 0.0-1.0 (default 0.3)."
    }
  },
  "required": ["query"]
}
```

#### `memory_get_context` - Get Context

Get contextually relevant memories for a query.

**Description:** "Get contextually relevant memories for a query. This performs a smart search across all memory types to gather the most helpful context for understanding or responding to a topic."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "The query or topic to get relevant context for."
    },
    "max_items": {
      "type": "integer",
      "description": "Maximum number of context items to return (default 10, max 30)."
    }
  },
  "required": ["query"]
}
```

### Task Tools

#### `task_create` - Create Task

Create a new background task.

**Description:** "Create a new background task for execution by specialist agents."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "Task name/description."
    },
    "description": {
      "type": "string",
      "description": "Detailed task description."
    },
    "agent_id": {
      "type": "string",
      "description": "Optional: specific agent to assign (e.g., 'coder', 'planner')."
    }
  },
  "required": ["name"]
}
```

#### `task_get` - Get Task

Get detailed task information.

**Description:** "Get detailed information about a specific task."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "task_id": {
      "type": "string",
      "description": "ID of the task to retrieve."
    }
  },
  "required": ["task_id"]
}
```

#### `task_list` - List Tasks

List all tasks.

**Description:** "Get list of all background tasks."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "status": {
      "type": "string",
      "description": "Filter by status: pending, in_progress, completed, failed."
    },
    "agent_id": {
      "type": "string",
      "description": "Filter by assigned agent."
    }
  },
  "required": []
}
```

#### `task_update` - Update Task

Update task status or properties.

**Description:** "Update task status or add notes/comments."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "task_id": {
      "type": "string",
      "description": "ID of the task to update."
    },
    "status": {
      "type": "string",
      "description": "New status: pending, in_progress, completed, failed."
    },
    "notes": {
      "type": "string",
      "description": "Additional notes or comments."
    }
  },
  "required": ["task_id"]
}
```

### Platform Tools

#### `platform_status` - Platform Status

Get current platform status including uptime and component health.

**Description:** "Get current meept platform status including uptime and component health."

**Parameters:**
```json
{
  "type": "object",
  "properties": {},
  "required": []
}
```

#### `platform_agents` - List Agents

List available agent specifications.

**Description:** "List available agent specifications with their IDs, names, roles, and purposes."

**Parameters:**
```json
{
  "type": "object",
  "properties": {},
  "required": []
}
```

#### `platform_tools` - List Tools

List registered tools with their names and descriptions.

**Description:** "List all registered tools with their names and descriptions."

**Parameters:**
```json
{
  "type": "object",
  "properties": {},
  "required": []
}
```

#### `delegate_task` - Delegate Task

Delegate a task to a specific specialist agent.

**Description:** "Delegate a task to a specific specialist agent by ID. Returns the agent's response. Use platform_agents first to discover available agents."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "agent_id": {
      "type": "string",
      "description": "The ID of the agent to delegate to (e.g., 'coder', 'planner', 'analyst')."
    },
    "message": {
      "type": "string",
      "description": "The message/task to send to the agent."
    },
    "context": {
      "type": "string",
      "description": "Optional additional context to provide to the agent."
    }
  },
  "required": ["agent_id", "message"]
}
```

## Additional Tools

These tools are available to specific agents based on their roles.

### Filesystem Tools

#### `file_read` - Read File

Read the contents of a file.

**Description:** "Read the contents of a file at the given path. Returns the text content. Optionally read a specific line range."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute or ~-prefixed path to the file."
    },
    "offset": {
      "type": "integer",
      "description": "Line number to start reading from (1-based, optional)."
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of lines to read (optional)."
    }
  },
  "required": ["path"]
}
```

**Limits:**
- Max file size: 5 MB
- Max lines: 500

#### `file_write` - Write File

Write text content to a file.

**Description:** "Write text content to a file. Creates the file if it does not exist, overwrites if it does. Parent directories are created automatically."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute or ~-prefixed path to the file."
    },
    "content": {
      "type": "string",
      "description": "The text content to write."
    },
    "append": {
      "type": "boolean",
      "description": "If true, append instead of overwrite (default false)."
    }
  },
  "required": ["path", "content"]
}
```

**Limits:**
- Max content size: 10 MB

#### `file_delete` - Delete File

Delete a file from the filesystem.

**Description:** "Delete a file at the given path. This is a destructive operation and cannot be undone."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute or ~-prefixed path to the file to delete."
    }
  },
  "required": ["path"]
}
```

#### `list_directory` - List Directory

List files and directories at the given path.

**Description:** "List files and directories at the given path. Returns names, types, and sizes."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute or ~-prefixed path to the directory."
    },
    "recursive": {
      "type": "boolean",
      "description": "If true, list recursively (default false)."
    },
    "max_entries": {
      "type": "integer",
      "description": "Maximum number of entries to return (default 200)."
    }
  },
  "required": ["path"]
}
```

**Limits:**
- Max entries: 500

### Shell Tools

#### `shell_execute` - Execute Shell Command

Execute a shell command and return its stdout and stderr.

**Description:** "Execute a shell command and return its stdout and stderr. Use for running system commands, scripts, and CLI tools. Commands run in a sandboxed subprocess with a timeout."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "command": {
      "type": "string",
      "description": "The shell command to execute."
    },
    "timeout": {
      "type": "number",
      "description": "Timeout in seconds (default 30)."
    },
    "working_dir": {
      "type": "string",
      "description": "Working directory for the command (optional)."
    }
  },
  "required": ["command"]
}
```

**Security:**
- Blocks dangerous commands (rm, shutdown, etc.)
- Rate limiting and timeout protection
- Output size limits (50KB)

### Web Tools

#### `web_fetch` - Fetch Web Page

Fetch content from a URL.

**Description:** "Fetch content from a URL and return the response body. Useful for accessing web APIs, documentation, or web pages."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "url": {
      "type": "string",
      "description": "The URL to fetch."
    },
    "method": {
      "type": "string",
      "description": "HTTP method: GET, POST, PUT, DELETE (default GET)."
    },
    "headers": {
      "type": "object",
      "description": "HTTP headers to include."
    },
    "body": {
      "type": "string",
      "description": "Request body for POST/PUT requests."
    }
  },
  "required": ["url"]
}
```

**Limits:**
- Max response size: 10 MB
- Timeout: 30 seconds

#### `web_search` - Web Search

Search the web using DuckDuckGo.

**Description:** "Search the web using DuckDuckGo and return results with titles, URLs, and snippets. Useful for finding current information, researching topics, and discovering relevant web pages."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "The search query string."
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of results to return (default 10, max 30)."
    }
  },
  "required": ["query"]
}
```

**Features:**
- No API key required
- Rate limiting (500ms between requests)
- Automatic HTML entity decoding
- URL cleaning and validation

### Scheduling Tools

#### `schedule_create` - Create Schedule

Create a new scheduled job.

**Description:** "Create a new scheduled job. Jobs run on a cron-like schedule and can trigger agent tasks, shell commands, reminders, or other recurring operations."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "Human-readable name for the job."
    },
    "schedule": {
      "type": "string",
      "description": "Cron expression for when to run the job (e.g., '0 9 * * *' for daily at 9am, '*/5 * * * *' for every 5 minutes). Supports 6-field cron with seconds."
    },
    "job_type": {
      "type": "string",
      "description": "Type of job: agent (runs an agent prompt), shell (executes a shell command), reminder (sends a reminder message).",
      "enum": ["agent", "shell", "reminder"]
    },
    "prompt": {
      "type": "string",
      "description": "For agent jobs: the prompt/message to send to the agent."
    },
    "command": {
      "type": "string",
      "description": "For shell jobs: the shell command to execute."
    },
    "message": {
      "type": "string",
      "description": "For reminder jobs: the reminder message to send."
    },
    "channels": {
      "type": "array",
      "description": "For reminder jobs: list of channels to send to (e.g., ['notification', 'telegram'])."
    },
    "working_dir": {
      "type": "string",
      "description": "For shell jobs: working directory for the command."
    },
    "enabled": {
      "type": "boolean",
      "description": "Whether the job is enabled immediately (default true)."
    }
  },
  "required": ["name", "schedule", "job_type"]
}
```

#### `schedule_list` - List Schedules

List scheduled jobs.

**Description:** "List all scheduled jobs with their status, next run time, and configuration."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "status": {
      "type": "string",
      "description": "Filter by status: enabled, disabled, error."
    },
    "job_type": {
      "type": "string",
      "description": "Filter by job type: agent, shell, reminder."
    }
  },
  "required": []
}
```

#### `schedule_delete` - Delete Schedule

Delete a scheduled job.

**Description:** "Delete a scheduled job by ID."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "job_id": {
      "type": "string",
      "description": "ID of the job to delete."
    }
  },
  "required": ["job_id"]
}
```

### Knowledge Graph Tools

#### `entity_create` - Create Entity

Create a knowledge graph entity/node.

**Description:** "Create a knowledge graph entity (node) representing a concept, person, or relationship. Entities are automatically created when storing memories, but this tool can explicitly ensure an entity exists before linking it."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "entity_id": {
      "type": "string",
      "description": "Unique identifier for the entity (will be used as memory_id in the graph)."
    },
    "entity_type": {
      "type": "string",
      "description": "Type of entity (e.g., 'person', 'concept', 'task', 'decision', 'project'). Stored as metadata."
    },
    "properties": {
      "type": "object",
      "description": "Additional properties to store with the entity (e.g., {'name': 'Alice', 'role': 'developer'})."
    }
  },
  "required": ["entity_id", "entity_type"]
}
```

#### `entity_link` - Link Entities

Create a relationship between two entities.

**Description:** "Create a relationship (edge) between two entities in the knowledge graph. This establishes how concepts, people, or memories are connected."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "source_id": {
      "type": "string",
      "description": "ID of the source entity (the entity the relationship originates from)."
    },
    "target_id": {
      "type": "string",
      "description": "ID of the target entity (the entity the relationship points to)."
    },
    "relation_type": {
      "type": "string",
      "description": "Type of relationship: 'reference' (one references another), 'similar' (semantic similarity), 'temporal' (same time/session), 'co_accessed' (accessed together), 'causal' (one led to another).",
      "enum": ["reference", "similar", "temporal", "co_accessed", "causal"]
    },
    "weight": {
      "type": "number",
      "description": "Strength of the relationship from 0.0 to 1.0 (default 0.5). Higher values indicate stronger connections."
    },
    "metadata": {
      "type": "object",
      "description": "Optional metadata to attach to the edge (e.g., context about why this relationship exists)."
    }
  },
  "required": ["source_id", "target_id", "relation_type"]
}
```

#### `entity_query` - Query Entities

Query the knowledge graph for related entities.

**Description:** "Query the knowledge graph to find related entities. Returns connected memories/entities with their relationship types and PageRank importance scores."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "entity_id": {
      "type": "string",
      "description": "ID of the entity to query for related entities."
    },
    "relation_type": {
      "type": "string",
      "description": "Optional: filter by specific relation type ('reference', 'similar', 'temporal', 'co_accessed', 'causal'). If not specified, returns all relations.",
      "enum": ["reference", "similar", "temporal", "co_accessed", "causal", ""]
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of related entities to return (default 20, max 100)."
    },
    "include_pagerank": {
      "type": "boolean",
      "description": "Whether to include PageRank scores (default true)."
    }
  },
  "required": ["entity_id"]
}
```

#### `graph_stats` - Graph Statistics

Get statistics about the knowledge graph.

**Description:** "Get statistics about the knowledge graph including node count, edge count, average degree, and community information. Useful for understanding the structure and connectivity of stored memories."

**Parameters:**
```json
{
  "type": "object",
  "properties": {},
  "required": []
}
```

### Code Intelligence Tools

#### `ast_parse` - Parse AST

Parse source code into an abstract syntax tree.

**Description:** "Parse source code into an abstract syntax tree. Can parse from a file path or inline source code. Supports: Go, Python, TypeScript, JavaScript, Rust, C, C++, Java, Ruby, YAML, TOML, Bash, and more."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "file_path": {
      "type": "string",
      "description": "Path to the source file to parse. Either file_path or source+language is required."
    },
    "source": {
      "type": "string",
      "description": "Inline source code to parse (use with 'language' parameter)."
    },
    "language": {
      "type": "string",
      "description": "Language of inline source: go, python, typescript, javascript, rust, c, cpp, java, ruby, yaml, toml, bash."
    },
    "max_depth": {
      "type": "integer",
      "description": "Maximum depth of AST nodes to return (default: 5, 0 for unlimited)."
    }
  },
  "required": []
}
```

#### `ast_symbols` - Extract Symbols

Extract code symbols from source files.

**Description:** "Extract code symbols (functions, classes, methods, interfaces, etc.) from source files. Returns symbol names, kinds, locations, and signatures. Useful for understanding code structure."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "file_path": {
      "type": "string",
      "description": "Path to the source file to analyze."
    },
    "kinds": {
      "type": "array",
      "description": "Filter by symbol kinds: function, method, class, interface, struct, enum, constant, variable, module. Empty means all."
    },
    "include_private": {
      "type": "boolean",
      "description": "Include private/unexported symbols (default: true)."
    },
    "max_depth": {
      "type": "integer",
      "description": "Maximum nesting depth for child symbols (default: 0 = unlimited)."
    }
  },
  "required": ["file_path"]
}
```

#### `ast_query` - AST Query

Run tree-sitter queries against source code.

**Description:** "Run tree-sitter S-expression queries to find specific patterns in source code. Use for advanced code analysis like finding all function calls, matching specific patterns, etc."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "file_path": {
      "type": "string",
      "description": "Path to the source file to query."
    },
    "query": {
      "type": "string",
      "description": "Tree-sitter S-expression query pattern. Use @name to capture nodes."
    },
    "query_name": {
      "type": "string",
      "description": "Use a predefined query: functions, classes, imports, strings, comments."
    },
    "language": {
      "type": "string",
      "description": "Override language detection (go, python, typescript, etc.)."
    },
    "max_matches": {
      "type": "integer",
      "description": "Maximum number of matches to return (default: 100)."
    }
  },
  "required": ["file_path"]
}
```

#### `lsp_goto_definition` - Go to Definition

Find the definition of a symbol at a given position.

**Description:** "Find the definition of a symbol at a specific location in code. Returns the file path, line, and column where the symbol is defined. Requires an LSP server for the file's language to be configured and running."

**Parameters:**
```json
{
  "type": "object",
  "properties": {
    "file_path": {
      "type": "string",
      "description": "Path to the source file containing the symbol."
    },
    "line": {
      "type": "integer",
      "description": "Line number (0-indexed) of the symbol."
    },
    "character": {
      "type": "integer",
      "description": "Column/character offset (0-indexed) within the line."
    }
  },
  "required": ["file_path", "line", "character"]
}
```

## Agent Tool Access

Different agents have access to different tools based on their roles:

| Agent | Baseline Tools | Additional Tools |
|-------|----------------|------------------|
| **dispatcher** | All baseline | None |
| **chat** | All baseline | web_fetch, web_search |
| **coder** | All baseline | file_read, file_write, file_delete, list_directory, shell_execute |
| **debugger** | All baseline | file_read, file_write, shell_execute |
| **planner** | All baseline | None |
| **analyst** | All baseline | web_fetch, web_search |
| **committer** | All baseline | shell_execute |
| **scheduler** | All baseline | schedule_create, schedule_list, schedule_delete |

## Risk Levels

Tools are assigned risk levels for security purposes:

- **Low**: Read-only operations (memory_search, platform_status)
- **Medium**: Controlled writes (memory_store, file_read)
- **High**: Potentially dangerous (file_write, shell_execute)
- **Critical**: Blocked by default (file_delete, dangerous shell commands)

## Security Considerations

- All tool executions are logged and audited
- File operations are subject to path restrictions
- Shell commands are scanned for dangerous patterns
- Web requests respect rate limits and size constraints
- Memory operations are subject to privacy controls