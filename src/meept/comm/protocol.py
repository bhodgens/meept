"""JSON-RPC 2.0 wire format for the meept communication layer.

Implements request/response serialisation, standard error codes, and
helpers for parsing raw bytes off the wire.
"""

from __future__ import annotations

import json
from dataclasses import asdict, dataclass, field
from typing import Any

# ---------------------------------------------------------------------------
# Standard JSON-RPC 2.0 error codes
# ---------------------------------------------------------------------------

PARSE_ERROR: int = -32700
INVALID_REQUEST: int = -32600
METHOD_NOT_FOUND: int = -32601
INVALID_PARAMS: int = -32602
INTERNAL_ERROR: int = -32603


# ---------------------------------------------------------------------------
# Data models
# ---------------------------------------------------------------------------


@dataclass(slots=True)
class JsonRpcError:
    """Structured error object embedded in a JSON-RPC response."""

    code: int
    message: str
    data: Any = None

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {"code": self.code, "message": self.message}
        if self.data is not None:
            d["data"] = self.data
        return d


@dataclass(slots=True)
class JsonRpcRequest:
    """A single JSON-RPC 2.0 request (or notification when *id* is ``None``)."""

    method: str
    params: dict[str, Any] | list[Any] | None = None
    id: str | int | None = None
    jsonrpc: str = field(default="2.0")

    # -- Serialisation -------------------------------------------------------

    def to_json(self) -> str:
        """Serialise to a compact JSON string."""
        payload: dict[str, Any] = {"jsonrpc": self.jsonrpc, "method": self.method}
        if self.params is not None:
            payload["params"] = self.params
        if self.id is not None:
            payload["id"] = self.id
        return json.dumps(payload, separators=(",", ":"))

    @classmethod
    def from_json(cls, data: str) -> JsonRpcRequest:
        """Deserialise from a JSON string.

        Raises
        ------
        ValueError
            If *data* is not valid JSON or is missing required fields.
        """
        try:
            obj = json.loads(data)
        except json.JSONDecodeError as exc:
            raise ValueError(f"Invalid JSON: {exc}") from exc

        if not isinstance(obj, dict):
            raise ValueError("JSON-RPC message must be a JSON object")

        method = obj.get("method")
        if not isinstance(method, str):
            raise ValueError("Missing or invalid 'method' field")

        return cls(
            jsonrpc=obj.get("jsonrpc", "2.0"),
            method=method,
            params=obj.get("params"),
            id=obj.get("id"),
        )


@dataclass(slots=True)
class JsonRpcResponse:
    """A JSON-RPC 2.0 response object."""

    result: Any = None
    error: JsonRpcError | None = None
    id: str | int | None = None
    jsonrpc: str = field(default="2.0")

    def to_json(self) -> str:
        """Serialise to a compact JSON string.

        Per the spec, exactly one of *result* or *error* is included.
        """
        payload: dict[str, Any] = {"jsonrpc": self.jsonrpc, "id": self.id}
        if self.error is not None:
            payload["error"] = self.error.to_dict()
        else:
            payload["result"] = self.result
        return json.dumps(payload, separators=(",", ":"))


# ---------------------------------------------------------------------------
# Wire helpers
# ---------------------------------------------------------------------------


def parse_message(data: bytes) -> JsonRpcRequest:
    """Parse raw bytes into a :class:`JsonRpcRequest`.

    Raises
    ------
    ValueError
        On any parse or validation failure.
    """
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise ValueError(f"Payload is not valid UTF-8: {exc}") from exc
    return JsonRpcRequest.from_json(text)


def format_response(result: Any, request_id: str | int | None) -> bytes:
    """Build a success response and return it as UTF-8 bytes."""
    resp = JsonRpcResponse(result=result, id=request_id)
    return resp.to_json().encode("utf-8")


def format_error(
    code: int,
    message: str,
    request_id: str | int | None = None,
    data: Any = None,
) -> bytes:
    """Build an error response and return it as UTF-8 bytes."""
    err = JsonRpcError(code=code, message=message, data=data)
    resp = JsonRpcResponse(error=err, id=request_id)
    return resp.to_json().encode("utf-8")
