"""Tests for the MeeptScheduler bus subscribers, agent jobs, and retry."""

from __future__ import annotations

import asyncio
from typing import Any

import pytest

from meept.models.messages import BusMessage, MessageType
from meept.scheduler.scheduler import MeeptScheduler


# ---------------------------------------------------------------------------
# Mock bus
# ---------------------------------------------------------------------------


class MockBus:
    """Minimal bus for testing -- supports publish, subscribe, unsubscribe."""

    def __init__(self) -> None:
        self.messages: list[tuple[str, BusMessage]] = []
        self._subscribers: dict[str, list[Any]] = {}

    async def publish(self, topic: str, msg: BusMessage) -> None:
        self.messages.append((topic, msg))
        for cb in self._subscribers.get(topic, []):
            await cb(topic, msg)

    def subscribe(self, topic: str, callback: Any) -> None:
        self._subscribers.setdefault(topic, []).append(callback)

    def unsubscribe(self, topic: str, callback: Any) -> None:
        subs = self._subscribers.get(topic, [])
        if callback in subs:
            subs.remove(callback)


class MockConfig:
    enabled = True
    timezone = "UTC"


# ---------------------------------------------------------------------------
# Tests: basic lifecycle
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_start_and_stop() -> None:
    """Scheduler should start and stop cleanly."""
    bus = MockBus()
    scheduler = MeeptScheduler(config=MockConfig(), bus=bus)

    await scheduler.start()
    assert scheduler._started is True

    await scheduler.stop()
    assert scheduler._started is False


@pytest.mark.asyncio
async def test_add_and_list_jobs() -> None:
    """Jobs added should appear in list_jobs."""
    bus = MockBus()
    scheduler = MeeptScheduler(config=MockConfig(), bus=bus)
    await scheduler.start()

    scheduler.add_job("test-job", lambda: None, "interval", seconds=60)
    jobs = scheduler.list_jobs()
    assert len(jobs) == 1
    assert jobs[0]["id"] == "test-job"

    await scheduler.stop()


# ---------------------------------------------------------------------------
# Tests: bus subscribers
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_bus_list_jobs_subscriber() -> None:
    """The scheduler should respond to scheduler.list_jobs on the bus."""
    bus = MockBus()
    scheduler = MeeptScheduler(config=MockConfig(), bus=bus)
    await scheduler.start()
    await scheduler.subscribe_to_bus()

    scheduler.add_job("job-1", lambda: None, "interval", seconds=60)

    # Simulate a list_jobs request on the bus.
    request = BusMessage(
        type=MessageType.STATUS_UPDATE,
        payload={"action": "list_jobs"},
        source="test",
    )
    await bus.publish("scheduler.list_jobs", request)

    # Find the response.
    responses = [
        (t, m) for t, m in bus.messages
        if t == "scheduler.result" and m.reply_to == request.id
    ]
    assert len(responses) == 1
    assert "jobs" in responses[0][1].payload
    assert len(responses[0][1].payload["jobs"]) == 1

    await scheduler.stop()


@pytest.mark.asyncio
async def test_bus_add_job_subscriber() -> None:
    """The scheduler should handle add_job requests from the bus."""
    bus = MockBus()
    scheduler = MeeptScheduler(config=MockConfig(), bus=bus)
    await scheduler.start()
    await scheduler.subscribe_to_bus()

    request = BusMessage(
        type=MessageType.STATUS_UPDATE,
        payload={
            "action": "add_job",
            "name": "test-add",
            "trigger": "interval",
            "trigger_args": {"seconds": 30},
            "job_id": "bus-test-1",
        },
        source="test",
    )
    await bus.publish("scheduler.add_job", request)

    # Verify response.
    responses = [
        (t, m) for t, m in bus.messages
        if t == "scheduler.result" and m.reply_to == request.id
    ]
    assert len(responses) == 1
    assert responses[0][1].payload["success"] is True
    assert responses[0][1].payload["job_id"] == "bus-test-1"

    # Verify job was actually added.
    jobs = scheduler.list_jobs()
    assert any(j["id"] == "bus-test-1" for j in jobs)

    await scheduler.stop()


# ---------------------------------------------------------------------------
# Tests: agent jobs
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_add_agent_job() -> None:
    """add_agent_job should create a job that publishes CHAT_REQUEST."""
    bus = MockBus()
    scheduler = MeeptScheduler(config=MockConfig(), bus=bus)
    await scheduler.start()

    scheduler.add_agent_job(
        job_id="agent-1",
        task_description="Check the weather",
        trigger="date",
    )

    jobs = scheduler.list_jobs()
    assert any(j["id"] == "agent-1" for j in jobs)

    await scheduler.stop()


# ---------------------------------------------------------------------------
# Tests: retry wrapper
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_retry_on_job_failure() -> None:
    """Jobs with max_retries should retry before reporting failure."""
    bus = MockBus()
    scheduler = MeeptScheduler(config=MockConfig(), bus=bus)
    await scheduler.start()

    attempt_count = 0

    def flaky_func() -> str:
        nonlocal attempt_count
        attempt_count += 1
        if attempt_count < 3:
            raise RuntimeError("transient")
        return "ok"

    scheduler.add_job(
        "retry-job", flaky_func, "date",
        max_retries=2, retry_delay=0.01,
    )

    # Wait for the date-trigger job to execute.
    await asyncio.sleep(0.3)

    assert attempt_count == 3

    # Check that a success result was published.
    results = [
        m for t, m in bus.messages
        if t == "scheduler.job.retry-job"
    ]
    assert len(results) >= 1
    assert results[-1].payload["success"] is True

    await scheduler.stop()


@pytest.mark.asyncio
async def test_retry_exhausted_reports_failure() -> None:
    """When all retries are exhausted, the scheduler should publish failure."""
    bus = MockBus()
    scheduler = MeeptScheduler(config=MockConfig(), bus=bus)
    await scheduler.start()

    def always_fails() -> None:
        raise RuntimeError("permanent")

    scheduler.add_job(
        "fail-job", always_fails, "date",
        max_retries=1, retry_delay=0.01,
    )

    await asyncio.sleep(0.3)

    results = [
        m for t, m in bus.messages
        if t == "scheduler.job.fail-job"
    ]
    assert len(results) >= 1
    assert results[-1].payload["success"] is False
    assert "permanent" in results[-1].payload["error"]

    await scheduler.stop()
