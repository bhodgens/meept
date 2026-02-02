"""Shared fixtures for ClawSkills tests."""

from __future__ import annotations

import io
import json
import zipfile
from pathlib import Path

import httpx
import pytest


# ---------------------------------------------------------------------------
# Sample SKILL.md content
# ---------------------------------------------------------------------------

SAMPLE_SKILL_MD = """\
---
name: "test-skill"
description: "A test skill for unit testing"
requires: ["code"]
allowed-tools: ["file_read", "shell_execute"]
risk-level: "low"
max-iterations: 20
---
# Test Skill

This is a test skill for ClawSkills unit tests.
"""

SAMPLE_SKILL_MD_MINIMAL = """\
---
name: "minimal"
description: "Minimal skill"
---
# Minimal

Does nothing.
"""


# ---------------------------------------------------------------------------
# ZIP archive helpers
# ---------------------------------------------------------------------------

def make_skill_zip(
    slug: str = "test-skill",
    skill_md: str = SAMPLE_SKILL_MD,
    extra_files: dict[str, str] | None = None,
) -> bytes:
    """Create an in-memory ZIP archive with a SKILL.md and optional extra files.

    Files are nested under a top-level directory named after the slug,
    matching the common convention for ClawHub downloads.
    """
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as zf:
        zf.writestr(f"{slug}/SKILL.md", skill_md)
        if extra_files:
            for name, content in extra_files.items():
                zf.writestr(f"{slug}/{name}", content)
    return buf.getvalue()


def make_bad_zip_path_traversal() -> bytes:
    """Create a ZIP with path traversal attack."""
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        zf.writestr("../../etc/passwd", "root:x:0:0:::/bin/bash")
    return buf.getvalue()


def make_bad_zip_forbidden_file() -> bytes:
    """Create a ZIP containing a forbidden .env file."""
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        zf.writestr("skill/.env", "SECRET=bad")
        zf.writestr("skill/SKILL.md", SAMPLE_SKILL_MD)
    return buf.getvalue()


def make_bad_zip_executable() -> bytes:
    """Create a ZIP with executable permissions."""
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        info = zipfile.ZipInfo("skill/run.md")
        # Set executable bit in Unix external attributes.
        info.external_attr = 0o755 << 16
        zf.writestr(info, "# run")
    return buf.getvalue()


def make_bad_zip_bad_extension() -> bytes:
    """Create a ZIP with a disallowed file extension."""
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        zf.writestr("skill/SKILL.md", SAMPLE_SKILL_MD)
        zf.writestr("skill/payload.py", "import os; os.system('rm -rf /')")
    return buf.getvalue()


# ---------------------------------------------------------------------------
# Temp install directory
# ---------------------------------------------------------------------------


@pytest.fixture()
def install_dir(tmp_path: Path) -> Path:
    """Return a temporary directory for clawskill installations."""
    d = tmp_path / "clawskills"
    d.mkdir()
    return d


# ---------------------------------------------------------------------------
# Mock HTTP transport
# ---------------------------------------------------------------------------


def _make_search_response() -> list[dict]:
    return [
        {"slug": "gifgrep", "description": "Search GIFs by text"},
        {"slug": "code-review", "description": "Automated code review"},
    ]


def _make_skill_detail() -> dict:
    return {
        "slug": "gifgrep",
        "description": "Search GIFs by text",
        "author": "testuser",
        "version": "1.2.0",
        "downloads": 1234,
    }


def _make_versions_response() -> list[dict]:
    return [
        {"version": "1.2.0", "created_at": "2025-01-15T00:00:00Z"},
        {"version": "1.1.0", "created_at": "2025-01-01T00:00:00Z"},
    ]


def _make_resolve_response() -> dict:
    return {"slug": "gifgrep", "version": "1.2.0"}


class MockTransport(httpx.AsyncBaseTransport):
    """Mock transport that returns canned responses for ClawHub API endpoints."""

    def __init__(self, zip_data: bytes | None = None) -> None:
        self.zip_data = zip_data or make_skill_zip("gifgrep")
        self.requests: list[httpx.Request] = []

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        path = request.url.path

        if path == "/api/v1/search":
            return httpx.Response(200, json=_make_search_response())

        if path == "/api/v1/skills" and not path.rstrip("/").split("/")[-1].isalpha():
            return httpx.Response(200, json={"skills": _make_search_response()})

        if path.startswith("/api/v1/skills/") and path.endswith("/versions"):
            return httpx.Response(200, json=_make_versions_response())

        if path.startswith("/api/v1/skills/") and "/file" in path:
            return httpx.Response(200, text=SAMPLE_SKILL_MD)

        if path.startswith("/api/v1/skills/"):
            return httpx.Response(200, json=_make_skill_detail())

        if path == "/api/v1/resolve":
            return httpx.Response(200, json=_make_resolve_response())

        if path == "/api/v1/download":
            return httpx.Response(200, content=self.zip_data)

        return httpx.Response(404, json={"error": "not found"})


@pytest.fixture()
def mock_transport() -> MockTransport:
    return MockTransport()
