"""Async LLM client speaking the OpenAI chat completions API."""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any

import httpx

from meept.llm.budget import TokenBudget
from meept.llm.models import (
    ChatMessage,
    LLMResponse,
    ModelConfig,
    TokenUsage,
    ToolCall,
    ToolCallFunction,
)

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------

_DEFAULT_TIMEOUT = 120.0  # seconds
_MAX_RETRIES = 3
_RETRY_BACKOFF_BASE = 2.0  # seconds -- exponential: 2, 4, 8 ...

# HTTP status codes that warrant a retry
_RETRYABLE_STATUS_CODES = frozenset({429, 500, 502, 503, 504})


# ---------------------------------------------------------------------------
# Exceptions
# ---------------------------------------------------------------------------

class LLMClientError(Exception):
    """Base exception for LLM client errors."""


class LLMAPIError(LLMClientError):
    """Raised when the remote API returns an error response."""

    def __init__(self, status_code: int, detail: str) -> None:
        self.status_code = status_code
        self.detail = detail
        super().__init__(f"HTTP {status_code}: {detail}")


class LLMBudgetExceeded(LLMClientError):
    """Raised when a request would exceed the token budget."""


# ---------------------------------------------------------------------------
# Client
# ---------------------------------------------------------------------------

class LLMClient:
    """Async client for OpenAI-compatible ``/v1/chat/completions`` endpoints.

    Parameters
    ----------
    config:
        Model and endpoint configuration.
    budget:
        Optional token budget tracker.  When provided, every request is
        checked against the budget and usage is recorded automatically.
    """

    def __init__(
        self,
        config: ModelConfig,
        budget: TokenBudget | None = None,
    ) -> None:
        self._config = config
        self._budget = budget
        self._http = self._build_http_client(config)

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    @staticmethod
    def _build_http_client(config: ModelConfig) -> httpx.AsyncClient:
        headers: dict[str, str] = {
            "Content-Type": "application/json",
            "Accept": "application/json",
        }
        if config.api_key:
            headers["Authorization"] = f"Bearer {config.api_key}"
        return httpx.AsyncClient(
            base_url=config.base_url.rstrip("/"),
            headers=headers,
            timeout=httpx.Timeout(_DEFAULT_TIMEOUT),
        )

    @staticmethod
    def _messages_to_dicts(messages: list[ChatMessage]) -> list[dict[str, Any]]:
        return [m.to_openai_dict() for m in messages]

    @staticmethod
    def _parse_tool_calls(raw: list[dict[str, Any]]) -> list[ToolCall]:
        result: list[ToolCall] = []
        for tc in raw:
            fn = tc.get("function", {})
            result.append(
                ToolCall(
                    id=tc["id"],
                    type=tc.get("type", "function"),
                    function=ToolCallFunction(
                        name=fn.get("name", ""),
                        arguments=fn.get("arguments", "{}"),
                    ),
                )
            )
        return result

    @staticmethod
    def _parse_usage(raw: dict[str, Any]) -> TokenUsage:
        return TokenUsage(
            prompt_tokens=raw.get("prompt_tokens", 0),
            completion_tokens=raw.get("completion_tokens", 0),
            total_tokens=raw.get("total_tokens", 0),
        )

    def _parse_response(self, body: dict[str, Any]) -> LLMResponse:
        """Turn a raw JSON body into a structured ``LLMResponse``."""
        choice = body["choices"][0]
        message = choice["message"]

        content: str | None = message.get("content")
        raw_tool_calls = message.get("tool_calls")
        tool_calls: list[ToolCall] | None = None
        if raw_tool_calls:
            tool_calls = self._parse_tool_calls(raw_tool_calls)

        usage = self._parse_usage(body.get("usage", {}))

        return LLMResponse(
            content=content,
            tool_calls=tool_calls,
            usage=usage,
            model=body.get("model", self._config.model_id),
            finish_reason=choice.get("finish_reason", "unknown"),
        )

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    async def chat(
        self,
        messages: list[ChatMessage],
        tools: list[dict] | None = None,
        temperature: float | None = None,
        max_tokens: int | None = None,
    ) -> LLMResponse:
        """Send a chat completion request and return the parsed response.

        Parameters
        ----------
        messages:
            The conversation history.
        tools:
            Optional list of tool/function definitions in OpenAI format.
        temperature:
            Override the default temperature from ``ModelConfig``.
        max_tokens:
            Override the default max_tokens from ``ModelConfig``.

        Raises
        ------
        LLMBudgetExceeded
            If the token budget does not allow further requests.
        LLMAPIError
            If the API returns a non-retryable error.
        LLMClientError
            On unexpected transport or parsing failures after all retries.
        """
        # Budget gate
        if self._budget is not None:
            if not self._budget.check_budget():
                raise LLMBudgetExceeded("Token budget exceeded -- request blocked.")
            await self._budget.wait_for_rate_limit()

        payload: dict[str, Any] = {
            "model": self._config.model_id,
            "messages": self._messages_to_dicts(messages),
            "temperature": temperature if temperature is not None else self._config.temperature,
            "max_tokens": max_tokens if max_tokens is not None else self._config.max_tokens,
        }
        if tools:
            payload["tools"] = tools

        last_exc: BaseException | None = None

        for attempt in range(1, _MAX_RETRIES + 1):
            try:
                response = await self._http.post("/v1/chat/completions", json=payload)

                if response.status_code in _RETRYABLE_STATUS_CODES:
                    detail = response.text[:500]
                    logger.warning(
                        "Retryable HTTP %d on attempt %d/%d: %s",
                        response.status_code,
                        attempt,
                        _MAX_RETRIES,
                        detail,
                    )
                    last_exc = LLMAPIError(response.status_code, detail)
                    if attempt < _MAX_RETRIES:
                        await asyncio.sleep(_RETRY_BACKOFF_BASE ** attempt)
                    continue

                if response.status_code != 200:
                    raise LLMAPIError(response.status_code, response.text[:1000])

                body = response.json()
                llm_response = self._parse_response(body)

                # Record usage
                if self._budget is not None:
                    self._budget.record_usage(llm_response.usage)

                return llm_response

            except (httpx.TimeoutException, httpx.ConnectError) as exc:
                logger.warning(
                    "Transport error on attempt %d/%d: %s",
                    attempt,
                    _MAX_RETRIES,
                    exc,
                )
                last_exc = exc
                if attempt < _MAX_RETRIES:
                    await asyncio.sleep(_RETRY_BACKOFF_BASE ** attempt)

            except json.JSONDecodeError as exc:
                logger.error("Failed to decode JSON response: %s", exc)
                raise LLMClientError(f"Invalid JSON in API response: {exc}") from exc

            except LLMAPIError:
                raise

            except LLMClientError:
                raise

            except Exception as exc:
                logger.error("Unexpected error during LLM request: %s", exc)
                raise LLMClientError(f"Unexpected error: {exc}") from exc

        # All retries exhausted
        raise LLMClientError(
            f"All {_MAX_RETRIES} attempts failed. Last error: {last_exc}"
        )

    def switch_model(self, config: ModelConfig) -> None:
        """Switch to a different model / endpoint at runtime.

        The existing HTTP client is replaced with a new one configured for
        the new endpoint.  The previous client is scheduled for async
        close but is not awaited here -- callers who need a clean shutdown
        should call :meth:`close` on the old client first.
        """
        old_http = self._http
        self._config = config
        self._http = self._build_http_client(config)

        # Best-effort close of the old client (fire-and-forget)
        try:
            loop = asyncio.get_running_loop()
            loop.create_task(old_http.aclose())
        except RuntimeError:
            # No running loop -- caller is responsible for cleanup
            pass

        logger.info(
            "Switched model to %s @ %s",
            config.model_id,
            config.base_url,
        )

    async def close(self) -> None:
        """Gracefully close the underlying HTTP client."""
        await self._http.aclose()

    # ------------------------------------------------------------------
    # Context-manager support
    # ------------------------------------------------------------------

    async def __aenter__(self) -> LLMClient:
        return self

    async def __aexit__(self, *exc: object) -> None:
        await self.close()
