"""Pre-configured provider presets for common LLM backends."""

from __future__ import annotations

from meept.llm.models import ModelConfig

# ---------------------------------------------------------------------------
# Preset definitions
# ---------------------------------------------------------------------------

_PRESETS: dict[str, ModelConfig] = {
    "ollama": ModelConfig(
        base_url="http://localhost:11434",
        model_id="llama3",
        api_key="",
        max_tokens=4096,
        temperature=0.7,
        cost_per_million_input=0.0,
        cost_per_million_output=0.0,
    ),
    "openrouter": ModelConfig(
        base_url="https://openrouter.ai/api",
        model_id="openai/gpt-4o-mini",
        api_key="",  # must be supplied at runtime
        max_tokens=4096,
        temperature=0.7,
        cost_per_million_input=0.15,
        cost_per_million_output=0.60,
    ),
    "vllm": ModelConfig(
        base_url="http://localhost:8000",
        model_id="default",
        api_key="",
        max_tokens=4096,
        temperature=0.7,
        cost_per_million_input=0.0,
        cost_per_million_output=0.0,
    ),
    "litellm": ModelConfig(
        base_url="http://localhost:4000",
        model_id="gpt-3.5-turbo",
        api_key="",
        max_tokens=4096,
        temperature=0.7,
        cost_per_million_input=0.0,
        cost_per_million_output=0.0,
    ),
}


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------

def get_provider_preset(name: str) -> ModelConfig:
    """Return a *copy* of a provider preset by name.

    A copy is returned so that callers can mutate the config (e.g. set an
    API key) without affecting the canonical preset.

    Parameters
    ----------
    name:
        One of ``"ollama"``, ``"openrouter"``, ``"vllm"``, ``"litellm"``.

    Raises
    ------
    ValueError
        If *name* does not match any known preset.
    """
    key = name.lower().strip()
    if key not in _PRESETS:
        available = ", ".join(sorted(_PRESETS))
        raise ValueError(
            f"Unknown provider preset {name!r}. Available presets: {available}"
        )
    return _PRESETS[key].model_copy(deep=True)
