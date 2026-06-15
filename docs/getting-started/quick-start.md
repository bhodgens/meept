# Quick Start

Get Meept running and chatting in under 5 minutes.

## Step 1: Start the Daemon

```bash
# Start daemon in foreground (you'll see log output)
./bin/meept-daemon -f
```

Expected output:
```
INFO  starting meept daemon  version=dev
INFO  config loaded  path=~/.meept/meept.toml
INFO  rpc server listening  addr=~/.meept/meept.sock
INFO  agent registry initialized  agents=8
INFO  tool registry initialized  tools=17
INFO  memory system initialized
INFO  daemon ready
```

## Step 2: Open a New Terminal and Chat

```bash
# Start interactive TUI
./bin/meept chat
```

Or send a single message:

```bash
./bin/meept chat "What tools do you have available?"
```

## Step 3: Try Agent Routing

Ask something that requires a specialist:

```bash
./bin/meept chat "List all files in the current directory"
```

The dispatcher will route this to the `coder` agent, which has file system tools.

```bash
./bin/meept chat "Research the pros and cons of using SQLite vs PostgreSQL"
```

This gets routed to the `analyst` agent with web search capabilities.

## Step 4: Check Agent Activity

In the TUI, press `Ctrl+S` to toggle the sidebar. You'll see:
- Active agents and their status
- Worker pool activity
- Recent task completions
- Memory access logs

## Step 5: Try Scheduling

```bash
./bin/meept chat "Set a reminder to check build status in 5 minutes"
```

The scheduler agent creates a cron job that fires a reminder through the message bus.

## Common First Commands

| Command | What It Does |
|---------|-------------|
| `./bin/meept status` | Check daemon health and uptime |
| `./bin/meept chat` | Interactive TUI chat |
| `./bin/meept chat "hello"` | Single message |
| `./bin/meept memory search "topic"` | Search stored memories |
| `./bin/meept jobs list` | List scheduled jobs |
| `./bin/meept sessions list` | List conversation sessions |

## Next Steps

- [First Run](first-run.md) — What to expect and how to verify everything works
- [Configuration](../configuration/index.md) — Customize your setup
- [Concepts](../concepts/index.md) — Understand how Meept works

## Optional: Using the Flutter GUI

Meept includes a cross-platform Flutter-based GUI for macOS, Linux, and Windows.

### Prerequisites

- [Flutter SDK](https://docs.flutter.dev/get-started/install) (3.0+)
- macOS: Xcode command line tools
- Linux: GTK development libraries
- Windows: Visual Studio Build Tools

### Build and Run

```bash
cd ui/flutter_ui

# Install dependencies
flutter pub get

# Run in development mode
flutter run

# Or build for your platform
flutter build macos    # macOS
flutter build linux    # Linux
flutter build windows  # Windows
```

### API Key Configuration

The Flutter app needs an API key to authenticate with the daemon:

**Development (default):** The app automatically uses the default dev API key (`meept_dev_default_key_CHANGE_ME`). No configuration needed.

**Production:** Generate a secure API key and configure it in the app:

1. Generate a key:
   ```bash
   ./bin/meept token generate
   ```

2. In the Flutter app:
   - Open Settings (gear icon)
   - Enter the API key in the "API Token" field
   - Save

Alternatively, set it in `~/.meept/menubar.json5`:
```json5
{
  "daemon": {
    "http_url": "https://localhost:8081",
    "api_token": "your-generated-key-here"
  }
}
```

### Troubleshooting

**401 Unauthorized errors:** The API key is missing or incorrect. Check Settings.

**TLS handshake errors:** Ensure the daemon is running with HTTPS enabled (default). The Flutter app requires HTTPS.

**Connection refused:** Verify the daemon is running on port 8081 (default) in `~/.meept/menubar.json5`.

