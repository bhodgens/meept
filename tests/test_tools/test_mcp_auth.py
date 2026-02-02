"""Tests for MCP OAuth token storage and provider factories."""

from __future__ import annotations

import json
import os
import stat

import pytest

from meept.tools.mcp_auth import FileTokenStorage

# ---------------------------------------------------------------------------
# FileTokenStorage
# ---------------------------------------------------------------------------


class TestFileTokenStorage:
    """Tests for the file-based token persistence layer."""

    @pytest.fixture()
    def storage(self, tmp_path):
        return FileTokenStorage("test-server", base_dir=tmp_path)

    @pytest.fixture()
    def storage_dir(self, tmp_path):
        return tmp_path / "test-server"

    # -- get_tokens / set_tokens -------------------------------------------

    async def test_get_tokens_returns_none_when_no_file(self, storage):
        result = await storage.get_tokens()
        assert result is None

    async def test_set_and_get_tokens_roundtrip(self, storage, storage_dir):
        """Tokens written via set_tokens can be read back via get_tokens."""
        try:
            from mcp.shared.auth import OAuthToken
        except ImportError:
            pytest.skip("mcp SDK not installed")

        token = OAuthToken(
            access_token="access-123",
            token_type="bearer",
            refresh_token="refresh-456",
            expires_in=3600,
        )

        await storage.set_tokens(token)
        assert (storage_dir / "tokens.json").exists()

        loaded = await storage.get_tokens()
        assert loaded is not None
        assert loaded.access_token == "access-123"
        assert loaded.refresh_token == "refresh-456"

    async def test_set_tokens_creates_directory(self, storage, storage_dir):
        """set_tokens creates the storage directory if missing."""
        try:
            from mcp.shared.auth import OAuthToken
        except ImportError:
            pytest.skip("mcp SDK not installed")

        assert not storage_dir.exists()

        token = OAuthToken(access_token="x", token_type="bearer")
        await storage.set_tokens(token)

        assert storage_dir.exists()
        assert (storage_dir / "tokens.json").exists()

    async def test_set_tokens_file_permissions(self, storage, storage_dir):
        """Token files are created with 0600 permissions."""
        try:
            from mcp.shared.auth import OAuthToken
        except ImportError:
            pytest.skip("mcp SDK not installed")

        token = OAuthToken(access_token="secret", token_type="bearer")
        await storage.set_tokens(token)

        path = storage_dir / "tokens.json"
        mode = stat.S_IMODE(os.stat(path).st_mode)
        assert mode == 0o600

    async def test_get_tokens_handles_corrupt_file(self, storage, storage_dir):
        """Corrupt token file returns None instead of raising."""
        storage_dir.mkdir(parents=True)
        (storage_dir / "tokens.json").write_text("not json!", encoding="utf-8")

        result = await storage.get_tokens()
        assert result is None

    # -- get_client_info / set_client_info ----------------------------------

    async def test_get_client_info_returns_none_when_no_file(self, storage):
        result = await storage.get_client_info()
        assert result is None

    async def test_set_and_get_client_info_roundtrip(self, storage, storage_dir):
        """Client info written via set_client_info can be read back."""
        try:
            from mcp.shared.auth import OAuthClientInformationFull
        except ImportError:
            pytest.skip("mcp SDK not installed")

        info = OAuthClientInformationFull(
            client_id="client-abc",
            client_secret="secret-xyz",
            redirect_uris=["http://localhost:8085/callback"],
        )

        await storage.set_client_info(info)
        assert (storage_dir / "client_info.json").exists()

        loaded = await storage.get_client_info()
        assert loaded is not None
        assert loaded.client_id == "client-abc"
        assert loaded.client_secret == "secret-xyz"

    async def test_get_client_info_handles_corrupt_file(self, storage, storage_dir):
        """Corrupt client info file returns None."""
        storage_dir.mkdir(parents=True)
        (storage_dir / "client_info.json").write_text("{bad", encoding="utf-8")

        result = await storage.get_client_info()
        assert result is None

    # -- Atomic write -------------------------------------------------------

    async def test_write_is_atomic(self, storage, storage_dir):
        """Writing tokens replaces the file atomically (no partial writes)."""
        try:
            from mcp.shared.auth import OAuthToken
        except ImportError:
            pytest.skip("mcp SDK not installed")

        # Write initial token.
        token1 = OAuthToken(access_token="first", token_type="bearer")
        await storage.set_tokens(token1)

        # Overwrite with second token.
        token2 = OAuthToken(access_token="second", token_type="bearer")
        await storage.set_tokens(token2)

        # File should contain only the second token.
        data = json.loads((storage_dir / "tokens.json").read_text(encoding="utf-8"))
        assert data["access_token"] == "second"


# ---------------------------------------------------------------------------
# OAuth provider factories
# ---------------------------------------------------------------------------


class TestBuildOauthProvider:
    """Tests for build_oauth_provider."""

    def test_raises_without_sdk(self):
        """build_oauth_provider raises RuntimeError without MCP SDK."""
        from meept.tools.mcp_auth import _MCP_AUTH_AVAILABLE

        if _MCP_AUTH_AVAILABLE:
            pytest.skip("MCP SDK is installed; can't test missing-SDK path")

        from meept.tools.mcp_auth import build_oauth_provider

        with pytest.raises(RuntimeError, match="OAuth requires the MCP SDK"):
            build_oauth_provider("test", "https://example.com/mcp", {})

    def test_creates_provider_with_sdk(self, tmp_path):
        """build_oauth_provider returns an OAuthClientProvider when SDK present."""
        try:
            from mcp.client.auth import OAuthClientProvider
        except ImportError:
            pytest.skip("mcp SDK not installed")

        from meept.tools.mcp_auth import build_oauth_provider

        provider = build_oauth_provider(
            server_name="test-srv",
            server_url="https://example.com/mcp",
            oauth_config={"client_name": "meept-test", "scope": "tools:read"},
            base_dir=tmp_path,
        )
        assert isinstance(provider, OAuthClientProvider)


class TestBuildClientCredentialsProvider:
    """Tests for build_client_credentials_provider."""

    def test_raises_without_sdk(self):
        """build_client_credentials_provider raises RuntimeError without MCP SDK."""
        from meept.tools.mcp_auth import _MCP_AUTH_AVAILABLE

        if _MCP_AUTH_AVAILABLE:
            pytest.skip("MCP SDK is installed; can't test missing-SDK path")

        from meept.tools.mcp_auth import build_client_credentials_provider

        with pytest.raises(RuntimeError, match="OAuth requires the MCP SDK"):
            build_client_credentials_provider(
                "test", "https://example.com/mcp", "id", "secret"
            )

    def test_creates_provider_with_sdk(self, tmp_path):
        """build_client_credentials_provider returns a provider when SDK present."""
        try:
            from mcp.client.auth import OAuthClientProvider
        except ImportError:
            pytest.skip("mcp SDK not installed")

        from meept.tools.mcp_auth import build_client_credentials_provider

        provider = build_client_credentials_provider(
            server_name="m2m-srv",
            server_url="https://example.com/mcp",
            client_id="my-client",
            client_secret="my-secret",
            scope="tools:read",
            base_dir=tmp_path,
        )
        assert isinstance(provider, OAuthClientProvider)
