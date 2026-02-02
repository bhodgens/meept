"""Tests for the tool interface and registry."""

from __future__ import annotations

from typing import Any

import pytest

from meept.tools.interface import (
    Tool,
    ToolDefinition,
    ToolParameter,
    ToolRegistry,
)


# ---------------------------------------------------------------------------
# Concrete tool for testing
# ---------------------------------------------------------------------------


class _EchoTool(Tool):
    """Minimal tool implementation for testing."""

    def __init__(self, name: str = "echo", description: str = "Echoes input back") -> None:
        self._name = name
        self._description = description

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name=self._name,
            description=self._description,
            parameters=[
                ToolParameter(
                    name="text",
                    type="string",
                    description="The text to echo",
                    required=True,
                ),
                ToolParameter(
                    name="uppercase",
                    type="boolean",
                    description="Whether to uppercase the output",
                    required=False,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        text = kwargs.get("text", "")
        if kwargs.get("uppercase"):
            text = text.upper()
        return {"result": text}


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_tool_definition_to_openai() -> None:
    """to_openai_schema() should produce a valid OpenAI function-calling dict."""
    tool = _EchoTool()
    schema = tool.definition().to_openai_schema()

    assert schema["type"] == "function"
    func = schema["function"]
    assert func["name"] == "echo"
    assert func["description"] == "Echoes input back"

    params = func["parameters"]
    assert params["type"] == "object"
    assert "text" in params["properties"]
    assert params["properties"]["text"]["type"] == "string"
    assert "uppercase" in params["properties"]
    assert "text" in params.get("required", [])
    # "uppercase" is not required.
    assert "uppercase" not in params.get("required", [])


def test_tool_registry() -> None:
    """Registering a tool should make it discoverable via get(), list_tools(), and names."""
    registry = ToolRegistry()
    tool = _EchoTool()

    registry.register(tool)

    assert "echo" in registry
    assert registry.get("echo") is tool
    assert len(registry) == 1

    definitions = registry.list_tools()
    assert len(definitions) == 1
    assert definitions[0].name == "echo"

    assert "echo" in registry.names


def test_tool_registry_unregister() -> None:
    """Unregistering a tool should remove it from the registry."""
    registry = ToolRegistry()
    registry.register(_EchoTool())

    assert "echo" in registry

    registry.unregister("echo")

    assert "echo" not in registry
    assert registry.get("echo") is None
    assert len(registry) == 0

    # Unregistering a non-existent tool should raise KeyError.
    with pytest.raises(KeyError):
        registry.unregister("echo")
