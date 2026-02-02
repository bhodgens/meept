"""Tests for the component registry."""

from __future__ import annotations

import pytest

from meept.core.registry import Registry


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


async def test_register_and_get() -> None:
    """register_instance() should make the object retrievable via get()."""
    reg = Registry()
    obj = {"hello": "world"}
    reg.register_instance("my_component", obj)

    assert reg.get("my_component") is obj


async def test_singleton() -> None:
    """get_or_create() should return the same instance on subsequent calls."""
    reg = Registry()
    call_count = 0

    def factory():
        nonlocal call_count
        call_count += 1
        return {"instance": call_count}

    reg.register("singleton", factory)

    first = await reg.get_or_create("singleton")
    second = await reg.get_or_create("singleton")

    assert first is second
    assert call_count == 1


async def test_async_factory() -> None:
    """An async factory function should be awaited and its result stored."""
    reg = Registry()

    async def async_factory():
        return {"async": True}

    reg.register("async_comp", async_factory)

    result = await reg.get_or_create("async_comp")
    assert result == {"async": True}

    # Second call should return the cached singleton.
    same = await reg.get_or_create("async_comp")
    assert same is result


async def test_missing_component() -> None:
    """Requesting a component with no registered factory should raise KeyError."""
    reg = Registry()

    # get() returns None for missing components.
    assert reg.get("nonexistent") is None

    # get_or_create() raises KeyError.
    with pytest.raises(KeyError, match="nonexistent"):
        await reg.get_or_create("nonexistent")
