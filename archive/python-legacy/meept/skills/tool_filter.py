"""Filtered tool registry exposing a read-only subset of tools.

:class:`FilteredToolRegistry` wraps a parent :class:`ToolRegistry` and
exposes only the tools named in ``allowed_tools``.  If ``allowed_tools``
is empty, all tools are passed through (no filtering).
"""

from __future__ import annotations

import logging
from typing import Any

from meept.tools.interface import Tool, ToolDefinition, ToolRegistry

log = logging.getLogger(__name__)


class FilteredToolRegistry(ToolRegistry):
    """Read-only view of a parent :class:`ToolRegistry`.

    Parameters
    ----------
    parent:
        The underlying full tool registry.
    allowed_tools:
        Names of the tools to expose.  An empty list means *all* tools.
    """

    def __init__(
        self,
        parent: ToolRegistry,
        allowed_tools: list[str] | None = None,
    ) -> None:
        # Do NOT call super().__init__() -- we delegate to parent instead.
        self._parent = parent
        self._allowed: set[str] = set(allowed_tools) if allowed_tools else set()

    def _is_allowed(self, name: str) -> bool:
        """Return True if *name* passes the filter."""
        if not self._allowed:
            return True  # Empty means all pass through.
        return name in self._allowed

    # ------------------------------------------------------------------
    # Read-only delegations
    # ------------------------------------------------------------------

    def get(self, name: str) -> Tool | None:
        if not self._is_allowed(name):
            return None
        return self._parent.get(name)

    def list_tools(self) -> list[ToolDefinition]:
        return [
            defn for defn in self._parent.list_tools()
            if self._is_allowed(defn.name)
        ]

    def get_openai_tools(self) -> list[dict[str, Any]]:
        return [
            schema for schema in self._parent.get_openai_tools()
            if self._is_allowed(schema.get("function", {}).get("name", ""))
        ]

    @property
    def names(self) -> list[str]:
        return sorted(n for n in self._parent.names if self._is_allowed(n))

    def __len__(self) -> int:
        return len(self.names)

    def __contains__(self, name: str) -> bool:
        return self._is_allowed(name) and name in self._parent

    # ------------------------------------------------------------------
    # Mutation blocked
    # ------------------------------------------------------------------

    def register(self, tool: Tool) -> None:
        raise RuntimeError("FilteredToolRegistry is read-only; register on the parent registry")

    def unregister(self, name: str) -> None:
        raise RuntimeError("FilteredToolRegistry is read-only; unregister from the parent registry")

    def __repr__(self) -> str:
        return f"<FilteredToolRegistry allowed={sorted(self._allowed) if self._allowed else 'all'!r}>"
