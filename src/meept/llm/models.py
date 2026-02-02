"""Data models for the LLM client layer."""

from __future__ import annotations

import enum
from dataclasses import dataclass, field

from pydantic import BaseModel, Field


# ---------------------------------------------------------------------------
# Enums
# ---------------------------------------------------------------------------

class Role(str, enum.Enum):
    """Roles in a chat conversation."""

    SYSTEM = "system"
    USER = "user"
    ASSISTANT = "assistant"
    TOOL = "tool"


# ---------------------------------------------------------------------------
# Dataclasses for chat messages and responses
# ---------------------------------------------------------------------------

@dataclass(slots=True)
class ChatMessage:
    """A single message in a chat conversation."""

    role: Role
    content: str
    name: str | None = None
    tool_calls: list[ToolCall] | None = None
    tool_call_id: str | None = None

    def to_openai_dict(self) -> dict:
        """Serialise to the dict format expected by the OpenAI API."""
        msg: dict = {"role": self.role.value, "content": self.content}
        if self.name is not None:
            msg["name"] = self.name
        if self.tool_calls is not None:
            msg["tool_calls"] = [tc.to_openai_dict() for tc in self.tool_calls]
        if self.tool_call_id is not None:
            msg["tool_call_id"] = self.tool_call_id
        return msg


@dataclass(slots=True)
class TokenUsage:
    """Token usage counters returned by the API."""

    prompt_tokens: int
    completion_tokens: int
    total_tokens: int


@dataclass(slots=True)
class ToolCallFunction:
    """The function payload inside a tool call."""

    name: str
    arguments: str  # Raw JSON string


@dataclass(slots=True)
class ToolCall:
    """A tool/function call returned by the model."""

    id: str
    type: str = "function"
    function: ToolCallFunction = field(default_factory=lambda: ToolCallFunction(name="", arguments=""))

    def to_openai_dict(self) -> dict:
        """Serialise to the dict format expected by the OpenAI API."""
        return {
            "id": self.id,
            "type": self.type,
            "function": {
                "name": self.function.name,
                "arguments": self.function.arguments,
            },
        }


@dataclass(slots=True)
class LLMResponse:
    """Parsed response from the LLM API."""

    content: str | None
    tool_calls: list[ToolCall] | None
    usage: TokenUsage
    model: str
    finish_reason: str


# ---------------------------------------------------------------------------
# Pydantic config model (validated, serialisable)
# ---------------------------------------------------------------------------

class ModelConfig(BaseModel):
    """Configuration for a specific LLM model endpoint."""

    base_url: str
    model_id: str
    api_key: str = ""
    cost_per_million_input: float = Field(default=0.0, ge=0.0)
    cost_per_million_output: float = Field(default=0.0, ge=0.0)
    max_tokens: int = Field(default=4096, gt=0)
    temperature: float = Field(default=0.7, ge=0.0, le=2.0)
    context_limit: int = Field(default=128000, gt=0)
    capabilities: frozenset[str] = Field(default_factory=frozenset)
    provider_id: str = ""

    model_config = {"frozen": False}
