"""Dashboard screen for the Meept TUI.

Presents a three-column overview of daemon status, recent agent actions,
and operational metrics.  Polls the daemon ``status`` RPC every 5 seconds
to keep the display current.
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
    from textual.timer import Timer
    from textual.widgets import Footer, Header, Static
except ImportError as _exc:
    raise ImportError(
        "The 'textual' package is required for the Meept TUI.\n"
        "Install it with:  pip install 'meept[cli]'  (or: pip install 'textual>=1.0')"
    ) from _exc

from cli.screens.chat import RpcClient
from cli.widgets.metrics import MetricsWidget
from cli.widgets.status_bar import StatusBarWidget
from cli.widgets.task_list import TaskListWidget

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Interval at which the dashboard polls daemon status (seconds).
# ---------------------------------------------------------------------------
_POLL_INTERVAL: float = 5.0


# ---------------------------------------------------------------------------
# Panel widgets
# ---------------------------------------------------------------------------


class StatusPanel(Static):
    """Left-hand panel showing daemon status, uptime, and current LLM model.

    Populated by :meth:`update_status` with data from the ``status`` RPC.
    """

    DEFAULT_CSS = """
    StatusPanel {
        width: 1fr;
        height: 100%;
        border: solid $primary;
        padding: 1 2;
    }
    """

    def __init__(
        self,
        *,
        name: str | None = None,
        id: str | None = None,
        classes: str | None = None,
    ) -> None:
        super().__init__(name=name, id=id, classes=classes)
        self._data: dict[str, Any] = {}

    def on_mount(self) -> None:
        self.update(self._build_markup())

    def update_status(self, data: dict[str, Any]) -> None:
        """Refresh the panel from daemon status *data*."""
        self._data = data
        self.update(self._build_markup())

    def _build_markup(self) -> str:
        if not self._data:
            return (
                "[bold underline]Daemon Status[/bold underline]\n\n"
                "[dim]Connecting...[/dim]"
            )

        d = self._data
        status = d.get("status", "unknown")
        status_colour = "green" if status == "running" else "red"

        uptime_s = d.get("uptime_seconds", 0)
        uptime = _format_uptime(float(uptime_s))

        model = d.get("model", d.get("default_model", "n/a"))

        methods = d.get("registered_methods", [])
        method_count = len(methods) if isinstance(methods, list) else methods

        bus_subs = d.get("bus_subscribers", 0)

        lines = [
            "[bold underline]Daemon Status[/bold underline]",
            "",
            f"  Status:       [{status_colour}]{status}[/{status_colour}]",
            f"  Uptime:       {uptime}",
            f"  LLM model:    [bold]{model}[/bold]",
            f"  RPC methods:  {method_count}",
            f"  Bus subs:     {bus_subs}",
        ]
        return "\n".join(lines)


class RecentTasksPanel(Static):
    """Centre panel listing recent agent actions with timestamps.

    Wraps a :class:`~cli.widgets.task_list.TaskListWidget` and forwards data
    to it.
    """

    DEFAULT_CSS = """
    RecentTasksPanel {
        width: 1fr;
        height: 100%;
        border: solid $primary;
        padding: 1 2;
        overflow-y: auto;
    }
    """

    def __init__(
        self,
        *,
        name: str | None = None,
        id: str | None = None,
        classes: str | None = None,
    ) -> None:
        super().__init__(name=name, id=id, classes=classes)
        self._tasks: list[dict[str, Any]] = []

    def on_mount(self) -> None:
        self.update(self._build_markup())

    def update_tasks(self, tasks: list[dict[str, Any]]) -> None:
        """Replace the displayed task list."""
        self._tasks = list(tasks)
        self.update(self._build_markup())

    def _build_markup(self) -> str:
        if not self._tasks:
            return (
                "[bold underline]Recent Actions[/bold underline]\n\n"
                "[dim]No recent actions.[/dim]"
            )

        _STATUS_ICONS: dict[str, tuple[str, str]] = {
            "completed": ("\u2713", "green"),
            "running": ("\u27f3", "cyan"),
            "in_progress": ("\u27f3", "cyan"),
            "failed": ("\u2717", "red"),
            "pending": ("\u25cb", "dim"),
            "cancelled": ("\u2717", "yellow"),
        }
        _DEFAULT_ICON = ("\u25cb", "dim")

        lines: list[str] = [
            "[bold underline]Recent Actions[/bold underline]",
            "",
        ]
        for task in self._tasks:
            status = str(task.get("status", "pending")).lower()
            icon, colour = _STATUS_ICONS.get(status, _DEFAULT_ICON)
            name = task.get("name", task.get("description", "unnamed"))
            ts = task.get("timestamp", "")
            if len(name) > 40:
                name = name[:37] + "..."
            ts_part = f"  [dim]{ts}[/dim]" if ts else ""
            lines.append(f"  [{colour}]{icon}[/{colour}]  {name}{ts_part}")

        return "\n".join(lines)


# ---------------------------------------------------------------------------
# Helper
# ---------------------------------------------------------------------------


def _format_uptime(seconds: float) -> str:
    """Convert seconds to ``Xd Xh Xm Xs`` notation."""
    if seconds < 0:
        return "n/a"
    days, rem = divmod(int(seconds), 86400)
    hours, rem = divmod(rem, 3600)
    minutes, secs = divmod(rem, 60)
    parts: list[str] = []
    if days:
        parts.append(f"{days}d")
    if hours:
        parts.append(f"{hours}h")
    if minutes:
        parts.append(f"{minutes}m")
    parts.append(f"{secs}s")
    return " ".join(parts)


# ---------------------------------------------------------------------------
# Dashboard screen
# ---------------------------------------------------------------------------


class DashboardScreen(Screen):
    """Three-column dashboard providing an at-a-glance system overview.

    Layout::

        +------------------+------------------+------------------+
        |   StatusPanel    | RecentTasksPanel |  MetricsWidget   |
        |  (daemon info)   |  (agent actions) | (token/budget)   |
        +------------------+------------------+------------------+

    The screen polls the daemon ``status`` RPC every 5 seconds and updates
    all three panels accordingly.

    Parameters
    ----------
    socket_path:
        Path to the meept daemon Unix socket.
    """

    BINDINGS = [
        Binding("c", "switch_screen('chat')", "Chat", show=True),
        Binding("m", "switch_screen('memory')", "Memory", show=True),
        Binding("t", "switch_screen('tasks')", "Tasks", show=True),
        Binding("q", "quit_app", "Quit", show=True, priority=True),
    ]

    DEFAULT_CSS = """
    DashboardScreen {
        layout: vertical;
    }

    #dashboard-grid {
        layout: horizontal;
        height: 1fr;
    }

    StatusPanel {
        width: 1fr;
    }

    RecentTasksPanel {
        width: 1fr;
    }

    MetricsWidget {
        width: 1fr;
        border: solid $primary;
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
        self._poll_timer: Timer | None = None

    # -- Compose -------------------------------------------------------------

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        yield StatusBarWidget(id="status-bar")
        with Horizontal(id="dashboard-grid"):
            yield StatusPanel(id="status-panel")
            yield RecentTasksPanel(id="recent-tasks")
            yield MetricsWidget(id="metrics-panel")
        yield Footer()

    # -- Lifecycle -----------------------------------------------------------

    async def on_mount(self) -> None:
        """Connect RPC and start the polling timer."""
        try:
            await self._rpc.connect()
        except OSError as exc:
            log.warning("Dashboard: cannot connect to daemon: %s", exc)

        # Perform an immediate poll, then schedule recurring.
        await self._poll_status()
        self._poll_timer = self.set_interval(
            _POLL_INTERVAL, self._poll_status, name="dashboard_poll"
        )

    async def on_unmount(self) -> None:
        """Stop polling and close the RPC connection."""
        if self._poll_timer is not None:
            self._poll_timer.stop()
            self._poll_timer = None
        await self._rpc.close()

    # -- Polling -------------------------------------------------------------

    async def _poll_status(self) -> None:
        """Fetch daemon status and update all panels."""
        try:
            status_data = await self._rpc.call("status", timeout=5.0)
        except (ConnectionError, RuntimeError, TimeoutError, asyncio.TimeoutError) as exc:
            log.debug("Dashboard poll failed: %s", exc)
            # Update status bar to show disconnection.
            self.query_one("#status-bar", StatusBarWidget).update_status(
                {"status": "disconnected"}
            )
            # Attempt reconnection for next poll.
            await self._rpc.close()
            try:
                await self._rpc.connect()
            except OSError:
                pass
            return

        if not isinstance(status_data, dict):
            status_data = {}

        # Update left panel -- daemon status.
        self.query_one("#status-panel", StatusPanel).update_status(status_data)

        # Update status bar.
        self.query_one("#status-bar", StatusBarWidget).update_status(status_data)

        # Update right panel -- metrics.
        # The status RPC may not include all budget fields; provide safe defaults.
        self.query_one("#metrics-panel", MetricsWidget).update_metrics(status_data)

        # Update centre panel -- recent tasks.
        # Attempt to fetch recent tasks from the scheduler or other RPC.
        await self._poll_tasks()

    async def _poll_tasks(self) -> None:
        """Fetch the recent task list and update the centre panel."""
        try:
            result = await self._rpc.call("scheduler.list_jobs", timeout=5.0)
        except (ConnectionError, RuntimeError, TimeoutError, asyncio.TimeoutError):
            return

        if not isinstance(result, dict):
            return

        jobs = result.get("jobs", [])
        if not isinstance(jobs, list):
            return

        # Normalise job records into the shape expected by RecentTasksPanel.
        tasks: list[dict[str, Any]] = []
        for job in jobs:
            tasks.append({
                "name": job.get("name", job.get("id", "unnamed")),
                "status": "running" if not job.get("paused", False) else "pending",
                "timestamp": job.get("next_run_time", ""),
            })

        self.query_one("#recent-tasks", RecentTasksPanel).update_tasks(tasks)

    # -- Actions -------------------------------------------------------------

    def action_switch_screen(self, screen_name: str) -> None:
        """Navigate to another screen by name."""
        from cli.screens.chat import ChatScreen
        from cli.screens.memory_browser import MemoryBrowserScreen
        from cli.screens.tasks import TasksScreen

        screen_map: dict[str, type[Screen]] = {
            "chat": ChatScreen,
            "memory": MemoryBrowserScreen,
            "tasks": TasksScreen,
        }
        cls = screen_map.get(screen_name)
        if cls is not None:
            self.app.switch_screen(cls(socket_path=self.socket_path))

    def action_quit_app(self) -> None:
        """Quit the application."""
        self.app.exit()
