"""Tests for the PipelineExecutor with retry and context passing."""

from __future__ import annotations

import asyncio
from typing import Any

import pytest

from meept.models.messages import BusMessage
from meept.scheduler.pipelines import Pipeline, PipelineExecutor, PipelineStep


# ---------------------------------------------------------------------------
# Mock bus
# ---------------------------------------------------------------------------


class MockBus:
    """Records published messages."""

    def __init__(self) -> None:
        self.messages: list[tuple[str, BusMessage]] = []

    async def publish(self, topic: str, msg: BusMessage) -> None:
        self.messages.append((topic, msg))


# ---------------------------------------------------------------------------
# Tests: basic execution
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_single_step_pipeline() -> None:
    """A single-step pipeline should execute and return success."""
    bus = MockBus()
    executor = PipelineExecutor(bus)

    def handler(ctx: dict) -> str:
        return "hello"

    pipeline = Pipeline(
        id="p1", name="Test",
        steps=[PipelineStep(id="s1", name="Step 1", handler=handler)],
    )

    results = await executor.execute(pipeline)
    assert results["s1"]["success"] is True
    assert results["s1"]["result"] == "hello"


@pytest.mark.asyncio
async def test_async_handler() -> None:
    """Async handlers should be awaited correctly."""
    bus = MockBus()
    executor = PipelineExecutor(bus)

    async def handler(ctx: dict) -> str:
        return "async_result"

    pipeline = Pipeline(
        id="p2", name="Async Test",
        steps=[PipelineStep(id="s1", name="Step 1", handler=handler)],
    )

    results = await executor.execute(pipeline)
    assert results["s1"]["success"] is True
    assert results["s1"]["result"] == "async_result"


@pytest.mark.asyncio
async def test_dependency_ordering() -> None:
    """Steps should wait for dependencies before running."""
    bus = MockBus()
    executor = PipelineExecutor(bus)
    order: list[str] = []

    async def handler_a(ctx: dict) -> str:
        order.append("a")
        return "a_done"

    async def handler_b(ctx: dict) -> str:
        order.append("b")
        return "b_done"

    pipeline = Pipeline(
        id="p3", name="Deps",
        steps=[
            PipelineStep(id="a", name="A", handler=handler_a),
            PipelineStep(id="b", name="B", handler=handler_b, depends_on=["a"]),
        ],
    )

    results = await executor.execute(pipeline)
    assert order == ["a", "b"]
    assert results["a"]["success"] is True
    assert results["b"]["success"] is True


@pytest.mark.asyncio
async def test_dependency_failure_skips_dependent() -> None:
    """When a dependency fails, dependents should be skipped."""
    bus = MockBus()
    executor = PipelineExecutor(bus)

    def failing(ctx: dict) -> None:
        raise RuntimeError("boom")

    def should_not_run(ctx: dict) -> str:
        return "ran"

    pipeline = Pipeline(
        id="p4", name="Fail",
        steps=[
            PipelineStep(id="a", name="A", handler=failing),
            PipelineStep(id="b", name="B", handler=should_not_run, depends_on=["a"]),
        ],
    )

    results = await executor.execute(pipeline)
    assert results["a"]["success"] is False
    assert results["b"]["success"] is False
    assert results["b"]["status"] == "skipped"


# ---------------------------------------------------------------------------
# Tests: context passing
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_context_passing_between_steps() -> None:
    """Results from earlier steps should be available in the context dict."""
    bus = MockBus()
    executor = PipelineExecutor(bus)
    received_context: dict[str, Any] = {}

    def producer(ctx: dict) -> str:
        return "produced_value"

    def consumer(ctx: dict) -> str:
        received_context.update(ctx)
        return f"consumed: {ctx.get('a')}"

    pipeline = Pipeline(
        id="p5", name="Context",
        steps=[
            PipelineStep(id="a", name="Producer", handler=producer),
            PipelineStep(id="b", name="Consumer", handler=consumer, depends_on=["a"]),
        ],
    )

    results = await executor.execute(pipeline)
    assert results["b"]["success"] is True
    assert results["b"]["result"] == "consumed: produced_value"
    assert received_context["a"] == "produced_value"


