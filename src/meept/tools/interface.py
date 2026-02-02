"""Tool abstract base class, definitions, and registry.

This module provides the core abstractions that every tool (built-in or
plugin-supplied) must implement, plus the :class:`ToolRegistry` that the
agent loop uses to discover and invoke tools.
"""

from __future__ import annotations

import logging
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Any

log = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# Data models
# ---------------------------------------------------------------------------


@dataclass(slots=True)
class ToolParameter:
    """Describes a single parameter accepted by a tool.

    Parameters
    ----------
    name:
        Parameter name (must be a valid identifier).
    type:
        JSON Schema type string (``"string"``, ``"integer"``, ``"boolean"``,
        ``"number"``, ``"array"``, ``"object"``).
    description:
        Human-readable description shown to the LLM.
    required:
        Whether the parameter is mandatory.
    enum:
        Optional list of allowed values.
    """

    name: str
    type: str
    description: str
    required: bool = True
    enum: list[str] | None = None


@dataclass(slots=True)
class ToolDefinition:
    """Metadata that fully describes a tool to the LLM.

    Parameters
    ----------
    name:
        Unique tool name (snake_case recommended).
    description:
        What the tool does -- this is included in the LLM system prompt.
    parameters:
        Ordered list of parameters the tool accepts.
    """

    name: str
    description: str
    parameters: list[ToolParameter] = field(default_factory=list)

    def to_openai_schema(self) -> dict[str, Any]:
        """Convert to the OpenAI function-calling tool format.

        Returns a dict suitable for inclusion in the ``tools`` array of a
        chat completion request::

            {
                "type": "function",
                "function": {
                    "name": "...",
                    "description": "...",
                    "parameters": {
                        "type": "object",
                        "properties": { ... },
                        "required": [ ... ]
                    }
                }
            }
        """
        properties: dict[str, Any] = {}
        required: list[str] = []

        for param in self.parameters:
            prop: dict[str, Any] = {
                "type": param.type,
                "description": param.description,
            }
            if param.enum is not None:
                prop["enum"] = param.enum
            properties[param.name] = prop
            if param.required:
                required.append(param.name)

        schema: dict[str, Any] = {
            "type": "function",
            "function": {
                "name": self.name,
                "description": self.description,
                "parameters": {
                    "type": "object",
                    "properties": properties,
                },
            },
        }

        if required:
            schema["function"]["parameters"]["required"] = required

        return schema


# ---------------------------------------------------------------------------
# Abstract base class
# ---------------------------------------------------------------------------


class Tool(ABC):
    """Abstract base class that every meept tool must implement.

    Subclasses define :meth:`definition` (metadata) and :meth:`execute`
    (the actual action).
    """

    @abstractmethod
    def definition(self) -> ToolDefinition:
        """Return the tool's metadata (name, description, parameters)."""
        ...

    @abstractmethod
    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        """Run the tool with the given keyword arguments.

        Returns
        -------
        dict
            A result dictionary.  The exact shape is tool-specific, but
            it should always include at minimum a ``"result"`` key or an
            ``"error"`` key.
        """
        ...


# ---------------------------------------------------------------------------
# Registry
# ---------------------------------------------------------------------------


class ToolRegistry:
    """Central registry of all available tools.

    The agent loop queries this registry to discover which tools to
    advertise to the LLM and to look up the implementation when the LLM
    requests a tool call.
    """

    def __init__(self) -> None:
        self._tools: dict[str, Tool] = {}

    def register(self, tool: Tool) -> None:
        """Register a tool instance.

        If a tool with the same name is already registered it will be
        silently replaced.
        """
        defn = tool.definition()
        name = defn.name
        if name in self._tools:
            log.warning("Replacing existing tool registration: %s", name)
        self._tools[name] = tool
        log.info("Registered tool: %s", name)

    def unregister(self, name: str) -> None:
        """Remove a tool by name.

        Raises
        ------
        KeyError
            If no tool with *name* is registered.
        """
        if name not in self._tools:
            raise KeyError(f"No tool registered with name {name!r}")
        del self._tools[name]
        log.info("Unregistered tool: %s", name)

    def get(self, name: str) -> Tool | None:
        """Look up a tool by name, returning ``None`` if not found."""
        return self._tools.get(name)

    def list_tools(self) -> list[ToolDefinition]:
        """Return the definitions of all registered tools."""
        return [tool.definition() for tool in self._tools.values()]

    def get_openai_tools(self) -> list[dict[str, Any]]:
        """Return all tool definitions in OpenAI function-calling format."""
        return [tool.definition().to_openai_schema() for tool in self._tools.values()]

    @property
    def names(self) -> list[str]:
        """Sorted list of registered tool names."""
        return sorted(self._tools.keys())

    def __len__(self) -> int:
        return len(self._tools)

    def __contains__(self, name: str) -> bool:
        return name in self._tools

    def __repr__(self) -> str:
        return f"<ToolRegistry tools={self.names!r}>"
