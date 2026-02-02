"""Tests for the JSON-RPC 2.0 protocol layer."""

from __future__ import annotations

import json

import pytest

from meept.comm.protocol import (
    INTERNAL_ERROR,
    PARSE_ERROR,
    JsonRpcError,
    JsonRpcRequest,
    JsonRpcResponse,
    format_error,
    format_response,
    parse_message,
)


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_request_serialization() -> None:
    """A JsonRpcRequest should round-trip through to_json() / from_json()."""
    req = JsonRpcRequest(
        method="memory.search",
        params={"query": "hello", "limit": 5},
        id="req-001",
    )

    json_str = req.to_json()
    parsed = json.loads(json_str)

    assert parsed["jsonrpc"] == "2.0"
    assert parsed["method"] == "memory.search"
    assert parsed["params"] == {"query": "hello", "limit": 5}
    assert parsed["id"] == "req-001"

    # Deserialise back.
    reconstructed = JsonRpcRequest.from_json(json_str)
    assert reconstructed.method == "memory.search"
    assert reconstructed.params == {"query": "hello", "limit": 5}
    assert reconstructed.id == "req-001"


def test_response_serialization() -> None:
    """A success JsonRpcResponse should serialise with 'result' and no 'error'."""
    resp = JsonRpcResponse(result={"status": "ok"}, id="req-001")
    json_str = resp.to_json()
    parsed = json.loads(json_str)

    assert parsed["jsonrpc"] == "2.0"
    assert parsed["id"] == "req-001"
    assert parsed["result"] == {"status": "ok"}
    assert "error" not in parsed


def test_error_response() -> None:
    """An error response should include the error object with code and message."""
    err = JsonRpcError(code=INTERNAL_ERROR, message="Something went wrong", data={"detail": "oops"})
    resp = JsonRpcResponse(error=err, id="req-002")
    json_str = resp.to_json()
    parsed = json.loads(json_str)

    assert parsed["jsonrpc"] == "2.0"
    assert parsed["id"] == "req-002"
    assert "result" not in parsed
    assert parsed["error"]["code"] == INTERNAL_ERROR
    assert parsed["error"]["message"] == "Something went wrong"
    assert parsed["error"]["data"] == {"detail": "oops"}

    # Also test the format_error() convenience helper.
    raw_bytes = format_error(PARSE_ERROR, "Parse error", request_id=None)
    parsed2 = json.loads(raw_bytes.decode("utf-8"))
    assert parsed2["error"]["code"] == PARSE_ERROR
    assert parsed2["id"] is None


def test_parse_invalid() -> None:
    """Malformed input should raise ValueError."""
    # Not valid JSON.
    with pytest.raises(ValueError, match="Invalid JSON"):
        JsonRpcRequest.from_json("{not json}")

    # Valid JSON but not an object.
    with pytest.raises(ValueError, match="JSON object"):
        JsonRpcRequest.from_json('"just a string"')

    # Missing 'method' field.
    with pytest.raises(ValueError, match="method"):
        JsonRpcRequest.from_json('{"jsonrpc":"2.0","id":1}')

    # parse_message with bad bytes.
    with pytest.raises(ValueError, match="UTF-8"):
        parse_message(b"\xff\xfe")
