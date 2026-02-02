"""FastAPI application factory for the meept web interface.

Provides :func:`create_app` which assembles the FastAPI application with
CORS middleware, authentication, and all API routes.
"""

from __future__ import annotations

import logging
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from meept.core.bus import MessageBus
    from meept.core.config import MeeptConfig

# ---------------------------------------------------------------------------
# Conditional import of FastAPI
# ---------------------------------------------------------------------------

try:
    from fastapi import FastAPI
    from fastapi.middleware.cors import CORSMiddleware
except ImportError as _exc:
    raise ImportError(
        "The 'fastapi' package is required for the web interface. "
        "Install it with:  pip install 'fastapi[standard]'"
    ) from _exc

from .auth import WebAuth, set_auth
from .routes import create_router

log = logging.getLogger(__name__)

# Default CORS origins when none are configured.
_DEFAULT_CORS_ORIGINS: list[str] = [
    "http://localhost:3000",
    "http://localhost:5173",
    "http://127.0.0.1:3000",
    "http://127.0.0.1:5173",
]


def create_app(bus: MessageBus, config: MeeptConfig) -> FastAPI:
    """Build and return a fully-configured :class:`FastAPI` application.

    Parameters
    ----------
    bus:
        The shared :class:`MessageBus` for inter-component communication.
    config:
        The loaded :class:`MeeptConfig` instance.

    Returns
    -------
    FastAPI
        The application, ready to be served by an ASGI server (e.g.
        ``uvicorn``).
    """
    web_cfg = config.settings.web

    # ------------------------------------------------------------------
    # Authentication
    # ------------------------------------------------------------------

    auth = WebAuth(secret_key=web_cfg.secret_key)
    set_auth(auth)

    # ------------------------------------------------------------------
    # Lifespan (startup / shutdown)
    # ------------------------------------------------------------------

    @asynccontextmanager
    async def lifespan(app: FastAPI) -> AsyncIterator[None]:
        """Execute startup and shutdown logic for the application."""
        log.info(
            "web: application starting on %s:%d", web_cfg.host, web_cfg.port
        )
        yield
        log.info("web: application shutting down")

    # ------------------------------------------------------------------
    # Application
    # ------------------------------------------------------------------

    app = FastAPI(
        title="meept",
        summary="Self-executing autonomous bot API",
        version="0.1.0",
        lifespan=lifespan,
        docs_url="/docs",
        redoc_url="/redoc",
    )

    # ------------------------------------------------------------------
    # CORS middleware
    # ------------------------------------------------------------------

    cors_origins = _DEFAULT_CORS_ORIGINS

    app.add_middleware(
        CORSMiddleware,
        allow_origins=cors_origins,
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    # ------------------------------------------------------------------
    # Routes
    # ------------------------------------------------------------------

    router = create_router(bus, config)
    app.include_router(router)

    return app
