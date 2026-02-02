"""JWT-based authentication for the meept web API.

Provides :class:`WebAuth` for token creation/verification and the
:func:`require_auth` FastAPI dependency for protecting routes.
"""

from __future__ import annotations

import logging
from datetime import datetime, timedelta, timezone
from typing import Annotated, Any

# ---------------------------------------------------------------------------
# Conditional import of PyJWT
# ---------------------------------------------------------------------------

try:
    import jwt  # PyJWT
except ImportError as _exc:
    raise ImportError(
        "The 'PyJWT' package is required for web authentication. "
        "Install it with:  pip install PyJWT"
    ) from _exc

# ---------------------------------------------------------------------------
# Conditional import of FastAPI (needed for Depends / Header)
# ---------------------------------------------------------------------------

try:
    from fastapi import Depends, Header, HTTPException, status
except ImportError as _exc:
    raise ImportError(
        "The 'fastapi' package is required for the web interface. "
        "Install it with:  pip install fastapi"
    ) from _exc

log = logging.getLogger(__name__)


class WebAuth:
    """Handles JWT creation and verification for the web API.

    Parameters
    ----------
    secret_key:
        The HMAC secret used to sign tokens.  Must be kept private.
    algorithm:
        The JWT signing algorithm (default ``HS256``).
    """

    def __init__(self, secret_key: str, algorithm: str = "HS256") -> None:
        if not secret_key:
            raise ValueError(
                "A non-empty secret_key is required for web authentication. "
                "Set [web] secret_key in meept.toml."
            )
        self._secret_key = secret_key
        self._algorithm = algorithm

    # ------------------------------------------------------------------
    # Token lifecycle
    # ------------------------------------------------------------------

    def create_token(
        self, user_id: str, *, expires_hours: int = 24
    ) -> str:
        """Create a signed JWT for *user_id*.

        Parameters
        ----------
        user_id:
            The subject claim (``sub``) stored in the token.
        expires_hours:
            Number of hours until the token expires.

        Returns
        -------
        str
            The encoded JWT string.
        """
        now = datetime.now(timezone.utc)
        payload: dict[str, Any] = {
            "sub": user_id,
            "iat": now,
            "exp": now + timedelta(hours=expires_hours),
        }
        return jwt.encode(payload, self._secret_key, algorithm=self._algorithm)

    def verify_token(self, token: str) -> dict[str, Any] | None:
        """Decode and verify *token*.

        Returns
        -------
        dict | None
            The decoded payload on success, or ``None`` if the token is
            invalid or expired.
        """
        try:
            payload: dict[str, Any] = jwt.decode(
                token,
                self._secret_key,
                algorithms=[self._algorithm],
            )
            return payload
        except jwt.ExpiredSignatureError:
            log.debug("auth: token expired")
            return None
        except jwt.InvalidTokenError as exc:
            log.debug("auth: invalid token -- %s", exc)
            return None

    def get_current_user(self, token: str) -> str:
        """Extract the user id from a verified token.

        Parameters
        ----------
        token:
            The raw JWT string.

        Returns
        -------
        str
            The ``sub`` claim (user id).

        Raises
        ------
        HTTPException
            If the token is missing, expired, or invalid.
        """
        payload = self.verify_token(token)
        if payload is None:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Invalid or expired token",
                headers={"WWW-Authenticate": "Bearer"},
            )
        user_id = payload.get("sub")
        if not user_id or not isinstance(user_id, str):
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Token payload missing subject",
                headers={"WWW-Authenticate": "Bearer"},
            )
        return user_id


# ---------------------------------------------------------------------------
# Singleton holder -- set by create_app() at startup
# ---------------------------------------------------------------------------

_auth_instance: WebAuth | None = None


def set_auth(auth: WebAuth) -> None:
    """Store the global :class:`WebAuth` instance for dependency injection."""
    global _auth_instance
    _auth_instance = auth


def get_auth() -> WebAuth:
    """Retrieve the global :class:`WebAuth` instance.

    Raises
    ------
    RuntimeError
        If :func:`set_auth` has not been called yet.
    """
    if _auth_instance is None:
        raise RuntimeError("WebAuth has not been initialised -- call set_auth() first")
    return _auth_instance


# ---------------------------------------------------------------------------
# FastAPI dependency
# ---------------------------------------------------------------------------


def _extract_bearer_token(
    authorization: Annotated[str | None, Header()] = None,
) -> str:
    """Parse the ``Authorization: Bearer <token>`` header.

    Raises
    ------
    HTTPException
        If the header is missing or malformed.
    """
    if authorization is None:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Missing Authorization header",
            headers={"WWW-Authenticate": "Bearer"},
        )

    scheme, _, token = authorization.partition(" ")
    if scheme.lower() != "bearer" or not token:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Authorization header must use Bearer scheme",
            headers={"WWW-Authenticate": "Bearer"},
        )
    return token


def require_auth(
    token: Annotated[str, Depends(_extract_bearer_token)],
) -> str:
    """FastAPI dependency that authenticates the request.

    Extracts the Bearer token from the ``Authorization`` header, verifies
    it via :class:`WebAuth`, and returns the authenticated user id.

    Usage::

        @router.get("/example")
        async def example(user_id: str = Depends(require_auth)):
            ...
    """
    auth = get_auth()
    return auth.get_current_user(token)
