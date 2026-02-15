"""Integration tests for Go daemon with Python client.

Run these tests after starting the Go daemon:
    ./bin/meept-daemon --state-dir /tmp/meept-test &
    pytest tests/test_go_integration.py -v
"""

from __future__ import annotations

import asyncio
import os
import subprocess
import time
from pathlib import Path

import pytest

from meept.comm.go_client import GoDaemonClient, check_go_daemon

# Test socket path
TEST_STATE_DIR = Path("/tmp/meept-test")
TEST_SOCKET = TEST_STATE_DIR / "meept.sock"


@pytest.fixture(scope="module")
def go_daemon():
    """Start Go daemon for tests."""
    TEST_STATE_DIR.mkdir(parents=True, exist_ok=True)

    # Find the Go binary
    bin_path = Path(__file__).parent.parent / "bin" / "meept-daemon"
    if not bin_path.exists():
        pytest.skip("Go daemon not built. Run 'make go-build' first.")

    # Start daemon
    proc = subprocess.Popen(
        [str(bin_path), "--state-dir", str(TEST_STATE_DIR), "--foreground"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )

    # Wait for socket to appear
    for _ in range(50):  # 5 seconds max
        if TEST_SOCKET.exists():
            break
        time.sleep(0.1)
    else:
        proc.kill()
        pytest.fail("Go daemon did not create socket in time")

    yield proc

    # Cleanup
    proc.terminate()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()


@pytest.mark.asyncio
async def test_ping(go_daemon):
    """Test basic ping/pong."""
    async with GoDaemonClient(TEST_SOCKET) as client:
        result = await client.ping()
        assert result == "pong"


@pytest.mark.asyncio
async def test_status(go_daemon):
    """Test daemon status."""
    async with GoDaemonClient(TEST_SOCKET) as client:
        result = await client.status()
        assert result["status"] == "running"
        assert "version" in result


@pytest.mark.asyncio
async def test_bus_stats(go_daemon):
    """Test bus statistics."""
    async with GoDaemonClient(TEST_SOCKET) as client:
        result = await client.bus_stats()
        assert "_total" in result


@pytest.mark.asyncio
async def test_bus_publish(go_daemon):
    """Test publishing to bus."""
    async with GoDaemonClient(TEST_SOCKET) as client:
        result = await client.bus_publish("test.topic", {"message": "hello"})
        assert "delivered" in result


@pytest.mark.asyncio
async def test_check_go_daemon(go_daemon):
    """Test daemon health check utility."""
    result = await check_go_daemon(TEST_SOCKET)
    assert result is True


@pytest.mark.asyncio
async def test_multiple_calls(go_daemon):
    """Test multiple sequential calls."""
    async with GoDaemonClient(TEST_SOCKET) as client:
        for i in range(10):
            result = await client.ping()
            assert result == "pong"


@pytest.mark.asyncio
async def test_concurrent_calls(go_daemon):
    """Test concurrent calls."""
    async with GoDaemonClient(TEST_SOCKET) as client:
        tasks = [client.call("ping") for _ in range(20)]
        results = await asyncio.gather(*tasks)
        assert all(r == "pong" for r in results)
