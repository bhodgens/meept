"""OAuth 2.1 token storage and provider factories for remote MCP servers.

Implements the MCP SDK's ``TokenStorage`` protocol with file-based
persistence at ``~/.meept/mcp-auth/<server_name>/``.  Tokens and client
registration info are written atomically with 0600 permissions.

Requires the ``mcp`` SDK (``pip install 'meept[mcp]'``).
"""

from __future__ import annotations

import json
import logging
import os
import tempfile
import webbrowser
from pathlib import Path
from typing import Any

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Conditional MCP SDK imports
# ---------------------------------------------------------------------------

try:
    from mcp.client.auth import OAuthClientProvider
    from mcp.shared.auth import (
        OAuthClientInformationFull,
        OAuthClientMetadata,
        OAuthToken,
    )

    _MCP_AUTH_AVAILABLE = True
except ImportError:
    _MCP_AUTH_AVAILABLE = False

# ---------------------------------------------------------------------------
# File-based token storage
# ---------------------------------------------------------------------------

_AUTH_BASE_DIR = Path("~/.meept/mcp-auth").expanduser()


class FileTokenStorage:
    """Persist OAuth tokens and client info to disk.

    Layout::

        ~/.meept/mcp-auth/<server_name>/
            tokens.json
            client_info.json

    All writes are atomic (write-to-temp then rename) and files are
    created with ``0o600`` permissions.
    """

    def __init__(self, server_name: str, base_dir: Path | None = None) -> None:
        self._dir = (base_dir or _AUTH_BASE_DIR) / server_name
        self._tokens_path = self._dir / "tokens.json"
        self._client_info_path = self._dir / "client_info.json"

    # -- TokenStorage protocol -----------------------------------------------

    async def get_tokens(self) -> Any | None:
        """Load persisted ``OAuthToken``, or ``None`` if not stored."""
        if not _MCP_AUTH_AVAILABLE:
            return None
        data = self._read_json(self._tokens_path)
        if data is None:
            return None
        try:
            return OAuthToken.model_validate(data)
        except Exception:
            log.warning("Corrupt token file %s -- ignoring", self._tokens_path)
            return None

    async def set_tokens(self, tokens: Any) -> None:
        """Persist an ``OAuthToken`` to ``tokens.json``."""
        if not _MCP_AUTH_AVAILABLE:
            return
        self._write_json(self._tokens_path, tokens.model_dump(mode="json"))

    async def get_client_info(self) -> Any | None:
        """Load persisted ``OAuthClientInformationFull``, or ``None``."""
        if not _MCP_AUTH_AVAILABLE:
            return None
        data = self._read_json(self._client_info_path)
        if data is None:
            return None
        try:
            return OAuthClientInformationFull.model_validate(data)
        except Exception:
            log.warning("Corrupt client info file %s -- ignoring", self._client_info_path)
            return None

    async def set_client_info(self, info: Any) -> None:
        """Persist ``OAuthClientInformationFull`` to ``client_info.json``."""
        if not _MCP_AUTH_AVAILABLE:
            return
        self._write_json(self._client_info_path, info.model_dump(mode="json"))

    # -- Internal helpers ----------------------------------------------------

    def _read_json(self, path: Path) -> dict[str, Any] | None:
        if not path.exists():
            return None
        try:
            return json.loads(path.read_text(encoding="utf-8"))
        except (json.JSONDecodeError, OSError) as exc:
            log.warning("Failed to read %s: %s", path, exc)
            return None

    def _write_json(self, path: Path, data: dict[str, Any]) -> None:
        self._dir.mkdir(parents=True, exist_ok=True)
        # Atomic write: temp file in same directory then rename.
        fd, tmp_path = tempfile.mkstemp(dir=self._dir, suffix=".tmp")
        fd_closed = False
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as fp:
                fd_closed = True  # os.fdopen now owns the fd.
                json.dump(data, fp, indent=2)
            os.chmod(tmp_path, 0o600)
            os.replace(tmp_path, path)
        except Exception:
            # Close the fd if os.fdopen never took ownership.
            if not fd_closed:
                try:
                    os.close(fd)
                except OSError:
                    pass
            # Clean up temp file on failure.
            try:
                os.unlink(tmp_path)
            except OSError:
                pass
            raise


# ---------------------------------------------------------------------------
# OAuth provider factories
# ---------------------------------------------------------------------------


def _default_redirect_handler(url: str) -> None:
    """Open the OAuth authorisation URL in the user's browser."""
    log.info("Opening browser for OAuth authorisation: %s", url)
    webbrowser.open(url)


