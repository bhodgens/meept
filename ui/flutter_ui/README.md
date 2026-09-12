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

The GUI's daemon endpoint is compiled in at build time, not discovered at
runtime. `make build-gui` / `make gui-web` pass it through `--dart-define`:

- `MEEPT_API_HOST` - daemon HTTP host
- `MEEPT_API_PORT` - daemon HTTP port
- `MEEPT_WS_PATH` - WebSocket path (default `/ws`)

`AppConstants` (`lib/core/constants.dart`) reads these with
`String.fromEnvironment` / `int.fromEnvironment`, so the shipped app carries
the installed endpoint. `scripts/gui-daemon-connect.py` derives the values
from the installed `$MEEPT_HOME/meept.json5` `transport.http` settings; it is
driven by `make gui-connect-setup`, self-checked by `make gui-connect-check`,
and the resulting connect path is proven by `scripts/verify-gui-connect.sh`.

The shipped config binds HTTP on loopback only:

- **addr**: `127.0.0.1:8081`
- **endpoint**: `http://127.0.0.1:8081/api/v1`
- **ws**: `ws://127.0.0.1:8081/ws`

The GUI host and port must equal `transport.http.addr`: the client cannot
discover a different bind address, and the daemon accepts whole-file config
writes (`POST /api/v1/config/main`, the meept.json5 editor in settings) from
loopback clients only. A from-source `flutter run` with no dart-defines falls
back to `localhost:8081`.

See `WEB_DEV.md` for the web-dev specifics.
