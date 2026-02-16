"""Google OAuth 2.0 authentication for the Calendar API."""

from __future__ import annotations

import asyncio
import json
import logging
from functools import partial
from pathlib import Path
from typing import Any

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Conditional imports for the Google client libraries.
# ---------------------------------------------------------------------------

_GOOGLE_INSTALL_HINT = (
    "Google Calendar integration requires the optional 'calendar' extras. "
    "Install them with:  pip install 'meept[calendar]'  "
    "(needs google-api-python-client and google-auth-oauthlib)"
)

try:
    from google.auth.transport.requests import Request as GoogleAuthRequest
    from google.oauth2.credentials import Credentials
    from google_auth_oauthlib.flow import InstalledAppFlow

    _HAS_GOOGLE = True
except ImportError:
    _HAS_GOOGLE = False
    Credentials = None  # type: ignore[assignment,misc]
    InstalledAppFlow = None  # type: ignore[assignment,misc]
    GoogleAuthRequest = None  # type: ignore[assignment,misc]

# The scope required to read/write the user's primary calendar.
_SCOPES = ["https://www.googleapis.com/auth/calendar"]


class CalendarAuth:
    """Manage Google OAuth 2.0 credentials for the Calendar API.

    Parameters
    ----------
    credentials_file:
        Path to the OAuth *client secret* JSON file downloaded from the
        Google Cloud console.
    token_file:
        Path where the refresh / access token JSON is persisted between
        runs.
    """

    def __init__(
        self,
        credentials_file: Path,
        token_file: Path,
    ) -> None:
        if not _HAS_GOOGLE:
            raise ImportError(_GOOGLE_INSTALL_HINT)

        self._credentials_file = Path(credentials_file).expanduser().resolve()
        self._token_file = Path(token_file).expanduser().resolve()
        self._credentials: Credentials | None = None

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def is_authorized(self) -> bool:
        """Return ``True`` if a valid (or refreshable) token is available."""
        creds = self._load_token()
        if creds is None:
            return False
        if creds.valid:
            return True
        if creds.expired and creds.refresh_token:
            # A refresh token exists -- we can transparently renew.
            return True
        return False

    async def get_credentials(self) -> Any:
        """Return a valid :class:`google.oauth2.credentials.Credentials`.

        If the stored access token has expired but a refresh token is
        available, the credentials are silently refreshed (blocking I/O is
        off-loaded to the default executor).

        Raises
        ------
        RuntimeError
            If no valid credentials are available and user authorisation is
            needed.
        """
        creds = self._load_token()

        if creds is not None and creds.valid:
            self._credentials = creds
            return creds

        if creds is not None and creds.expired and creds.refresh_token:
            loop = asyncio.get_running_loop()
            await loop.run_in_executor(None, partial(creds.refresh, GoogleAuthRequest()))
            self._persist_token(creds)
            self._credentials = creds
            log.debug("calendar auth: refreshed access token")
            return creds

        raise RuntimeError(
            "No valid Google Calendar credentials found. "
            "Run CalendarAuth.authorize() to start the OAuth consent flow."
        )

    async def authorize(self) -> Any:
        """Run the full OAuth 2.0 installed-app consent flow.

        This will open the user's default browser for consent and start a
        local HTTP server to receive the callback.  The resulting token is
        persisted to *token_file*.

        Returns the :class:`Credentials` object.
        """
        if not self._credentials_file.exists():
            raise FileNotFoundError(
                f"OAuth client-secret file not found: {self._credentials_file}. "
                "Download it from the Google Cloud Console "
                "(APIs & Services > Credentials > OAuth 2.0 Client IDs)."
            )

        loop = asyncio.get_running_loop()

        def _run_flow() -> Any:
            flow = InstalledAppFlow.from_client_secrets_file(
                str(self._credentials_file),
                scopes=_SCOPES,
            )
            creds = flow.run_local_server(port=0)
            return creds

        creds = await loop.run_in_executor(None, _run_flow)
        self._persist_token(creds)
        self._credentials = creds
        log.info("calendar auth: authorization successful -- token stored at %s", self._token_file)
        return creds

    # ------------------------------------------------------------------
    # Token persistence
    # ------------------------------------------------------------------

    def _load_token(self) -> Any | None:
        """Load credentials from *token_file*, returning ``None`` on failure."""
        if not self._token_file.exists():
            return None
        try:
            creds = Credentials.from_authorized_user_file(str(self._token_file), _SCOPES)
            return creds
        except Exception:
            log.warning("calendar auth: failed to load token from %s", self._token_file)
            return None

    def _persist_token(self, creds: Any) -> None:
        """Write *creds* to *token_file* as JSON."""
        self._token_file.parent.mkdir(parents=True, exist_ok=True)
        data = {
            "token": creds.token,
            "refresh_token": creds.refresh_token,
            "token_uri": creds.token_uri,
            "client_id": creds.client_id,
            "client_secret": creds.client_secret,
            "scopes": list(creds.scopes or _SCOPES),
        }
        self._token_file.write_text(json.dumps(data, indent=2), encoding="utf-8")
        log.debug("calendar auth: persisted token to %s", self._token_file)
