"""Memory browser screen for the Meept TUI.

Provides a search interface for querying the daemon's memory subsystem
(episodic, task, personality) and viewing full result details.
"""

from __future__ import annotations

import asyncio
import logging
from typing import Any

try:
    from textual.app import ComposeResult
    from textual.binding import Binding
    from textual.containers import Horizontal, Vertical
    from textual.screen import Screen
    from textual.widgets import (
        Footer,
        Header,
        Input,
        ListItem,
        ListView,
        Static,
    )
except ImportError as _exc:
    raise ImportError(
        "The 'textual' package is required for the Meept TUI.\n"
        "Install it with:  pip install 'meept[cli]'  (or: pip install 'textual>=1.0')"
    ) from _exc

from cli.screens.chat import RpcClient

log = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# Detail panel
# ---------------------------------------------------------------------------


class MemoryDetailPanel(Static):
    """Right-hand panel that shows the full content of a selected memory item."""

    DEFAULT_CSS = """
    MemoryDetailPanel {
        width: 2fr;
        height: 100%;
        border: solid $primary;
        padding: 1 2;
        overflow-y: auto;
    }
    """

    def show_item(self, item: dict[str, Any]) -> None:
        """Render the full detail for a single memory *item*."""
        content = item.get("content", "")
        mem_type = item.get("memory_type", item.get("type", "unknown"))
        category = item.get("category", "")
        created = item.get("created_at", "")
        updated = item.get("updated_at", "")
        score = item.get("relevance_score", 0.0)
        mem_id = item.get("id", "")
        metadata = item.get("metadata", {})

        type_colour = {
            "episodic": "cyan",
            "task": "yellow",
            "personality": "magenta",
        }.get(str(mem_type).lower(), "white")

        lines = [
            "[bold underline]Memory Detail[/bold underline]",
            "",
            f"  [bold]ID:[/bold]        {mem_id}",
            f"  [bold]Type:[/bold]      [{type_colour}]{mem_type}[/{type_colour}]",
        ]
        if category:
            lines.append(f"  [bold]Category:[/bold]  {category}")
        lines.append(f"  [bold]Relevance:[/bold] {score:.2f}")
        if created:
            lines.append(f"  [bold]Created:[/bold]   {created}")
        if updated:
            lines.append(f"  [bold]Updated:[/bold]   {updated}")
        if metadata:
            lines.append(f"  [bold]Metadata:[/bold]  {metadata}")
        lines.extend([
            "",
            "[bold]Content:[/bold]",
            "",
            content,
        ])

        self.update("\n".join(lines))

    def clear_detail(self) -> None:
        """Reset to the empty placeholder state."""
        self.update(
            "[bold underline]Memory Detail[/bold underline]\n\n"
            "[dim]Select an item to view details.[/dim]"
        )


# ---------------------------------------------------------------------------
# Result list item
# ---------------------------------------------------------------------------


class MemoryResultItem(ListItem):
    """A single row in the memory search results list.

    Stores the full result ``dict`` so it can be displayed in the detail
    panel when selected.
    """

    def __init__(self, data: dict[str, Any], **kwargs: Any) -> None:
        self.data = data
        label = self._build_label(data)
        super().__init__(Static(label), **kwargs)

    @staticmethod
    def _build_label(data: dict[str, Any]) -> str:
        """Create a compact Rich-markup preview line."""
        content = data.get("content", "")
        preview = content[:60].replace("\n", " ")
        if len(content) > 60:
            preview += "..."

        mem_type = data.get("memory_type", data.get("type", "?"))
        score = data.get("relevance_score", 0.0)
        ts = data.get("created_at", "")

        type_colour = {
            "episodic": "cyan",
            "task": "yellow",
            "personality": "magenta",
        }.get(str(mem_type).lower(), "white")

        parts = [
            f"[{type_colour}][{mem_type}][/{type_colour}]",
            f"[bold]{preview}[/bold]",
        ]
        if ts:
            parts.append(f"[dim]{ts}[/dim]")
        parts.append(f"score: {score:.2f}")

        return "  ".join(parts)


# ---------------------------------------------------------------------------
# Memory browser screen
# ---------------------------------------------------------------------------


