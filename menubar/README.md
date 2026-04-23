# Meept MenuBar

A native macOS menu bar application for monitoring and controlling the Meept daemon.

## Features

- **Menu Bar Status**: Always-visible status indicator showing daemon state
- **Daemon Control**: Start, stop, and restart the Meept daemon from the menu
- **Settings Window**:
  - Client configuration editor (client.json5)
  - Models configuration with preset support
  - Agent management (add, edit, remove agents)
- **Analytics Dashboard**:
  - Live metrics (active agents, requests/sec, token usage, queue depth)
  - Historical reports with date selection
  - Real-time updates via polling

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│              MacOS MenuBar App (SwiftUI)                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │
│  │  Menu Bar   │  │  Settings   │  │    Analytics    │  │
│  │  (status)   │  │  (config)   │  │   (dashboard)   │  │
│  └──────┬──────┘  └──────┬──────┘  └────────┬────────┘  │
│         │                │                   │           │
│         └────────────────┴───────────────────┘           │
│                        │                                  │
│                 HTTP Client (REST)                        │
└────────────────────────┼──────────────────────────────────┘
                         │ HTTP (localhost:8081)
┌────────────────────────┼──────────────────────────────────┐
│                        ▼                                  │
│            ┌─────────────────────┐                        │
│            │  HTTP REST API      │                        │
│            │  (internal/comm/http)│                       │
│            └───────────┬─────────┘                        │
│                        │                                   │
│    ┌───────────────────┼───────────────────┐              │
│    │                   │                   │              │
│    ▼                   ▼                   ▼              │
│ ┌──────────┐   ┌──────────────┐   ┌──────────────┐       │
│ │  Config  │   │   Daemon     │   │   Metrics    │       │
│ │ Service  │   │  Controller  │   │    Store     │       │
│ └──────────┘   └──────────────┘   └──────────────┘       │
│                                                          │
│                  Meept Daemon (Go)                       │
└──────────────────────────────────────────────────────────┘
```

## Project Structure

```
menubar/
├── MeeptMenuBar/          # Main SwiftUI app
│   ├── AppDelegate.swift  # Lifecycle, status item
│   ├── MeeptMenuBarApp.swift
│   └── Assets.xcassets/
├── Views/
│   ├── Menu/
│   │   └── MenuView.swift         # Dropdown menu
│   ├── Settings/
│   │   ├── SettingsWindow.swift   # Tabbed settings
│   │   ├── ClientConfigView.swift # Client config editor
│   │   ├── ModelsConfigView.swift # Models editor
│   │   └── AgentsConfigView.swift # Agent management
│   └── Analytics/
│       ├── DashboardWindow.swift  # Analytics dashboard
│       ├── LiveMetricsView.swift  # Real-time gauges
│       └── HistoricalReportView.swift # Historical charts
├── Services/
│   ├── APIClient.swift            # HTTP client
│   ├── DaemonController.swift     # launchd control
│   └── ConfigService.swift        # Config file management
├── Models/
│   ├── ConfigModels.swift         # Data models
│   └── Presets.swift              # Model presets
├── Utilities/
│   └── (future helpers)
├── Resources/
│   └── (future resources)
├── Package.swift
└── README.md
```

## Prerequisites

- macOS 13.0 (Ventura) or later
- Xcode 15 or later
- Meept daemon built and installed

## Build Instructions

### Using Swift Package Manager

```bash
cd menubar

# Build
swift build

# Run (for testing)
swift run MeeptMenuBar

# Build release
swift build -c release
```

### Using Xcode

1. Generate Xcode project:
   ```bash
   cd menubar
   swift package generate-xcodeproj
   ```

2. Open in Xcode:
   ```bash
   open MeeptMenuBar.xcodeproj
   ```

3. Select the MeeptMenuBar scheme and build (Cmd+B)

4. To run: Product > Run

## Installation

### Option 1: Copy to Applications

```bash
# Build release
cd menubar
swift build -c release

# Copy app to Applications
cp -r .build/release/MeeptMenuBar.app /Applications/
```

### Option 2: Create DMG

```bash
# Build release
swift build -c release

# Create disk image
create-dmg \
  --volname "Meept MenuBar" \
  --window-pos 200 120 \
  --window-size 800 400 \
  --icon-size 100 \
  --app-drop-link 600 185 \
  "MeeptMenuBar.dmg" \
  ".build/release/MeeptMenuBar.app"
```

## Configuration

The menubar app communicates with the Meept daemon via HTTP REST API on `localhost:8081`.

### Daemon Configuration

Ensure the daemon is configured to start the HTTP server for the menubar app:

```toml
# In ~/.meept/meept.toml

[web]
enabled = true
addr = ":8081"  # Menubar API port
```

### launchd Integration

The app automatically creates a launchd plist at:
`~/Library/LaunchAgents/com.caimlas.meept-daemon.plist`

To manually install the launchd agent:

```bash
launchctl load ~/Library/LaunchAgents/com.caimlas.meept-daemon.plist
```

## API Reference

### Config Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/config/client` | Get client.json5 content |
| POST | `/api/v1/config/client` | Save client.json5 |
| GET | `/api/v1/config/models` | Get models.json5 content |
| POST | `/api/v1/config/models` | Save models.json5 |
| GET | `/api/v1/config/agents` | List agents |
| GET | `/api/v1/config/agents/:id` | Get agent config |
| POST | `/api/v1/config/agents/:id` | Save agent config |
| DELETE | `/api/v1/config/agents/:id` | Remove agent |

### Daemon Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/daemon/status` | Get daemon status |
| POST | `/api/v1/daemon/restart` | Restart daemon |

### Metrics Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/metrics/live` | Get live metrics |
| GET | `/api/v1/metrics/historical` | Get historical metrics |
| GET | `/api/v1/metrics/stream` | WebSocket stream (future) |

## Model Presets

The app includes built-in presets for model configuration:

| Preset | Temperature | Use Case |
|--------|-------------|----------|
| Development | 0.3 | Balanced for coding |
| Debugging | 0.2 | Methodical troubleshooting |
| Planning | 0.4 | Structured thinking |
| Creative | 0.9 | High creativity |
| Research | 0.5 | Analytical tasks |
| Fast | 0.3 | Quick responses |
| Detailed | 0.5 | Comprehensive answers |

## Troubleshooting

### App doesn't show in menu bar

1. Check if the app is running in Activity Monitor
2. Try moving the app to /Applications
3. Restart the app

### Cannot connect to daemon

1. Ensure daemon is running: `./bin/meept-daemon -f`
2. Check HTTP server is listening on port 8081
3. Verify firewall isn't blocking localhost connections

### launchd agent fails to load

1. Check plist syntax: `plutil -lint ~/Library/LaunchAgents/com.caimlas.meept-daemon.plist`
2. Ensure daemon binary path is correct in plist
3. Check daemon logs: `tail -f ~/.meept/daemon.log`

## Development

### Running in development

```bash
# Terminal 1: Start daemon
./bin/meept-daemon -f

# Terminal 2: Run menubar app
cd menubar
swift run MeeptMenuBar
```

### Code style

- All UI text in lowercase (per Meept conventions)
- Use SwiftUI for all UI components
- Follow MVVM pattern for views
- Keep view models testable

## Future Enhancements

- [ ] WebSocket support for real-time metrics
- [ ] Native notifications for job completion
- [ ] Touch Bar support
- [ ] Apple Script automation
- [ ] Shortcuts app integration
- [ ] iCloud config sync
- [ ] Dark mode status icon
- [ ] Custom app icon

## License

Same license as Meept.
