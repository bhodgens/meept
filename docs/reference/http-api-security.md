# HTTP API Security Guide

The Meept HTTP API supports multiple security layers for production deployments.

## Current Security Features

### 1. API Key Authentication

**Status:** Implemented but not wired to config

The HTTP server includes API key authentication middleware (`internal/comm/http/auth.go`):

- Validates API keys from `Authorization` header
- Supports `Bearer <key>` or raw `<key>` format
- Constant-time comparison to prevent timing attacks
- Skips auth for health checks and CORS preflight

**How to enable (current workaround):**

```go
// In daemon.go, after creating httpCfg:
httpCfg.RequireAuth = true
httpCfg.APIKeys = []string{"your-secure-api-key-here"}
```

### 2. CORS Configuration

**Status:** Enabled by default for menubar app

- Configurable via `ServerConfig.EnableCORS`
- Allows all origins (`*`) - suitable for localhost menubar
- For production, restrict to specific origins

## Security Gaps & Recommendations

### Gap 1: No Config Wiring for Auth

**Problem:** `RequireAuth` and `APIKeys` are not exposed in `HTTPTransportConfig`.

**Fix:** Add to `internal/config/schema.go`:

```go
type HTTPTransportConfig struct {
    Enabled    bool     `json:"enabled" toml:"enabled"`
    Addr       string   `json:"addr"    toml:"addr"`
    RequireAuth bool   `json:"require_auth" toml:"require_auth"`
    APIKeys    []string `json:"api_keys"   toml:"api_keys"`
}
```

Then wire in `internal/daemon/daemon.go`:
```go
httpCfg.RequireAuth = fullCfg.Transport.HTTP.RequireAuth
httpCfg.APIKeys = fullCfg.Transport.HTTP.APIKeys
```

### Gap 2: No HTTPS/TLS Support

**Problem:** HTTP server only supports plaintext HTTP.

**Fix Options:**

1. **Reverse Proxy (Recommended):** Deploy behind nginx, Caddy, or Traefik:
   ```nginx
   server {
       listen 443 ssl;
       server_name meept.example.com;
       
       ssl_certificate /path/to/cert.pem;
       ssl_certificate_key /path/to/key.pem;
       
       location / {
           proxy_pass http://localhost:8081;
           proxy_set_header Host $host;
           proxy_set_header X-Real-IP $remote_addr;
       }
   }
   ```

2. **Native TLS:** Add to `ServerConfig`:
   ```go
   type ServerConfig struct {
       // ... existing fields ...
       TLSCertFile string `json:"tls_cert_file"`
       TLSKeyFile  string `json:"tls_key_file"`
   }
   ```
   
   Then in `Serve()`:
   ```go
   if s.config.TLSCertFile != "" {
       return http.ListenAndServeTLS(s.config.Addr, s.config.TLSCertFile, s.config.TLSKeyFile, handler)
   }
   return http.ListenAndServe(s.config.Addr, handler)
   ```

### Gap 3: No Rate Limiting

**Problem:** No protection against brute-force or DoS attacks.

**Fix:** Add rate limiting middleware:
```go
import "golang.org/x/time/rate"

type RateLimiter struct {
    limiter *rate.Limiter
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if !rl.limiter.Allow() {
            http.Error(w, `{"error": "rate limit exceeded"}`, http.StatusTooManyRequests)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

### Gap 4: No Request Logging/Audit

**Problem:** Limited visibility into API usage.

**Fix:** The existing `loggingResponseWriter` captures status codes. Enhance with:
- Request body logging (optional, for debugging)
- Client IP tracking
- Request timing metrics
- Audit log for sensitive operations

### Gap 5: No Input Validation

**Problem:** Request bodies are decoded but not validated for size or content.

**Fix:** Add request size limits:
```go
httpCfg.MaxHeaderBytes = 1 << 20 // 1 MB - already configured
// Add body size limit in handlers:
r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
```

## Production Deployment Checklist

- [ ] Enable API key authentication (`require_auth: true`)
- [ ] Replace default key `d@ng3r_NOT_A_Secure_key_REGENERATE_M3`
- [ ] Generate strong API keys (32+ random bytes, base64 encoded)
- [ ] Store API keys in environment variables or secrets manager
- [ ] Deploy behind HTTPS-terminating reverse proxy
- [ ] Configure firewall to restrict access to port 8081
- [ ] Enable request logging and monitoring
- [ ] Set up rate limiting (e.g., 100 requests/minute per client)
- [ ] Configure CORS for specific origins (disable `*` in production)
- [ ] Regular key rotation (every 90 days)
- [ ] Monitor for failed auth attempts

## Example Production Config

```json5
{
  transport: {
    http: {
      enabled: true,
      addr: "127.0.0.1:8081",  // Bind to localhost only
      require_auth: true,
      api_keys: [
        // Load from environment: $MEEPT_HTTP_API_KEY
        "${MEEPT_HTTP_API_KEY}"
      ],
    },
  },
}
```

## Security Headers (via reverse proxy)

Add these headers in nginx/Caddy:

```
Strict-Transport-Security: max-age=31536000; includeSubDomains
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
```

## Future Enhancements

1. **OAuth2/OIDC:** For multi-user deployments
2. **JWT tokens:** For session-based auth
3. **mTLS:** For service-to-service authentication
4. **Request signing:** HMAC signatures for webhook-style security
5. **Audit logging:** Structured logs for compliance
