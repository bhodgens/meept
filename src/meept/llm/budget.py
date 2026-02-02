"""Token budget tracking with sliding-window hourly limits and daily resets."""

from __future__ import annotations

import asyncio
import logging
import time
from collections import deque
from datetime import UTC, datetime

from meept.llm.models import TokenUsage

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Timestamped usage record kept in the sliding window
# ---------------------------------------------------------------------------

_UsageRecord = tuple[float, int]  # (unix_timestamp, total_tokens)


class TokenBudget:
    """Track and enforce token consumption budgets.

    Parameters
    ----------
    hourly_limit:
        Maximum total tokens allowed in any rolling 1-hour window.
    daily_limit:
        Maximum total tokens allowed per calendar day (UTC).
    rate_limit_rpm:
        Maximum requests per minute.  ``0`` means unlimited.
    aggressiveness:
        Float in ``[0.0, 1.0]``.  Controls how much of the budget is
        actually usable:
        - ``0.0`` -- stop when 50 % of the limit is reached (conservative).
        - ``1.0`` -- use the full limit.
        The effective limit is ``base * (0.5 + 0.5 * aggressiveness)``.
    """

    def __init__(
        self,
        hourly_limit: int = 500_000,
        daily_limit: int = 5_000_000,
        rate_limit_rpm: int = 0,
        aggressiveness: float = 0.5,
    ) -> None:
        if not 0.0 <= aggressiveness <= 1.0:
            raise ValueError("aggressiveness must be between 0.0 and 1.0")

        self._hourly_limit = hourly_limit
        self._daily_limit = daily_limit
        self._rate_limit_rpm = rate_limit_rpm
        self._aggressiveness = aggressiveness

        # Sliding window for the last hour
        self._hourly_window: deque[_UsageRecord] = deque()

        # Daily tracking -- reset at midnight UTC
        self._daily_used: int = 0
        self._current_day: int = datetime.now(UTC).toordinal()

        # RPM tracking (sliding window of request timestamps)
        self._request_timestamps: deque[float] = deque()

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _effective_limit(self, base: int) -> int:
        """Return the effective limit after applying aggressiveness."""
        factor = 0.5 + 0.5 * self._aggressiveness
        return int(base * factor)

    def _prune_hourly_window(self) -> None:
        """Remove entries older than 1 hour from the sliding window."""
        cutoff = time.time() - 3600.0
        while self._hourly_window and self._hourly_window[0][0] < cutoff:
            self._hourly_window.popleft()

    def _prune_rpm_window(self) -> None:
        """Remove request timestamps older than 60 seconds."""
        cutoff = time.time() - 60.0
        while self._request_timestamps and self._request_timestamps[0] < cutoff:
            self._request_timestamps.popleft()

    def _maybe_reset_daily(self) -> None:
        """Reset the daily counter if we've crossed into a new UTC day."""
        today = datetime.now(UTC).toordinal()
        if today != self._current_day:
            logger.info("Daily token budget reset (new UTC day).")
            self._daily_used = 0
            self._current_day = today

    def _hourly_used(self) -> int:
        """Return total tokens used in the current sliding hour."""
        self._prune_hourly_window()
        return sum(tokens for _, tokens in self._hourly_window)

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def record_usage(self, usage: TokenUsage) -> None:
        """Record a completed API call's token usage."""
        now = time.time()
        self._maybe_reset_daily()

        self._hourly_window.append((now, usage.total_tokens))
        self._daily_used += usage.total_tokens
        self._request_timestamps.append(now)

        logger.debug(
            "Recorded %d tokens (hourly: %d, daily: %d)",
            usage.total_tokens,
            self._hourly_used(),
            self._daily_used,
        )

    def check_budget(self) -> bool:
        """Return ``True`` if the current usage is within all budget limits."""
        self._maybe_reset_daily()
        self._prune_hourly_window()

        hourly_ok = self._hourly_used() < self._effective_limit(self._hourly_limit)
        daily_ok = self._daily_used < self._effective_limit(self._daily_limit)
        return hourly_ok and daily_ok

    def get_status(self) -> dict:
        """Return a snapshot of current budget status."""
        self._maybe_reset_daily()
        self._prune_hourly_window()
        self._prune_rpm_window()

        eff_hourly = self._effective_limit(self._hourly_limit)
        eff_daily = self._effective_limit(self._daily_limit)
        hourly_used = self._hourly_used()

        return {
            "hourly_used": hourly_used,
            "hourly_limit": eff_hourly,
            "hourly_remaining": max(0, eff_hourly - hourly_used),
            "daily_used": self._daily_used,
            "daily_limit": eff_daily,
            "daily_remaining": max(0, eff_daily - self._daily_used),
            "rpm_current": len(self._request_timestamps),
            "rpm_limit": self._rate_limit_rpm,
            "aggressiveness": self._aggressiveness,
            "within_budget": self.check_budget(),
        }

    async def wait_for_rate_limit(self) -> None:
        """Sleep until the RPM rate limit window allows another request.

        If ``rate_limit_rpm`` is ``0`` (unlimited), this returns immediately.
        """
        if self._rate_limit_rpm <= 0:
            return

        self._prune_rpm_window()

        if len(self._request_timestamps) < self._rate_limit_rpm:
            return

        # Calculate how long until the oldest request falls out of the window
        oldest = self._request_timestamps[0]
        wait_seconds = 60.0 - (time.time() - oldest)
        if wait_seconds > 0:
            logger.info("Rate limited -- sleeping %.2fs", wait_seconds)
            await asyncio.sleep(wait_seconds)
            self._prune_rpm_window()
