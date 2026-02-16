"""Configuration for the self-improvement system."""

from __future__ import annotations

from pathlib import Path
from typing import TYPE_CHECKING

from pydantic import BaseModel, Field

if TYPE_CHECKING:
    pass


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
            "src/meept/selfimprove/*",  # Cannot modify itself
            "src/meept/security/*",  # Cannot modify security engine
            ".git/*",  # Cannot touch git internals
            "*.toml",  # Cannot modify config files
            "*.json5",  # Cannot modify config files
            "**/*credentials*",  # Cannot touch credentials
            "**/*secret*",  # Cannot touch secrets
            "**/.env*",  # Cannot touch environment files
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
    auto_run_interval_hours: int = 0  # 0 = disabled
    ai_infra: AIInfraConfig = Field(default_factory=AIInfraConfig)
    sandbox: SandboxConfig = Field(default_factory=SandboxConfig)
    safety: SafetyConfig = Field(default_factory=SafetyConfig)
    detection: DetectionConfig = Field(default_factory=DetectionConfig)

    def expanded_path(self, raw: str) -> Path:
        """Return *raw* with ``~`` expanded to the real home directory."""
        return Path(raw).expanduser()

    @property
    def data_path(self) -> Path:
        """Return the expanded data directory path."""
        return self.expanded_path(self.data_dir)
