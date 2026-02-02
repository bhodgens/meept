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

    def expanded_path(self, raw: str) -> Path:
        """Return *raw* with ``~`` expanded to the real home directory."""
        return Path(raw).expanduser()
