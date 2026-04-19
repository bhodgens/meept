# Production Configuration Example

This example shows a robust production setup with multiple LLM providers, enhanced security, and optimized performance.

## Configuration Files

### ~/.meept/meept.toml

```toml
[daemon]
socket_path = "~/.meept/meept.sock"
pid_file = "~/.meept/meept.pid"
log_level = "WARN"
data_dir = "~/.meept"

[llm.budget]
hourly_token_limit = 500000
daily_token_limit = 5000000
rate_limit_rpm = 60
aggressiveness = 0.8

[llm.broker]
max_error_rate = 0.05
max_p95_latency_ms = 15000
fallback_enabled = true

[llm.adaptive_timeout]
enabled = true
stddev_multiplier = 2.5
stddev_token_rate_timeout = true
min_timeout_seconds = 15
max_timeout_seconds = 180
warmup_requests = 50

[llm.context_firewall]
enabled = true
max_context_tokens = 64000
summarization_threshold = 0.8
summarization_model = "small"

[memory]
data_dir = "~/.meept/memory"
consolidation_interval_hours = 12

[memory.episodic]
enabled = true
max_context_items = 30

[memory.task]
enabled = true
domains = ["general", "code", "commands", "research"]

[memory.personality]
enabled = true
update_interval_conversations = 25

[memory.security]
enabled = true
redact_sensitive = true
block_patterns = ["api_key", "password", "secret"]

[memory.caching]
enabled = true
max_cache_size_mb = 100

[security]
sanitize_inputs = true
sanitize_strictness = "strict"
llm_filter_external = true
monitor_output = true
redact_output = true
scan_shell_commands = true
tirith_binary = "tirith"
require_confirmation_high = true
require_confirmation_critical = true
block_financial = true
allowed_paths = ["~/projects/*", "~/work/*"]
blocked_paths = ["~/.ssh/*", "~/.gnupg/*", "~/.meept/*", "/etc/*", "/var/*"]
enable_audit_log = true
audit_db_path = "~/.meept/audit.db"

[scheduler]
enabled = true
timezone = "America/New_York"

[agents]
enabled = true
config_dirs = ["~/.meept/agents", "config/agents"]
prompts_dir = "config/prompts"
default_model = "openrouter/claude-3-sonnet"
dispatcher_id = "dispatcher"

[telegram]
enabled = false

[web]
enabled = true
host = "127.0.0.1"
port = 8420
secret_key = "${MEEPT_WEB_SECRET}"

[mcp]
enabled = true
config_file = "~/.meept/mcp_servers.json"

[plugins]
enabled = true
directory = "~/.meept/plugins"

[workspace]
enabled = true
base_dir = "~/.meept/workspaces"
auto_commit = true
commit_on_plan = true
commit_on_step = true
cleanup_completed = true

[skills]
enabled = true
search_paths = ["~/.meept/skills"]
auto_reload = false

[clawskills]
enabled = true
registry_url = "https://clawhub.ai"
install_dir = "~/.meept/clawskills"
auto_update = true
max_installed = 25
default_risk_level = "high"
max_iterations = 8
blocked_slugs = ["financial", "crypto"]

[selfimprove]
enabled = true
data_dir = "~/.meept/selfimprove"
max_iterations_per_cycle = 3
max_fixes_per_cycle = 5
auto_run_interval_hours = 24

[selfimprove.ai_infra]
enabled = true
base_url = "http://localhost:8100"
api_key_env = "MEEPT_AI_INFRA_KEY"
analysis_model = "openrouter/claude-3-opus"
generation_model = "openrouter/claude-3-sonnet"
review_model = "openrouter/claude-3-opus"
timeout_seconds = 180.0
max_retries = 3

[selfimprove.sandbox]
worktree_dir = "~/.meept/selfimprove/worktrees"
cleanup_on_success = true
cleanup_on_failure = false
max_worktrees = 3
test_timeout_seconds = 600.0

[selfimprove.safety]
require_human_approval = true
max_files_per_fix = 5
max_lines_changed_per_fix = 200
blocked_paths = [
    "src/meept/selfimprove/*",
    "src/meept/security/*",
    ".git/*",
    "*.toml",
    "*.json5",
    "**/*credentials*",
    "**/*secret*",
    "**/.env*",
]
allowed_risk_levels = ["low", "medium"]
block_critical_risk = true
require_tests_pass = true
min_confidence_threshold = 0.8

[selfimprove.detection]
scan_pytest = true
scan_runtime_logs = true
scan_type_check = true
scan_lint = true
log_file = "~/.meept/meept.log"
log_lookback_hours = 48
pytest_args = ["-v", "--tb=short"]
mypy_args = ["--ignore-missing-imports"]
ruff_args = []

[agent]
progress_enabled = true
progress_interval_seconds = 60

[agent.cache]
enabled = true
max_entries = 2000
default_ttl_seconds = 600
cleanup_freq_seconds = 120
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
max_suggestion_length = 1000

[review]
enabled = true
require_review = ["code", "refactor", "debug", "git", "fix"]
skip_review = ["chat", "report", "recall", "search", "analyze"]
reviewer_mapping = {
  coder = "code-reviewer",
  debugger = "debug-reviewer",
  planner = "planner-reviewer",
  analyst = "analyst-reviewer",
  committer = "code-reviewer"
}
max_revision_cycles = 2
auto_approve_patterns = ["*.md", "LICENSE", "*.txt", "*.json"]
```