class MemoryBrowserScreen(Screen):
    """Search and inspect the meept memory store.

    Layout::

        +--------------------------------------------------+
        |  [Search input]                                  |
        +------------------------+-------------------------+
        |  Search results list   |   Detail panel          |
        |                        |                         |
        +------------------------+-------------------------+

    Parameters
    ----------
    socket_path:
        Path to the meept daemon Unix socket.
    """

    BINDINGS = [
        Binding("slash", "focus_search", "Search", show=True, key_display="/"),
        Binding("d", "switch_screen('dashboard')", "Dashboard", show=True),
        Binding("c", "switch_screen('chat')", "Chat", show=True),
        Binding("t", "switch_screen('tasks')", "Tasks", show=True),
        Binding("q", "go_back", "Back", show=True, priority=True),
    ]

    DEFAULT_CSS = """
    MemoryBrowserScreen {
        layout: vertical;
    }

    #memory-search {
        dock: top;
        margin: 1 2;
    }

    #memory-body {
        layout: horizontal;
        height: 1fr;
    }

    #results-list {
        width: 1fr;
        height: 100%;
        border: solid $primary;
    }

    MemoryDetailPanel {
        width: 2fr;
    }
    """

    def __init__(
        self,
        socket_path: str,
        name: str | None = None,
        id: str | None = None,
        classes: str | None = None,
    ) -> None:
        super().__init__(name=name, id=id, classes=classes)
        self.socket_path = socket_path
        self._rpc = RpcClient(socket_path)
        self._results: list[dict[str, Any]] = []

    # -- Compose -------------------------------------------------------------

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        yield Input(
            placeholder="Search memory... (press / to focus, Enter to search)",
            id="memory-search",
        )
        with Horizontal(id="memory-body"):
            yield ListView(id="results-list")
            yield MemoryDetailPanel(id="detail-panel")
        yield Footer()

    # -- Lifecycle -----------------------------------------------------------

    async def on_mount(self) -> None:
        """Connect to the daemon when the screen mounts."""
        try:
            await self._rpc.connect()
        except OSError as exc:
            log.warning("MemoryBrowser: cannot connect to daemon: %s", exc)

        detail = self.query_one("#detail-panel", MemoryDetailPanel)
        detail.clear_detail()

    async def on_unmount(self) -> None:
        """Clean up the RPC connection."""
        await self._rpc.close()

    # -- Search handling -----------------------------------------------------

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        """Execute a memory search when the user presses Enter."""
        query = event.value.strip()
        if not query:
            return

        results_view = self.query_one("#results-list", ListView)
        detail_panel = self.query_one("#detail-panel", MemoryDetailPanel)

        # Clear previous results.
        await results_view.clear()
        detail_panel.clear_detail()
        self._results.clear()

        try:
            result = await self._rpc.call(
                "memory.query",
                {"query": query, "limit": 25},
                timeout=30.0,
            )
        except (ConnectionError, RuntimeError, TimeoutError, asyncio.TimeoutError) as exc:
            detail_panel.update(f"[bold red]Search error:[/bold red] {exc}")
            return

        if not isinstance(result, dict):
            detail_panel.update("[dim]Unexpected response format.[/dim]")
            return

        # The daemon may return results under different keys.
        items: list[dict[str, Any]] = result.get(
            "results",
            result.get("items", result.get("memories", [])),
        )
        if not isinstance(items, list):
            items = []

        if not items:
            detail_panel.update("[dim]No results found.[/dim]")
            return

        self._results = items

        for item in items:
            await results_view.append(MemoryResultItem(item))

    # -- Selection handling --------------------------------------------------

    def on_list_view_selected(self, event: ListView.Selected) -> None:
        """Show full detail for the selected memory item."""
        item_widget = event.item
        if isinstance(item_widget, MemoryResultItem):
            detail = self.query_one("#detail-panel", MemoryDetailPanel)
            detail.show_item(item_widget.data)

    # -- Actions -------------------------------------------------------------

    def action_focus_search(self) -> None:
        """Move focus to the search input."""
        self.query_one("#memory-search", Input).focus()

    def action_go_back(self) -> None:
        """Return to the dashboard screen."""
        from cli.screens.dashboard import DashboardScreen

        self.app.switch_screen(DashboardScreen(socket_path=self.socket_path))

    def action_switch_screen(self, screen_name: str) -> None:
        """Navigate to another screen by name."""
        from cli.screens.chat import ChatScreen
        from cli.screens.dashboard import DashboardScreen
        from cli.screens.tasks import TasksScreen

        screen_map: dict[str, type[Screen]] = {
            "dashboard": DashboardScreen,
            "chat": ChatScreen,
            "tasks": TasksScreen,
        }
        cls = screen_map.get(screen_name)
        if cls is not None:
            self.app.switch_screen(cls(socket_path=self.socket_path))
