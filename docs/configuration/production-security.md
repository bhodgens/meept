# Production Security Configuration

This guide covers the production-ready security features for Meept's HTTP API and Flutter GUI client.

## Overview

Meept now uses **HTTPS/TLS** and **API token authentication** by default for all communication between the Flutter GUI client and the daemon. This ensures:

- **Encrypted communication** via self-signed TLS certificates (localhost-only)
- **Authenticated access** via API tokens stored in macOS Keychain
- **Sandboxed Flutter app** with minimal entitlements (`network.client`, `keychain`)

## Quick Start

### Step 1: Generate an API Token

```bash
meept token generate --save
```

This generates a cryptographically secure token and saves it to `~/.meept/meept.json5`.

### Step 2: Restart the Daemon

```bash
meept daemon stop
meept daemon start
```

The daemon will automatically:
- Generate a self-signed TLS certificate on first run (`~/.meept/tls/cert.pem`, `~/.meept/tls/key.pem`)
- Require API token authentication for all HTTP/WebSocket endpoints

### Step 3: Configure the Flutter GUI

1. Open the Meept GUI app
2. Go to **Settings** (gear icon)
3. In the "API Token" section, enter your token:
   - Option A: Copy the token from `meept token generate --save` output
   - Option B: Run `meept token list` to see existing tokens, then copy
4. Click **Save Token** - the token is stored in macOS Keychain

### Step 4: Rebuild the Flutter App (if modified entitlements)

If you've updated the entitlements files:

```bash
cd ui/flutter_ui
flutter clean
flutter build macos --release
make install  # Copies to ~/Applications/
```

## Configuration Reference

### Daemon Configuration (`~/.meept/meept.json5`)

```json5
{
  transport: {
    http: {
      "enabled": true,
      "addr": ":8081",
      // TLS configuration - enabled by default
      "use_tls": true,           // Enable HTTPS
      "auto_tls_cert": true,     // Auto-generate self-signed cert
      "tls_cert_file": "~/.meept/tls/cert.pem",
      "tls_key_file": "~/.meept/tls/key.pem",
      // Authentication
      "require_auth": true,      // Require API token
      "api_keys": [              // List of valid tokens
        "meept_abc123...",
      ],
      // Endpoints
      "rest": true,
      "websocket": true,
      "ws_path": "/ws",
    },
  },
}
```

### CLI Token Commands

```bash
# Generate a new token (print to stdout)
meept token generate

# Generate and save to config directly
meept token generate --save

# List all configured tokens (masked)
meept token list

# Revoke a specific token
meept token revoke <full-token>
```

### Flutter GUI Storage

The Flutter app stores sensitive data in macOS Keychain:

| Key | Storage | Purpose |
|-----|---------|---------|
| `api_key` | Keychain + SharedPreferences | API token for authentication |
| `use_tls` | SharedPreferences | Whether to use HTTPS/WSS (default: true) |
| `api_host` | SharedPreferences | Daemon hostname (default: localhost) |
| `api_port` | SharedPreferences | Daemon port (default: 8081) |

## Verification

### Test HTTPS Connection

```bash
# Should return 200 OK (health endpoint is excluded from auth)
curl -k https://localhost:8081/api/v1/health

# Should return 401 Unauthorized (auth required)
curl -k https://localhost:8081/api/v1/sessions

# Should return 200 OK (valid token)
curl -k -H "Authorization: Bearer meept_..." https://localhost:8081/api/v1/sessions
```

### Test WebSocket Connection

The Flutter GUI handles this automatically, but you can verify via Console.app:
- No "Operation not permitted" errors (sandbox working)
- No "Connection refused" errors (TLS working)
- WebSocket connects with `wss://` protocol

### Verify Entitlements

```bash
codesign -d --entitlements - ~/Applications/Meept\ GUI\ Client.app
```

Expected output:
```xml
<key>com.apple.security.app-sandbox</key>
<true/>
<key>com.apple.security.network.client</key>
<true/>
<key>com.apple.security.keychain</key>
<true/>
```

## Troubleshooting

### "Unauthorized" Errors

**Symptom:** Flutter GUI shows 401 Unauthorized errors

**Solution:**
1. Verify token is configured: `meept token list`
2. Ensure token is saved in Flutter Settings
3. Restart daemon after adding token to config

### TLS Certificate Errors

**Symptom:** Connection fails with certificate errors

**Solution:**
1. Delete existing certs: `rm ~/.meept/tls/cert.pem ~/.meept/tls/key.pem`
2. Restart daemon - new self-signed certs will be generated
3. Rebuild Flutter app if necessary

### Sandbox Errors

**Symptom:** Console.app shows "Operation not permitted" errors

**Solution:**
1. Verify entitlements have `app-sandbox = true`
2. Ensure `network.client` entitlement is present
3. For Keychain access, verify `keychain` entitlement is present
4. Rebuild Flutter app: `flutter clean && flutter build macos`

### Token Not Persisting

**Symptom:** API token disappears after app restart

**Solution:**
1. Check Keychain Access app for "meept_ui" entries
2. Verify Keychain is unlocked (should unlock on first login)
3. Try re-saving token in Settings panel

## Migration from Insecure HTTP

If you're upgrading from the insecure HTTP configuration:

1. **Backup existing config:**
   ```bash
   cp ~/.meept/meept.json5 ~/.meept/meept.json5.backup
   ```

2. **Generate API token:**
   ```bash
   meept token generate --save
   ```

3. **Update config manually (if needed):**
   Ensure these fields are set in `~/.meept/meept.json5`:
   ```json5
   {
     transport: {
       http: {
         "use_tls": true,
         "require_auth": true,
         "api_keys": ["meept_..."],
       },
     },
   }
   ```

4. **Update Flutter GUI:**
   - Open Settings
   - Enter API token
   - Verify "Connected securely via HTTPS" status

5. **Rebuild Flutter app with sandbox:**
   ```bash
   cd ui/flutter_ui
   flutter clean
   flutter build macos --release
   make install
   ```

## Security Considerations

### Self-Signed Certificates

Self-signed certificates are acceptable for localhost-only communication because:
- They're automatically generated and trusted by the app
- No browser warnings apply to native apps
- The connection is still encrypted end-to-end

### API Token Storage

macOS Keychain provides:
- Encrypted storage backed by Secure Enclave (on supported hardware)
- Automatic unlocking on user login
- Protection against keychain extraction attacks

### Sandbox Entitlements

The minimal entitlements required:
- `app-sandbox`: Enables macOS App Sandbox
- `network.client`: Allows outbound network connections (localhost)
- `keychain`: Allows access to macOS Keychain

## Disabling Security (Development Only)

For local development, you can disable TLS and auth:

```json5
{
  transport: {
    http: {
      "use_tls": false,
      "require_auth": false,
    },
  },
}
```

**Warning:** This is insecure for any network-accessible deployment. Only use for local development.

## Related Files

| File | Purpose |
|------|---------|
| `config/meept.json5` | Daemon config template (production defaults) |
| `cmd/meept/token.go` | CLI token management commands |
| `internal/comm/http/server.go` | HTTP server with TLS + WebSocket auth |
| `internal/comm/http/auth.go` | API key authentication middleware |
| `ui/flutter_ui/lib/services/storage_service.dart` | Keychain integration |
| `ui/flutter_ui/lib/services/api_client.dart` | HTTPS API client |
| `ui/flutter_ui/lib/services/websocket_service.dart` | WSS WebSocket client |
| `ui/flutter_ui/macos/Runner/*.entitlements` | macOS sandbox entitlements |
