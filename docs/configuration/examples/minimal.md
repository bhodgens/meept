# Minimal Configuration Example

This example shows the smallest working Meept configuration using only local Ollama.

## Configuration Files

### ~/.meept/meept.toml

```toml
[daemon]
socket_path = "~/.meept/meept.sock"
pid_file = "~/.meept/meept.pid"
log_level = "INFO"
data_dir = "~/.meept"

[llm.budget]
hourly_token_limit = 100000
daily_token_limit = 1000000
rate_limit_rpm = 30
aggressiveness = 0.5

[memory]
data_dir = "~/.meept/memory"
consolidation_interval_hours = 6

[memory.episodic]
enabled = true
max_context_items = 20

[memory.task]
enabled = true
domains = ["general", "code", "commands"]

[memory.personality]
enabled = true
update_interval_conversations = 10

[security]
sanitize_inputs = true
sanitize_strictness = "standard"
monitor_output = true
redact_output = true
scan_shell_commands = true
tirith_binary = "tirith"
require_confirmation_high = true
require_confirmation_critical = true
block_financial = true
allowed_paths = ["~/*"]
blocked_paths = ["~/.ssh/*", "~/.gnupg/*", "~/.meept/meept.toml"]

[scheduler]
enabled = true
timezone = "UTC"

[agent]
progress_enabled = true
progress_interval_seconds = 30

[agent.cache]
enabled = true
max_entries = 1000
default_ttl_seconds = 300
cleanup_freq_seconds = 60
enabled_tools = [
    "file_read",
    "list_directory",
    "memory_search",
    "memory_get_context",
    "platform_status",
    "platform_agents",
    "platform_tools"
]

[agent.errors]
detailed_errors = true
include_examples = true
max_suggestion_length = 500

[review]
enabled = true
require_review = ["code", "refactor", "debug", "git", "fix"]
skip_review = ["chat", "report", "recall", "search", "analyze"]
reviewer_mapping = {coder = "code-reviewer", debugger = "debug-reviewer", planner = "planner-reviewer", analyst = "analyst-reviewer", committer = "code-reviewer"}
max_revision_cycles = 3
auto_approve_patterns = ["*.md", "LICENSE", "*.txt"]

# Disable advanced features for minimal setup
[agents]
enabled = false

[telegram]
enabled = false

[web]
enabled = false

[mcp]
enabled = false

[skills]
enabled = false

[clawskills]
enabled = false

[selfimprove]
enabled = false
```

### ~/.meept/models.json5

```json5
{
  "model": "ollama/llama3.2",
  "small_model": "ollama/llama3.2",
  "classifier_model": "ollama/llama3.2",
  "model_aliases": {
    "coder": {
      "models": ["ollama/llama3.2"],
      "timeout": 30,
      "max_fails": 3
    },
    "planner": {
      "models": ["ollama/llama3.2"],
      "timeout": 15,
      "max_fails": 2
    },
    "analyst": {
      "models": ["ollama/llama3.2"],
      "timeout": 20,
      "max_fails": 3
    }
  },
  "default_timeout": 3000,
  "disabled_providers": [],
  "providers": {
    "ollama": {
      "api": "openai",
      "options": {
        "baseURL": "http://localhost:11434/v1"
      },
      "models": {
        "llama3.2": {
          "name": "llama3.2",
          "capabilities": ["completion", "code", "reasoning", "tool_use"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 128000,
          "max_output": 4096,
          "temperature": 0.7
        }
      }
    }
  }
}
```

## Setup Instructions

### 1. Install Ollama

```bash
# Install Ollama
curl -fsSL https://ollama.ai/install.sh | sh

# Pull the model
ollama pull llama3.2
```

### 2. Create Configuration Directory

```bash
mkdir -p ~/.meept
```

### 3. Copy Configuration Files

Copy the above configurations to:
- `~/.meept/meept.toml`
- `~/.meept/models.json5`

### 4. Start the Daemon

```bash
# Build the daemon
go build -o bin/meept-daemon ./cmd/meept-daemon

# Start the daemon
./bin/meept-daemon -f
```

### 5. Test the CLI

```bash
# Build the CLI
go build -o bin/meept ./cmd/meept

# Test basic functionality
./bin/meept status
./bin/meept chat "Hello, how are you?"
```

## Features Included

This minimal configuration provides:

- ✅ **Basic LLM functionality** with Ollama
- ✅ **Memory system** with episodic, task, and personality memory
- ✅ **Security features** including input sanitization and command scanning
- ✅ **Agent caching** for performance
- ✅ **Review system** for code changes
- ✅ **Scheduler** for background jobs

## Features Disabled

To keep the configuration minimal, these features are disabled:

- ❌ **Multi-agent system** (`agents.enabled = false`)
- ❌ **Telegram integration** (`telegram.enabled = false`)
- ❌ **Web interface** (`web.enabled = false`)
- ❌ **MCP servers** (`mcp.enabled = false`)
- ❌ **Skills system** (`skills.enabled = false`)
- ❌ **ClawSkills** (`clawskills.enabled = false`)
- ❌ **Self-improvement** (`selfimprove.enabled = false`)

## Next Steps

Once you have the basic setup working, you can:

1. **Add external providers** like OpenRouter or Anthropic
2. **Enable multi-agent system** for specialized task handling
3. **Configure skills** for extended functionality
4. **Set up Telegram/Web interfaces** for remote access
5. **Enable self-improvement** for automated code fixes

## Troubleshooting

### Ollama Connection Issues

If Ollama isn't running:
```bash
# Start Ollama service
ollama serve

# Check available models
ollama list
```

### Permission Issues

Ensure the daemon can access the configuration directory:
```bash
chmod 700 ~/.meept
```

### Socket Issues

If the socket file gets corrupted:
```bash
rm ~/.meept/meept.sock
# Restart the daemon
```