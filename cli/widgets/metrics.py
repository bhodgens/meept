"""Metrics display widget for the Meept TUI dashboard.

Shows token usage (hourly/daily), budget remaining, LLM call count,
and memory item count using Rich markup for colour-coded formatting.
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


def _pct_colour(pct: float) -> str:
    """Return a Rich colour name based on a percentage value.

    Green when healthy (>50 %), yellow when moderate (20-50 %), red when
    critical (<20 %).
    """
    if pct > 50.0:
        return "green"
    if pct > 20.0:
        return "yellow"
    return "red"


def _format_tokens(n: int) -> str:
    """Human-friendly token count (e.g. ``1.2k``, ``3.5M``)."""
    if n >= 1_000_000:
        return f"{n / 1_000_000:.1f}M"
    if n >= 1_000:
        return f"{n / 1_000:.1f}k"
    return str(n)


class MetricsWidget(Static):
    """Dashboard widget displaying key operational metrics.

    Call :meth:`update_metrics` with a status dict to refresh the display.
    The expected dict shape matches the output of the daemon ``status`` RPC
    combined with budget information::

        {
            "hourly_used": 12345,
            "hourly_limit": 100000,
            "daily_used": 50000,
            "daily_limit": 1000000,
            "llm_calls": 42,
            "memory_items": 128,
            "budget_remaining_pct": 75.0,
        }
    """

    DEFAULT_CSS = """
    MetricsWidget {
        padding: 1 2;
        height: auto;
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
        """Render initial placeholder content."""
        self.update(self._render_content())

    def update_metrics(self, data: dict[str, Any]) -> None:
        """Update the display with fresh metrics *data*.

        Parameters
        ----------
        data:
            Dict containing metric keys.  Missing keys are handled
            gracefully with sensible defaults.
        """
        self._data = data
        self.update(self._render_content())

    def _render_content(self) -> str:
        """Build Rich-markup text for the current data."""
        if not self._data:
            return (
                "[bold underline]Metrics[/bold underline]\n\n"
                "[dim]Waiting for data...[/dim]"
            )

        d = self._data

        # -- Token usage (hourly) -------------------------------------------
        hourly_used = d.get("hourly_used", 0)
        hourly_limit = d.get("hourly_limit", 1)
        hourly_pct = (hourly_used / max(hourly_limit, 1)) * 100.0
        hourly_colour = _pct_colour(100.0 - hourly_pct)

        # -- Token usage (daily) --------------------------------------------
        daily_used = d.get("daily_used", 0)
        daily_limit = d.get("daily_limit", 1)
        daily_pct = (daily_used / max(daily_limit, 1)) * 100.0
        daily_colour = _pct_colour(100.0 - daily_pct)

        # -- Budget remaining -----------------------------------------------
        budget_pct = d.get("budget_remaining_pct")
        if budget_pct is None:
            # Derive from daily remaining if not explicitly provided.
            budget_pct = max(0.0, 100.0 - daily_pct)
        budget_colour = _pct_colour(budget_pct)

        # -- LLM calls & memory items --------------------------------------
        llm_calls = d.get("llm_calls", d.get("rpm_current", 0))
        memory_items = d.get("memory_items", d.get("total_count", 0))

        lines = [
            "[bold underline]Metrics[/bold underline]",
            "",
            f"[bold]Tokens (hourly):[/bold]  [{hourly_colour}]{_format_tokens(hourly_used)}"
            f" / {_format_tokens(hourly_limit)}[/{hourly_colour}]",
            f"[bold]Tokens (daily):[/bold]   [{daily_colour}]{_format_tokens(daily_used)}"
            f" / {_format_tokens(daily_limit)}[/{daily_colour}]",
            "",
            f"[bold]Budget remaining:[/bold] [{budget_colour}]{budget_pct:.1f}%[/{budget_colour}]",
            "",
            f"[bold]LLM calls:[/bold]        {llm_calls}",
            f"[bold]Memory items:[/bold]     {memory_items}",
        ]
        return "\n".join(lines)
