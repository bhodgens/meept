"""Tests for the ClawHub HTTP client."""

from __future__ import annotations

import hashlib
import time

import httpx
import pytest

from meept.clawskills.client import (
    ClawHubAPIError,
    ClawHubClient,
    DownloadResult,
    RateLimiter,
    ResponseCache,
)
from tests.test_clawskills.conftest import MockTransport, make_skill_zip


# ---------------------------------------------------------------------------
# RateLimiter
# ---------------------------------------------------------------------------


class TestRateLimiter:
    def test_allows_under_limit(self) -> None:
        rl = RateLimiter(max_requests=5, window_seconds=60)
        for _ in range(5):
            assert rl.acquire() is True

    def test_blocks_over_limit(self) -> None:
        rl = RateLimiter(max_requests=2, window_seconds=60)
        assert rl.acquire() is True
        assert rl.acquire() is True
        assert rl.acquire() is False

    def test_wait_time_zero_under_limit(self) -> None:
        rl = RateLimiter(max_requests=10, window_seconds=60)
        assert rl.wait_time() == 0.0

    def test_wait_time_positive_over_limit(self) -> None:
        rl = RateLimiter(max_requests=1, window_seconds=60)
        rl.acquire()
        assert rl.wait_time() > 0.0


# ---------------------------------------------------------------------------
# ResponseCache
# ---------------------------------------------------------------------------


class TestResponseCache:
    def test_put_and_get(self) -> None:
        cache = ResponseCache(ttl=60)
        cache.put("key", {"data": 1})
        assert cache.get("key") == {"data": 1}

    def test_miss(self) -> None:
        cache = ResponseCache()
        assert cache.get("missing") is None

    def test_expiry(self) -> None:
        cache = ResponseCache(ttl=0)  # Immediate expiry.
        cache.put("key", "value")
        # The entry should be expired by the time we read it.
        assert cache.get("key") is None

    def test_invalidate(self) -> None:
        cache = ResponseCache(ttl=300)
        cache.put("key", "value")
        cache.invalidate("key")
        assert cache.get("key") is None

    def test_clear(self) -> None:
        cache = ResponseCache(ttl=300)
        cache.put("a", 1)
        cache.put("b", 2)
        cache.clear()
        assert cache.get("a") is None
        assert cache.get("b") is None


# ---------------------------------------------------------------------------
# ClawHubClient (using MockTransport)
# ---------------------------------------------------------------------------


@pytest.fixture()
async def client(mock_transport: MockTransport) -> ClawHubClient:
    c = ClawHubClient(base_url="https://clawhub.ai")
    c._client = httpx.AsyncClient(
        transport=mock_transport,
        base_url="https://clawhub.ai",
    )
    yield c
    await c.close()


class TestClawHubClient:
    @pytest.mark.asyncio
    async def test_search(self, client: ClawHubClient) -> None:
        results = await client.search("gif")
        assert len(results) == 2
        assert results[0]["slug"] == "gifgrep"

    @pytest.mark.asyncio
    async def test_skill_detail(self, client: ClawHubClient) -> None:
        detail = await client.skill_detail("gifgrep")
        assert detail["slug"] == "gifgrep"
        assert detail["author"] == "testuser"

    @pytest.mark.asyncio
    async def test_skill_versions(self, client: ClawHubClient) -> None:
        versions = await client.skill_versions("gifgrep")
        assert len(versions) == 2
        assert versions[0]["version"] == "1.2.0"

    @pytest.mark.asyncio
    async def test_skill_file(self, client: ClawHubClient) -> None:
        content = await client.skill_file("gifgrep")
        assert "test-skill" in content

    @pytest.mark.asyncio
    async def test_resolve_version(self, client: ClawHubClient) -> None:
        resolved = await client.resolve_version("gifgrep")
        assert resolved["version"] == "1.2.0"

    @pytest.mark.asyncio
    async def test_download(self, client: ClawHubClient) -> None:
        result = await client.download("gifgrep")
        assert isinstance(result, DownloadResult)
        assert result.size > 0
        assert len(result.sha256) == 64

    @pytest.mark.asyncio
    async def test_rate_limit_error(self, client: ClawHubClient) -> None:
        # Exhaust the rate limiter.
        client._rate_limiter = RateLimiter(max_requests=0, window_seconds=60)
        with pytest.raises(ClawHubAPIError) as exc_info:
            await client.search("test")
        assert exc_info.value.status_code == 429

    @pytest.mark.asyncio
    async def test_api_error_404(self) -> None:
        async def always_404(request: httpx.Request) -> httpx.Response:
            return httpx.Response(404, json={"error": "not found"})

        transport = httpx.MockTransport(always_404)
        c = ClawHubClient(base_url="https://clawhub.ai")
        c._client = httpx.AsyncClient(transport=transport, base_url="https://clawhub.ai")
        try:
            with pytest.raises(ClawHubAPIError) as exc_info:
                await c.skill_detail("missing")
            assert exc_info.value.status_code == 404
        finally:
            await c.close()

    @pytest.mark.asyncio
    async def test_cache_hit(self, client: ClawHubClient, mock_transport: MockTransport) -> None:
        await client.search("test")
        initial_count = len(mock_transport.requests)
        await client.search("test")
        # Second call should be served from cache.
        assert len(mock_transport.requests) == initial_count
