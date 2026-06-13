# Security Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close 5 security gaps: TLS cert bypass in Flutter, PTY WebSocket origin bypass, notification CORS wildcard, missing config path expansion, and STT command injection.

**Architecture:** Each fix is independent. Apply the same security patterns already established elsewhere in the codebase (origin checking, proper CORS, `expandHome`, argument passing over interpolation).

**Tech Stack:** Go 1.22+, Dart/Flutter

---

## Investigation Results

| Issue | Risk Level | Fix Complexity |
|-------|-----------|----------------|
| Flutter TLS cert bypass | HIGH (local MITM) | Medium — needs cert pinning |
| PTY WebSocket origin bypass | MEDIUM (local) | Low — reuse existing `isLocalOrigin` |
| Notification CORS wildcard | MEDIUM | Low — remove manual header |
| `expandConfigPaths` gaps | LOW (misconfiguration) | Low — add missing path expansions |
| STT command injection | MEDIUM (defense-in-depth) | Low — pass path as argv |

---

### Task 1: Fix PTY WebSocket upgrader — restrict origins to localhost

**Files:**
- Modify: `internal/comm/http/pty_handler.go:16-20`

**Context:** The PTY WebSocket upgrader has `CheckOrigin: func(r *http.Request) bool { return true }` which allows connections from any origin. The main server in `server.go` has an `isLocalOrigin()` function that checks for localhost/127.0.0.1/::1.

- [ ] **Step 1: Read the current PTY handler and the existing origin check**

Read `internal/comm/http/pty_handler.go` and `internal/comm/http/server.go` to find `isLocalOrigin`.

- [ ] **Step 2: Replace the upgrader's CheckOrigin with local-only check**

In `internal/comm/http/pty_handler.go`, change the upgrader initialization from:

```go
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}
```

To reference the server's existing origin check. If `isLocalOrigin` is unexported, either:
- Export it as `IsLocalOrigin` in `server.go`, or
- Define a local helper:

```go
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        origin := r.Header.Get("Origin")
        if origin == "" {
            return true // non-browser clients
        }
        u, err := url.Parse(origin)
        if err != nil {
            return false
        }
        host := u.Hostname()
        return host == "localhost" || host == "127.0.0.1" || host == "::1"
    },
}
```

Add `"net/url"` to imports if not present.

- [ ] **Step 3: Verify build**

Run: `go build ./internal/comm/http/...`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add internal/comm/http/pty_handler.go
git commit -m "fix(security): restrict PTY WebSocket to local origins only"
```

---

### Task 2: Fix HTTP notification handler CORS wildcard

**Files:**
- Modify: `internal/comm/http/notification_handlers.go:141`

**Context:** The `ServeHTTP` method on the polling handler manually sets `Access-Control-Allow-Origin: *`, bypassing the server middleware's CORS logic which correctly echoes the specific origin for localhost.

- [ ] **Step 1: Read the notification handler and server CORS middleware**

Read `internal/comm/http/notification_handlers.go` around line 141 and `internal/comm/http/server.go` CORS middleware section.

- [ ] **Step 2: Remove the manual CORS header**

In `internal/comm/http/notification_handlers.go`, remove the line:
```go
w.Header().Set("Access-Control-Allow-Origin", "*")
```

If the handler is registered through the server's route system, the middleware chain will handle CORS. If it's registered directly, add the same origin-echo logic:

```go
origin := r.Header.Get("Origin")
if origin != "" {
    u, err := url.Parse(origin)
    if err == nil && isLocalhost(u.Hostname()) {
        w.Header().Set("Access-Control-Allow-Origin", origin)
        w.Header().Set("Access-Control-Allow-Credentials", "true")
    }
}
```

Where `isLocalhost` checks for localhost/127.0.0.1/::1 (reuse the pattern from Task 1).

- [ ] **Step 3: Verify build**

Run: `go build ./internal/comm/http/...`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add internal/comm/http/notification_handlers.go
git commit -m "fix(security): remove CORS wildcard from notification handler, use local-only origin echo"
```

---

### Task 3: Fix `expandConfigPaths` missing path fields

**Files:**
- Modify: `internal/config/config.go:137-170`

**Context:** `expandConfigPaths` uses `expandHome` to expand `~` in path fields but misses several. The missing paths are:
- `cfg.Projects.BaseDir`
- `cfg.OAuth.TokenDir`
- `cfg.Bots.DataDir`

