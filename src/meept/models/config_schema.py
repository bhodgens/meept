"""Pydantic models that mirror the ``meept.toml`` configuration file."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from pydantic import BaseModel, Field


# ---------------------------------------------------------------------------
# Section models
# ---------------------------------------------------------------------------


class DaemonConfig(BaseModel):
    """``[daemon]`` section."""

    socket_path: str = "~/.meept/meept.sock"
    pid_file: str = "~/.meept/meept.pid"
    log_level: str = "INFO"
    data_dir: str = "~/.meept"


class LLMModelConfig(BaseModel):
    """A single entry under ``[llm.models.<name>]``."""

    base_url: str = "http://localhost:11434/v1"
    model_id: str = "llama3.2"
    api_key: str = ""
    cost_per_million_input: float = 0.0
    cost_per_million_output: float = 0.0


class LLMBudgetConfig(BaseModel):
    """``[llm.budget]`` section."""

    hourly_token_limit: int = 100_000
    daily_token_limit: int = 1_000_000
    rate_limit_rpm: int = 30
    aggressiveness: float = Field(default=0.5, ge=0.0, le=1.0)


class LLMConfig(BaseModel):
    """``[llm]`` section."""

    default_model: str = "default"
    models: dict[str, LLMModelConfig] = Field(default_factory=dict)
    budget: LLMBudgetConfig = Field(default_factory=LLMBudgetConfig)


class EpisodicMemoryConfig(BaseModel):
    """``[memory.episodic]`` section."""

    enabled: bool = True
    max_context_items: int = 20


class TaskMemoryConfig(BaseModel):
    """``[memory.task]`` section."""

    enabled: bool = True
    domains: list[str] = Field(default_factory=lambda: ["general", "code", "commands"])


class PersonalityMemoryConfig(BaseModel):
    """``[memory.personality]`` section."""

    enabled: bool = True
    update_interval_conversations: int = 10


class MemoryConfig(BaseModel):
    """``[memory]`` section."""

    data_dir: str = "~/.meept/memory"
    consolidation_interval_hours: int = 6
    episodic: EpisodicMemoryConfig = Field(default_factory=EpisodicMemoryConfig)
    task: TaskMemoryConfig = Field(default_factory=TaskMemoryConfig)
    personality: PersonalityMemoryConfig = Field(default_factory=PersonalityMemoryConfig)


class SecurityConfig(BaseModel):
    """``[security]`` section."""

    security_db: str = "~/.meept/security.db"
    sanitize_inputs: bool = True
    llm_filter_external: bool = False
    require_confirmation_high: bool = True
    require_confirmation_critical: bool = True
    block_financial: bool = True
    allowed_paths: list[str] = Field(default_factory=lambda: ["~/*"])
    blocked_paths: list[str] = Field(
        default_factory=lambda: ["~/.ssh/*", "~/.gnupg/*", "~/.meept/meept.toml"]
    )
    tirith_enabled: bool = False
    tirith_binary: str = "tirith"


class SchedulerConfig(BaseModel):
    """``[scheduler]`` section."""

    enabled: bool = True
    timezone: str = "UTC"


class TelegramConfig(BaseModel):
    """``[telegram]`` section."""

    enabled: bool = False
    token: str = ""
    creator_id: int = 0


class WebConfig(BaseModel):
    """``[web]`` section."""

    enabled: bool = False
    host: str = "127.0.0.1"
    port: int = 8420
    secret_key: str = ""


class McpConfig(BaseModel):
    """``[mcp]`` section."""

    enabled: bool = False
    config_file: str = "~/.meept/mcp_servers.json"


class PluginConfig(BaseModel):
    """``[plugins]`` section."""

    enabled: bool = True
    directory: str = "~/.meept/plugins"


class WorkspaceConfig(BaseModel):
    """``[workspace]`` section."""

    enabled: bool = True
    base_dir: str = "~/.meept/workspaces"
    auto_commit: bool = True
    commit_on_plan: bool = True
    commit_on_step: bool = True
    cleanup_completed: bool = False


class SkillsConfig(BaseModel):
    """``[skills]`` section."""

    enabled: bool = False


class ClawSkillsConfig(BaseModel):
    """``[clawskills]`` section -- third-party skills from ClawHub."""

    enabled: bool = False
    registry_url: str = "https://clawhub.ai"
    install_dir: str = "~/.meept/clawskills"
    auto_update: bool = False
    max_installed: int = 50
    default_risk_level: str = "high"
    max_iterations: int = 10
    blocked_slugs: list[str] = Field(default_factory=list)


class AIInfraConfig(BaseModel):
    """Configuration for connecting to ai-infra LLM service."""

    enabled: bool = False
    base_url: str = "http://localhost:8100"
    api_key_env: str = "MEEPT_AI_INFRA_KEY"
    analysis_model: str = "claude-opus-4-5-20251101"
    generation_model: str = "claude-sonnet-4-5-20241022"
    review_model: str = "claude-opus-4-5-20251101"
    timeout_seconds: float = 120.0
    max_retries: int = 3


class SandboxConfig(BaseModel):
    """Configuration for the git worktree sandbox."""

    worktree_dir: str = "~/.meept/selfimprove/worktrees"
    cleanup_on_success: bool = True
    cleanup_on_failure: bool = False
    max_worktrees: int = 5
    test_timeout_seconds: float = 300.0


class SafetyConfig(BaseModel):
    """Safety guardrails for self-improvement."""

    require_human_approval: bool = True
    max_files_per_fix: int = 10
    max_lines_changed_per_fix: int = 500
    blocked_paths: list[str] = Field(
        default_factory=lambda: [
            "src/meept/selfimprove/*",
            "src/meept/security/*",
            ".git/*",
            "*.toml",
            "*.json5",
            "**/*credentials*",
            "**/*secret*",
            "**/.env*",
        ]
    )
    allowed_risk_levels: list[str] = Field(
        default_factory=lambda: ["low", "medium", "high"]
    )
    block_critical_risk: bool = True
    require_tests_pass: bool = True
    min_confidence_threshold: float = 0.7


class DetectionConfig(BaseModel):
    """Configuration for issue detection."""

    scan_pytest: bool = True
    scan_runtime_logs: bool = True
    scan_type_check: bool = True
    scan_lint: bool = True
    log_file: str = "~/.meept/meept.log"
    log_lookback_hours: int = 24
    pytest_args: list[str] = Field(default_factory=lambda: ["-v", "--tb=short"])
    mypy_args: list[str] = Field(default_factory=lambda: ["--ignore-missing-imports"])
    ruff_args: list[str] = Field(default_factory=list)


class SelfImproveConfig(BaseModel):
    """``[selfimprove]`` configuration section."""

    enabled: bool = False
    data_dir: str = "~/.meept/selfimprove"
    max_iterations_per_cycle: int = 5
    max_fixes_per_cycle: int = 10
    auto_run_interval_hours: int = 0
    ai_infra: AIInfraConfig = Field(default_factory=AIInfraConfig)
    sandbox: SandboxConfig = Field(default_factory=SandboxConfig)
    safety: SafetyConfig = Field(default_factory=SafetyConfig)
    detection: DetectionConfig = Field(default_factory=DetectionConfig)


# ---------------------------------------------------------------------------
# Top-level aggregator
# ---------------------------------------------------------------------------


class MeeptSettings(BaseModel):
    """Root model representing the entire ``meept.toml`` config file.

    Every section is optional and falls back to sensible defaults so that a
    completely empty TOML file still yields a valid configuration.
    """

    daemon: DaemonConfig = Field(default_factory=DaemonConfig)
    llm: LLMConfig = Field(default_factory=LLMConfig)
    memory: MemoryConfig = Field(default_factory=MemoryConfig)
    security: SecurityConfig = Field(default_factory=SecurityConfig)
    scheduler: SchedulerConfig = Field(default_factory=SchedulerConfig)
    telegram: TelegramConfig = Field(default_factory=TelegramConfig)
    web: WebConfig = Field(default_factory=WebConfig)
    mcp: McpConfig = Field(default_factory=McpConfig)
    plugins: PluginConfig = Field(default_factory=PluginConfig)
    skills: SkillsConfig = Field(default_factory=SkillsConfig)
    clawskills: ClawSkillsConfig = Field(default_factory=ClawSkillsConfig)
    workspace: WorkspaceConfig = Field(default_factory=WorkspaceConfig)
    selfimprove: SelfImproveConfig = Field(default_factory=SelfImproveConfig)

    def expanded_path(self, raw: str) -> Path:
        """Return *raw* with ``~`` expanded to the real home directory."""
        return Path(raw).expanduser()
