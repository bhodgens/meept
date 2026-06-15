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

## Optional: Build the Flutter GUI

Meept includes a cross-platform GUI built with Flutter.

### Prerequisites

- [Flutter SDK 3.0+](https://docs.flutter.dev/get-started/install)
- Platform-specific tools:
  - **macOS**: Xcode command line tools
  - **Linux**: GTK development libraries (`libgtk-3-dev`)
  - **Windows**: Visual Studio Build Tools

### Build

```bash
cd ui/flutter_ui

# Install dependencies
flutter pub get

# Run in development mode
flutter run

# Or build for release
flutter build macos    # macOS
flutter build linux    # Linux  
flutter build windows  # Windows
```

The built app will be in:
- macOS: `build/macos/Build/Products/Release/flutter_ui.app`
- Linux: `build/linux/x64/release/bundle/`
- Windows: `build/windows/runner/Release/`

### API Key Setup

**Development:** The Flutter app automatically uses the default dev API key. No setup required.

**Production:** Generate a secure key:

```bash
./bin/meept token generate
```

Then configure in the app:
1. Open the Flutter app
2. Go to Settings (gear icon)
3. Enter the API key
4. Save

Or set in `~/.meept/menubar.json5`:
```json5
{
  "daemon": {
    "http_url": "https://localhost:8081",
    "api_token": "your-generated-key"
  }
}
```

See [Quick Start](quick-start.md#optional-using-the-flutter-gui) for more details.

