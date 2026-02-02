"""Status bar widget for the Meept TUI.

Displays daemon connection status, current LLM model name, uptime, and
budget percentage with colour-coded indicators.
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


def _format_uptime(seconds: float) -> str:
    """Convert raw seconds to a human-readable ``Xd Xh Xm Xs`` string."""
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


def _budget_colour(pct: float) -> str:
    """Return a Rich colour string for the given budget percentage."""
    if pct > 50.0:
        return "green"
    if pct > 20.0:
        return "yellow"
    return "red"


class StatusBarWidget(Static):
    """A compact status bar showing connection state, model, uptime, and budget.

    Call :meth:`update_status` with a dict from the daemon ``status`` RPC
    (optionally augmented with budget info) to refresh the display.

    Expected dict shape::

        {
            "status": "running",
            "uptime_seconds": 1234.5,
            "model": "llama3.2",
            "budget_remaining_pct": 75.0,
        }
    """

    DEFAULT_CSS = """
    StatusBarWidget {
        dock: top;
        height: 3;
        padding: 0 2;
        background: $surface;
        border-bottom: solid $primary;
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
        """Render initial disconnected state."""
        self.update(self._render_content())

    def update_status(self, data: dict[str, Any]) -> None:
        """Refresh the status bar from daemon data.

        Parameters
        ----------
        data:
            Status dict -- see class docstring for expected keys.
        """
        self._data = data
        self.update(self._render_content())

    def _render_content(self) -> str:
        """Build a single-line Rich-markup status string."""
        d = self._data

        # -- Connection indicator -------------------------------------------
        daemon_status = d.get("status", "")
        if daemon_status == "running":
            conn_indicator = "[green]\u25cf[/green] Connected"
        else:
            conn_indicator = "[red]\u25cb[/red] Disconnected"

        # -- Model name -----------------------------------------------------
        model = d.get("model", d.get("default_model", "n/a"))
        model_part = f"[bold]{model}[/bold]"

        # -- Uptime ---------------------------------------------------------
        uptime_raw = d.get("uptime_seconds", -1)
        uptime_str = _format_uptime(float(uptime_raw))

        # -- Budget ---------------------------------------------------------
        budget_pct = d.get("budget_remaining_pct")
        if budget_pct is None:
            # Derive from daily usage if available.
            daily_used = d.get("daily_used", 0)
            daily_limit = d.get("daily_limit", 0)
            if daily_limit > 0:
                budget_pct = max(0.0, (1.0 - daily_used / daily_limit) * 100.0)
            else:
                budget_pct = 100.0

        colour = _budget_colour(budget_pct)
        budget_part = f"[{colour}]{budget_pct:.0f}%[/{colour}]"

        # -- Assemble -------------------------------------------------------
        return (
            f"  {conn_indicator}"
            f"   |   Model: {model_part}"
            f"   |   Uptime: {uptime_str}"
            f"   |   Budget: {budget_part}"
        )
