"""Model resolver with capability matching.

The :class:`ModelResolver` selects the appropriate LLM model for a skill
based on capability requirements.  It implements the algorithm:

1. If skill.requires is empty -> use current/default model
2. If current model capabilities >= skill.requires -> use current model
3. Otherwise -> find cheapest model where capabilities >= requires
4. If none found -> raise CapabilityError
"""

from __future__ import annotations

import logging

from meept.llm.models import ModelConfig
from meept.llm.providers import ModelsConfig, get_all_models, resolve_model_ref
from meept.skills.models import SkillDefinition

log = logging.getLogger(__name__)


class CapabilityError(Exception):
    """Raised when no model satisfies a skill's capability requirements."""

    def __init__(self, skill_name: str, requires: set[str]) -> None:
        self.skill_name = skill_name
        self.requires = requires
        super().__init__(
            f"No model satisfies capability requirements {sorted(requires)} "
            f"for skill {skill_name!r}"
        )


class ModelResolver:
    """Resolves model selection based on capability matching.

    Parameters
    ----------
    models_config:
        The loaded models.json5 configuration.
    """

    def __init__(self, models_config: ModelsConfig) -> None:
        self._config = models_config
        self._default_model = resolve_model_ref(models_config.model, models_config)
        self._small_model = resolve_model_ref(models_config.small_model, models_config)
        self._all_models = get_all_models(models_config)

    @property
    def default_model(self) -> ModelConfig | None:
        return self._default_model

    @property
    def small_model(self) -> ModelConfig | None:
        return self._small_model

    def resolve_for_skill(
        self,
        skill: SkillDefinition | None,
        current_model: ModelConfig | None = None,
    ) -> ModelConfig:
        """Select the appropriate model for a skill.

        Parameters
        ----------
        skill:
            The skill to resolve a model for.  If ``None``, returns the
            current or default model.
        current_model:
            The model currently in use.  Preferred if it satisfies the
            skill's requirements.

        Returns
        -------
        ModelConfig
            The resolved model configuration.

        Raises
        ------
        CapabilityError
            If no available model satisfies the skill's requirements.
        """
        effective_current = current_model or self._default_model

        # No skill or no requirements -> use current model.
        if skill is None or not skill.requires:
            if effective_current is not None:
                return effective_current
            # Fallback: return first available model.
            if self._all_models:
                return self._all_models[0]
            raise CapabilityError("(none)", set())

        required = set(skill.requires)

        # Check if current model satisfies requirements.
        if effective_current is not None and effective_current.capabilities >= required:
            log.debug(
                "resolver: current model %s satisfies %s",
                effective_current.model_id,
                sorted(required),
            )
            return effective_current

        # Find cheapest model that satisfies requirements.
        candidates = [
            m for m in self._all_models if m.capabilities >= required
        ]

        if not candidates:
            raise CapabilityError(skill.name, required)

        # Sort by total cost (input + output), cheapest first.
        candidates.sort(
            key=lambda m: m.cost_per_million_input + m.cost_per_million_output
        )

        selected = candidates[0]
        log.info(
            "resolver: escalated to %s (provider=%s) for skill %r requiring %s",
            selected.model_id,
            selected.provider_id,
            skill.name,
            sorted(required),
        )
        return selected

    def resolve_ref(self, ref: str) -> ModelConfig | None:
        """Resolve a ``provider/model-id`` reference."""
        return resolve_model_ref(ref, self._config)
