# Advanced Configuration Example

This example demonstrates a full-featured Meept configuration with all advanced capabilities enabled.

## Configuration Files

### ~/.meept/meept.toml

```toml
[daemon]
socket_path = "~/.meept/meept.sock"
pid_file = "~/.meept/meept.pid"
log_level = "INFO"
data_dir = "~/.meept"

[llm.budget]
hourly_token_limit = 1000000
daily_token_limit = 10000000
rate_limit_rpm = 100
aggressiveness = 0.9

[llm.broker]
max_error_rate = 0.02
max_p95_latency_ms = 10000
fallback_enabled = true

[llm.adaptive_timeout]
enabled = true
stddev_multiplier = 2.0
stddev_token_rate_timeout = true
min_timeout_seconds = 10
max_timeout_seconds = 120
warmup_requests = 100

[llm.context_firewall]
enabled = true
max_context_tokens = 128000
summarization_threshold = 0.85
summarization_model = "small"

[llm.metrics]
enabled = true
flush_interval_seconds = 60
max_metrics_age_hours = 24

[memory]
backend = "memvid"
data_dir = "~/.meept/memory"
consolidation_interval_hours = 4

[memory.episodic]
enabled = true
max_context_items = 50

[memory.task]
enabled = true
domains = ["general", "code", "commands", "research", "debug", "planning"]

[memory.personality]
enabled = true
update_interval_conversations = 50

[memory.embeddings]
enabled = true
model = "all-MiniLM-L6-v2"

[memory.security]
enabled = true
redact_sensitive = true
block_patterns = ["api_key", "password", "secret", "token", "key"]

[memory.caching]
enabled = true
max_cache_size_mb = 500

[memory.limits]
general = 10000
code = 20000
commands = 5000
research = 15000

[memory.expiration]
enabled = true
default_ttl_days = 30
episodic_ttl_days = 90
task_ttl_days = 7

[memory.versioning]
enabled = true
max_versions = 10

[memvid]
enabled = true
max_memory_size_mb = 1000
compression_level = 6

[multiagent]
enabled = true
max_concurrent_agents = 8
agent_timeout_seconds = 900

[agents]
enabled = true
config_dirs = ["~/.meept/agents", "config/agents", "config/agents-custom"]
prompts_dir = "config/prompts"
default_model = "anthropic/claude-3-opus"
dispatcher_id = "dispatcher"

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
allowed_paths = ["~/projects/*", "~/work/*", "/tmp/meept/*"]
blocked_paths = ["~/.ssh/*", "~/.gnupg/*", "~/.meept/*", "/etc/*", "/var/*", "/root/*"]
enable_audit_log = true
audit_db_path = "~/.meept/audit.db"
# Override matching: opt-in strict mode (default: false for backwards compatibility)
# When true: uses strict glob/exact matching for permission overrides
# When false: uses lenient three-strategy cascade (substring, glob, trimmed substring)
strict_override_matching = false

[scheduler]
enabled = true
timezone = "America/New_York"

[queue]
enabled = true
max_queue_size = 1000
worker_timeout_seconds = 3600

[workers]
enabled = true
max_workers = 10
worker_idle_timeout_seconds = 300

[isolation]
enabled = true
workdir = "/tmp/meept-isolation"
max_workdir_size_mb = 1000

[telegram]
enabled = true
token = "${MEEPT_TELEGRAM_TOKEN}"
creator_id = 123456789

[web]
enabled = true
host = "0.0.0.0"
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
search_paths = ["~/.meept/skills", "~/projects/*/.meept/skills"]
auto_reload = true
max_cached_skills = 100

[clawskills]
enabled = true
registry_url = "https://clawhub.ai"
install_dir = "~/.meept/clawskills"
auto_update = true
max_installed = 100
default_risk_level = "high"
max_iterations = 15
blocked_slugs = ["financial", "crypto", "hacking"]

[selfimprove]
enabled = true
data_dir = "~/.meept/selfimprove"
max_iterations_per_cycle = 10
max_fixes_per_cycle = 20
auto_run_interval_hours = 6

[selfimprove.ai_infra]
enabled = true
base_url = "http://localhost:8100"
api_key_env = "MEEPT_AI_INFRA_KEY"
analysis_model = "anthropic/claude-3-opus"
generation_model = "anthropic/claude-3-sonnet"
review_model = "anthropic/claude-3-opus"
timeout_seconds = 300.0
max_retries = 5

[selfimprove.sandbox]
worktree_dir = "~/.meept/selfimprove/worktrees"
cleanup_on_success = true
cleanup_on_failure = false
max_worktrees = 10
test_timeout_seconds = 900.0

[selfimprove.safety]
require_human_approval = false
max_files_per_fix = 20
max_lines_changed_per_fix = 1000
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
allowed_risk_levels = ["low", "medium", "high"]
block_critical_risk = true
require_tests_pass = true
min_confidence_threshold = 0.6

[selfimprove.detection]
scan_pytest = true
scan_runtime_logs = true
scan_type_check = true
scan_lint = true
log_file = "~/.meept/meept.log"
log_lookback_hours = 72
pytest_args = ["-v", "--tb=short"]
mypy_args = ["--ignore-missing-imports"]
ruff_args = []

[shadow]
enabled = true
training_data_dir = "~/.meept/shadow/training"
max_training_examples = 10000

[distributed_memory]
enabled = true
coordinator_url = "http://localhost:8421"
node_id = "primary"

[code_intel]
enabled = true
lsp_servers = ["gopls", "rust-analyzer", "pylsp"]
ast_parsers = ["go", "python", "javascript", "typescript"]

[orchestrator]
enabled = true
max_parallel_tasks = 5

[agent]
progress_enabled = true
progress_interval_seconds = 30

[agent.cache]
enabled = true
max_entries = 5000
default_ttl_seconds = 1800
cleanup_freq_seconds = 300
enabled_tools = [
    "file_read",
    "list_directory",
    "memory_search",
    "memory_get_context",
    "platform_status",
    "platform_agents",
    "platform_tools",
    "web_search",
    "code_ast_parse"
]

[agent.errors]
detailed_errors = true
include_examples = true
max_suggestion_length = 2000

[review]
enabled = true
require_review = ["code", "refactor", "debug", "git", "fix", "deploy"]
skip_review = ["chat", "report", "recall", "search", "analyze"]
reviewer_mapping = {
  coder = "code-reviewer",
  debugger = "debug-reviewer",
  planner = "planner-reviewer",
  analyst = "analyst-reviewer",
  committer = "code-reviewer",
  scheduler = "planner-reviewer"
}
max_revision_cycles = 5
auto_approve_patterns = ["*.md", "LICENSE", "*.txt", "*.json", "*.yaml", "*.yml"]
```

