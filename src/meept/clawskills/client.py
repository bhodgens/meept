"""Async HTTP client for the ClawHub registry API (clawhub.ai).

Provides search, download, version resolution, and file fetching with
built-in rate limiting, response caching, and size caps.
"""

from __future__ import annotations

import hashlib
import logging
import time
from collections import deque
from dataclasses import dataclass, field
from typing import Any

import httpx

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Errors
# ---------------------------------------------------------------------------


class ClawHubAPIError(Exception):
    """Raised for non-200 responses from the ClawHub API."""

    def __init__(self, status_code: int, detail: str = "") -> None:
        self.status_code = status_code
        self.detail = detail
        super().__init__(f"ClawHub API error {status_code}: {detail}")


# ---------------------------------------------------------------------------
# Rate limiter
# ---------------------------------------------------------------------------

_DEFAULT_WINDOW_SECONDS = 60
_DEFAULT_MAX_REQUESTS = 100  # Below ClawHub's 120/min limit


class RateLimiter:
    """Sliding-window rate limiter."""

    def __init__(
        self,
        max_requests: int = _DEFAULT_MAX_REQUESTS,
        window_seconds: float = _DEFAULT_WINDOW_SECONDS,
    ) -> None:
        self.max_requests = max_requests
        self.window_seconds = window_seconds
        self._timestamps: deque[float] = deque()

    def _purge(self) -> None:
        cutoff = time.monotonic() - self.window_seconds
        while self._timestamps and self._timestamps[0] < cutoff:
            self._timestamps.popleft()

    def acquire(self) -> bool:
        """Return ``True`` if the request is allowed, ``False`` otherwise."""
        self._purge()
        if len(self._timestamps) >= self.max_requests:
            return False
        self._timestamps.append(time.monotonic())
        return True

    def wait_time(self) -> float:
        """Seconds to wait before the next request would be allowed."""
        self._purge()
        if len(self._timestamps) < self.max_requests:
            return 0.0
        if not self._timestamps:
            return self.window_seconds
        oldest = self._timestamps[0]
        return max(0.0, oldest + self.window_seconds - time.monotonic())


# ---------------------------------------------------------------------------
# Response cache
# ---------------------------------------------------------------------------

_CACHE_TTL_SECONDS = 300  # 5 minutes


@dataclass(slots=True)
class _CacheEntry:
    data: Any
    expires: float


class ResponseCache:
    """In-memory TTL cache for GET responses."""

    def __init__(self, ttl: float = _CACHE_TTL_SECONDS) -> None:
        self.ttl = ttl
        self._store: dict[str, _CacheEntry] = {}

    def get(self, key: str) -> Any | None:
        entry = self._store.get(key)
        if entry is None:
            return None
        if time.monotonic() > entry.expires:
            del self._store[key]
            return None
        return entry.data

    def put(self, key: str, data: Any) -> None:
        self._store[key] = _CacheEntry(data=data, expires=time.monotonic() + self.ttl)

    def invalidate(self, key: str) -> None:
        self._store.pop(key, None)

    def clear(self) -> None:
        self._store.clear()


# ---------------------------------------------------------------------------
# Download result
# ---------------------------------------------------------------------------

_MAX_DOWNLOAD_BYTES = 10 * 1024 * 1024  # 10 MB
_MAX_FILE_BYTES = 200 * 1024  # 200 KB


@dataclass(slots=True)
class DownloadResult:
    """Result of a streaming ZIP download."""

    data: bytes
    sha256: str
    size: int


# ---------------------------------------------------------------------------
# ClawHubClient
# ---------------------------------------------------------------------------


