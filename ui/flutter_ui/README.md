# Meept Cyberpunk UI

Hacker-themed Flutter UI for Meept multi-agent system.

## Requirements

- Flutter 3.10+
- Dart 3.0+
- Xcode (for macOS builds)

## Running

### Web
```bash
flutter run -d chrome
```

### macOS Desktop
```bash
flutter run -d macos
```

### Production Build

Web:
```bash
flutter build web --release
```

macOS:
```bash
flutter build macos --release
```

## Architecture

- `lib/core/` - Theme, routing, constants
- `lib/features/` - Feature modules (chat, sessions, agents, tasks, metrics)
- `lib/models/` - Data models
- `lib/services/` - API clients, WebSocket, storage
- `lib/widgets/` - Reusable UI components

## API Connection

Connects to Meept HTTP API at `http://localhost:8081/api/v1/*`
