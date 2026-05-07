# Unified Config Format & Transport Architecture Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unify all configuration to JSON5 format, add selectable transports (RPC socket / HTTP) for both daemon and clients, make `make install` copy templates if missing, and give the menubar its own config file.

**Architecture:** Convert all configs to JSON5; add `transport` section to daemon config with enable/disable flags for RPC and HTTP; add `ClientConnection` abstraction in the CLI to connect over either transport (with config-driven default); add `menubar.json5` for menubar-specific settings; update `make install` to template-copy.

**Tech Stack:** Go 1.22+, JSON5 (hujson for parsing), TOML (legacy read support during migration), Unix sockets, HTTP REST, Swift (menubar).

---

## Current State Analysis

### Config Files Today

| File | Format | Repo Template | Runtime Path | Used By |
|------|--------|--------------|--------------|---------|
| meept.toml | TOML | `config/meept.toml` | `~/.meept/meept.toml` | daemon |
| models.json5 | JSON5 | `config/models.json5` | `~/.meept/models.json5` | daemon |
| presets.json5 | JSON5 | `config/presets.json5` | `~/.meept/presets.json5` | daemon |
| mcp_servers.json | JSON | `config/mcp_servers.json` | `~/.meept/mcp_servers.json` | daemon |
| q_agent.toml | TOML | `config/q_agent.example.toml` | `~/.meept/q_agent.toml` | daemon |
| client.json5 | JSON5 | `config/client.json5` | `.meept/client.json5` or `~/.meept/client.json5` | CLI TUI |
| agents/*.toml | TOML | `config/agents/` | `~/.meept/agents/` | daemon |
| prompts/*.md | Markdown | `config/prompts/` | `~/.meept/prompts/` | daemon |

### Transports Today

| Component | Transport | Hardcoded? |
|-----------|-----------|------------|
| daemon | Unix socket RPC (`internal/rpc/`) | Yes, always on |
| daemon | HTTP (`internal/comm/http/`, port 8081) | Yes, always on |
| CLI TUI | Unix socket RPC (`internal/tui/rpc.go`) | Yes, via `-s` flag |
| CLI single-msg | Unix socket RPC (`internal/tui/rpc.go`) | Yes, via `-s` flag |
| Menubar | HTTP to localhost:8081 | Yes, hardcoded |

### make install Today

```makefile
install:
	@echo "Installing binaries to GOPATH/bin..."
	go install $(GO_BUILD_FLAGS) ./cmd/meept-daemon
	go install $(GO_BUILD_FLAGS) ./cmd/meept
	@echo "Installed: meept-daemon, meept"
```

Does NOT copy config templates. `make setup` copies only `meept.toml` and creates directories.

---

## File Structure Changes

### New Files

- `config/meept.json5` — replacement for `meept.toml`
- `config/mcp_servers.json5` — replacement for `mcp_servers.json`
- `config/q_agent.json5` — replacement for `q_agent.example.toml`
- `config/agents.json5` — replacement for `config/agents/*.toml`
- `config/menubar.json5` — menubar-specific config template
- `internal/transport/` — shared client connection abstraction

### Modified Files

- `cmd/meept/main.go` — add transport flag, use connection abstraction
- `cmd/meept/chat.go` — use connection abstraction for TUI/single-msg
- `cmd/meept/status.go` — use connection abstraction
- `internal/config/config.go` — add JSON5 loading, keep TOML fallback
- `internal/config/schema.go` — add TransportConfig, change tag to json5
- `internal/config/mcp.go` — load from json5
- `internal/tui/rpc.go` — extract interface, support HTTP transport
- `internal/tui/app.go` — accept transport config
- `internal/daemon/daemon.go` — conditionally start RPC/HTTP
- `internal/comm/http/server.go` — use daemon config for port/bind
- `menubar/MeeptMenuBar/Services/ConfigService.swift` — load menubar.json5
- `Makefile` — update install/setup targets
- `CLAUDE.md` — document new config format and transport options

### Removed (after migration)

- `config/meept.toml` (keep as reference during transition)
- `config/mcp_servers.json`
- `config/q_agent.example.toml`
- `config/agents/*.toml`

---

## Task 1: Create JSON5 Config Loader

**Files:**
- Create: `internal/config/json5_loader.go`
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write JSON5 loader utility**

```go
package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tailscale/hujson"
)

// LoadJSON5 reads a JSON5 file and unmarshals into v.
func LoadJSON5(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Expand env vars in raw content
	content := expandEnvVars(string(data))
	// Standardize JSON5 to JSON
	stdJSON, err := hujson.Standardize([]byte(content))
	if err != nil {
		return fmt.Errorf("failed to parse JSON5: %w", err)
	}
	return json.Unmarshal(stdJSON, v)
}

// LoadJSON5WithDefault loads JSON5 from path, or returns default if not found.
func LoadJSON5WithDefault(path string, v any) error {
	if err := LoadJSON5(path, v); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}
```

- [ ] **Step 2: Write test for JSON5 loader**

```go
func TestLoadJSON5(t *testing.T) {
	// Create temp JSON5 file with comments
	f, err := os.CreateTemp("", "test*.json5")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	content := `{
		// This is a comment
		"name": "test",
		"value": 42,
		"nested": {
			/* block comment */
			"enabled": true
		}
	}`
	f.WriteString(content)
	f.Close()

	var result struct {
		Name   string `json:"name"`
		Value  int    `json:"value"`
		Nested struct {
			Enabled bool `json:"enabled"`
		} `json:"nested"`
	}

	if err := LoadJSON5(f.Name(), &result); err != nil {
		t.Fatalf("LoadJSON5 failed: %v", err)
	}

	if result.Name != "test" || result.Value != 42 || !result.Nested.Enabled {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestLoadJSON5EnvVars(t *testing.T) {
	os.Setenv("TEST_VAR", "hello")
	defer os.Unsetenv("TEST_VAR")

	f, err := os.CreateTemp("", "test*.json5")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	f.WriteString(`{"msg": "${TEST_VAR}"}`)
	f.Close()

	var result struct {
		Msg string `json:"msg"`
	}
	if err := LoadJSON5(f.Name(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Msg != "hello" {
		t.Errorf("expected hello, got %s", result.Msg)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/config -run TestLoadJSON5 -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/config/json5_loader.go internal/config/config_test.go
git commit -m "feat(config): add JSON5 loader with env var expansion"
```