class ClawHubClient:
    """Async HTTP client for the ClawHub API.

    Parameters
    ----------
    base_url:
        Base URL of the ClawHub registry (e.g. ``https://clawhub.ai``).
    timeout:
        HTTP timeout in seconds.
    """

    def __init__(
        self,
        base_url: str = "https://clawhub.ai",
        timeout: float = 30.0,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self._timeout = timeout
        self._rate_limiter = RateLimiter()
        self._cache = ResponseCache()
        self._client: httpx.AsyncClient | None = None

    async def _ensure_client(self) -> httpx.AsyncClient:
        if self._client is None or self._client.is_closed:
            self._client = httpx.AsyncClient(
                base_url=self.base_url,
                timeout=self._timeout,
                follow_redirects=True,
                headers={"User-Agent": "meept-clawskills/1.0"},
            )
        return self._client

    async def close(self) -> None:
        """Close the underlying HTTP client."""
        if self._client is not None and not self._client.is_closed:
            await self._client.aclose()
            self._client = None

    async def __aenter__(self) -> ClawHubClient:
        return self

    async def __aexit__(self, *exc: object) -> None:
        await self.close()

    async def _get_json(self, path: str, params: dict[str, Any] | None = None) -> Any:
        """Issue a GET request and return the parsed JSON body."""
        if not self._rate_limiter.acquire():
            wait = self._rate_limiter.wait_time()
            raise ClawHubAPIError(429, f"Rate limit exceeded, retry after {wait:.1f}s")

        cache_key = f"{path}?{params}" if params else path
        cached = self._cache.get(cache_key)
        if cached is not None:
            return cached

        client = await self._ensure_client()
        resp = await client.get(path, params=params)
        if resp.status_code != 200:
            raise ClawHubAPIError(resp.status_code, resp.text[:500])

        data = resp.json()
        self._cache.put(cache_key, data)
        return data

    # -- Public API ---------------------------------------------------------

    async def search(self, query: str, limit: int = 20) -> list[dict[str, Any]]:
        """Search ClawHub for skills matching *query*."""
        data = await self._get_json("/api/v1/search", {"q": query, "limit": limit})
        return data if isinstance(data, list) else data.get("results", [])

    async def list_remote(
        self, limit: int = 20, sort: str = "popular",
    ) -> list[dict[str, Any]]:
        """List skills from the ClawHub registry."""
        data = await self._get_json("/api/v1/skills", {"limit": limit, "sort": sort})
        return data if isinstance(data, list) else data.get("skills", [])

    async def skill_detail(self, slug: str) -> dict[str, Any]:
        """Fetch detail for a specific skill by *slug*."""
        return await self._get_json(f"/api/v1/skills/{slug}")

    async def skill_versions(self, slug: str) -> list[dict[str, Any]]:
        """Fetch the version history for *slug*."""
        data = await self._get_json(f"/api/v1/skills/{slug}/versions")
        return data if isinstance(data, list) else data.get("versions", [])

    async def skill_file(
        self, slug: str, path: str = "SKILL.md", version: str | None = None,
    ) -> str:
        """Fetch raw file content from a skill.

        Raises :class:`ClawHubAPIError` if the response exceeds 200 KB.
        """
        params: dict[str, str] = {"path": path}
        if version:
            params["version"] = version

        if not self._rate_limiter.acquire():
            raise ClawHubAPIError(429, "Rate limit exceeded")

        client = await self._ensure_client()
        resp = await client.get(f"/api/v1/skills/{slug}/file", params=params)
        if resp.status_code != 200:
            raise ClawHubAPIError(resp.status_code, resp.text[:500])

        if len(resp.content) > _MAX_FILE_BYTES:
            raise ClawHubAPIError(413, f"File exceeds {_MAX_FILE_BYTES} byte limit")

        return resp.text

    async def resolve_version(self, slug: str) -> dict[str, Any]:
        """Resolve the latest version for *slug*."""
        return await self._get_json("/api/v1/resolve", {"slug": slug})

    async def download(
        self, slug: str, version: str | None = None,
    ) -> DownloadResult:
        """Download a skill ZIP archive with streaming + SHA-256.

        Raises :class:`ClawHubAPIError` if the archive exceeds 10 MB.
        """
        if not self._rate_limiter.acquire():
            raise ClawHubAPIError(429, "Rate limit exceeded")

        params: dict[str, str] = {"slug": slug}
        if version:
            params["version"] = version

        client = await self._ensure_client()
        hasher = hashlib.sha256()
        chunks: list[bytes] = []
        total_size = 0

        async with client.stream("GET", "/api/v1/download", params=params) as resp:
            if resp.status_code != 200:
                body = await resp.aread()
                raise ClawHubAPIError(resp.status_code, body.decode("utf-8", errors="replace")[:500])

            async for chunk in resp.aiter_bytes(chunk_size=8192):
                total_size += len(chunk)
                if total_size > _MAX_DOWNLOAD_BYTES:
                    raise ClawHubAPIError(
                        413,
                        f"Archive exceeds {_MAX_DOWNLOAD_BYTES // (1024 * 1024)} MB limit",
                    )
                hasher.update(chunk)
                chunks.append(chunk)

        data = b"".join(chunks)
        return DownloadResult(data=data, sha256=hasher.hexdigest(), size=total_size)
