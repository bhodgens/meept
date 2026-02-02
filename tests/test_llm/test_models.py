"""Tests for LLM data models."""

from __future__ import annotations

import json

from meept.llm.models import (
    ChatMessage,
    LLMResponse,
    ModelConfig,
    Role,
    TokenUsage,
    ToolCall,
    ToolCallFunction,
)


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_chat_message_creation() -> None:
    """ChatMessage should store role, content, and optional fields correctly."""
    msg = ChatMessage(role=Role.USER, content="Hello, meept!")

    assert msg.role is Role.USER
    assert msg.content == "Hello, meept!"
    assert msg.name is None
    assert msg.tool_calls is None
    assert msg.tool_call_id is None

    # Serialise to the OpenAI dict format.
    d = msg.to_openai_dict()
    assert d == {"role": "user", "content": "Hello, meept!"}


def test_llm_response_parsing() -> None:
    """LLMResponse should capture all fields returned by the API."""
    usage = TokenUsage(prompt_tokens=10, completion_tokens=20, total_tokens=30)
    resp = LLMResponse(
        content="Sure, I can help with that.",
        tool_calls=None,
        usage=usage,
        model="test-model",
        finish_reason="stop",
    )

    assert resp.content == "Sure, I can help with that."
    assert resp.tool_calls is None
    assert resp.usage.total_tokens == 30
    assert resp.model == "test-model"
    assert resp.finish_reason == "stop"


def test_tool_call_parsing() -> None:
    """ToolCall should serialise to the OpenAI function-calling format."""
    tc = ToolCall(
        id="call_abc123",
        type="function",
        function=ToolCallFunction(
            name="read_file",
            arguments=json.dumps({"path": "/tmp/test.txt"}),
        ),
    )

    d = tc.to_openai_dict()
    assert d["id"] == "call_abc123"
    assert d["type"] == "function"
    assert d["function"]["name"] == "read_file"
    assert json.loads(d["function"]["arguments"]) == {"path": "/tmp/test.txt"}

    # A ChatMessage with tool_calls should include them in to_openai_dict().
    msg = ChatMessage(
        role=Role.ASSISTANT,
        content="",
        tool_calls=[tc],
    )
    msg_dict = msg.to_openai_dict()
    assert len(msg_dict["tool_calls"]) == 1
    assert msg_dict["tool_calls"][0]["function"]["name"] == "read_file"


def test_model_config_defaults() -> None:
    """ModelConfig should have sensible defaults for optional fields."""
    cfg = ModelConfig(base_url="http://localhost:11434/v1", model_id="llama3.2")

    assert cfg.api_key == ""
    assert cfg.cost_per_million_input == 0.0
    assert cfg.cost_per_million_output == 0.0
    assert cfg.max_tokens == 4096
    assert cfg.temperature == 0.7