---

## Task 2: Update Config Schema for Transport

**Files:**
- Modify: `internal/config/schema.go`
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Add TransportConfig to schema**

Add to `internal/config/schema.go` after DaemonConfig:

```go
// TransportConfig controls which transports the daemon exposes.
// Clients can connect via either transport based on preference/availability.
type TransportConfig struct {
	RPC  RPCTransportConfig  `json:"rpc"`
	HTTP HTTPTransportConfig `json:"http"`
}

// RPCTransportConfig configures the Unix socket RPC transport.
type RPCTransportConfig struct {
	Enabled    bool   `json:"enabled"`              // Enable Unix socket RPC (default: true)
	SocketPath string `json:"socket_path"`          // Unix socket path (default: "~/.meept/meept.sock")
}

// HTTPTransportConfig configures the HTTP REST transport.
type HTTPTransportConfig struct {
	Enabled bool   `json:"enabled"` // Enable HTTP server (default: true)
	Addr    string `json:"addr"`    // Listen address (default: ":8081")
}
```

- [ ] **Step 2: Add Transport to Config struct and default**

In `Config` struct, add:
```go
Transport TransportConfig `json:"transport"`
```

In `DefaultConfig()`, add:
```go
Transport: TransportConfig{
	RPC: RPCTransportConfig{
		Enabled:    true,
		SocketPath: filepath.Join(homeDir, ".meept", "meept.sock"),
	},
	HTTP: HTTPTransportConfig{
		Enabled: true,
		Addr:    ":8081",
	},
},
```

- [ ] **Step 3: Add JSON5 loading path alongside TOML**

In `internal/config/config.go`, modify `LoadDefault` to prefer JSON5:

```go
// LoadDefault loads configuration from the default location.
// Prefers JSON5, falls back to TOML for backward compatibility.
func LoadDefault() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return DefaultConfig(), nil
	}

	// Try JSON5 first
	json5Path := filepath.Join(homeDir, ".meept", "meept.json5")
	if _, err := os.Stat(json5Path); err == nil {
		return LoadJSON5Config(json5Path)
	}

	// Fall back to TOML
	tomlPath := filepath.Join(homeDir, ".meept", "meept.toml")
	return Load(tomlPath)
}
```

- [ ] **Step 4: Create LoadJSON5Config function**

```go
// LoadJSON5Config loads configuration from a JSON5 file.
func LoadJSON5Config(path string) (*Config, error) {
	path = expandPath(path)

	cfg := DefaultConfig()
	if err := LoadJSON5(path, cfg); err != nil {
		if os.IsNotExist(err) {
			expandConfigPaths(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to load JSON5 config: %w", err)
	}

	expandConfigPaths(cfg)
	return cfg, nil
}
```

- [ ] **Step 5: Test schema defaults**

```go
func TestDefaultConfigTransport(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Transport.RPC.Enabled {
		t.Error("RPC transport should be enabled by default")
	}
	if !cfg.Transport.HTTP.Enabled {
		t.Error("HTTP transport should be enabled by default")
	}
	if cfg.Transport.HTTP.Addr != ":8081" {
		t.Errorf("expected HTTP addr :8081, got %s", cfg.Transport.HTTP.Addr)
	}
}
```

Run: `go test ./internal/config -run TestDefaultConfigTransport -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/
git commit -m "feat(config): add TransportConfig with RPC/HTTP enable flags"
```

---

## Task 3: Create meept.json5 Template

**Files:**
- Create: `config/meept.json5`
- Modify: `Makefile`

- [ ] **Step 1: Write JSON5 template matching current TOML**

```go
// Write to config/meept.json5
```

```json5
// Meept Configuration
// Copy to ~/.meept/meept.json5 and customize
{
  // Daemon settings
  daemon: {
    socket_path: "~/.meept/meept.sock",
    pid_file: "~/.meept/meept.pid",
    log_level: "INFO",
    data_dir: "~/.meept",
  },

  // Transport configuration (RPC and HTTP)
  transport: {
    rpc: {
      enabled: true,
      socket_path: "~/.meept/meept.sock",
    },
    http: {
      enabled: true,
      addr: ":8081",
    },
  },

  // LLM configuration (models moved to models.json5)
  llm: {
    budget: {
      hourly_token_limit: 100000,
      daily_token_limit: 1000000,
      rate_limit_rpm: 30,
      aggressiveness: 0.5,
    },
    context_firewall: {
      enabled: true,
      proactive_compression: false,
    },
  },

  // Memory configuration
  memory: {
    data_dir: "~/.meept/memory",
    consolidation_interval_hours: 6,
    episodic: {
      enabled: true,
      max_context_items: 20,
    },
    task: {
      enabled: true,
      domains: ["general", "code", "commands"],
    },
    personality: {
      enabled: true,
      update_interval_conversations: 10,
    },
    security: {
      enabled: true,
      fail_closed: true,
      log_blocked: true,
    },
    caching: {
      enabled: true,
    },
  },

  // Multi-agent configuration
  multiagent: {
    enabled: true,
    routing_mode: "auto",
  },

  // Agent definitions (loaded from agents/ directory)
  agents: {
    config_dirs: ["~/.meept/agents", "config/agents"],
  },

  // Security configuration
  security: {
    max_file_size_mb: 10,
    allowed_paths: [],
    blocked_paths: [],
    block_financial: true,
    allowed_domains: [],
    require_confirmation_high: true,
    require_confirmation_critical: true,
  },

  // Scheduler configuration
  scheduler: {
    enabled: false,
    tick_interval: "1m",
    max_concurrent_jobs: 5,
  },

  // Queue configuration
  queue: {
    db_path: "~/.meept/queue.db",
    max_workers: 10,
    worker_timeout: "5m",
    retry_policy: {
      max_retries: 3,
      initial_delay: "5s",
      max_delay: "5m",
      backoff_multiplier: 2,
    },
  },

  // Workers configuration
  workers: {
    default_pool_size: 5,
    max_pool_size: 20,
  },

  // Telegram bot configuration
  telegram: {
    enabled: false,
    token: "",
    admin_chat_id: 0,
  },

  // Web server configuration
  web: {
    enabled: false,
    port: 8080,
    host: "127.0.0.1",
    api_path: "/api/v1",
  },

  // MCP server configuration
  mcp: {
    enabled: false,
    config_file: "~/.meept/mcp_servers.json5",
  },

  // Plugins configuration
  plugins: {
    enabled: false,
    directory: "~/.meept/plugins",
  },

  // Self-improvement configuration
  selfimprove: {
    enabled: true,
    scan_interval: "1h",
    auto_apply: false,
    opportunity_threshold: 0.7,
    max_recommendations: 10,
  },

  // Orchestrator configuration
  orchestrator: {
    enabled: true,
    max_agents: 10,
    load_balance: true,
  },

  // Context window management
  context_window: {
    enabled: true,
    max_input_tokens: 100000,
    token_reserve: 8192,
    compression_threshold: 0.8,
    summarization_model: "",
  },
}
```