Read `internal/config/schema.go` to verify these field names.

- [ ] **Step 1: Read current `expandConfigPaths` and verify field names**

Read `internal/config/config.go` lines 137-170 and `internal/config/schema.go` to confirm the field paths.

- [ ] **Step 2: Add missing path expansions**

After the existing expansion block, add:

```go
// Projects
if cfg.Projects.BaseDir != "" {
    cfg.Projects.BaseDir = expandHome(cfg.Projects.BaseDir)
}

// OAuth
if cfg.OAuth.TokenDir != "" {
    cfg.OAuth.TokenDir = expandHome(cfg.OAuth.TokenDir)
}

// Bots
if cfg.Bots.DataDir != "" {
    cfg.Bots.DataDir = expandHome(cfg.Bots.DataDir)
}
```

Verify the field paths match `schema.go` — read the schema to confirm `cfg.OAuth`, `cfg.Bots`, and `cfg.Projects` struct field names.

- [ ] **Step 3: Verify build**

Run: `go build ./internal/config/...`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "fix(config): expand tilde in Projects.BaseDir, OAuth.TokenDir, and Bots.DataDir"
```

---

### Task 4: Fix STT command injection — pass filepath as argument

**Files:**
- Modify: `internal/stt/native.go:196-212`

**Context:** `transcribeWithPython` interpolates `filePath` into a Python script using `%q`:
```go
script := fmt.Sprintf(`... with sr.AudioFile(%q) as source: ...`, filePath)
cmd := exec.Command("python3", "-c", script)
```

While Go's `%q` provides robust escaping, this is a defense-in-depth violation. The fix: read the file path from `sys.argv[1]` instead.

- [ ] **Step 1: Read the current `transcribeWithPython` implementation**

Read `internal/stt/native.go` lines 196-212.

- [ ] **Step 2: Rewrite to pass filepath as argument**

Change from string interpolation to argument passing:

```go
script := `
import speech_recognition as sr
import sys
if len(sys.argv) < 2:
    sys.exit(1)
r = sr.Recognizer()
with sr.AudioFile(sys.argv[1]) as source:
    audio = r.record(source)
try:
    text = r.recognize_google(audio)
    print(text)
except sr.UnknownValueError:
    print("", end="")
except Exception:
    print("", end="")
    sys.exit(1)
`
cmd := exec.Command("python3", "-c", script, filePath)
```

Note: `exec.Command("python3", "-c", script, filePath)` passes `filePath` as `sys.argv[1]` to the Python interpreter when using `-c`. This is standard Python behavior — arguments after `-c SCRIPT` go to `sys.argv`.

- [ ] **Step 3: Remove `fmt` import if no longer needed**

Check if `fmt` is still used elsewhere in `native.go`. If `fmt.Sprintf` was the only use, remove the import. Otherwise keep it.

- [ ] **Step 4: Verify build**

Run: `go build ./internal/stt/...`
Expected: success

- [ ] **Step 5: Commit**

```bash
git add internal/stt/native.go
git commit -m "fix(stt): pass audio filepath as sys.argv instead of interpolating into Python script"
```

---

### Task 5: Fix Flutter TLS cert bypass — pin daemon self-signed cert

**Files:**
- Modify: `ui/flutter_ui/lib/services/api_client.dart:49-60`
- Modify: `ui/flutter_ui/lib/services/websocket_service.dart:450-456`

**Context:** Both the HTTP client and WebSocket client accept any self-signed cert for localhost. The daemon auto-generates a self-signed cert to `~/.meept/tls/` when `auto_tls_cert: true`. There is already a `pkg/tlsutil/pin.go` with cert pinning infrastructure in Go, but Flutter needs its own approach.

**Approach:** Read the daemon's cert fingerprint from the local cert file and compare it in the callback. This provides pinning without requiring the Flutter app to know the cert at build time.

- [ ] **Step 1: Read current TLS bypass in both files**

Read `ui/flutter_ui/lib/services/api_client.dart` lines 49-60 and `ui/flutter_ui/lib/services/websocket_service.dart` lines 450-456.

- [ ] **Step 2: Read the daemon's cert generation to find the cert file path**

Read `internal/comm/http/server.go` — search for `auto_tls_cert`, `tlsCertPath`, or the cert generation logic to find where the cert PEM is stored on disk. Likely `~/.meept/tls/cert.pem` or similar.

- [ ] **Step 3: Add cert pinning helper to `api_client.dart`**

Add a method that reads the daemon's cert, computes its SHA-256 fingerprint, and validates incoming certs against it:

```dart
import 'dart:io';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'dart:convert';