@pytest.mark.asyncio
async def test_context_key_override() -> None:
    """A custom context_key should be used instead of step id."""
    bus = MockBus()
    executor = PipelineExecutor(bus)

    def producer(ctx: dict) -> str:
        return "data"

    def consumer(ctx: dict) -> str:
        return f"got: {ctx.get('custom_key')}"

    pipeline = Pipeline(
        id="p6", name="CtxKey",
        steps=[
            PipelineStep(id="a", name="Producer", handler=producer, context_key="custom_key"),
            PipelineStep(id="b", name="Consumer", handler=consumer, depends_on=["a"]),
        ],
    )

    results = await executor.execute(pipeline)
    assert results["b"]["result"] == "got: data"


# ---------------------------------------------------------------------------
# Tests: retry logic
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_retry_on_failure() -> None:
    """Steps with max_retries should retry on failure."""
    bus = MockBus()
    executor = PipelineExecutor(bus)
    attempt_count = 0

    def flaky(ctx: dict) -> str:
        nonlocal attempt_count
        attempt_count += 1
        if attempt_count < 3:
            raise RuntimeError("transient error")
        return "success"

    pipeline = Pipeline(
        id="p7", name="Retry",
        steps=[PipelineStep(
            id="s1", name="Flaky", handler=flaky,
            max_retries=2, retry_delay=0.01,
        )],
    )

    results = await executor.execute(pipeline)
    assert results["s1"]["success"] is True
    assert attempt_count == 3


@pytest.mark.asyncio
async def test_retry_exhausted() -> None:
    """When all retries are exhausted, the step should fail."""
    bus = MockBus()
    executor = PipelineExecutor(bus)

    def always_fails(ctx: dict) -> None:
        raise RuntimeError("permanent error")

    pipeline = Pipeline(
        id="p8", name="RetryFail",
        steps=[PipelineStep(
            id="s1", name="Failing", handler=always_fails,
            max_retries=1, retry_delay=0.01,
        )],
    )

    results = await executor.execute(pipeline)
    assert results["s1"]["success"] is False
    assert results["s1"]["status"] == "failed"
    assert "permanent error" in results["s1"]["error"]


@pytest.mark.asyncio
async def test_no_retry_by_default() -> None:
    """With default max_retries=0, there should be exactly 1 attempt."""
    bus = MockBus()
    executor = PipelineExecutor(bus)
    attempts = 0

    def fail_once(ctx: dict) -> None:
        nonlocal attempts
        attempts += 1
        raise RuntimeError("fail")

    pipeline = Pipeline(
        id="p9", name="NoRetry",
        steps=[PipelineStep(id="s1", name="No Retry", handler=fail_once)],
    )

    results = await executor.execute(pipeline)
    assert results["s1"]["success"] is False
    assert attempts == 1


# ---------------------------------------------------------------------------
# Tests: cancel
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_cancel_pipeline() -> None:
    """Cancelling a pipeline should stop pending steps."""
    bus = MockBus()
    executor = PipelineExecutor(bus)
    started = asyncio.Event()

    async def slow_handler(ctx: dict) -> str:
        started.set()
        await asyncio.sleep(10)
        return "should not complete"

    pipeline = Pipeline(
        id="p10", name="Cancel",
        steps=[PipelineStep(id="s1", name="Slow", handler=slow_handler)],
    )

    task = asyncio.create_task(executor.execute(pipeline))
    await started.wait()
    executor.cancel()
    results = await task

    assert results["s1"]["success"] is False
    assert results["s1"]["status"] == "cancelled"


# ---------------------------------------------------------------------------
# Tests: bus events
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_progress_events_published() -> None:
    """The executor should publish started, step_started, step_completed, and completed events."""
    bus = MockBus()
    executor = PipelineExecutor(bus)

    def handler(ctx: dict) -> str:
        return "ok"

    pipeline = Pipeline(
        id="p11", name="Events",
        steps=[PipelineStep(id="s1", name="Step 1", handler=handler)],
    )

    await executor.execute(pipeline)

    topics = [t for t, _ in bus.messages]
    assert f"pipeline.p11.started" in topics
    assert f"pipeline.p11.step_started" in topics
    assert f"pipeline.p11.step_completed" in topics
    assert f"pipeline.p11.completed" in topics


# ---------------------------------------------------------------------------
# Tests: DAG validation
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_invalid_dependency_raises() -> None:
    """A step depending on a non-existent step should raise ValueError."""
    bus = MockBus()
    executor = PipelineExecutor(bus)

    pipeline = Pipeline(
        id="p12", name="Bad DAG",
        steps=[PipelineStep(
            id="s1", name="Step 1",
            handler=lambda ctx: None,
            depends_on=["nonexistent"],
        )],
    )

    with pytest.raises(ValueError, match="unknown step"):
        await executor.execute(pipeline)
