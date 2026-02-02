"""Tests for the token budget tracker."""

from __future__ import annotations

import asyncio
import time
from unittest.mock import patch

import pytest

from meept.llm.budget import TokenBudget
from meept.llm.models import TokenUsage


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _usage(total: int) -> TokenUsage:
    """Create a TokenUsage with prompt/completion split arbitrarily."""
    return TokenUsage(
        prompt_tokens=total // 2,
        completion_tokens=total - total // 2,
        total_tokens=total,
    )


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_within_budget() -> None:
    """When usage is well under the limit, check_budget() should return True."""
    budget = TokenBudget(hourly_limit=100_000, daily_limit=1_000_000, aggressiveness=1.0)
    budget.record_usage(_usage(1000))

    assert budget.check_budget() is True


def test_over_hourly_budget() -> None:
    """Exceeding the effective hourly limit should cause check_budget() to return False."""
    # With aggressiveness=1.0 the effective limit equals the raw limit.
    budget = TokenBudget(hourly_limit=5000, daily_limit=1_000_000, aggressiveness=1.0)

    # Record usage that exactly matches the limit.
    budget.record_usage(_usage(5000))

    assert budget.check_budget() is False


def test_over_daily_budget() -> None:
    """Exceeding the effective daily limit should cause check_budget() to return False."""
    budget = TokenBudget(hourly_limit=1_000_000, daily_limit=5000, aggressiveness=1.0)

    budget.record_usage(_usage(5000))

    assert budget.check_budget() is False


def test_aggressiveness() -> None:
    """Lower aggressiveness should reduce the effective budget, causing earlier blocking.

    With aggressiveness=0.0 the effective limit is 50% of the raw limit.
    """
    budget = TokenBudget(hourly_limit=10_000, daily_limit=1_000_000, aggressiveness=0.0)

    # Effective hourly limit = 10_000 * 0.5 = 5_000.
    budget.record_usage(_usage(5000))

    assert budget.check_budget() is False

    # With aggressiveness=1.0, 5000 tokens against a 10_000 limit is fine.
    budget_high = TokenBudget(hourly_limit=10_000, daily_limit=1_000_000, aggressiveness=1.0)
    budget_high.record_usage(_usage(5000))

    assert budget_high.check_budget() is True


async def test_rate_limiting() -> None:
    """When RPM limit is reached, wait_for_rate_limit() should sleep."""
    budget = TokenBudget(
        hourly_limit=1_000_000,
        daily_limit=10_000_000,
        rate_limit_rpm=2,
        aggressiveness=1.0,
    )

    # Simulate two requests in the current minute.
    budget.record_usage(_usage(100))
    budget.record_usage(_usage(100))

    # The third call should trigger a sleep.  We mock asyncio.sleep to avoid
    # actually waiting 60 seconds.
    async def _fake_sleep(seconds: float) -> None:
        pass

    with patch("meept.llm.budget.asyncio.sleep", side_effect=_fake_sleep):
        await budget.wait_for_rate_limit()

    # With rpm=0, wait_for_rate_limit returns immediately.
    budget_unlimited = TokenBudget(rate_limit_rpm=0)
    await budget_unlimited.wait_for_rate_limit()  # Should not raise.