async def _default_callback_handler(redirect_uri: str) -> str:
    """Start a minimal localhost HTTP server to capture the OAuth redirect.

    Returns the full callback URL including query parameters.
    """
    import asyncio
    from http.server import BaseHTTPRequestHandler, HTTPServer
    from urllib.parse import urlparse

    parsed = urlparse(redirect_uri)
    port = parsed.port or 8085

    result: list[str] = []

    class _Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:  # noqa: N802
            result.append(f"http://localhost:{port}{self.path}")
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            self.wfile.write(
                b"<html><body><h1>Authorization complete</h1>"
                b"<p>You may close this window.</p></body></html>"
            )

        def log_message(self, format: str, *args: Any) -> None:  # noqa: A002
            pass  # Suppress default stderr logging.

    loop = asyncio.get_running_loop()
    server = HTTPServer(("127.0.0.1", port), _Handler)

    # Run the blocking handle_request in a thread so we don't block the event loop.
    await loop.run_in_executor(None, server.handle_request)
    server.server_close()

    if result:
        return result[0]
    raise RuntimeError("OAuth callback was not received")


def build_oauth_provider(
    server_name: str,
    server_url: str,
    oauth_config: dict[str, Any],
    *,
    base_dir: Path | None = None,
) -> Any:
    """Create an ``OAuthClientProvider`` from configuration.

    Parameters
    ----------
    server_name:
        Used to namespace the token storage directory.
    server_url:
        The MCP server URL (used by the SDK for discovery).
    oauth_config:
        Dict with optional keys: ``client_name``, ``scope``,
        ``redirect_uris``, ``client_id``, ``client_secret``.
    base_dir:
        Override the token storage base directory (for testing).

    Returns
    -------
    An ``OAuthClientProvider`` instance (subclasses ``httpx.Auth``).
    """
    if not _MCP_AUTH_AVAILABLE:
        raise RuntimeError(
            "OAuth requires the MCP SDK. Install with: pip install 'meept[mcp]'"
        )

    client_name = oauth_config.get("client_name", "meept")
    scope = oauth_config.get("scope")
    redirect_uris = oauth_config.get(
        "redirect_uris", ["http://localhost:8085/callback"]
    )

    metadata = OAuthClientMetadata(
        client_name=client_name,
        redirect_uris=redirect_uris,
        grant_types=["authorization_code", "refresh_token"],
        response_types=["code"],
        scope=scope,
    )

    storage = FileTokenStorage(server_name, base_dir=base_dir)

    return OAuthClientProvider(
        server_url=server_url,
        client_metadata=metadata,
        storage=storage,
        redirect_handler=_default_redirect_handler,
        callback_handler=_default_callback_handler,
    )


def build_client_credentials_provider(
    server_name: str,
    server_url: str,
    client_id: str,
    client_secret: str,
    *,
    scope: str | None = None,
    base_dir: Path | None = None,
) -> Any:
    """Create an ``OAuthClientProvider`` configured for client-credentials (M2M) flow.

    Parameters
    ----------
    server_name:
        Used to namespace the token storage directory.
    server_url:
        The MCP server URL.
    client_id:
        Pre-registered OAuth client ID.
    client_secret:
        OAuth client secret.
    scope:
        Optional scope string.
    base_dir:
        Override the token storage base directory (for testing).

    Returns
    -------
    An ``OAuthClientProvider`` configured for client credentials.
    """
    if not _MCP_AUTH_AVAILABLE:
        raise RuntimeError(
            "OAuth requires the MCP SDK. Install with: pip install 'meept[mcp]'"
        )

    metadata = OAuthClientMetadata(
        client_name="meept",
        redirect_uris=["http://localhost:8085/callback"],
        grant_types=["client_credentials"],
        response_types=["code"],
        scope=scope,
    )

    storage = FileTokenStorage(server_name, base_dir=base_dir)

    # Pre-populate client info so the provider skips dynamic registration.
    # FileTokenStorage.set_client_info is async but uses sync I/O internally,
    # so we write directly to disk to avoid async scheduling issues.
    client_info = OAuthClientInformationFull(
        client_id=client_id,
        client_secret=client_secret,
        redirect_uris=["http://localhost:8085/callback"],
    )
    storage._write_json(
        storage._client_info_path,
        client_info.model_dump(mode="json"),
    )

    return OAuthClientProvider(
        server_url=server_url,
        client_metadata=metadata,
        storage=storage,
        redirect_handler=_default_redirect_handler,
        callback_handler=_default_callback_handler,
    )
