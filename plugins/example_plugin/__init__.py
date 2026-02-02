"""Example plugin demonstrating the Meept plugin interface.

This plugin registers two simple tools:

* **hello_world** -- greets someone by name.
* **random_number** -- generates a random integer within a given range.

It serves as a reference implementation for third-party plugin authors.
"""

from __future__ import annotations

import random
from typing import Any

from meept.tools.interface import Tool, ToolDefinition, ToolParameter, ToolRegistry


# ---------------------------------------------------------------------------
# hello_world
# ---------------------------------------------------------------------------


class HelloWorldTool(Tool):
    """A trivial greeting tool.

    Accepts a ``name`` parameter and returns a personalised greeting.
    """

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="hello_world",
            description="Says hello to someone",
            parameters=[
                ToolParameter(
                    name="name",
                    type="string",
                    description="The name of the person to greet",
                    required=True,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        name: str = kwargs.get("name", "World")
        return {"greeting": f"Hello, {name}! I'm Meept."}


# ---------------------------------------------------------------------------
# random_number
# ---------------------------------------------------------------------------


class RandomNumberTool(Tool):
    """Generate a random integer within an inclusive range.

    Both ``min`` and ``max`` are optional and default to 1 and 100
    respectively.
    """

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="random_number",
            description="Generate a random number in range",
            parameters=[
                ToolParameter(
                    name="min",
                    type="integer",
                    description="Lower bound (inclusive)",
                    required=False,
                ),
                ToolParameter(
                    name="max",
                    type="integer",
                    description="Upper bound (inclusive)",
                    required=False,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        lower: int = int(kwargs.get("min", 1))
        upper: int = int(kwargs.get("max", 100))
        if lower > upper:
            return {"error": f"min ({lower}) must be <= max ({upper})"}
        return {"number": random.randint(lower, upper)}


# ---------------------------------------------------------------------------
# Plugin registration entry point
# ---------------------------------------------------------------------------


def register(registry: ToolRegistry) -> None:
    """Register all tools provided by this plugin.

    The plugin loader calls this function with the application's
    :class:`~meept.tools.interface.ToolRegistry` instance, giving the
    plugin the opportunity to register its tools.
    """
    registry.register(HelloWorldTool())
    registry.register(RandomNumberTool())