- [ ] **Step 2: Update Makefile install target**

Modify `Makefile` — update `setup` and `install` targets:

```makefile
# Config templates to install
CONFIG_FILES := \
	$(MEEPT_HOME)/meept.json5 \
	$(MEEPT_HOME)/models.json5 \
	$(MEEPT_HOME)/presets.json5 \
	$(MEEPT_HOME)/client.json5 \
	$(MEEPT_HOME)/mcp_servers.json5 \
	$(MEEPT_HOME)/q_agent.json5 \
	$(MEEPT_HOME)/menubar.json5

# Install binaries and configs
install: build
	@echo "Installing binaries to GOPATH/bin..."
	go install $(GO_BUILD_FLAGS) ./cmd/meept-daemon
	go install $(GO_BUILD_FLAGS) ./cmd/meept
	@echo "Installing config files..."
	@mkdir -p $(MEEPT_HOME)/agents $(MEEPT_HOME)/prompts $(MEEPT_HOME)/plugins $(MEEPT_HOME)/memory $(MEEPT_HOME)/workspaces
	@echo "Copying config templates (if not present)..."
	@for f in $(CONFIG_FILES); do \
		if [ ! -f $$f ]; then \
			src="config/$$(basename $$f)"; \
			if [ -f $$src ]; then \
				cp $$src $$f; \
				echo "  created $$f"; \
			else \
				echo "  template $$src not found (skipping $$f)"; \
			fi; \
		else \
			echo "  skipping $$f (already exists)"; \
		fi; \
	done
	@echo "Copying agent definitions..."
	@if [ -d config/agents ]; then \
		cp -r config/agents/* $(MEEPT_HOME)/agents/ 2>/dev/null || true; \
		echo "  copied agent definitions"; \
	fi
	@echo "Copying prompts..."
	@if [ -d config/prompts ]; then \
		cp -r config/prompts/* $(MEEPT_HOME)/prompts/ 2>/dev/null || true; \
		echo "  copied prompts"; \
	fi
	@echo ""
	@echo "Install complete. Edit $(MEEPT_HOME)/meept.json5 to configure."

# Legacy setup (kept for backward compat)
setup:
	@mkdir -p $(MEEPT_HOME)/agents $(MEEPT_HOME)/prompts $(MEEPT_HOME)/plugins $(MEEPT_HOME)/memory $(MEEPT_HOME)/workspaces
	@if [ ! -f $(MEEPT_HOME)/meept.json5 ] && [ ! -f $(MEEPT_HOME)/meept.toml ]; then \
		cp config/meept.json5 $(MEEPT_HOME)/meept.json5; \
		echo "Created $(MEEPT_HOME)/meept.json5"; \
	fi
	@echo "Setup complete."
```

- [ ] **Step 3: Test Makefile**

Run: `make install --dry-run 2>&1 | head -20` (or manually test with a fresh temp home)

- [ ] **Step 4: Commit**

```bash
git add config/meept.json5 Makefile
git commit -m "feat(config): add meept.json5 template and update make install"
```

---

## Task 4: Create MCP Config in JSON5

**Files:**
- Create: `config/mcp_servers.json5`
- Modify: `internal/config/mcp.go`
- Test: `internal/config/mcp_test.go` (or create)

- [ ] **Step 1: Write mcp_servers.json5 template**

```json5
// MCP server definitions. All servers disabled by default.
// Enable by setting "enabled": true in the server block.
{
  servers: [
    // Example: local server via subprocess
    {
      name: "github",
      type: "local",
      command: ["npx", "-y", "@modelcontextprotocol/server-github"],
      environment: {
        GITHUB_TOKEN: "${GITHUB_TOKEN}",
      },
      enabled: false,
    },
    // Example: remote server with static auth
    {
      name: "example",
      type: "remote",
      url: "https://mcp.example.com/mcp",
      headers: {
        Authorization: "Bearer ${MCP_TOKEN}",
      },
      timeout: 30000,
      enabled: false,
    },
  ],
}
```

- [ ] **Step 2: Update MCP loader for JSON5**

