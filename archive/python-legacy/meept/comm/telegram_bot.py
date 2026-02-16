"""Telegram bot interface for meept (creator-only).

Provides :class:`TelegramBot` which wraps python-telegram-bot to expose a
single-user conversational interface restricted to the configured creator.
"""

from __future__ import annotations

import asyncio
import logging
from typing import TYPE_CHECKING, Any

from meept.models.messages import BusMessage, MessageType

if TYPE_CHECKING:
    from meept.core.bus import MessageBus
    from meept.models.config_schema import TelegramConfig

# ---------------------------------------------------------------------------
# Conditional import of python-telegram-bot
# ---------------------------------------------------------------------------

try:
    from telegram import Update
    from telegram.ext import (
        Application,
        CommandHandler,
        ContextTypes,
        MessageHandler,
        filters,
    )
except ImportError as _exc:
    raise ImportError(
        "The 'python-telegram-bot' package is required for Telegram support. "
        "Install it with:  pip install 'python-telegram-bot>=20'"
    ) from _exc

log = logging.getLogger(__name__)

# Timeout (seconds) when waiting for the agent to respond via the bus.
_RESPONSE_TIMEOUT: float = 120.0


class TelegramBot:
    """Single-user Telegram bot that bridges messages to the meept bus.

    Only messages originating from ``config.creator_id`` are accepted;
    everything else is silently ignored.

    Parameters
    ----------
    config:
        The ``[telegram]`` section from ``meept.toml``.
    bus:
        The shared :class:`MessageBus` instance.
    """

    def __init__(self, config: TelegramConfig, bus: MessageBus) -> None:
        self._config = config
        self._bus = bus
        self._app: Application | None = None  # type: ignore[type-arg]

        # In-flight requests: message-id -> asyncio.Future[str]
        self._pending: dict[str, asyncio.Future[str]] = {}

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    async def start(self) -> None:
        """Build the Application, register handlers, and start polling."""
        if not self._config.token:
            log.warning("telegram: no token configured -- bot will not start")
            return

        builder = Application.builder().token(self._config.token)
        self._app = builder.build()

        # Register command handlers.
        self._app.add_handler(CommandHandler("start", self.handle_start))
        self._app.add_handler(CommandHandler("status", self.handle_status))

        # Catch-all for regular text messages.
        self._app.add_handler(
            MessageHandler(filters.TEXT & ~filters.COMMAND, self.handle_message)
        )

        # Subscribe to chat responses coming back through the bus.
        self._bus.subscribe("chat.response", self._on_chat_response)

        await self._app.initialize()
        await self._app.start()
        await self._app.updater.start_polling(drop_pending_updates=True)  # type: ignore[union-attr]

        log.info(
            "telegram: bot started (creator_id=%d)", self._config.creator_id
        )

    async def stop(self) -> None:
        """Gracefully shut down the Telegram bot."""
        if self._app is None:
            return

        self._bus.unsubscribe("chat.response", self._on_chat_response)

        # Cancel any pending response futures.
        for fut in self._pending.values():
            if not fut.done():
                fut.cancel()
        self._pending.clear()

        if self._app.updater and self._app.updater.running:
            await self._app.updater.stop()
        await self._app.stop()
        await self._app.shutdown()
        self._app = None

        log.info("telegram: bot stopped")

    # ------------------------------------------------------------------
    # Authentication
    # ------------------------------------------------------------------

    def _is_creator(self, update: Update) -> bool:
        """Return ``True`` if the message sender is the configured creator."""
        if update.effective_user is None:
            return False
        return update.effective_user.id == self._config.creator_id

    # ------------------------------------------------------------------
    # Handlers
    # ------------------------------------------------------------------

    async def handle_start(
        self, update: Update, context: ContextTypes.DEFAULT_TYPE
    ) -> None:
        """Handle the ``/start`` command -- greet the creator."""
        if not self._is_creator(update):
            log.debug(
                "telegram: /start from unauthorized user %s",
                update.effective_user.id if update.effective_user else "?",
            )
            return

        assert update.message is not None
        await update.message.reply_text(
            "Hello! I'm meept, your autonomous assistant. "
            "Send me a message and I'll get to work."
        )

    async def handle_status(
        self, update: Update, context: ContextTypes.DEFAULT_TYPE
    ) -> None:
        """Handle the ``/status`` command -- return daemon status info."""
        if not self._is_creator(update):
            return

        assert update.message is not None

        # Publish a status request via the bus and collect the response.
        request_msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"request": True},
            source="telegram",
        )

        future: asyncio.Future[str] = asyncio.get_running_loop().create_future()
        self._pending[request_msg.id] = future

        await self._bus.publish("status.request", request_msg)

        try:
            response_text = await asyncio.wait_for(future, timeout=15.0)
        except asyncio.TimeoutError:
            response_text = (
                "Status: running\n"
                "(Detailed status not available -- agent did not respond in time.)"
            )
        finally:
            self._pending.pop(request_msg.id, None)

        await update.message.reply_text(response_text)

    async def handle_message(
        self, update: Update, context: ContextTypes.DEFAULT_TYPE
    ) -> None:
        """Handle incoming text messages from the creator.

        Publishes the message to the bus as a ``CHAT_REQUEST``, waits for
        the agent to produce a ``CHAT_RESPONSE``, then replies on Telegram.
        """
        if not self._is_creator(update):
            log.debug(
                "telegram: message from unauthorized user %s",
                update.effective_user.id if update.effective_user else "?",
            )
            return

        assert update.message is not None
        text = update.message.text or ""
        if not text.strip():
            return

        # Build a bus message and register a future for the reply.
        request_msg = BusMessage(
            type=MessageType.CHAT_REQUEST,
            payload={
                "text": text,
                "source_channel": "telegram",
                "user_id": str(update.effective_user.id),  # type: ignore[union-attr]
            },
            source="telegram",
        )

        future: asyncio.Future[str] = asyncio.get_running_loop().create_future()
        self._pending[request_msg.id] = future

        await self._bus.publish("chat.request", request_msg)

        try:
            response_text = await asyncio.wait_for(future, timeout=_RESPONSE_TIMEOUT)
        except asyncio.TimeoutError:
            response_text = (
                "I'm sorry, I wasn't able to generate a response in time. "
                "Please try again."
            )
        finally:
            self._pending.pop(request_msg.id, None)

        # Telegram has a 4096-char limit per message; split if needed.
        for chunk in _split_message(response_text):
            await update.message.reply_text(chunk)

    # ------------------------------------------------------------------
    # Bus callback
    # ------------------------------------------------------------------

    async def _on_chat_response(self, _topic: str, msg: BusMessage) -> None:
        """Resolve the matching pending future when a response arrives."""
        reply_to = msg.reply_to
        if reply_to is None:
            return

        future = self._pending.get(reply_to)
        if future is None or future.done():
            return

        text = msg.payload.get("text", "")
        future.set_result(text)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

_TELEGRAM_MAX_LENGTH = 4096


def _split_message(text: str, max_length: int = _TELEGRAM_MAX_LENGTH) -> list[str]:
    """Split *text* into chunks that fit within Telegram's message limit."""
    if len(text) <= max_length:
        return [text]

    chunks: list[str] = []
    while text:
        if len(text) <= max_length:
            chunks.append(text)
            break

        # Try to split on a newline boundary for readability.
        split_at = text.rfind("\n", 0, max_length)
        if split_at == -1:
            # Fall back to splitting on a space.
            split_at = text.rfind(" ", 0, max_length)
        if split_at == -1:
            # No good break point -- hard split.
            split_at = max_length

        chunks.append(text[:split_at])
        text = text[split_at:].lstrip("\n")

    return chunks