### ~/.meept/models.json5

```json5
{
  "model": "anthropic/claude-3-opus",
  "small_model": "anthropic/claude-3-haiku",
  "classifier_model": "anthropic/claude-3-haiku",
  "model_aliases": {
    "coder": {
      "models": ["anthropic/claude-3-sonnet", "openai/gpt-4", "ollama/codellama"],
      "timeout": 60,
      "max_fails": 2
    },
    "planner": {
      "models": ["anthropic/claude-3-haiku", "openai/gpt-3.5-turbo", "ollama/llama3.2"],
      "timeout": 45,
      "max_fails": 3
    },
    "analyst": {
      "models": ["anthropic/claude-3-sonnet", "openai/gpt-4", "ollama/llama3.2"],
      "timeout": 90,
      "max_fails": 2
    },
    "reviewer": {
      "models": ["anthropic/claude-3-opus", "openai/gpt-4"],
      "timeout": 120,
      "max_fails": 1
    },
    "shadow": {
      "models": ["anthropic/claude-3-haiku", "openai/gpt-3.5-turbo"],
      "timeout": 30,
      "max_fails": 5
    }
  },
  "default_timeout": 9000,
  "disabled_providers": [],
  "providers": {
    "anthropic": {
      "api": "anthropic",
      "options": {
        "apiKey": "${ANTHROPIC_API_KEY}"
      },
      "models": {
        "claude-3-opus": {
          "name": "claude-3-opus-20240229",
          "capabilities": ["completion", "reasoning", "extended_thinking", "tool_use"],
          "input_cost": 0.015,
          "output_cost": 0.075,
          "context_limit": 200000,
          "max_output": 4096
        },
        "claude-3-sonnet": {
          "name": "claude-3-sonnet-20240229",
          "capabilities": ["completion", "code", "reasoning", "tool_use"],
          "input_cost": 0.003,
          "output_cost": 0.015,
          "context_limit": 200000,
          "max_output": 4096
        },
        "claude-3-haiku": {
          "name": "claude-3-haiku-20240307",
          "capabilities": ["completion", "reasoning"],
          "input_cost": 0.00025,
          "output_cost": 0.00125,
          "context_limit": 200000,
          "max_output": 4096
        }
      }
    },
    "openai": {
      "api": "openai",
      "options": {
        "apiKey": "${OPENAI_API_KEY}"
      },
      "models": {
        "gpt-4": {
          "name": "gpt-4",
          "capabilities": ["completion", "code", "reasoning", "tool_use"],
          "input_cost": 0.03,
          "output_cost": 0.06,
          "context_limit": 128000,
          "max_output": 4096
        },
        "gpt-3.5-turbo": {
          "name": "gpt-3.5-turbo",
          "capabilities": ["completion", "code", "reasoning"],
          "input_cost": 0.0015,
          "output_cost": 0.002,
          "context_limit": 16385,
          "max_output": 4096
        }
      }
    },
    "openrouter": {
      "api": "openai",
      "options": {
        "baseURL": "https://openrouter.ai/api/v1",
        "apiKey": "${OPENROUTER_API_KEY}",
        "headers": {
          "HTTP-Referer": "https://github.com/your-org/meept",
          "X-Title": "Meept Advanced"
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
        },
        "qwen2.5-coder": {
          "name": "qwen2.5-coder",
          "capabilities": ["code", "tool_use"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 32768,
          "max_output": 8192,
          "temperature": 0.7
        }
      }
    }
  }
}
```

