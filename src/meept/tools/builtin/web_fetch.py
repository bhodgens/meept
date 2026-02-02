"""URL fetching tool with HTML-to-text conversion."""

from __future__ import annotations

import logging
import re
from typing import Any

from meept.tools.interface import Tool, ToolDefinition, ToolParameter

log = logging.getLogger(__name__)

# Defaults
_DEFAULT_TIMEOUT = 10.0
_MAX_RESPONSE_SIZE = 100 * 1024  # 100 KB
_MAX_OUTPUT_LENGTH = 50_000  # characters after stripping

# Regex patterns for basic HTML tag removal.
_TAG_RE = re.compile(r"<[^>]+>")
_SCRIPT_STYLE_RE = re.compile(
    r"<(script|style)[^>]*>.*?</\1>",
    re.DOTALL | re.IGNORECASE,
)
_WHITESPACE_RE = re.compile(r"\n{3,}")
_HTML_ENTITY_MAP: dict[str, str] = {
    "&amp;": "&",
    "&lt;": "<",
    "&gt;": ">",
    "&quot;": '"',
    "&#39;": "'",
    "&apos;": "'",
    "&nbsp;": " ",
}


def _strip_html(html: str) -> str:
    """Convert HTML to plain text using regex-based tag removal.

    This is deliberately simple -- no dependency on beautifulsoup or
    lxml.  It handles the common cases well enough for LLM consumption.
    """
    # Remove script and style blocks entirely.
    text = _SCRIPT_STYLE_RE.sub("", html)
    # Replace block-level tags with newlines for readability.
    text = re.sub(r"<(br|p|div|h[1-6]|li|tr)[^>]*>", "\n", text, flags=re.IGNORECASE)
    # Strip remaining tags.
    text = _TAG_RE.sub("", text)
    # Decode common HTML entities.
    for entity, char in _HTML_ENTITY_MAP.items():
        text = text.replace(entity, char)
    # Collapse excessive whitespace.
    text = _WHITESPACE_RE.sub("\n\n", text)
    return text.strip()


class WebFetchTool(Tool):
    """Fetch the content of a URL and return it as plain text.

    Uses ``httpx`` for async HTTP requests.  HTML responses are
    stripped to plain text.
    """

    def __init__(
        self,
        timeout: float = _DEFAULT_TIMEOUT,
        max_length: int = _MAX_OUTPUT_LENGTH,
    ) -> None:
        self._timeout = timeout
        self._max_length = max_length

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="web_fetch",
            description=(
                "Fetch the content of a URL and return it as plain text. "
                "HTML is automatically stripped. Useful for reading web pages, "
                "API responses, and documentation."
            ),
            parameters=[
                ToolParameter(
                    name="url",
                    type="string",
                    description="The URL to fetch.",
                ),
                ToolParameter(
                    name="max_length",
                    type="integer",
                    description="Maximum characters to return (default 50000).",
                    required=False,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        url: str = kwargs.get("url", "")
        max_length: int = kwargs.get("max_length", self._max_length)

        if not url:
            return {"success": False, "result": None, "error": "No URL specified"}

        # Validate URL scheme.
        if not url.startswith(("http://", "https://")):
            return {
                "success": False,
                "result": None,
                "error": "Only http:// and https:// URLs are supported",
            }

        try:
            import httpx
        except ImportError:
            return {
                "success": False,
                "result": None,
                "error": "httpx is required for web fetching (pip install httpx)",
            }

        log.info("Fetching URL: %s", url)

        try:
            async with httpx.AsyncClient(
                timeout=self._timeout,
                follow_redirects=True,
                max_redirects=5,
            ) as client:
                response = await client.get(
                    url,
                    headers={
                        "User-Agent": "Meept/0.1 (autonomous assistant)",
                        "Accept": "text/html,application/xhtml+xml,text/plain,*/*",
                    },
                )
                response.raise_for_status()
        except httpx.TimeoutException:
            return {
                "success": False,
                "result": None,
                "error": f"Request timed out after {self._timeout}s",
            }
        except httpx.HTTPStatusError as exc:
            return {
                "success": False,
                "result": None,
                "error": f"HTTP {exc.response.status_code}: {exc.response.reason_phrase}",
            }
        except httpx.RequestError as exc:
            return {
                "success": False,
                "result": None,
                "error": f"Request failed: {exc}",
            }

        # Enforce max response size on raw bytes.
        raw_bytes = response.content
        if len(raw_bytes) > _MAX_RESPONSE_SIZE:
            raw_bytes = raw_bytes[:_MAX_RESPONSE_SIZE]

        content_type = response.headers.get("content-type", "")
        text = raw_bytes.decode("utf-8", errors="replace")

        # Strip HTML if the response looks like HTML.
        if "html" in content_type.lower() or text.lstrip().startswith("<!"):
            text = _strip_html(text)

        # Truncate to max length.
        truncated = False
        if len(text) > max_length:
            text = text[:max_length]
            truncated = True

        return {
            "success": True,
            "result": text,
            "url": str(response.url),
            "status_code": response.status_code,
            "content_type": content_type,
            "truncated": truncated,
            "error": None,
        }
