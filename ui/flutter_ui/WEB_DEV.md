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

The Flutter web app connects to the Meept daemon HTTP API at:
- **Host**: `localhost`
- **Port**: `8081`
- **Endpoint**: `http://localhost:8081/api/v1`

## Requirements

1. **Daemon must be running** with HTTP API enabled
2. **CORS enabled** in daemon config (default: enabled)
3. **Flutter SDK** installed

## Daemon Configuration

Ensure your `~/.meept/meept.json5` has HTTP transport enabled:

```json5
{
  transport: {
    http: {
      enabled: true,
      addr: ":8081",
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
- Change port in `lib/core/constants.dart`
- Or kill process: `lsof -ti:8081 | xargs kill`