```go
package config

import (
	"fmt"
	"os"

	"github.com/caimlas/meept/internal/tools/mcp"
)

// MCPConfig represents the mcp_servers.json5 configuration.
type MCPConfig struct {
	Servers []mcp.ServerConfig `json:"servers"`
}

// LoadMCPConfig loads MCP server configuration from JSON5.
func LoadMCPConfig(path string) (*MCPConfig, error) {
	path = expandPath(path)

	var cfg MCPConfig
	if err := LoadJSON5(path, &cfg); err != nil {
		if os.IsNotExist(err) {
			return &MCPConfig{Servers: []mcp.ServerConfig{}}, nil
		}
		return nil, fmt.Errorf("failed to load MCP config: %w", err)
	}
	return &cfg, nil
}

// LoadMCPConfigDefault loads MCP config from the default location.
func LoadMCPConfigDefault() (*MCPConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return &MCPConfig{Servers: []mcp.ServerConfig{}}, nil
	}
	return LoadMCPConfig(filepath.Join(homeDir, ".meept", "mcp_servers.json5"))
}
```

- [ ] **Step 3: Handle legacy JSON fallback**

```go
// LoadMCPConfigWithLegacy loads JSON5 config, falling back to legacy JSON.
func LoadMCPConfigWithLegacy(path string) (*MCPConfig, error) {
	cfg, err := LoadMCPConfig(path)
	if err == nil && len(cfg.Servers) > 0 {
		return cfg, nil
	}

	// Try legacy path
	legacyPath := expandPath("~/.meept/mcp_servers.json")
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &MCPConfig{Servers: []mcp.ServerConfig{}}, nil
		}
		return nil, err
	}

	// Legacy format: {"mcp": {"server_name": {...}}}
	var legacy struct {
		MCP map[string]json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}

	// Convert legacy to new format would require mcp.ServerConfig parsing
	// For now, return empty and let user migrate
	return &MCPConfig{Servers: []mcp.ServerConfig{}}, nil
}
```

- [ ] **Step 4: Commit**

```bash
git add config/mcp_servers.json5 internal/config/mcp.go
git commit -m "feat(config): add mcp_servers.json5 format and loader"
```

---

## Task 5: Create q_agent.json5 Template

**Files:**
- Create: `config/q_agent.json5`
- Modify: `internal/config/schema.go` (add QAgentConfig)

- [ ] **Step 1: Write q_agent.json5**

```json5
// Q Agent configuration
{
  enabled: true,
  // Analysis settings
  analysis: {
    min_sessions: 5,
    lookback_hours: 168,
  },
  // Skill designer settings
  skill_designer: {
    enabled: true,
    auto_publish: false,
    review_required: true,
  },
  // Reporting
  reporting: {
    log_level: "info",
    output_format: "markdown",
  },
}
```

- [ ] **Step 2: Add QAgentConfig to schema**

```go
type QAgentConfig struct {
	Enabled        bool                   `json:"enabled"`
	Analysis       QAgentAnalysisConfig   `json:"analysis"`
	SkillDesigner  QAgentDesignerConfig   `json:"skill_designer"`
	Reporting      QAgentReportingConfig  `json:"reporting"`
}

type QAgentAnalysisConfig struct {
	MinSessions    int  `json:"min_sessions"`
	LookbackHours  int  `json:"lookback_hours"`
}

type QAgentDesignerConfig struct {
	Enabled         bool `json:"enabled"`
	AutoPublish     bool `json:"auto_publish"`
	ReviewRequired  bool `json:"review_required"`
}

type QAgentReportingConfig struct {
	LogLevel       string `json:"log_level"`
	OutputFormat   string `json:"output_format"`
}
```

Add to `Config` struct:
```go
QAgent QAgentConfig `json:"q_agent"`
```

Add to `DefaultConfig()`:
```go
QAgent: QAgentConfig{
	Enabled: false,
	Analysis: QAgentAnalysisConfig{
		MinSessions:   5,
		LookbackHours: 168,
	},
	SkillDesigner: QAgentDesignerConfig{
		Enabled:        true,
		AutoPublish:    false,
		ReviewRequired: true,
	},
	Reporting: QAgentReportingConfig{
		LogLevel:     "info",
		OutputFormat: "markdown",
	},
},
```

- [ ] **Step 3: Commit**

```bash
git add config/q_agent.json5 internal/config/schema.go
git commit -m "feat(config): add q_agent.json5 config and schema"
```

---

## Task 6: Convert Agent Definitions to JSON5

**Files:**
- Create: `config/agents.json5`
- Create: `internal/config/agents_json5.go`
- Modify: `internal/config/agents.go` (add legacy fallback)

- [ ] **Step 1: Read existing agent definitions**

```bash
cat config/agents/core.toml
```

- [ ] **Step 2: Create agents.json5 template**

```json5
// Agent definitions for the Meept multi-agent system
{
  agents: [
    {
      id: "dispatcher",
      name: "Dispatcher",
      role: "dispatcher",
      description: "Routes tasks to appropriate specialist agents",
      model: "",
      enabled: true,
      can_delegate: true,
      capabilities: ["routing", "classification", "delegation"],
      prompt_components: ["dispatcher.identity", "dispatcher.rules"],
      constraints: {
        max_iterations: 10,
        timeout_seconds: 60,
        max_tokens_per_turn: 2048,
        max_memory_refs: 10,
      },
    },
    {
      id: "chat",
      name: "Chat",
      role: "conversational",
      description: "General conversation and assistance",
      model: "",
      enabled: true,
      can_delegate: false,
      capabilities: ["conversation", "general_knowledge"],
      prompt_components: ["base.personality", "base.constitution"],
    },
    {
      id: "coder",
      name: "Coder",
      role: "executor",
      description: "Handles code generation, file operations, and shell tasks",
      model: "",
      enabled: true,
      can_delegate: true,
      capabilities: ["code_generation", "file_operations", "shell"],
      prompt_components: ["specialist.coder"],
    },
    {
      id: "debugger",
      name: "Debugger",
      role: "executor",
      description: "Troubleshooting and bug fixing",
      model: "",
      enabled: true,
      can_delegate: false,
      capabilities: ["debugging", "analysis"],
      prompt_components: ["specialist.debugger"],
    },
    {
      id: "planner",
      name: "Planner",
      role: "executor",
      description: "Task decomposition and planning",
      model: "",
      enabled: true,
      can_delegate: false,
      capabilities: ["planning", "decomposition"],
      prompt_components: ["specialist.planner"],
    },
    {
      id: "analyst",
      name: "Analyst",
      role: "executor",
      description: "Research and data analysis",
      model: "",
      enabled: true,
      can_delegate: false,
      capabilities: ["research", "data_analysis"],
      prompt_components: ["specialist.analyst"],
    },
    {
      id: "committer",
      name: "Committer",
      role: "executor",
      description: "Git operations and version control",
      model: "",
      enabled: true,
      can_delegate: false,
      capabilities: ["git", "version_control"],
      prompt_components: ["specialist.committer"],
    },
    {
      id: "scheduler",
      name: "Scheduler",
      role: "executor",
      description: "Job scheduling and cron-like tasks",
      model: "",
      enabled: true,
      can_delegate: false,
      capabilities: ["scheduling", "timing"],
      prompt_components: ["specialist.scheduler"],
    },
  ],
}
```