### ~/.meept/models.json5

```json5
{
  "model": "openrouter/claude-3-sonnet",
  "small_model": "openrouter/claude-3-haiku",
  "classifier_model": "openrouter/claude-3-haiku",
  "model_aliases": {
    "coder": {
      "models": ["openrouter/claude-3-sonnet", "ollama/codellama"],
      "timeout": 45,
      "max_fails": 2
    },
    "planner": {
      "models": ["openrouter/claude-3-haiku", "ollama/llama3.2"],
      "timeout": 30,
      "max_fails": 3
    },
    "analyst": {
      "models": ["openrouter/claude-3-sonnet", "ollama/llama3.2"],
      "timeout": 60,
      "max_fails": 2
    },
    "reviewer": {
      "models": ["openrouter/claude-3-opus", "openrouter/claude-3-sonnet"],
      "timeout": 90,
      "max_fails": 1
    }
  },
  "default_timeout": 6000,
  "disabled_providers": [],
  "providers": {
    "openrouter": {
      "api": "openai",
      "options": {
        "baseURL": "https://openrouter.ai/api/v1",
        "apiKey": "${OPENROUTER_API_KEY}",
        "headers": {
          "HTTP-Referer": "https://github.com/your-org/meept",
          "X-Title": "Meept Production"
        }
      },
      "models": {
        "claude-3-opus": {
          "name": "anthropic/claude-3-opus",
          "capabilities": ["completion", "reasoning", "extended_thinking"],
          "input_cost": 0.015,
          "output_cost": 0.075,
          "context_limit": 200000,
          "max_output": 4096
        },
        "claude-3-sonnet": {
          "name": "anthropic/claude-3-sonnet",
          "capabilities": ["completion", "code", "reasoning", "tool_use"],
          "input_cost": 0.003,
          "output_cost": 0.015,
          "context_limit": 200000,
          "max_output": 4096
        },
        "claude-3-haiku": {
          "name": "anthropic/claude-3-haiku",
          "capabilities": ["completion", "reasoning"],
          "input_cost": 0.00025,
          "output_cost": 0.00125,
          "context_limit": 200000,
          "max_output": 4096
        }
      }
    },
    "ollama": {
      "api": "openai",
      "options": {
        "baseURL": "http://localhost:11434/v1"
      },
      "models": {
        "llama3.2": {
          "name": "llama3.2",
          "capabilities": ["completion", "code", "reasoning"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 128000,
          "max_output": 4096,
          "temperature": 0.7
        },
        "codellama": {
          "name": "codellama",
          "capabilities": ["code"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 16384,
          "max_output": 4096,
          "temperature": 0.2
        }
      }
    }
  }
}
```

### ~/.meept/mcp_servers.json

```json
{
  "servers": [
    {
      "name": "filesystem",
      "type": "stdio",
      "command": ["npx", "-y", "@modelcontextprotocol/server-filesystem", "${HOME}"]
    },
    {
      "name": "sqlite",
      "type": "stdio",
      "command": ["npx", "-y", "@modelcontextprotocol/server-sqlite"]
    }
  ]
}
```

## Production Features

### Enhanced Security

- **Strict input sanitization** with external LLM filtering
- **Credential detection** and redaction in outputs
- **Path restrictions** limiting file operations to work directories
- **Audit logging** for security event tracking
- **Tirith command scanning** for shell command safety

### High Availability

- **Multiple LLM providers** with automatic failover
- **Model aliases** with cooldown-based switching
- **Adaptive timeouts** based on request characteristics
- **Context firewall** preventing context overflow

### Performance Optimization

- **Aggressive budget usage** (0.8) for better responsiveness
- **Enhanced caching** with larger TTL and cache size
- **Memory optimization** with security and caching layers
- **Broker optimization** with lower error thresholds

### Monitoring & Maintenance

- **Self-improvement system** for automated bug fixes
- **Audit logging** for security monitoring
- **Memory consolidation** every 12 hours
- **Auto-update for ClawSkills**

## Environment Variables

Set these environment variables for production:

```bash
export OPENROUTER_API_KEY="your-openrouter-key"
export MEEPT_WEB_SECRET="your-web-secret-key"
export MEEPT_AI_INFRA_KEY="your-ai-infra-key"
```

## Deployment Considerations

### Resource Requirements

- **Memory**: 2-4GB RAM for optimal performance
- **Storage**: 1-5GB for memory, logs, and workspaces
- **Network**: Stable internet connection for external providers

### Security Hardening

- Run daemon as non-root user
- Restrict file permissions on configuration directory
- Use firewall rules to limit web interface access
- Regularly review audit logs

### Backup Strategy

- Backup `~/.meept/memory/` directory regularly
- Export important memory items before major updates
- Keep configuration files in version control

## Monitoring

### Key Metrics to Monitor

- **Token usage** against budget limits
- **Error rates** per provider
- **Memory usage** growth
- **Response times** for critical operations

### Log Analysis

Set up log aggregation for:
- Security events (audit log)
- LLM provider failures
- Memory consolidation events
- Self-improvement cycles

## Scaling

For higher workloads, consider:

- Increasing budget limits
- Adding more providers (Anthropic, OpenAI, etc.)
- Enabling distributed memory
- Using external MCP servers for specialized tasks