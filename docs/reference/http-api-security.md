# HTTP API Security Guide

The Meept HTTP API supports multiple security layers for production deployments.

## Current Security Features

### 1. API Key Authentication

**Status:** Implemented and wired to config.

The HTTP server includes API key authentication middleware
(`internal/comm/http/auth.go`, `server.go`):

- Validates API keys from the `Authorization: Bearer <key>` header, or from
  the `Sec-WebSocket-Protocol: bearer.<key>` subprotocol for WebSocket
  upgrades.
- Constant-time comparison to prevent timing attacks.
- Skips auth for `OPTIONS` (CORS preflight) and health endpoints only.
- Enabled by default (`transport.http.require_auth: true`).

When `require_auth` is true and no keys are configured, the server falls back
to a per-installation dev key stored at `~/.meept/dev_key` (0600). Both daemon
and CLI resolve this file, so local development works out of the box. For any
exposed deployment, configure explicit keys.

Known-public legacy default keys are rejected at startup: configuring one is
a fatal error. This closes the door on the historical hardcoded defaults that
appear in old releases and documentation.

Generate and save a key:

```bash
meept token generate --save   # prints once; stores in config if supported
```

Then wire it into the server via config:

```json5
{
  transport: {
    http: {
      require_auth: true,
      api_keys: ["${MEEPT_HTTP_API_KEY}"],  // or literal keys
    },
  },
}
```

### 2. Rate Limiting

**Status:** Implemented, per-IP, enabled by default.

Defaults: 120 requests/minute per IP with burst 30. Configurable:

```json5
{
  transport: {
    http: {
      rate_limit_rpm: 120,
      rate_limit_burst: 30,
    },
  },
}
```

Health endpoints are exempt. The limiter runs before auth, so unauthenticated
floods cannot burn CPU in the auth path.

### 3. Request Size Limits

Request bodies are capped (`http.MaxBytesReader`, ~1 MB) by the shared JSON
decoder helpers; max header bytes are also configured.

### 4. Request Logging

Status-code-capturing response writer logs every >=400 response at Warn level
(method, path, status, remote address, duration). Debug-level logging covers
successful requests.

### 5. CORS

Enabled by default but hardened: origins are echoed ONLY for empty origin or
trusted localhost hosts (`localhost`, `127.0.0.1`, `::1`). No wildcard. The
same allowlist governs WebSocket upgrades.

## HTTPS/TLS Support

**Status:** Implemented — HTTPS by default with auto-generated self-signed certificate.

- Auto-generates a self-signed certificate on first startup.
- Certificate valid for 1 year, ECDSA P-256.
- Stored in `~/.meept/meept.crt` and `~/.meept/meept.key`.
- Fingerprint written to the fingerprint file for client pinning/discovery.

**Configuration:**

```json5
{
  transport: {
    http: {
      use_tls: true,        // Enable HTTPS
      auto_tls_cert: true,  // Auto-generate self-signed cert
    },
  },
}
```

## Creating Real Certificates

Self-signed certs trigger browser warnings and can't be pinned by unfamiliar
clients. For production, use either option below.

### Option A: Reverse proxy terminates TLS (recommended)

Run the daemon on loopback HTTP and let Caddy/nginx serve public TLS. With a
proxy terminating TLS, you can set `use_tls: false` and bind to
`127.0.0.1:8081`.

Caddy (automatic Let's Encrypt):

```caddyfile
meept.example.com {
    reverse_proxy 127.0.0.1:8081
}
```

That is the whole file. Caddy obtains and renews the certificate
automatically.

nginx with Let's Encrypt (certbot):

```bash
sudo certbot --nginx -d meept.example.com
```

```nginx
server {
    listen 443 ssl;
    server_name meept.example.com;

    ssl_certificate     /etc/letsencrypt/live/meept.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/meept.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        # WebSocket upgrade support:
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
    }
}
```

Note the WebSocket block — the GUI and TUI connect over WebSocket, and a
missing `Upgrade` mapping breaks live updates through nginx.

Internal/air-gapped networks (no CA reachable): use your organization's
internal CA, or generate a long-lived self-signed pair and distribute it:

```bash
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
  -keyout meept.key -out meept.crt -days 3650 -nodes \
  -subj "/CN=meept.internal" \
  -addext "subjectAltName=DNS:meept.internal,IP:10.0.0.5"
```

Point the daemon at it with `tls_cert_file` / `tls_key_file` equivalents in
the transport config, and disable `auto_tls_cert`. Clients should verify via
certificate fingerprint where supported instead of disabling verification.

### Option B: Native TLS with a real certificate

Terminate TLS inside the daemon itself when no proxy sits in front of it.
Obtain a cert (certbot standalone mode works well since the daemon owns the
port), then configure the daemon to load the PEM files and turn off auto-
generation. Keep renewal simple by running certbot with a deploy hook that
restarts the daemon, or run everything behind Option A's proxy and avoid the
problem entirely.

## Unix Socket RPC Security Model

The daemon exposes a second transport: JSON-RPC 2.0 over a Unix domain
socket (`~/.meept/meept.sock`). It has NO application-layer auth — by
design:

- The socket is created with 0600 permissions. Only processes running as
  the SAME operating-system user may connect; the kernel enforces this.
- There is no bearer key to leak, rotate, or accidentally commit.
- The socket must NEVER be exposed over TCP or a network filesystem —
  doing so removes the only access control it has.

This makes RPC strictly MORE trusted than the HTTP API: every RPC caller
is, by construction, the daemon owner. Consequences:

- The CLI and TUI default to RPC for loopback operation; they need no
  key material on disk beyond what the OS already protects.
- In a future multi-user deployment, RPC calls bypass per-user identity
  (they act as the owner). Keep `transport.rpc.enabled: false` on any
  node where untrusted local users exist, or run each user's daemon under
  their own OS account. See the peer-credential note below.

Planned defense-in-depth: verify peer credentials on each accepted
connection (`LOCAL_PEERCRED` on macOS / `SO_PEERCRED` on Linux), log the
connecting UID, and optionally restrict to an explicit UID allowlist.
Tracked as an open question in
`docs/plans/2026-08-26-multiuser-access/master.md` (Q3).

## Production Deployment Checklist

- [ ] Replace dev-key fallback with explicit `api_keys` in config
- [ ] Keys sourced from environment/secrets manager, not literals in files
      checked into VCS
- [ ] `require_auth: true` (default) never disabled on exposed interfaces
- [ ] Bind address stays `127.0.0.1` unless external access is intended
- [ ] Real TLS certificate (Option A proxy preferred) or internal CA cert;
      `auto_tls_cert: false` once real certs are in place
- [ ] Firewall restricts the daemon port to expected sources
- [ ] Rate limits tuned for expected client count
- [ ] CORS left at localhost-only defaults unless a specific web origin is
      needed
- [ ] Key rotation every 90 days (multi-user plan adds per-user expiry)
- [ ] Monitor Warn-level HTTP error logs for failed auth attempts

## Future Enhancements

1. **Multi-user identity** — planned: per-user keys with expiry, cluster
   pooling, ownership scoping. Design: `docs/plans/2026-08-26-multiuser-access/master.md`.
2. **OAuth2/OIDC:** federation for larger multi-user deployments.
3. **mTLS:** service-to-service authentication.
4. **Audit logging:** structured logs for compliance.
