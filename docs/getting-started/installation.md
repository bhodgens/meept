# Installation

Meept is built from source. You need Go 1.22+ and an LLM provider.

## Prerequisites

- **Go 1.22+** — [Install Go](https://go.dev/doc/install)
- **An LLM provider** — at least one of:
  - [Ollama](https://ollama.ai) (local, free, no API key needed)
  - OpenAI, Anthropic, or any OpenAI-compatible API (requires API key)

## Build from Source

```bash
# Clone the repository
git clone https://github.com/caimlas/meept.git
cd meept

# Build both daemon and CLI
make build

# Or build individually
go build -o bin/meept-daemon ./cmd/meept-daemon
go build -o bin/meept ./cmd/meept
```

Binaries are placed in `bin/`:

| Binary | Description |
|--------|-------------|
| `bin/meept-daemon` | The background agent daemon |
| `bin/meept` | The CLI client |

## Initial Setup

```bash
# Create config directory and copy defaults
make setup

# Copy the models configuration
cp config/models.json5 ~/.meept/models.json5
```

### Configure Your LLM Provider

Edit `~/.meept/models.json5` to add your API keys. For a local Ollama setup, no API key is needed:

```json5
{
  "model": "ollama/llama3.2",
  "small_model": "ollama/llama3.2",
  "providers": {
    "ollama": {
      "api": "openai",
      "options": {
        "baseURL": "http://localhost:11434/v1"
      },
      "models": {
        "llama3.2": {
          "capabilities": ["code", "tool_use", "reasoning"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 128000
        }
      }
    }
  }
}
```

For a cloud provider like OpenRouter:

```json5
{
  "model": "openrouter/claude-sonnet",
  "providers": {
    "openrouter": {
      "api": "openai",
      "options": {
        "baseURL": "https://openrouter.ai/api/v1",
        "apiKey": "${OPENROUTER_API_KEY}"
      },
      "models": {
        "claude-sonnet": {
          "name": "anthropic/claude-3-sonnet",
          "capabilities": ["code", "reasoning", "tool_use"],
          "input_cost": 3.0,
          "output_cost": 15.0,
          "context_limit": 200000
        }
      }
    }
  }
}
```

## Verify Installation

```bash
# Check that binaries exist
ls -l bin/meept bin/meept-daemon

# Check Go version
go version
```

## Next Steps

Continue to [Quick Start](quick-start.md) for your first agent session.
