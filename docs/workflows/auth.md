# OAuth Device-Code Providers

## Overview

`internal/auth` implements OAuth device-code connections for LLM and
service providers, so meept can use subscription billing (ChatGPT Plus,
Claude Pro/Max, SuperGrok) instead of per-token API keys where the
provider supports it.

Tokens are stored encrypted under `~/.meept/oauth/<provider>.json`. A
background refresh manager keeps tokens warm before expiry.

## Problem

Several providers bill through consumer subscriptions rather than
platform API keys. Using them requires interactive user login (device
code or PKCE paste flows) plus automatic token refresh — none of which
static API-key configuration can express.

## Behavior

Connect, inspect, and disconnect via the CLI:

```bash
meept config oauth connect <provider>
meept config oauth status
meept config oauth disconnect <provider>
```

`connect` runs the provider's flow, prints a verification URL (and code
where applicable), polls until the user authorizes, then saves the
encrypted token. `status` lists stored tokens with expiry.

### Key Components

- `internal/auth/providers.go` — provider registry
  (`OAuthProviders` map) + `FlowKind` selection:
  - `device_rfc8628` — standard RFC 8628 device flow (default; e.g.
    github-models, google-oauth, google-calendar). `FormEncoded`
    switches the device request body between JSON and
    `application/x-www-form-urlencoded` (xAI requires form).
  - `device_codex` — OpenAI "Sign in with ChatGPT" variant with
    separate user-code and poll endpoints.
  - `pkce_paste` — PKCE browser flow where the user pastes back a code.
- `internal/auth/device_flow.go` — RFC 8628 flow: `StartDeviceFlow`,
  `PollForToken`, `RefreshTokenRequest`.
- `internal/auth/discovery.go` — `ResolveTokenEndpoint` resolves a
  token endpoint from a provider's OIDC discovery document (xAI).
- `internal/auth/token_store.go` — encrypted per-provider token
  storage; implements `llm.TokenResolver` so LLM clients resolve fresh
  access tokens per request.
- `internal/auth/refresh.go` — `RefreshManager` refreshes tokens within
  a configurable margin of expiry.

### Configuration

```json5
{
  "oauth": {
    "enabled": true,
    "refresh_interval": "5m",
    "refresh_margin": "10m",
    // "token_dir": "~/.meept/oauth",   // default
    "providers": {
      // "<provider>": { "client_id": "...", "client_secret": "..." }
    }
  }
}
```

Per-provider client IDs default to embedded public values and can be
overridden via the provider's env var (e.g. `MEEPT_GITHUB_CLIENT_ID`)
or the `oauth.providers` config section.

Providers with an LLM `BaseURL` are auto-registered with the LLM
provider manager once a token is stored — no manual model wiring
needed.

### Observability

- `slog` debug/info logging on flow start/complete, token refresh, and
  registration (`component=oauth`).
- Refresh failures log a warning per attempt; after 3 consecutive
  failures the provider is marked stale in logs and the user is
  directed to reconnect.

## Edge Cases

- Token expired without a refresh token → error directs the user to
  `meept config oauth connect <provider>`.
- Device-code polling honors `authorization_pending` / `slow_down`
  (interval +5s) per RFC 8628; `expired_token` / `access_denied`
  abort immediately.
- SIGINT/SIGTERM during `connect` cancels the polling loop cleanly.
- Providers without an LLM BaseURL (e.g. google-calendar) register
  service tools but no chat endpoint.
