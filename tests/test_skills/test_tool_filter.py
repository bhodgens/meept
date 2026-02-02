"""Tests for FilteredToolRegistry."""

from __future__ import annotations

from typing import Any

import pytest

from meept.skills.tool_filter import FilteredToolRegistry
from meept.tools.interface import Tool, ToolDefinition, ToolParameter, ToolRegistry


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


class _DummyTool(Tool):
    """Minimal tool implementation for testing."""

    def __init__(self, name: str) -> None:
        self._name = name

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name=self._name,
            description=f"Dummy tool: {self._name}",
            parameters=[
                ToolParameter(name="arg", type="string", description="An argument"),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        return {"result": f"executed {self._name}"}


def _make_registry(*tool_names: str) -> ToolRegistry:
    """Create a ToolRegistry with dummy tools."""
    reg = ToolRegistry()
    for name in tool_names:
        reg.register(_DummyTool(name))
    return reg


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_empty_allowed_passes_all() -> None:
    """Empty allowed_tools means all tools pass through."""
    parent = _make_registry("file_read", "shell", "web_fetch")
    filtered = FilteredToolRegistry(parent, allowed_tools=[])

    assert len(filtered) == 3
    assert "file_read" in filtered
    assert filtered.get("shell") is not None
    assert len(filtered.list_tools()) == 3
    assert len(filtered.get_openai_tools()) == 3


def test_none_allowed_passes_all() -> None:
    """None allowed_tools means all tools pass through."""
    parent = _make_registry("file_read", "shell")
    filtered = FilteredToolRegistry(parent, allowed_tools=None)

    assert len(filtered) == 2


def test_filter_restricts_tools() -> None:
    """Only named tools should be visible."""
    parent = _make_registry("file_read", "shell", "web_fetch")
    filtered = FilteredToolRegistry(parent, allowed_tools=["file_read", "shell"])

    assert len(filtered) == 2
    assert "file_read" in filtered
    assert "shell" in filtered
    assert "web_fetch" not in filtered

    assert filtered.get("web_fetch") is None
    assert filtered.get("file_read") is not None

    names = filtered.names
    assert names == ["file_read", "shell"]


def test_list_tools_filtered() -> None:
    """list_tools should only return allowed tool definitions."""
    parent = _make_registry("file_read", "shell", "web_fetch")
    filtered = FilteredToolRegistry(parent, allowed_tools=["shell"])

    defs = filtered.list_tools()
    assert len(defs) == 1
    assert defs[0].name == "shell"


def test_get_openai_tools_filtered() -> None:
    """get_openai_tools should only return schemas for allowed tools."""
    parent = _make_registry("file_read", "shell")
    filtered = FilteredToolRegistry(parent, allowed_tools=["file_read"])

    schemas = filtered.get_openai_tools()
    assert len(schemas) == 1
    assert schemas[0]["function"]["name"] == "file_read"


def test_register_blocked() -> None:
    """register() should raise on a filtered registry."""
    parent = _make_registry("file_read")
    filtered = FilteredToolRegistry(parent)

    with pytest.raises(RuntimeError, match="read-only"):
        filtered.register(_DummyTool("new_tool"))


def test_unregister_blocked() -> None:
    """unregister() should raise on a filtered registry."""
    parent = _make_registry("file_read")
    filtered = FilteredToolRegistry(parent)

    with pytest.raises(RuntimeError, match="read-only"):
        filtered.unregister("file_read")
