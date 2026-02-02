"""Tests for capability-based model resolution."""

from __future__ import annotations

import pytest

from meept.llm.models import ModelConfig
from meept.llm.providers import ModelsConfig, ModelDefinition, ProviderConfig, ProviderOptions
from meept.llm.resolver import CapabilityError, ModelResolver
from meept.skills.models import SkillDefinition


def _make_config() -> ModelsConfig:
    """Create a test ModelsConfig with two providers."""
    return ModelsConfig(
        model="cheap/small",
        small_model="cheap/small",
        providers={
            "cheap": ProviderConfig(
                options=ProviderOptions(baseURL="http://cheap:11434/v1"),
                models={
                    "small": ModelDefinition(
                        name="small",
                        capabilities=["code", "tool_use"],
                        input_cost=0.0,
                        output_cost=0.0,
                    ),
                },
            ),
            "pro": ProviderConfig(
                options=ProviderOptions(baseURL="http://pro:11434/v1"),
                models={
                    "large": ModelDefinition(
                        name="large",
                        capabilities=["code", "tool_use", "reasoning", "vision", "long_context"],
                        input_cost=3.0,
                        output_cost=15.0,
                    ),
                },
            ),
        },
    )


def test_resolve_no_skill() -> None:
    """No skill -> return default model."""
    resolver = ModelResolver(_make_config())
    result = resolver.resolve_for_skill(None)
    assert result.model_id == "small"


def test_resolve_empty_requires() -> None:
    """Skill with empty requires -> use current/default model."""
    resolver = ModelResolver(_make_config())
    skill = SkillDefinition(name="general")
    result = resolver.resolve_for_skill(skill)
    assert result.model_id == "small"


def test_resolve_current_satisfies() -> None:
    """Current model satisfies requirements -> use it."""
    resolver = ModelResolver(_make_config())
    skill = SkillDefinition(name="test", requires=["code", "tool_use"])
    current = ModelConfig(
        base_url="http://current:11434/v1",
        model_id="current",
        capabilities=frozenset(["code", "tool_use", "reasoning"]),
    )
    result = resolver.resolve_for_skill(skill, current_model=current)
    assert result.model_id == "current"


def test_resolve_escalates_to_capable() -> None:
    """Current model lacks capabilities -> escalate to capable model."""
    resolver = ModelResolver(_make_config())
    skill = SkillDefinition(name="vision-task", requires=["vision", "code"])
    result = resolver.resolve_for_skill(skill)
    # Only "pro/large" has vision.
    assert result.model_id == "large"
    assert result.provider_id == "pro"


def test_resolve_picks_cheapest() -> None:
    """When multiple models satisfy but the default doesn't, pick cheapest."""
    config = ModelsConfig(
        model="c/basic",
        providers={
            "c": ProviderConfig(
                options=ProviderOptions(baseURL="http://c:11434/v1"),
                models={
                    "basic": ModelDefinition(
                        name="basic",
                        capabilities=["code"],
                        input_cost=0.0,
                        output_cost=0.0,
                    ),
                },
            ),
            "a": ProviderConfig(
                options=ProviderOptions(baseURL="http://a:11434/v1"),
                models={
                    "model1": ModelDefinition(
                        name="model1",
                        capabilities=["code", "reasoning"],
                        input_cost=5.0,
                        output_cost=10.0,
                    ),
                },
            ),
            "b": ProviderConfig(
                options=ProviderOptions(baseURL="http://b:11434/v1"),
                models={
                    "model2": ModelDefinition(
                        name="model2",
                        capabilities=["code", "reasoning"],
                        input_cost=1.0,
                        output_cost=2.0,
                    ),
                },
            ),
        },
    )
    resolver = ModelResolver(config)
    skill = SkillDefinition(name="test", requires=["code", "reasoning"])
    result = resolver.resolve_for_skill(skill)
    assert result.model_id == "model2"  # Cheaper of the two that satisfy


def test_resolve_raises_capability_error() -> None:
    """No model satisfies -> raise CapabilityError."""
    config = ModelsConfig(
        model="a/small",
        providers={
            "a": ProviderConfig(
                options=ProviderOptions(baseURL="http://a:11434/v1"),
                models={
                    "small": ModelDefinition(
                        name="small",
                        capabilities=["code"],
                    ),
                },
            ),
        },
    )
    resolver = ModelResolver(config)
    skill = SkillDefinition(name="impossible", requires=["vision", "long_context"])

    with pytest.raises(CapabilityError) as exc_info:
        resolver.resolve_for_skill(skill)

    assert "impossible" in str(exc_info.value)
    assert "vision" in str(exc_info.value) or exc_info.value.requires == {"vision", "long_context"}


def test_default_model_property() -> None:
    """default_model should return the configured default."""
    resolver = ModelResolver(_make_config())
    assert resolver.default_model is not None
    assert resolver.default_model.model_id == "small"


def test_small_model_property() -> None:
    """small_model should return the configured small model."""
    resolver = ModelResolver(_make_config())
    assert resolver.small_model is not None
    assert resolver.small_model.model_id == "small"


def test_resolve_ref() -> None:
    """resolve_ref should resolve a provider/model reference."""
    resolver = ModelResolver(_make_config())
    result = resolver.resolve_ref("pro/large")
    assert result is not None
    assert result.model_id == "large"
    assert "vision" in result.capabilities