class DaemonCertPinner {
  static String? _cachedFingerprint;

  static Future<String?> _getCertFingerprint() async {
    if (_cachedFingerprint != null) return _cachedFingerprint;

    final certPath = Platform.environment['HOME'] != null
        ? '${Platform.environment['HOME']}/.meept/tls/cert.pem'
        : null;
    if (certPath == null) return null;

    try {
      final certFile = File(certPath);
      if (!await certFile.exists()) return null;

      final certBytes = await certFile.readAsBytes();
      _cachedFingerprint = sha256.convert(certBytes).toString();
      return _cachedFingerprint;
    } catch (_) {
      return null;
    }
  }

  static Future<bool> validateCert(X509Certificate cert, String host) async {
    if (host != 'localhost' && host != '127.0.0.1' && host != '::1') {
      return false; // non-local hosts must use proper TLS
    }

    final expected = await _getCertFingerprint();
    if (expected == null) {
      return false; // no pinned cert, reject
    }

    final certDer = cert.der;
    final actual = sha256.convert(certDer).toString();
    return actual == expected;
  }
}
```

**Note:** This uses `package:crypto` — check if it's already in `pubspec.yaml`. If not, add it. Alternatively, use `dart:io`'s raw DER comparison without hashing.

- [ ] **Step 4: Replace the `badCertificateCallback` in `api_client.dart`**

Change from:
```dart
client.badCertificateCallback =
    (X509Certificate cert, String host, int port) =>
        host == 'localhost' || host == '127.0.0.1' || host == '::1';
```

To:
```dart
client.badCertificateCallback = (X509Certificate cert, String host, int port) {
  // Synchronous check — full async pinning requires a different approach
  // For now, restrict to localhost and document the limitation
  return host == 'localhost' || host == '127.0.0.1' || host == '::1';
};
```

**Important:** `badCertificateCallback` is synchronous, so we can't do async file I/O. The proper fix is to load the cert fingerprint at app startup (in `StorageService` or `ApiClient` constructor) and use the cached value:

```dart
class ApiClient {
  static String? _daemonCertFingerprint;

  static Future<void> initCertPinning() async {
    _daemonCertFingerprint = await DaemonCertPinner._getCertFingerprint();
  }

  // In constructor:
  client.badCertificateCallback = (cert, host, port) {
    if (host != 'localhost' && host != '127.0.0.1' && host != '::1') {
      return false;
    }
    if (_daemonCertFingerprint == null) {
      return false; // no cert pinned, reject
    }
    final actual = sha256.convert(cert.der).toString();
    return actual == _daemonCertFingerprint;
  };
}
```

Call `ApiClient.initCertPinning()` during app startup in `main.dart` before creating the client.

- [ ] **Step 5: Apply the same pinning to `websocket_service.dart`**

The WebSocket service creates its own `HttpClient` for the WebSocket upgrade. Apply the same cached fingerprint check.

- [ ] **Step 6: Add `package:crypto` to pubspec.yaml if not present**

Check `ui/flutter_ui/pubspec.yaml` for `crypto:`. If missing, add it under dependencies.

- [ ] **Step 7: Verify Flutter analyze**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no errors

- [ ] **Step 8: Commit**

```bash
git add ui/flutter_ui/lib/services/api_client.dart \
        ui/flutter_ui/lib/services/websocket_service.dart \
        ui/flutter_ui/pubspec.yaml
git commit -m "fix(security): pin daemon TLS certificate in Flutter HTTP and WebSocket clients"
```

---

### Task 6: Final verification

- [ ] **Step 1: Run Go build and tests**

Run: `go build ./... && go test ./internal/comm/http/... ./internal/config/... ./internal/stt/... -count=1`
Expected: all pass

- [ ] **Step 2: Run Flutter analyze**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no errors

- [ ] **Step 3: Commit any remaining changes**

```bash
git add -A
git commit -m "chore: security hardening verification"
```
