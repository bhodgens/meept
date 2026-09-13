# Flutter Web Development for Meept

## Quick Start

### Option 1: Run web dev server (recommended for development)

```bash
# Start the daemon first (in one terminal)
make daemon

# Then run the Flutter web dev server with hot reload (in another terminal)
make gui-web-run
```

This opens Chrome automatically at `http://localhost:59714` with hot reload enabled.

### Option 2: Web server target (custom port)

```bash
make gui-dev-server
```

Runs on `http://localhost:59714` - useful for testing in any browser.

## Configuration

The GUI's daemon endpoint is compiled in at build time; it is not discovered
at runtime. `make build-gui` / `make gui-web` pass it through `--dart-define`:

- `MEEPT_API_HOST` - daemon HTTP host
- `MEEPT_API_PORT` - daemon HTTP port
- `MEEPT_WS_PATH` - WebSocket path (default `/ws`)

`AppConstants` (`lib/core/constants.dart`) reads these with
`String.fromEnvironment` / `int.fromEnvironment`, so the shipped app carries
the installed endpoint. The values are derived from the installed
`$MEEPT_HOME/meept.json5` `transport.http` settings by
`scripts/gui-daemon-connect.py`, driven by `make gui-connect-setup` and
verified by `make gui-connect-check`.

The shipped config binds HTTP on loopback only:

- **addr**: `127.0.0.1:8081`
- **endpoint**: `http://127.0.0.1:8081/api/v1`
- **ws**: `ws://127.0.0.1:8081/ws`

The GUI host and port must equal `transport.http.addr`: the client cannot
discover a different bind address, and the daemon accepts `GET`/`POST
/api/v1/config/main` (the meept.json5 editor in settings, and the read-only
multi-user probe) from loopback clients only. The read is gated as well as the
write because the response is the verbatim `meept.json5`, which carries
`transport.http.api_keys`. A GUI that reaches the daemon from off-host
therefore sees 403 on both — the multi-user row reports "could not load daemon
config" — while a same-host GUI (the shipped web build and the desktop build
both fall in this class) is unaffected. A from-source `flutter run` with no
dart-defines falls back to `localhost:8081`.

## Requirements

1. **Daemon must be running** with HTTP API enabled
2. **CORS enabled** in daemon config (default: enabled)
3. **Flutter SDK** installed

## Daemon Configuration

Ensure your `~/.meept/meept.json5` has HTTP transport enabled. `addr` must
match the host/port the GUI was built with:

```json5
{
  transport: {
    http: {
      enabled: true,
      addr: "127.0.0.1:8081",  // GUI host/port must equal this
      require_auth: false,  // false for local dev
    },
  },
}
```

## Hot Reload

- **Hot reload**: Press `r` in the terminal
- **Hot restart**: Press `R` in the terminal
- **Quit**: Press `q` in the terminal

## Building for Production

```bash
make gui-web
```

Output: `ui/flutter_ui/build/web/`

## Troubleshooting

### Connection refused errors
- Ensure daemon is running: `make daemon`
- Check HTTP API is enabled in config

### CORS errors
- Verify `enable_cors: true` in daemon config
- Web browser requires CORS; macOS app does not

### Port already in use
- The GUI talks to whatever `transport.http.addr` names, so change that
  address in `~/.meept/meept.json5`, re-derive the GUI endpoint with
  `make gui-connect-setup`, and rebuild (`make build-gui`).
- For a from-source `flutter run`, pass matching dart-defines, for example
  `--dart-define=MEEPT_API_HOST=127.0.0.1 --dart-define=MEEPT_API_PORT=<port>`.
- Or kill the listener: `lsof -ti:8081 | xargs kill`