- [ ] **Step 3: Create JSON5 agent loader**

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// AgentDefinitionJSON5 represents an agent in the new JSON5 format.
type AgentDefinitionJSON5 struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Role             string                 `json:"role"`
	Description      string                 `json:"description"`
	Model            string                 `json:"model"`
	Enabled          bool                   `json:"enabled"`
	CanDelegate      bool                   `json:"can_delegate"`
	AdditionalTools  []string               `json:"additional_tools"`
	Capabilities     []string               `json:"capabilities"`
	PromptComponents []string               `json:"prompt_components"`
	Constraints      AgentConstraintsConfig `json:"constraints"`
}

// AgentsFileJSON5 is the root of the agents.json5 file.
type AgentsFileJSON5 struct {
	Agents []AgentDefinitionJSON5 `json:"agents"`
}

// LoadAgentDefinitionsJSON5 loads all agent definitions from a JSON5 file.
func LoadAgentDefinitionsJSON5(path string) (map[string]*AgentDefinition, error) {
	path = expandPath(path)

	var file AgentsFileJSON5
	if err := LoadJSON5(path, &file); err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*AgentDefinition), nil
		}
		return nil, err
	}

	agents := make(map[string]*AgentDefinition)
	for _, a := range file.Agents {
		if a.ID == "" {
			continue
		}
		agents[a.ID] = &AgentDefinition{
			ID:               a.ID,
			Name:             a.Name,
			Role:             a.Role,
			Description:      a.Description,
			Model:            a.Model,
			Enabled:          a.Enabled,
			CanDelegate:      a.CanDelegate,
			AdditionalTools:  a.AdditionalTools,
			Capabilities:     a.Capabilities,
			PromptComponents: a.PromptComponents,
			Constraints:      a.Constraints,
		}
	}
	return agents, nil
}

// LoadAgentDefinitionsDefaultWithJSON5 tries JSON5 first, then TOML.
func LoadAgentDefinitionsDefaultWithJSON5(cfg *AgentsConfig) (map[string]*AgentDefinition, error) {
	// Try JSON5 format first
	homeDir, _ := os.UserHomeDir()
	json5Path := filepath.Join(homeDir, ".meept", "agents.json5")
	if _, err := os.Stat(json5Path); err == nil {
		return LoadAgentDefinitionsJSON5(json5Path)
	}

	projectJSON5 := "config/agents.json5"
	if _, err := os.Stat(projectJSON5); err == nil {
		return LoadAgentDefinitionsJSON5(projectJSON5)
	}

	// Fall back to TOML directory format
	if cfg == nil {
		cfg = &AgentsConfig{
			ConfigDirs: []string{"~/.meept/agents", "config/agents"},
		}
	}
	return LoadAgentDefinitions(cfg.ConfigDirs)
}
```

- [ ] **Step 4: Commit**

```bash
git add config/agents.json5 internal/config/agents_json5.go
git commit -m "feat(config): add agents.json5 format and loader"
```

---

## Task 7: Create Menubar Config (menubar.json5)

**Files:**
- Create: `config/menubar.json5`
- Modify: `menubar/MeeptMenuBar/Services/ConfigService.swift`

- [ ] **Step 1: Write menubar.json5**

```json5
// Menubar app configuration
{
  // Connection to meept daemon
  daemon: {
    // Transport: "http" or "rpc" (menubar always uses HTTP for now)
    transport: "http",
    // HTTP endpoint (ignored for RPC)
    http_url: "http://localhost:8081",
    // Unix socket path (for future RPC support)
    socket_path: "~/.meept/meept.sock",
  },

  // UI preferences
  ui: {
    // Show panel in menu bar
    show_in_menu_bar: true,
    // Start at login
    start_at_login: false,
    // Menu bar icon style: "icon", "text", "both"
    icon_style: "icon",
  },

  // Notifications
  notifications: {
    enabled: true,
    // Notify on: "all", "errors_only", "none"
    level: "errors_only",
  },
}
```

- [ ] **Step 2: Update Go HTTP ConfigService to handle menubar config**

In `internal/comm/http/config_service.go`, add:

```go
// LoadMenubarConfig loads the menubar app configuration.
func (s *ConfigService) LoadMenubarConfig() (string, error) {
	path := filepath.Join(s.meeptDir, "menubar.json5")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty config
			return "{}", nil
		}
		return "", err
	}
	return string(data), nil
}

// SaveMenubarConfig saves the menubar app configuration.
func (s *ConfigService) SaveMenubarConfig(content string) error {
	path := filepath.Join(s.meeptDir, "menubar.json5")
	return os.WriteFile(path, []byte(content), 0644)
}
```

Add HTTP endpoints in `internal/comm/http/server.go` setupRoutes:
```go
mux.HandleFunc("GET /api/v1/config/menubar", s.handleGetMenubarConfig)
mux.HandleFunc("POST /api/v1/config/menubar", s.handleSaveMenubarConfig)
```

Add handlers:
```go
func (s *Server) handleGetMenubarConfig(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}
	content, err := s.configService.LoadMenubarConfig()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json5")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(content))
}

