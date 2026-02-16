"""Web search tool (placeholder -- provider integration deferred)."""

from __future__ import annotations

from typing import Any

from meept.tools.interface import Tool, ToolDefinition, ToolParameter


class WebSearchTool(Tool):
    """Placeholder web search tool.

    Actual search-provider integration (SearXNG, Brave Search, etc.) is
    deferred to a future release.  This stub returns a helpful message so
    the LLM knows the capability is not yet available.
    """

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="web_search",
            description=(
                "Search the web for information. "
                "NOTE: This tool is not yet configured -- it will return a "
                "placeholder response until a search provider is set up."
            ),
            parameters=[
                ToolParameter(
                    name="query",
                    type="string",
                    description="The search query.",
                ),
                ToolParameter(
                    name="max_results",
                    type="integer",
                    description="Maximum number of results to return (default 5).",
                    required=False,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        query: str = kwargs.get("query", "")
        return {
            "success": False,
            "result": (
                "Web search not yet configured. "
                "To enable web search, configure a search provider in meept.toml. "
                f"(Query was: {query!r})"
            ),
            "error": "Web search provider not configured",
        }