## Advanced Features Enabled

### Multi-Agent Architecture

- **8 concurrent agents** with specialized roles
- **Custom agent configurations** with multiple search paths
- **Advanced prompt engineering** with component-based prompts
- **Agent collaboration** with task delegation

### Enhanced Memory System

- **Memvid backend** for high-performance memory storage
- **Embedding support** for semantic memory search
- **Memory versioning** with rollback capability
- **Expiration policies** for automatic cleanup
- **Security filtering** for sensitive information

### Distributed Capabilities

- **Distributed memory** across multiple nodes
- **Shadow training** for model improvement
- **Code intelligence** with AST parsing and LSP integration
- **Web search integration** for research tasks

### Security & Isolation

- **Strict isolation** with dedicated work directories
- **Advanced auditing** with comprehensive event tracking
- **Taint tracking** for security analysis
- **Resource limits** preventing system overload

### Self-Improvement Ecosystem

- **Automated bug detection** and fixing
- **Sandboxed testing** with worktree management
- **Confidence-based approval** for automated changes
- **Extensive safety controls** for risk management

## Performance Considerations

This configuration is resource-intensive:

- **Memory**: 8-16GB RAM recommended
- **Storage**: 10-50GB for memory, logs, and training data
- **CPU**: Multi-core system for parallel processing
- **Network**: High-bandwidth connection for external providers

## Monitoring & Maintenance

### Key Metrics

- **Agent performance** and error rates
- **Memory usage** and consolidation efficiency
- **LLM provider health** and response times
- **Self-improvement cycle** success rates

### Log Management

- **Structured logging** with comprehensive metadata
- **Log rotation** for large log files
- **Centralized logging** for distributed deployments
- **Alerting** for critical events

## Scaling Strategies

### Horizontal Scaling

- Deploy multiple Meept instances
- Use distributed memory coordination
- Load balance across instances

### Vertical Scaling

- Increase memory and CPU resources
- Add more LLM providers
- Enable additional advanced features

## Security Best Practices

- **Network isolation** for sensitive deployments
- **Regular security audits** of configuration
- **Backup and recovery** procedures
- **Access control** for web and Telegram interfaces

This advanced configuration represents the full capability of the Meept platform, suitable for enterprise deployments and research applications.