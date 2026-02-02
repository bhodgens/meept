"""Task list widget for the Meept TUI.

Renders a scrollable list of agent tasks/actions with status indicators.
"""

from __future__ import annotations

from typing import Any

try:
    from textual.widgets import Static
except ImportError as _exc:
    raise ImportError(
        "The 'textual' package is required for the Meept TUI.\n"
        "Install it with:  pip install 'meept[cli]'  (or: pip install 'textual>=1.0')"
    ) from _exc


# Status label -> icon and colour mappings.
_STATUS_ICONS: dict[str, tuple[str, str]] = {
    "completed": ("\u2713", "green"),        # checkmark
    "running": ("\u27f3", "cyan"),            # rotating arrows
    "in_progress": ("\u27f3", "cyan"),        # rotating arrows (alias)
    "failed": ("\u2717", "red"),              # ballot X
    "pending": ("\u25cb", "dim"),             # white circle
    "cancelled": ("\u2717", "yellow"),        # ballot X (yellow)
}

_DEFAULT_ICON = ("\u25cb", "dim")


class TaskListWidget(Static):
    """Widget that displays a list of tasks/actions with status indicators.

    Each task is expected to be a ``dict`` with at least::

        {
            "name": "Task description",
            "status": "completed",       # or running/failed/pending
            "timestamp": "2025-05-12T...",  # optional ISO timestamp
        }

    Call :meth:`update_tasks` to refresh the display.
    """

    DEFAULT_CSS = """
    TaskListWidget {
        padding: 1 2;
        height: auto;
        max-height: 100%;
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
        """Render initial placeholder content."""
        self.update(self._render_content())

    def update_tasks(self, tasks: list[dict[str, Any]]) -> None:
        """Replace the current task list and re-render.

        Parameters
        ----------
        tasks:
            Ordered list of task dicts.  Newest tasks should come first
            for the best UX.
        """
        self._tasks = list(tasks)
        self.update(self._render_content())

    def _render_content(self) -> str:
        """Build Rich-markup text for the task list."""
        if not self._tasks:
            return (
                "[bold underline]Recent Tasks[/bold underline]\n\n"
                "[dim]No tasks yet.[/dim]"
            )

        lines: list[str] = [
            "[bold underline]Recent Tasks[/bold underline]",
            "",
        ]

        for task in self._tasks:
            status = str(task.get("status", "pending")).lower()
            icon, colour = _STATUS_ICONS.get(status, _DEFAULT_ICON)
            task_name = task.get("name", task.get("description", "Unnamed task"))
            timestamp = task.get("timestamp", "")

            # Truncate long names to keep the display tidy.
            if len(task_name) > 48:
                task_name = task_name[:45] + "..."

            ts_part = f"  [dim]{timestamp}[/dim]" if timestamp else ""
            lines.append(f"  [{colour}]{icon}[/{colour}]  {task_name}{ts_part}")

        return "\n".join(lines)
