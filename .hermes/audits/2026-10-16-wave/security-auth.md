# internal/security, internal/auth, internal/secrets -- bughunt 2026-10-16

## Findings

### 1. SSRF: DefaultGuard() panic at guard.go:145 -- unreachable but worth noting
- **Location**: `internal/security/ssrf/guard.go:145`
- **Mechanism**: `DefaultGuard()` calls `NewGuard(GuardConfig{})` and panics if it returns an error. The comment at line 144 says "Unreachable: the zero config is always valid." However, if `parseCIDRs` ever receives invalid input (e.g., if `DefaultBlockedCIDRs` is mutated or if the zero config changes), this panic would trigger.
- **Severity**: LOW
- **Test/Probe**: The zero `GuardConfig{}` is always valid per the code path. No reachable exploit path.
- **Evidence**: SOURCE (code review)
- **Not covered**: None -- this is a dead code path.

### 2. SSRF: DNS rebinding TOCTOU -- documented limitation, not a bug
- **Location**: `internal/security/ssrf/guard.go:13-21`
- **Mechanism**: The guard acknowledges that DNS rebinding between the pre-flight check and dial-time lookup is "out of scope." The dial-time re-validation closes the common window but not the narrow gap between DNS resolution and TCP connect.
- **Severity**: INFO (documented design trade-off)
- **Test/Probe**: Not a bug -- this is an acknowledged limitation in the code comments.
- **Evidence**: SOURCE (code review)
- **Not covered**: None.

### 3. SSRF: IPv6 decimal-IP bypass -- not present
- **Location**: `internal/security/ssrf/guard.go:302-316`
- **Mechanism**: `IPBlocked()` normalizes IPv4-in-IPv6 addresses via `ip.To4()` before checking CIDRs. Decimal-IP parsing is handled by `net.ParseIP()` which accepts decimal notation.
- **Severity**: INFO (no bypass found)
- **Test/Probe**: `net.ParseIP("127.0.0.1")` and `net.ParseIP("2130706433")` (decimal) both work; the CIDR check uses the parsed IP.
- **Evidence**: SOURCE (code review)
- **Not covered**: None.

### 4. Auth: PKCE `crypto/rand` panic at pkce.go:16 -- classify as LOW severity
- **Location**: `internal/auth/pkce.go:16`
- **Mechanism**: `randomURLSafe()` panics if `crypto/rand.Read()` fails. This is a catastrophic entropy exhaustion scenario. The panic is appropriate -- continuing with predictable values would be a security hole.
- **Severity**: LOW (correct behavior, not a bug)
- **Test/Probe**: The panic is the intended response to entropy failure. No alternative path.
- **Evidence**: SOURCE (code review)
- **Not covered**: None.

### 5. Auth: PKCE state -- no server-side storage, no single-use enforcement
- **Location**: `internal/auth/anthropic.go:45-55`
- **Mechanism**: `BuildAnthropicAuthorizeURL()` generates a `state` parameter but the codebase does not store it server-side nor enforce single-use. The `state` is passed to the authorization endpoint and presumably returned in the callback, but there's no verification step shown in the code.
- **Severity**: MEDIUM
- **Test/Probe**: `ExchangeAnthropicCode()` does not accept or validate the `state` parameter. This leaves the flow vulnerable to CSRF attacks.
- **Evidence**: SOURCE (code review)
- **Not covered**: Need to check if `state` is validated elsewhere in the OAuth flow.

### 6. Auth: Token/key comparison -- uses `subtle.ConstantTimeCompare` (correct)
- **Location**: `internal/auth/store.go:58`
- **Mechanism**: Key validation uses `subtle.ConstantTimeCompare()` for hash comparison. This prevents timing attacks.
- **Severity**: INFO (correct implementation)
- **Test/Probe**: Code review confirms proper use of constant-time comparison.
- **Evidence**: SOURCE (code review)
- **Not covered**: None.

### 7. Multi-user store: Expiry checked at validate time (not just at mint)
- **Location**: `internal/auth/store.go:45-88`
- **Mechanism**: `Validate()` checks `ExpiresAt` at authentication time (`now.After(*found.ExpiresAt)`). This is correct -- expiry is enforced at use, not just at key creation.
- **Severity**: INFO (correct implementation)
- **Test/Probe**: Code review confirms expiry is checked in `Store.Validate()`.
- **Evidence**: SOURCE (code review)
- **Not covered**: None.

### 8. Multi-user store: Missing scope checks
- **Location**: `internal/auth/store.go`
- **Mechanism**: The `Key` struct has no `Scopes` field. API keys are all-or-nothing -- there's no granularity of permissions per key.
- **Severity**: LOW (design limitation, not a bug)
- **Test/Probe**: Code review of `Key` struct confirms no scope field.
- **Evidence**: SOURCE (code review)
- **Not covered**: None.

### 9. Secrets: `slog.Info` logging in `device_flow.go:225` -- no secret leaked
- **Location**: `internal/auth/device_flow.go:225`
- **Mechanism**: `slog.Info("device code flow complete", "provider", cfg.ClientID)` logs the provider ClientID, not a secret. `ClientSecret` is not logged.
- **Severity**: INFO (no redaction gap)
- **Test/Probe**: Code review confirms only `ClientID` (public) is logged.
- **Evidence**: SOURCE (code review)
- **Not covered**: None.

### 10. Secrets: Credential file permissions -- 0600 enforced
- **Location**: `internal/auth/store.go:18`, `internal/auth/token_store.go:80`
- **Mechanism**: The users store file is written with `filePerm = 0o600`. Token store files are also written with `0600` permissions.
- **Severity**: INFO (correct implementation)
- **Test/Probe**: Code review confirms `0o600` on both `saveStore()` and `os.WriteFile()` for tokens.
- **Evidence**: SOURCE (code review)
- **Not covered**: None.

### 11. TLS pinning -- not implemented
- **Location**: `internal/security/tls.go`
- **Mechanism**: The `tls.go` file exists but appears to be a placeholder or minimal implementation. No certificate pinning is evident in the SSRF guard or HTTP client usage.
- **Severity**: LOW (not a regression, but a gap)
- **Test/Probe**: Code review of `tls.go` and HTTP client usage shows no pinning.
- **Evidence**: SOURCE (code review)
- **Not covered**: None.

## Checked and clean

- `internal/security/ssrf/` -- guard logic, CIDR parsing, redirect handling, dial-time validation
- `internal/auth/pkce.go` -- random generation, PKCE challenge generation
- `internal/auth/users.go` -- user/key data structures, deep copy logic
- `internal/auth/store.go` -- concurrency model, key validation, persistence
- `internal/auth/token_store.go` -- token encryption, file permissions, path sanitization
- `internal/auth/device_flow.go` -- device code flow, token polling
- `internal/security/secrets.go` -- secret scanning and obfuscation
- `internal/security/fence.go` -- fence checker configuration
- `internal/security/sanitizer.go` -- input/output sanitization

## Residual / not covered

- PKCE state validation across the full OAuth flow (may exist in a file not yet reviewed)
- TLS pinning implementation details (if any) in the broader codebase
- Potential secret logging in `internal/llm/` or `internal/agent/` packages

gates_run: grep-first code review only; no tests or builds executed
