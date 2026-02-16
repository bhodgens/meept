"""Task/job monitoring screen for the Meept TUI.

Displays scheduled jobs from the daemon scheduler, allows adding,
deleting, and pausing/resuming jobs.
"""

from __future__ import annotations

import asyncio
import logging
from typing import Any

try:
    from textual import work
    from textual.app import ComposeResult
    from textual.binding import Binding
    from textual.containers import Horizontal, Vertical
    from textual.screen import Screen
    from textual.widgets import (
        DataTable,
        Footer,
        Header,
        Input,
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
# Job detail panel
# ---------------------------------------------------------------------------


class JobDetailPanel(Static):
    """Bottom panel showing detailed information about the selected job."""

    DEFAULT_CSS = """
    JobDetailPanel {
        height: auto;
        min-height: 8;
        border: solid $primary;
        padding: 1 2;
    }
    """

    def show_job(self, job: dict[str, Any]) -> None:
        """Render full detail for a single *job*."""
        name = job.get("name", "unnamed")
        job_id = job.get("id", "n/a")
        schedule = job.get("schedule", job.get("trigger", "n/a"))
        next_run = job.get("next_run_time", "n/a")
        last_result = job.get("last_result", "n/a")
        paused = job.get("paused", False)
        action = job.get("action", "")
        args = job.get("args", {})

        status_str = "[yellow]paused[/yellow]" if paused else "[green]active[/green]"

        lines = [
            "[bold underline]Job Detail[/bold underline]",
            "",
            f"  [bold]ID:[/bold]          {job_id}",
            f"  [bold]Name:[/bold]        {name}",
            f"  [bold]Status:[/bold]      {status_str}",
            f"  [bold]Schedule:[/bold]    {schedule}",
            f"  [bold]Next run:[/bold]    {next_run or '[dim]n/a[/dim]'}",
            f"  [bold]Last result:[/bold] {last_result}",
        ]
        if action:
            lines.append(f"  [bold]Action:[/bold]      {action}")
        if args:
            lines.append(f"  [bold]Args:[/bold]        {args}")

        self.update("\n".join(lines))

    def clear_detail(self) -> None:
        """Reset to empty placeholder."""
        self.update(
            "[bold underline]Job Detail[/bold underline]\n\n"
            "[dim]Select a job from the list above.[/dim]"
        )


# ---------------------------------------------------------------------------
# Add-job input bar
# ---------------------------------------------------------------------------


class AddJobBar(Horizontal):
    """Inline widget for entering new job details.

    Provides three input fields: name, schedule (cron), and action.
    Hidden by default; toggled visible by the ``a`` key binding.
    """

    DEFAULT_CSS = """
    AddJobBar {
        dock: bottom;
        height: 3;
        padding: 0 1;
        display: none;
    }

    AddJobBar.visible {
        display: block;
    }

    AddJobBar Input {
        width: 1fr;
        margin-right: 1;
    }
    """

    def compose(self) -> ComposeResult:
        yield Input(placeholder="Job name", id="add-job-name")
        yield Input(placeholder="Cron schedule (e.g. 0 9 * * *)", id="add-job-schedule")
        yield Input(placeholder="Action / command", id="add-job-action")


# ---------------------------------------------------------------------------
# Tasks screen
# ---------------------------------------------------------------------------


class TasksScreen(Screen):
    """Job/task monitoring screen.

    Layout::

        +--------------------------------------------------+
        |  Job list (DataTable)                            |
        +--------------------------------------------------+
        |  Job detail panel                                |
        +--------------------------------------------------+

    Columns: Name | Schedule | Next Run | Last Result | Status

    Parameters
    ----------
    socket_path:
        Path to the meept daemon Unix socket.
    """

    BINDINGS = [
        Binding("a", "toggle_add_bar", "Add Job", show=True),
        Binding("delete", "delete_job", "Delete", show=True, key_display="d"),
        Binding("p", "pause_resume_job", "Pause/Resume", show=True),
        Binding("r", "refresh_jobs", "Refresh", show=True),
        Binding("c", "switch_screen('chat')", "Chat", show=True),
        Binding("d", "switch_screen('dashboard')", "Dashboard", show=True),
        Binding("m", "switch_screen('memory')", "Memory", show=True),
        Binding("q", "go_back", "Back", show=True, priority=True),
    ]

    DEFAULT_CSS = """
    TasksScreen {
        layout: vertical;
    }

    #job-table {
        height: 1fr;
        border: solid $primary;
    }

    JobDetailPanel {
        height: auto;
        min-height: 8;
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
        self._jobs: list[dict[str, Any]] = []
        self._selected_row_key: str | None = None

    # -- Compose -------------------------------------------------------------

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        yield DataTable(id="job-table")
        yield JobDetailPanel(id="job-detail")
        yield AddJobBar(id="add-bar")
        yield Footer()

    # -- Lifecycle -----------------------------------------------------------

    async def on_mount(self) -> None:
        """Connect to the daemon and load the initial job list."""
        try:
            await self._rpc.connect()
        except OSError as exc:
            log.warning("TasksScreen: cannot connect to daemon: %s", exc)

        table = self.query_one("#job-table", DataTable)
        table.cursor_type = "row"
        table.add_columns("Name", "Schedule", "Next Run", "Last Result", "Status")

        detail = self.query_one("#job-detail", JobDetailPanel)
        detail.clear_detail()

        await self._load_jobs()

    async def on_unmount(self) -> None:
        """Clean up the RPC connection."""
        await self._rpc.close()

    # -- Data loading --------------------------------------------------------

    async def _load_jobs(self) -> None:
        """Fetch the job list from the daemon and populate the table."""
        table = self.query_one("#job-table", DataTable)
        table.clear()

        try:
            result = await self._rpc.call("scheduler.list_jobs", timeout=10.0)
        except (ConnectionError, RuntimeError, TimeoutError, asyncio.TimeoutError) as exc:
            log.debug("Failed to load jobs: %s", exc)
            self._jobs = []
            return

        if not isinstance(result, dict):
            self._jobs = []
            return

        jobs: list[dict[str, Any]] = result.get("jobs", [])
        if not isinstance(jobs, list):
            jobs = []

        self._jobs = jobs

        for job in jobs:
            paused = job.get("paused", False)
            status_str = "paused" if paused else "active"
            name = job.get("name", job.get("id", "unnamed"))
            schedule = job.get("schedule", job.get("trigger", "n/a"))
            next_run = job.get("next_run_time", "n/a") or "n/a"
            last_result = job.get("last_result", "n/a")

            table.add_row(
                name,
                schedule,
                next_run,
                str(last_result),
                status_str,
                key=job.get("id", name),
            )

    # -- Table selection -----------------------------------------------------

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        """Show detail for the selected row."""
        row_key = str(event.row_key.value) if event.row_key else None
        self._selected_row_key = row_key
        if row_key is None:
            return

        # Find the job data matching this key.
        for job in self._jobs:
            if job.get("id") == row_key or job.get("name") == row_key:
                detail = self.query_one("#job-detail", JobDetailPanel)
                detail.show_job(job)
                return

    def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
        """Update the detail panel when the cursor moves."""
        row_key = str(event.row_key.value) if event.row_key else None
        self._selected_row_key = row_key
        if row_key is None:
            return

        for job in self._jobs:
            if job.get("id") == row_key or job.get("name") == row_key:
                detail = self.query_one("#job-detail", JobDetailPanel)
                detail.show_job(job)
                return

    # -- Add job flow --------------------------------------------------------

    def action_toggle_add_bar(self) -> None:
        """Show or hide the add-job input bar."""
        bar = self.query_one("#add-bar", AddJobBar)
        bar.toggle_class("visible")
        if bar.has_class("visible"):
            self.query_one("#add-job-name", Input).focus()

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        """Handle Enter in the add-job bar -- submit when action field is filled."""
        if event.input.id != "add-job-action":
            # Move focus to the next input field.
            bar = self.query_one("#add-bar", AddJobBar)
            if event.input.id == "add-job-name":
                self.query_one("#add-job-schedule", Input).focus()
            elif event.input.id == "add-job-schedule":
                self.query_one("#add-job-action", Input).focus()
            return

        # All three fields populated -- send to daemon.
        name_input = self.query_one("#add-job-name", Input)
        schedule_input = self.query_one("#add-job-schedule", Input)
        action_input = self.query_one("#add-job-action", Input)

        name = name_input.value.strip()
        schedule = schedule_input.value.strip()
        action = action_input.value.strip()

        if not name:
            return

        await self._add_job(name, schedule, action)

        # Clear inputs and hide bar.
        name_input.value = ""
        schedule_input.value = ""
        action_input.value = ""
        self.query_one("#add-bar", AddJobBar).remove_class("visible")

    async def _add_job(self, name: str, schedule: str, action: str) -> None:
        """Send a ``scheduler.add_job`` RPC to the daemon."""
        params: dict[str, Any] = {"name": name}
        if schedule:
            params["schedule"] = schedule
        if action:
            params["action"] = action

        try:
            await self._rpc.call("scheduler.add_job", params, timeout=10.0)
        except (ConnectionError, RuntimeError, TimeoutError, asyncio.TimeoutError) as exc:
            log.warning("Failed to add job: %s", exc)
            detail = self.query_one("#job-detail", JobDetailPanel)
            detail.update(f"[bold red]Error adding job:[/bold red] {exc}")
            return

        await self._load_jobs()

    # -- Delete job ----------------------------------------------------------

    async def action_delete_job(self) -> None:
        """Delete the currently selected job."""
        if not self._selected_row_key:
            return

        job_id = self._selected_row_key
        try:
            await self._rpc.call(
                "scheduler.add_job",
                {"name": job_id, "action": "delete"},
                timeout=10.0,
            )
        except (ConnectionError, RuntimeError, TimeoutError, asyncio.TimeoutError) as exc:
            log.warning("Failed to delete job: %s", exc)
            detail = self.query_one("#job-detail", JobDetailPanel)
            detail.update(f"[bold red]Error deleting job:[/bold red] {exc}")
            return

        await self._load_jobs()
        self.query_one("#job-detail", JobDetailPanel).clear_detail()

    # -- Pause / Resume ------------------------------------------------------

    async def action_pause_resume_job(self) -> None:
        """Toggle pause state for the currently selected job."""
        if not self._selected_row_key:
            return

        # Determine current state.
        current_job: dict[str, Any] | None = None
        for job in self._jobs:
            if job.get("id") == self._selected_row_key or job.get("name") == self._selected_row_key:
                current_job = job
                break

        if current_job is None:
            return

        is_paused = current_job.get("paused", False)
        new_action = "resume" if is_paused else "pause"

        try:
            await self._rpc.call(
                "scheduler.add_job",
                {"name": self._selected_row_key, "action": new_action},
                timeout=10.0,
            )
        except (ConnectionError, RuntimeError, TimeoutError, asyncio.TimeoutError) as exc:
            log.warning("Failed to %s job: %s", new_action, exc)
            return

        await self._load_jobs()

    # -- Refresh -------------------------------------------------------------

    async def action_refresh_jobs(self) -> None:
        """Manually refresh the job list."""
        await self._load_jobs()

    # -- Navigation ----------------------------------------------------------

    def action_go_back(self) -> None:
        """Return to the dashboard screen."""
        from cli.screens.dashboard import DashboardScreen

        self.app.switch_screen(DashboardScreen(socket_path=self.socket_path))

    def action_switch_screen(self, screen_name: str) -> None:
        """Navigate to another screen by name."""
        from cli.screens.chat import ChatScreen
        from cli.screens.dashboard import DashboardScreen
        from cli.screens.memory_browser import MemoryBrowserScreen

        screen_map: dict[str, type[Screen]] = {
            "dashboard": DashboardScreen,
            "chat": ChatScreen,
            "memory": MemoryBrowserScreen,
        }
        cls = screen_map.get(screen_name)
        if cls is not None:
            self.app.switch_screen(cls(socket_path=self.socket_path))
