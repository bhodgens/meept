"""JSON5 model configuration loader and provider definitions.

Loads ``models.json5`` which defines providers, their models, capabilities,
and cost information.  Uses regex-based comment/trailing-comma stripping
so that no external JSON5 library is required.
"""

from __future__ import annotations

import json
import logging
import os
import re
from pathlib import Path
from typing import Any

from pydantic import BaseModel, Field

from meept.llm.models import ModelConfig

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# JSON5 helpers (no external dependency)
# ---------------------------------------------------------------------------

_BLOCK_COMMENT_RE = re.compile(r"/\*.*?\*/", re.DOTALL)
_TRAILING_COMMA_RE = re.compile(r",\s*([\]}])")
_ENV_VAR_RE = re.compile(r"\$\{([^}]+)\}")


def strip_json5(text: str) -> str:
    """Strip JSON5-specific syntax (comments, trailing commas) to produce valid JSON.

    Line comments are removed with a string-aware scan so that ``//``
    inside quoted strings (e.g. URLs) is preserved.
    """
    text = _BLOCK_COMMENT_RE.sub("", text)

    # String-aware line-comment removal: track whether we are inside a
    # quoted string so that ``//`` inside strings is never stripped.
    lines: list[str] = []
    for line in text.splitlines(keepends=True):
        in_string = False
        escape_next = False
        comment_start = -1
        for i, ch in enumerate(line):
            if escape_next:
                escape_next = False
                continue
            if ch == "\\":
                escape_next = True
                continue
            if ch == '"':
                in_string = not in_string
            elif ch == "/" and not in_string:
                if i + 1 < len(line) and line[i + 1] == "/":
                    comment_start = i
                    break
        if comment_start >= 0:
            lines.append(line[:comment_start] + "\n")
        else:
            lines.append(line)

    text = "".join(lines)
    text = _TRAILING_COMMA_RE.sub(r"\1", text)
    return text


def expand_env_vars(obj: Any) -> Any:
    """Recursively replace ``${VAR}`` references in string values."""
    if isinstance(obj, str):

        def _replacer(match: re.Match[str]) -> str:
            var = match.group(1)
            value = os.environ.get(var)
            if value is None:
                log.debug("providers: env var %r not set -- keeping placeholder", var)
                return match.group(0)
            return value

        return _ENV_VAR_RE.sub(_replacer, obj)
    if isinstance(obj, dict):
        return {k: expand_env_vars(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [expand_env_vars(item) for item in obj]
    return obj


def load_json5(path: Path) -> dict[str, Any]:
    """Read a JSON5 file, strip comments/trailing commas, expand env vars."""
    text = path.read_text(encoding="utf-8")
    cleaned = strip_json5(text)
    data = json.loads(cleaned)
    return expand_env_vars(data)


# ---------------------------------------------------------------------------
# Pydantic config models
# ---------------------------------------------------------------------------


class ModelDefinition(BaseModel):
    """A single model within a provider."""

    name: str = ""
    capabilities: list[str] = Field(default_factory=list)
    input_cost: float = 0.0
    output_cost: float = 0.0
    context_limit: int = 128000
    max_output: int = 4096
    temperature: float = 0.7


class ProviderOptions(BaseModel):
    """Connection options for a provider."""

    baseURL: str = ""
    apiKey: str = ""
    timeout: int = 120


class ProviderConfig(BaseModel):
    """Configuration for a single LLM provider."""

    api: str = "openai"
    options: ProviderOptions = Field(default_factory=ProviderOptions)
    models: dict[str, ModelDefinition] = Field(default_factory=dict)


class ModelsConfig(BaseModel):
    """Root configuration from ``models.json5``."""

    model: str = "ollama/llama3.2"
    small_model: str = "ollama/llama3.2"
    disabled_providers: list[str] = Field(default_factory=list)
    providers: dict[str, ProviderConfig] = Field(default_factory=dict)


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------


def load_models_config(path: Path) -> ModelsConfig:
    """Load and validate a ``models.json5`` file.

    Parameters
    ----------
    path:
        Path to the JSON5 configuration file.

    Returns
    -------
    ModelsConfig
        Validated configuration.

    Raises
    ------
    FileNotFoundError
        If the file does not exist.
    """
    if not path.exists():
        log.warning("providers: models config not found at %s -- using defaults", path)
        return ModelsConfig()

    raw = load_json5(path)
    return ModelsConfig.model_validate(raw)


def resolve_model_ref(ref: str, config: ModelsConfig) -> ModelConfig | None:
    """Resolve a ``provider/model-id`` reference to a :class:`ModelConfig`.

    Parameters
    ----------
    ref:
        Model reference in ``provider/model-id`` format (e.g. ``"ollama/llama3.2"``).
    config:
        The loaded models configuration.

    Returns
    -------
    ModelConfig or None
        The resolved model config, or ``None`` if the reference cannot be resolved.
    """
    parts = ref.split("/", 1)
    if len(parts) != 2:
        log.warning("providers: invalid model ref %r (expected provider/model-id)", ref)
        return None

    provider_name, model_id = parts

    if provider_name in config.disabled_providers:
        log.warning("providers: provider %r is disabled", provider_name)
        return None

    provider = config.providers.get(provider_name)
    if provider is None:
        log.warning("providers: unknown provider %r", provider_name)
        return None

    model_def = provider.models.get(model_id)
    if model_def is None:
        log.warning("providers: model %r not found in provider %r", model_id, provider_name)
        return None

    return ModelConfig(
        base_url=provider.options.baseURL.rstrip("/"),
        model_id=model_def.name or model_id,
        api_key=provider.options.apiKey,
        cost_per_million_input=model_def.input_cost,
        cost_per_million_output=model_def.output_cost,
        max_tokens=model_def.max_output,
        temperature=model_def.temperature,
        context_limit=model_def.context_limit,
        capabilities=frozenset(model_def.capabilities),
        provider_id=provider_name,
    )


def get_all_models(config: ModelsConfig) -> list[ModelConfig]:
    """Return :class:`ModelConfig` for every model across all enabled providers."""
    models: list[ModelConfig] = []
    for prov_name, prov in config.providers.items():
        if prov_name in config.disabled_providers:
            continue
        for model_id, model_def in prov.models.items():
            mc = ModelConfig(
                base_url=prov.options.baseURL.rstrip("/"),
                model_id=model_def.name or model_id,
                api_key=prov.options.apiKey,
                cost_per_million_input=model_def.input_cost,
                cost_per_million_output=model_def.output_cost,
                max_tokens=model_def.max_output,
                temperature=model_def.temperature,
                context_limit=model_def.context_limit,
                capabilities=frozenset(model_def.capabilities),
                provider_id=prov_name,
            )
            models.append(mc)
    return models
