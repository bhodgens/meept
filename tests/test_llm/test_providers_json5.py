"""Tests for JSON5 model configuration loading."""

from __future__ import annotations

import os
from pathlib import Path

from meept.llm.providers import (
    ModelsConfig,
    expand_env_vars,
    get_all_models,
    load_models_config,
    resolve_model_ref,
    strip_json5,
)


def test_strip_line_comments() -> None:
    """Line comments should be removed."""
    text = '{"key": "value"} // a comment'
    assert '"key"' in strip_json5(text)
    assert "comment" not in strip_json5(text)


def test_strip_block_comments() -> None:
    """Block comments should be removed."""
    text = '{"key": /* block */ "value"}'
    result = strip_json5(text)
    assert "block" not in result
    assert '"key"' in result


def test_strip_trailing_commas() -> None:
    """Trailing commas should be removed."""
    text = '{"a": 1, "b": 2,}'
    result = strip_json5(text)
    assert result.count(",") == 1  # Only one comma remains.


def test_expand_env_vars(monkeypatch) -> None:
    """Environment variables should be expanded."""
    monkeypatch.setenv("TEST_KEY", "secret123")
    result = expand_env_vars({"key": "${TEST_KEY}", "nested": {"k": "${TEST_KEY}"}})
    assert result["key"] == "secret123"
    assert result["nested"]["k"] == "secret123"


def test_expand_env_vars_missing() -> None:
    """Missing env vars should be kept as placeholders."""
    result = expand_env_vars("${NONEXISTENT_VAR_12345}")
    assert result == "${NONEXISTENT_VAR_12345}"


def test_load_models_config(tmp_path: Path) -> None:
    """Loading a well-formed models.json5 should succeed."""
    config_file = tmp_path / "models.json5"
    config_file.write_text("""{
  // Default model
  "model": "ollama/llama3.2",
  "small_model": "ollama/llama3.2",
  "disabled_providers": [],
  "providers": {
    "ollama": {
      "api": "openai",
      "options": {
        "baseURL": "http://localhost:11434/v1",
        "timeout": 120,
      },
      "models": {
        "llama3.2": {
          "name": "llama3.2",
          "capabilities": ["code", "tool_use", "reasoning"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 128000,
          "max_output": 4096,
          "temperature": 0.7,
        },
      },
    },
  },
}""", encoding="utf-8")

    config = load_models_config(config_file)
    assert isinstance(config, ModelsConfig)
    assert config.model == "ollama/llama3.2"
    assert "ollama" in config.providers
    assert "llama3.2" in config.providers["ollama"].models

    model = config.providers["ollama"].models["llama3.2"]
    assert model.capabilities == ["code", "tool_use", "reasoning"]
    assert model.context_limit == 128000


def test_load_models_config_missing(tmp_path: Path) -> None:
    """Missing config file should return defaults."""
    config = load_models_config(tmp_path / "missing.json5")
    assert isinstance(config, ModelsConfig)
    assert config.model == "ollama/llama3.2"


def test_resolve_model_ref() -> None:
    """Resolving a provider/model-id reference should produce a ModelConfig."""
    from meept.llm.providers import ModelDefinition, ProviderConfig, ProviderOptions

    config = ModelsConfig(
        providers={
            "test": ProviderConfig(
                options=ProviderOptions(baseURL="http://test:11434/v1", apiKey="key123"),
                models={
                    "mymodel": ModelDefinition(
                        name="mymodel",
                        capabilities=["code", "reasoning"],
                        input_cost=1.0,
                        output_cost=2.0,
                        max_output=8192,
                    ),
                },
            ),
        },
    )

    mc = resolve_model_ref("test/mymodel", config)
    assert mc is not None
    assert mc.model_id == "mymodel"
    assert mc.base_url == "http://test:11434/v1"
    assert mc.api_key == "key123"
    assert mc.capabilities == frozenset(["code", "reasoning"])
    assert mc.cost_per_million_input == 1.0
    assert mc.max_tokens == 8192
    assert mc.provider_id == "test"


def test_resolve_model_ref_invalid() -> None:
    """Invalid ref formats should return None."""
    config = ModelsConfig()
    assert resolve_model_ref("no-slash", config) is None
    assert resolve_model_ref("unknown/model", config) is None


def test_resolve_model_ref_disabled_provider() -> None:
    """Disabled providers should return None."""
    from meept.llm.providers import ModelDefinition, ProviderConfig, ProviderOptions

    config = ModelsConfig(
        disabled_providers=["disabled"],
        providers={
            "disabled": ProviderConfig(
                options=ProviderOptions(baseURL="http://x"),
                models={"m": ModelDefinition(name="m")},
            ),
        },
    )
    assert resolve_model_ref("disabled/m", config) is None


def test_get_all_models() -> None:
    """get_all_models should return configs for all enabled models."""
    from meept.llm.providers import ModelDefinition, ProviderConfig, ProviderOptions

    config = ModelsConfig(
        disabled_providers=["off"],
        providers={
            "on": ProviderConfig(
                options=ProviderOptions(baseURL="http://on"),
                models={
                    "a": ModelDefinition(name="a", capabilities=["code"]),
                    "b": ModelDefinition(name="b", capabilities=["reasoning"]),
                },
            ),
            "off": ProviderConfig(
                options=ProviderOptions(baseURL="http://off"),
                models={"c": ModelDefinition(name="c")},
            ),
        },
    )

    models = get_all_models(config)
    assert len(models) == 2
    names = {m.model_id for m in models}
    assert names == {"a", "b"}


def test_env_var_in_config(tmp_path: Path, monkeypatch) -> None:
    """Environment variables in the config should be expanded."""
    monkeypatch.setenv("TEST_API_KEY", "expanded-key")
    config_file = tmp_path / "models.json5"
    config_file.write_text("""{
  "model": "test/m",
  "providers": {
    "test": {
      "options": {
        "baseURL": "http://test",
        "apiKey": "${TEST_API_KEY}",
      },
      "models": {
        "m": {"name": "m"},
      },
    },
  },
}""", encoding="utf-8")

    config = load_models_config(config_file)
    assert config.providers["test"].options.apiKey == "expanded-key"
