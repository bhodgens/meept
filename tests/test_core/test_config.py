"""Tests for the TOML configuration loader."""

from __future__ import annotations

import os
from pathlib import Path

import pytest

from meept.core.config import MeeptConfig


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_load_toml(tmp_config: Path) -> None:
    """Loading a well-formed TOML file should populate settings correctly."""
    cfg = MeeptConfig(config_path=tmp_config)

    assert cfg.settings.daemon.log_level == "DEBUG"
    assert cfg.settings.llm.default_model == "test"
    assert cfg.settings.llm.budget.hourly_token_limit == 10000


def test_env_expansion(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """``${VAR}`` references inside string values should be expanded from the environment."""
    monkeypatch.setenv("MEEPT_TEST_KEY", "expanded-secret")

    config_dir = tmp_path / "env_cfg"
    config_dir.mkdir()

    toml_text = """\
[llm.models.default]
base_url = "http://localhost:11434/v1"
model_id = "test"
api_key = "${MEEPT_TEST_KEY}"
"""
    toml_path = config_dir / "meept.toml"
    toml_path.write_text(toml_text, encoding="utf-8")

    cfg = MeeptConfig(config_path=toml_path)
    default_model_cfg = cfg.settings.llm.models["default"]
    assert default_model_cfg.api_key == "expanded-secret"


def test_path_expansion(tmp_path: Path) -> None:
    """Tilde (``~``) in path values should be expanded to the home directory."""
    config_dir = tmp_path / "path_cfg"
    config_dir.mkdir()

    toml_text = """\
[daemon]
data_dir = "~/meept-data"
"""
    toml_path = config_dir / "meept.toml"
    toml_path.write_text(toml_text, encoding="utf-8")

    cfg = MeeptConfig(config_path=toml_path)

    home = str(Path.home())
    assert cfg.settings.daemon.data_dir.startswith(home)
    assert "~" not in cfg.settings.daemon.data_dir


def test_load_markdown(tmp_config: Path) -> None:
    """Companion Markdown files should be loaded as raw strings."""
    cfg = MeeptConfig(config_path=tmp_config)

    assert "helpful" in cfg.constitution
    assert "reveal" in cfg.restrictions.lower() or "Never" in cfg.restrictions
    assert "Assist" in cfg.purpose


def test_reload(tmp_config: Path) -> None:
    """Calling reload() should pick up changes written to disk."""
    cfg = MeeptConfig(config_path=tmp_config)
    assert cfg.settings.daemon.log_level == "DEBUG"

    # Mutate the TOML on disk.
    raw = tmp_config.read_text(encoding="utf-8")
    raw = raw.replace('log_level = "DEBUG"', 'log_level = "WARNING"')
    tmp_config.write_text(raw, encoding="utf-8")

    cfg.reload()
    assert cfg.settings.daemon.log_level == "WARNING"