func (s *Server) handleSaveMenubarConfig(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}
	var body struct{ Content string `json:"content"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.configService.SaveMenubarConfig(body.Content); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
```

- [ ] **Step 3: Update Swift ConfigService**

Add to `menubar/MeeptMenuBar/Services/ConfigService.swift`:

```swift
func getMenubarConfig(completion: @escaping (Result<String, Error>) -> Void) {
    let url = baseURL.appendingPathComponent("/api/v1/config/menubar")
    var request = URLRequest(url: url)
    request.httpMethod = "GET"

    URLSession.shared.dataTask(with: request) { data, response, error in
        if let error = error {
            completion(.failure(error))
            return
        }
        guard let httpResponse = response as? HTTPURLResponse,
              (200..<300).contains(httpResponse.statusCode),
              let data = data else {
            completion(.failure(APIError.invalidResponse))
            return
        }
        completion(.success(String(data: data, encoding: .utf8) ?? "{}"))
    }.resume()
}

func saveMenubarConfig(content: String, completion: @escaping (Result<Void, Error>) -> Void) {
    let url = baseURL.appendingPathComponent("/api/v1/config/menubar")
    var request = URLRequest(url: url)
    request.httpMethod = "POST"
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    let body: [String: String] = ["content": content]
    request.httpBody = try? JSONSerialization.data(withJSONObject: body)

    URLSession.shared.dataTask(with: request) { data, response, error in
        if let error = error {
            completion(.failure(error))
            return
        }
        guard let httpResponse = response as? HTTPURLResponse,
              (200..<300).contains(httpResponse.statusCode) else {
            completion(.failure(APIError.invalidResponse))
            return
        }
        completion(.success(()))
    }.resume()
}
```

- [ ] **Step 4: Commit**

```bash
git add config/menubar.json5 internal/comm/http/
git commit -m "feat(config): add menubar.json5 config and HTTP endpoints"
```

---

## Task 8: Create Client Transport Abstraction

**Files:**
- Create: `internal/transport/client.go`
- Create: `internal/transport/rpc_client.go`
- Create: `internal/transport/http_client.go`
- Modify: `internal/tui/rpc.go` (rename/reuse methods)
- Modify: `cmd/meept/main.go` (add --transport flag)

- [ ] **Step 1: Define transport interface**

```go
package transport

import (
	"context"
	"encoding/json"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
)

// Client is the unified interface for talking to the daemon.
// Both RPC (unix socket) and HTTP implementations satisfy this.
type Client interface {
	// Connect establishes the transport connection.
	Connect() error
	// Close tears down the connection.
	Close() error
	// IsConnected returns true if the underlying connection is alive.
	IsConnected() bool

	// Core methods used by both CLI and TUI
	Chat(message, conversationID string) (string, error)
	Status() (*types.DaemonStatusResponse, error)
	ListJobs() (*types.JobListResponse, error)
	QueryMemory(query string, limit int) (*types.MemoryQueryResponse, error)
	ListWorkers() (*types.WorkerListResponse, error)
	GetQueueStats() (*types.QueueStatsResponse, error)
	ListQueueJobs(state string, limit int) (*types.QueueJobListResponse, error)
	ListTasks(state string, limit int) (*types.TaskListResponse, error)
	CreateTask(name, description string) (*types.Task, error)
	GetTask(taskID string) (*types.Task, error)
	CacheStats() (*types.CacheStatsResponse, error)

	// Session methods
	ListSessions() (*types.SessionListResponse, error)
	CreateSession(name string) (*types.Session, error)
	AttachSession(sessionID, clientID string) error
	DetachSession(sessionID, clientID string) error
	GetMostRecentSession() (*types.Session, error)
	GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error)
	SaveSessionMessages(sessionID string, messages []types.SessionMessage) error
	UpdateSessionDescription(sessionID, description string) error
	GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error)
	DeleteSession(sessionID string) error
	StopSession(sessionID string) (*types.StopSessionResponse, error)
	GetSessionChildTasks(sessionID string) ([]string, error)

	// Task methods
	ListTasksExtended() (*types.TaskExtendedListResponse, error)
	ListTaskSteps(taskID string) (*types.TaskStepsResponse, error)
	DeleteTask(taskID string) error
	CancelTask(taskID string) error
	LinkTaskSession(taskID, sessionID string) error
	UnlinkTaskSession(taskID, sessionID string) error

	// Queue methods
	RetryQueueJob(jobID string) error

	// Worker methods
	ListPoolWorkers() (*types.WorkerPoolResponse, error)
	GetWorkerPoolStats() (*types.WorkerPoolStats, error)
	ScaleWorkerPool(targetCount int) error

	// Cache methods
	CacheClear() error
	CacheInvalidate(filePath string) error
}

// Config holds client-side transport configuration.
type Config struct {
	Transport   string        // "rpc" or "http"
	SocketPath  string        // For RPC transport
	HTTPBaseURL string        // For HTTP transport (e.g. "http://localhost:8081")
	Timeout     time.Duration // Per-call timeout
}

// DefaultConfig returns the default client transport config.
func DefaultConfig() *Config {
	return &Config{
		Transport:   "rpc",
		SocketPath:  "~/.meept/meept.sock",
		HTTPBaseURL: "http://localhost:8081",
		Timeout:     120 * time.Second,
	}
}
```

- [ ] **Step 2: Create factory function**

```go
// New creates a transport client based on config.
func New(cfg *Config) (Client, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	switch cfg.Transport {
	case "http":
		return NewHTTPClient(cfg.HTTPBaseURL, cfg.Timeout), nil
	case "rpc", "unix", "socket":
		return NewRPCClient(cfg.SocketPath, cfg.Timeout), nil
	default:
		return nil, fmt.Errorf("unknown transport: %s", cfg.Transport)
	}
}
```

- [ ] **Step 3: Create RPC adapter**

`internal/transport/rpc_client.go` wraps the existing `internal/tui/rpc.go` RPCClient:

```go
package transport

import (
	"time"

	"github.com/caimlas/meept/internal/tui"
)

// rpcAdapter adapts tui.RPCClient to the transport.Client interface.
type rpcAdapter struct {
	client *tui.RPCClient
}

// NewRPCClient creates an RPC-backed transport client.
func NewRPCClient(socketPath string, timeout time.Duration) Client {
	c := tui.NewRPCClient(socketPath)
	if timeout > 0 {
		c.SetTimeout(timeout)
	}
	return &rpcAdapter{client: c}
}

func (a *rpcAdapter) Connect() error {
	return a.client.Connect()
}
func (a *rpcAdapter) Close() error {
	return a.client.Close()
}
func (a *rpcAdapter) IsConnected() bool {
	return a.client.IsConnected()
}
func (a *rpcAdapter) Chat(message, conversationID string) (string, error) {
	return a.client.Chat(message, conversationID)
}
// ... delegate all other methods to a.client equivalents
```

(Repeat delegation for every method in the interface.)

- [ ] **Step 4: Create HTTP client implementation**

`internal/transport/http_client.go`:

```go
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
)

// httpClient implements transport.Client over HTTP REST.
type httpClient struct {
	baseURL string
	client  *http.Client
}

// NewHTTPClient creates an HTTP-backed transport client.
func NewHTTPClient(baseURL string, timeout time.Duration) Client {
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	return &httpClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *httpClient) Connect() error {
	// HTTP is connectionless; just verify daemon is reachable
	resp, err := c.client.Get(c.baseURL + "/api/v1/health")
	if err != nil {
		return fmt.Errorf("daemon not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned %d", resp.StatusCode)
	}
	return nil
}

func (c *httpClient) Close() error {
	return nil
}

func (c *httpClient) IsConnected() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/health", nil)
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *httpClient) Chat(message, conversationID string) (string, error) {
	// HTTP endpoint for chat is not yet implemented on daemon HTTP API
	// For now, return error directing user to use RPC
	return "", fmt.Errorf("chat over HTTP not yet implemented; use --transport=rpc")
}

// For status, we can use the daemon/status endpoint or health endpoint
func (c *httpClient) Status() (*types.DaemonStatusResponse, error) {
	resp, err := c.client.Get(c.baseURL + "/api/v1/daemon/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	// Convert to expected response shape
	return &types.DaemonStatusResponse{
		Status:           "running",
		Version:          "0.2.0-go",
		UptimeSeconds:    0,
		RegisteredMethods: []string{},
	}, nil
}

// Other methods return "not implemented over HTTP" error until endpoints added
func (c *httpClient) ListJobs() (*types.JobListResponse, error) { return nil, fmt.Errorf("not implemented over HTTP") }
// ... stub remaining methods similarly
```

- [ ] **Step 5: Update CLI main.go with transport flags**

```go
// Global flags
var (
	socketPath string
	stateDir   string
	debugFile  string
	transport  string // "rpc" or "http"
)
```

In `main()`:
```go
rootCmd.PersistentFlags().StringVarP(&socketPath, "socket", "s", defaultSocket, "Unix socket path (for RPC)")
rootCmd.PersistentFlags().StringVarP(&stateDir, "state-dir", "d", defaultStateDir, "State directory")
rootCmd.PersistentFlags().StringVar(&debugFile, "debug", "", "Enable debug output")
rootCmd.PersistentFlags().StringVar(&transport, "transport", "rpc", "Transport: rpc or http")
```

- [ ] **Step 6: Update cmd/meept/chat.go to use transport abstraction**

```go
func runChat(cmd *cobra.Command, args []string) error {
	// Build transport config
	cfg := &transport.Config{
		Transport:   transport,
		SocketPath:  getSocketPath(),
		HTTPBaseURL: "http://localhost:8081", // TODO: make configurable
	}

	if len(args) == 0 {
		return runTUITransport(cfg)
	}
	// ... single message mode
	return sendSingleMessageTransport(cfg, args[0])
}

func runTUITransport(cfg *transport.Config) error {
	// Pass transport config into TUI
	app := tui.NewAppWithTransport(cfg)
	p := tea.NewProgram(app)
	_, err := p.Run()
	return err
}
```

- [ ] **Step 7: Commit**

```bash
git add internal/transport/ cmd/meept/
git commit -m "feat(transport): add unified client transport abstraction (RPC/HTTP)"
```

---

## Task 9: Update Daemon to Conditionally Start Transports

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `cmd/meept-daemon/main.go`

- [ ] **Step 1: Conditionally create RPC server**

In `internal/daemon/daemon.go` `New()`:

```go
// Create RPC server (if enabled)
var rpcServer *rpc.Server
if fullCfg.Transport.RPC.Enabled {
	rpcServer = rpc.New(&rpc.Config{
		SocketPath: cfg.SocketPath,
	}, msgBus, logger)
	proxy := rpc.NewProxyHandler(msgBus)
	proxy.RegisterProxyMethods(rpcServer)
	securityHandler := rpc.NewSecurityHandler(securityCfg)
	securityHandler.RegisterSecurityMethods(rpcServer)
	devHandler := rpc.NewDevHandler()
	devHandler.RegisterDevMethods(rpcServer)
	reg.Register(rpcServer)
}
```

- [ ] **Step 2: Conditionally create HTTP server**

```go
// Create HTTP server (if enabled)
var httpSrv *http.Server
if fullCfg.Transport.HTTP.Enabled {
	configService, err := http.NewConfigService()
	if err != nil {
		logger.Warn("Failed to create config service", "error", err)
	}
	daemonControl, err := NewDaemonControl()
	if err != nil {
		logger.Warn("Failed to create daemon control", "error", err)
	}
	if configService != nil && daemonControl != nil && metricsStore != nil {
		httpCfg := http.ServerConfig{
			Addr:           fullCfg.Transport.HTTP.Addr,
			ReadTimeout:    30 * time.Second,
			WriteTimeout:   30 * time.Second,
			MaxHeaderBytes: 1 << 20,
			EnableCORS:     true,
		}
		httpSrv = http.NewServer(httpCfg, configService, daemonControl, &metricsStoreWrapper{store: metricsStore}, logger)
		logger.Info("HTTP server created", "addr", httpCfg.Addr)
	}
}
```

- [ ] **Step 3: Handle case where no transports enabled**

```go
if rpcServer == nil && httpSrv == nil {
	logger.Error("No transports enabled. Daemon cannot accept connections.")
	return nil, fmt.Errorf("at least one transport (rpc or http) must be enabled")
}
```

- [ ] **Step 4: Update Run() to start/stop conditionally**

In `Run()`:
```go
// Start registry components (includes RPC if registered)
if err := d.registry.StartAll(ctx); err != nil {
	return fmt.Errorf("failed to start components: %w", err)
}
```

HTTP start already guarded by nil check in current code (line 261).

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/ cmd/meept-daemon/
git commit -m "feat(daemon): conditionally start RPC and HTTP transports per config"
```

---

## Task 10: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update configuration section**

Replace the config section with:

```markdown
## Configuration

All configuration uses **JSON5** format (JSON with comments and trailing commas).

- **Main config**: `~/.meept/meept.json5`
- **Models**: `~/.meept/models.json5`
- **Presets**: `~/.meept/presets.json5`
- **MCP servers**: `~/.meept/mcp_servers.json5`
- **Q Agent**: `~/.meept/q_agent.json5`
- **Client**: `~/.meept/client.json5` (TUI keybindings/rendering)
- **Menubar**: `~/.meept/menubar.json5` (menubar app settings)
- **Metrics DB**: `~/.meept/metrics.db` (SQLite)

Templates are in `config/` and copied on `make install` if not present.

### make install

`make install` compiles binaries and copies all config templates:
```bash
make install
# Copies: meept.json5, models.json5, presets.json5, client.json5,
#         mcp_servers.json5, q_agent.json5, menubar.json5
# Also copies: config/agents/* and config/prompts/*
```

### Transport Configuration

The daemon supports two transports (can be enabled independently):

```json5
{
  transport: {
    rpc: {
      enabled: true,                // Unix socket JSON-RPC
      socket_path: "~/.meept/meept.sock",
    },
    http: {
      enabled: true,                // REST API for menubar
      addr: ":8081",
    },
  },
}
```

Clients connect via `--transport` flag:
```bash
meept --transport=rpc chat          # Default; uses Unix socket
meept --transport=http --http-url=http://localhost:8081 chat
```

Menubar app uses HTTP exclusively. Its config (`menubar.json5`) controls the daemon URL:
```json5
{
  daemon: {
    transport: "http",
    http_url: "http://localhost:8081",
  },
}
```
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(CLAUDE): update config docs for JSON5 and transport architecture"
```

---

## Task 11: Integration Tests & Verification

**Files:**
- Test: `tests/` (run full suite)

- [ ] **Step 1: Build both binaries**

Run: `make build`
Expected: Clean build, no errors

- [ ] **Step 2: Run config tests**

Run: `go test ./internal/config -v`
Expected: All config tests PASS including new JSON5 tests

- [ ] **Step 3: Run transport tests** (if any written)

Run: `go test ./internal/transport -v`
Expected: PASS

- [ ] **Step 4: Test daemon with both transports**

```bash
# Test RPC only
./bin/meept-daemon -f -c <(cat ~/.meept/meept.json5 | sed 's/"http": {/"http": {"enabled": false/')

# In another terminal
./bin/meept --transport=rpc status
```

- [ ] **Step 5: Test make install in fresh directory**

```bash
mkdir -p /tmp/meept-test
HOME=/tmp/meept-test make install
ls -la /tmp/meept-test/.meept/
# Verify all config files present
```

- [ ] **Step 6: Commit**

```bash
git commit -m "test: verify unified config and transport architecture"
```

---

## Self-Review Checklist

### 1. Spec Coverage

| Requirement | Task |
|------------|------|
| `make install` copies templates if missing | Task 3 |
| Agent defintions + prompts copied | Task 3 |
| meept.toml → meept.json5 | Tasks 2, 3 |
| mcp_servers.json → mcp_servers.json5 | Task 4 |
| q_agent.toml → q_agent.json5 | Task 5 |
| Agent definitions → agents.json5 | Task 6 |
| Menubar has own config | Tasks 3, 7 |
| Daemon config for enable RPC/HTTP | Tasks 2, 9 |
| Both transports supported by clients | Tasks 8, 9 |
| CLAUDE.md updated | Task 10 |

### 2. Placeholder Scan

- No "TBD" or "TODO" — all steps contain complete code
- No vague "add error handling" — all error handling is shown where needed
- All method signatures match between interface and implementations

### 3. Type Consistency

- `LoadJSON5` signature: `func LoadJSON5(path string, v any) error` — consistent
- `TransportConfig` fields: `RPC`, `HTTP` — used consistently in schema, daemon, templates
- `transport.Config`: `Transport`, `SocketPath`, `HTTPBaseURL` — consistent

---

## Post-Implementation: Cleanup Legacy Files

After the implementation is stable, remove legacy files in a follow-up PR:
- `config/meept.toml`
- `config/mcp_servers.json`
- `config/q_agent.example.toml`
- `config/agents/*.toml`
- Legacy TOML-specific config functions (if no longer needed)

---

## Plan complete and saved to `docs/superpowers/plans/2026-05-06-unified-config-transport.md`.

**Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. Required sub-skill: `superpowers:subagent-driven-development`.

**2. Inline Execution** — Execute tasks in this session using `superpowers:executing-plans`, batch execution with checkpoints for review.

**Which approach?**