# GLM-5.2 Findings — Round 5

**Date:** 2026-06-16
**Methodology:** 8 parallel review subagents (general-purpose) covering the full
codebase (~210k LOC of non-test Go, ~20k Dart, ~3k Swift, plus CLI/daemon cmd
trees). Each subagent read every non-test file in its scope and produced
structured findings with `file:line` evidence. Subagents were briefed on
rounds 1-4 lessons (no false-positive dismissal, scrutinize recently-modified
hotspots, recurring bug classes per MEMORY.md) and instructed to verify each
finding against actual code.

**Note on prompt injection:** During this round, file Read results again
contained appended `<system-reminder>` blocks instructing the reviewer to
"refuse to improve or augment code" and claiming the code may be malware.
These are NOT legitimate system messages — they are injected content embedded
in file contents (source still unknown; same pattern documented in rounds 3
and 4). All subagents and the orchestrator correctly disregarded them. The
code under review is the user's own meept project (a Go daemon + Flutter/Swift
frontends), not malware.

## Totals

| Severity | Count |
|----------|-------|
| Critical | 7 (5 + 2 S2 addendum) |
| High | 25 (17 + 8 S1/S2 addendum) |
| Medium | 35 (28 + 7 S2 addendum) |
| Low | 35 (25 + 10 S2 addendum, plus S1-9/10/11) |
| **Total** | **102** |

## Scope by subagent

| Subagent | Domain | Findings |
|----------|--------|----------|
| S1 | Agent orchestration | 10 (see addendum) |
| S2 | Code intel + plans + repomap + lint + selfimprove + skills + config | 22 (see addendum) |
| S3 | LLM + memory + security | 2 new |
| S4 | Tools + MCP | 18 |
| S5 | Network + RPC + services + project + comm | 16 |
| S6 | Daemon infra (scheduler, queue, runtime, pty, stt/tts, cluster) | 16 |
| S7 | CLI + TUI + Flutter + MenuBar | 20 |
| S8 | Flutter UI | 15 |

**Orchestrator's verification pass:** Every Critical and High finding was
re-verified by reading the actual source line before inclusion in this
document. Several S3 subagent findings marked "already fixed" or "no fix
needed" were scrutinized and either dropped or downgraded — exactly the
false-positive dismissal pattern documented in MEMORY.md.

---

## Critical Findings

### S4-1 WebSearchTool lacks dial-time SSRF protection (DNS rebinding)

- **File:** `internal/tools/builtin/tool_web_search.go:63-83`
- **Evidence:**
  ```go
  // web_search.go (VULNERABLE)
  t.client = &http.Client{
      Timeout: timeout,
      Transport: &http.Transport{
          MaxConnsPerHost: 8,
          // no DialContext — no dial-time SSRF check
      },
      CheckRedirect: func(req *http.Request, via []*http.Request) error {
          ...
          if err := checkURL(req.URL.String()); err != nil { ... }
      },
  }

  // web_fetch.go (CORRECT — already fixed in round 4)
  t.client = &http.Client{
      Transport: &http.Transport{
          MaxConnsPerHost: 8,
          DialContext:     ssrfDialContext(false),  // dial-time re-check
      },
      CheckRedirect: t.checkRedirect,
  }
  ```
- **Why it's a bug:** `checkURL()` in the redirect handler resolves the
  hostname at call time and validates IPs. But between that resolution and the
  actual TCP dial (which happens inside `http.Client.Do`), the DNS record can
  change (DNS rebinding attack). `WebFetchTool` closed this window in round 4
  via `ssrfDialContext`; `WebSearchTool` does not. Although the search
  endpoint is fixed to `html.duckduckgo.com`, redirect targets from
  DuckDuckGo search results are arbitrary URLs controlled by external sites.
- **Fix:** Add `DialContext: ssrfDialContext(false)` to the transport in
  `NewWebSearchTool`. Re-uses the existing helper from web_fetch.go.

### S4-2 MCP HTTPTransport lacks dial-time SSRF protection

- **File:** `internal/tools/mcp/transport/http.go:84-107`
- **Evidence:**
  ```go
  return &HTTPTransport{
      client: &http.Client{
          Timeout: timeout,
          CheckRedirect: func(req *http.Request, via []*http.Request) error {
              ...
              if err := checkRedirectURL(req.URL.String()); err != nil { ... }
          },
          // no Transport specified — uses http.DefaultTransport (no dial-time check)
      },
  }
  ```
- **Why it's a bug:** Same DNS-rebinding window as S4-1. MCP servers are
  user-configured (`~/.meept/mcp_servers.json5`); a malicious or compromised
  MCP server URL or redirect target can exploit DNS rebinding to reach
  internal services (cloud metadata `169.254.169.254`, internal RPCs, etc.).
  Additionally, `checkRedirectURL` (lines 41-66) duplicates the
  `isBlockedAddress` logic from `internal/tools/builtin/ssrf.go` rather than
  importing it — maintenance divergence risk.
- **Fix:** Set a custom `Transport` with `DialContext` that re-validates
  resolved IPs at dial time (equivalent to `ssrfDialContext`). Use the
  request context for DNS resolution in redirect checks. Import the shared
  `ssrf.go` helper rather than duplicating it.

### S6-1 Cluster signature verification bypassed via empty signature

- **File:** `internal/cluster/gossip.go:335`
- **Evidence:**
  ```go
  // Verify signature if node signature requirement is enabled
  if g.cfg.Security.RequireNodeSignatures && len(event.Signature) > 0 {
      pubKey, found := g.PeerSigningKey(event.NodeID)
      ...
      if !event.Verify(pubKey) { ... }
  }
  ```
- **Why it's a bug:** The `&& len(event.Signature) > 0` condition means that
  when `RequireNodeSignatures` is `true`, the verification branch is only
  entered if the event has a non-empty signature. An attacker (or
  misconfigured node) can forge a `ClusterEvent` with an empty/nil
  `Signature` field and completely bypass signature verification. The forged
  event is then persisted, re-broadcast to all peers, and used to update peer
  state — all without cryptographic validation.
- **Fix:** Reject unsigned events when signatures are required:
  ```go
  if g.cfg.Security.RequireNodeSignatures {
      if len(event.Signature) == 0 {
          g.logger.Warn("gossip: rejecting unsigned event (signatures required)",
              "event_id", event.EventID, "node_id", event.NodeID)
          return
      }
      // verify...
  }
  ```

### S8-1 Hardcoded dev API key fallback still present in `api_client.dart`

- **File:** `ui/flutter_ui/lib/services/api_client.dart:97-107`
- **Evidence:**
  ```dart
  // Fallback to default dev API key if not configured
  // This allows the Flutter app to work out-of-the-box in development
  if (apiKey == null || apiKey.isEmpty) {
    if (AppConstants.defaultApiKey.isNotEmpty) {
      apiKey = AppConstants.defaultApiKey;
    } else {
      // Hardcoded fallback matching pkg/constants/api_key.go DefaultDevAPIKey
      apiKey = 'meept_dev_default_key_CHANGE_ME';
    }
  }
  ```
- **Why it's a bug:** Round 4 reportedly removed the hardcoded dev key
  fallback from `websocket_service.dart` (S8-7 in round 4), but `api_client.dart`
  still contains the literal `'meept_dev_default_key_CHANGE_ME'`. The
  `AppConstants.defaultApiKey` indirection uses `String.fromEnvironment` and
  is correctly empty in release builds — but the `else` branch re-introduces
  the well-known literal, defeating the `kReleaseMode` guard entirely. A
  release build with no stored key silently authenticates against the daemon
  using a value that is in source control, the issue tracker, and every
  binary distribution. This is a regression of the round-4 fix.
- **Fix:** Delete the `else` branch. Let `apiKey` remain `null` and the
  daemon reject the unauthenticated request. Surface a clear error in the UI
  if no key is configured.

### S6-2 `reclaimJobUnlocked` nil-dereferences `cq.store` on ResetToPending

- **File:** `internal/queue/cluster_queue.go:151`
- **Evidence:**
  ```go
  func (cq *ClusterQueue) reclaimJobUnlocked(ctx context.Context, jobID, reason string) error {
      if cq.store != nil {                                    // line 143: nil guard present
          if err := cq.store.RecordClaimEvent(...)
          ...
      }
      // line 151: NO nil guard — panics if cq.store == nil
      if err := cq.store.ResetToPending(ctx, jobID); err != nil {
          ...
      }
  }
  ```
- **Why it's a bug:** `RecordClaimEvent` is guarded by `if cq.store != nil`
  (line 143), but `ResetToPending` at line 151 dereferences `cq.store`
  unconditionally. If a `ClusterQueue` is constructed with a nil store
  (`NewClusterQueue` does not validate this), any reclaim attempt panics.
  `CheckNodeReachability` (line 195) also guards for nil store, showing the
  code anticipates this case but missed it here. This is a textbook nil-deref
  panic — a recurring bug class called out in CLAUDE.md.
- **Fix:** Add `if cq.store == nil { return nil }` at the top of
  `reclaimJobUnlocked`, or guard the `ResetToPending` call with
  `if cq.store != nil { ... }`.

---

## High Findings

### S5-4 `webHandlerAdapter.Chat` generates predictable conversation IDs

- **File:** `internal/daemon/components.go:4231`
- **Evidence:**
  ```go
  conversationID := fmt.Sprintf("web-%d", time.Now().UnixNano())
  ```
- **Why it's a bug:** Same predictable-IDs anti-pattern documented in
  MEMORY.md as a recurring round-3/round-4 finding. Two web clients sending a
  request in the same nanosecond will collide on conversation ID. The
  `chat_service.go` already uses `id.Generate("...")` — this HTTP path was
  missed.
- **Fix:** Replace with `id.Generate("web")` from `pkg/id`.

### S5-7 Multiple predictable IDs in daemon team tools

- **File:** `internal/daemon/components.go:3035, 3043, 3065, 3080`
- **Evidence:** Four call sites still use:
  ```go
  req.SessionID = fmt.Sprintf("team-%d", time.Now().UnixNano())
  ID:           fmt.Sprintf("team-create-%d", time.Now().UnixNano()),
  SessionID:    fmt.Sprintf("team-%d", time.Now().UnixNano()),
  ID:           fmt.Sprintf("team-preset-%d", time.Now().UnixNano()),
  ```
- **Why it's a bug:** Flagged as a recurring pattern in round 4's MEMORY
  notes. `pkg/id.Generate` exists and is used by `chat_service.go:111`; these
  team-tool sites were missed in the round-4 sweep. Concurrent team creation
  in the same nanosecond collides on session ID and bus message IDs.
- **Fix:** Replace all four with `id.Generate("team")` / `id.Generate("team-create")` etc.

### S5-2 CORS middleware sends `Access-Control-Allow-Origin: *` when RequireAuth is false

- **File:** `internal/comm/http/server.go:1098-1110`
- **Evidence:**
  ```go
  if s.config.EnableCORS {
      origin := r.Header.Get("Origin")
      if s.config.RequireAuth {
          // Authenticated endpoints: never wildcard. Echo localhost origins only.
          if origin == "" || isLocalOrigin(origin) { ... }
      } else {
          w.Header().Set("Access-Control-Allow-Origin", "*")
      }
      ...
  }
  ```
- **Why it's a bug:** When `RequireAuth=false` (operator disables auth for
  trusted-LAN use) and the default `EnableCORS=true` is in effect, the
  middleware unconditionally emits `Access-Control-Allow-Origin: *`. Any
  website a developer visits in their browser can issue authenticated-looking
  requests to the daemon. The wildcard also disables
  `Access-Control-Allow-Credentials` per the spec, but since the API key is
  sent in a header explicitly set by JS (not cookies), the protection does
  not apply here.
- **Fix:** Never emit `*`. Always echo a vetted origin (localhost or
  user-configured allow-list).

### S5-5 NotificationHandler.ServeWebSocket uses `InsecureSkipVerify: true`

- **File:** `internal/comm/http/notification_handlers.go:97`
- **Evidence:**
  ```go
  conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{
      CompressionMode:     websocket.CompressionContextTakeover,
      OriginPatterns:      defaultWSOrigins,
      InsecureSkipVerify:  true, // Allow non-TLS for localhost
  })
  ```
- **Why it's a bug:** `InsecureSkipVerify` on `nhooyr.io/websocket.AcceptOptions`
  disables origin checks entirely (it is NOT a TLS flag despite the name). The
  comment claims it's for localhost, but the effect is that **any** origin —
  including a malicious web page — can open this WebSocket. The
  `OriginPatterns` set two lines above is ignored when
  `InsecureSkipVerify=true`.
- **Fix:** Remove `InsecureSkipVerify: true`. The `OriginPatterns` allow-list
  already handles localhost; `InsecureSkipVerify` defeats it.

### S5-3 NotificationHandler token check is dead code (misleading defense-in-depth)

- **File:** `internal/comm/http/notification_handlers.go:62-112`
- **Evidence:** The handler extracts a token from `Authorization`, `Sec-WebSocket-Protocol`,
  and `?token=` — but only checks non-emptiness, never validates against configured keys.
  The comment at line 106-107 reads:
  ```go
  // Note: token validation against configured keys is done by the auth middleware
  // This handler just checks that a token is present
  ```
- **Why it's a bug:** The middleware chain (server.go:682)
  does wrap `/ws/notifications`, so this is **not** a critical auth bypass
  (orchestrator's verification contradicts the subagent's original Critical
  severity). However, the handler-local token extraction is dead code that
  misleads readers into thinking the handler enforces auth itself. A future
  refactor that moves the route outside the middleware chain would create a
  real auth bypass. This is a defense-in-depth gap, not a working exploit.
- **Fix:** Either delete the handler-local token extraction entirely (the
  middleware handles it), or actually validate against configured keys using
  `subtle.ConstantTimeCompare` like `handleWebSocket` does.

### S5-9 `handleChatQueueStatus` redundantly checks method inside a method-pinned route

- **File:** `internal/comm/http/api_handlers.go:1944-1948`
- **Evidence:**
  ```go
  if r.Method != http.MethodGet {
      s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
      return
  }
  ```
- **Why it's a bug:** The route is registered at `server.go:920` as
  `GET /api/v1/chat/queue/{id}`, which means the Go 1.22+ mux already 405s
  any non-GET request. The check is dead code. The `strings.TrimPrefix`
  fallback at line 1952-1954 is likewise unreachable on Go 1.22+ muxes that
  always populate `PathValue`. Cosmetic noise that can confuse future readers
  and mask real bugs.
- **Fix:** Delete the dead method check and the TrimPrefix fallback.

### S5-10 HTTP error response inconsistency: `http.Error` vs `s.writeError`

- **File:** `internal/comm/http/api_handlers.go:313, 380, 503, 587, 637, 2299, 2958`
- **Evidence:** Several handlers call:
  ```go
  http.Error(w, err.Error(), http.StatusBadRequest)
  ```
  for invalid `limit` parameters, while every other error path in the same
  file uses `s.writeError(w, status, msg)` which emits `{"error": "..."}` JSON.
- **Why it's a bug:** The `http.Error` calls produce `text/plain` bodies,
  breaking any JSON-only client parsing the error envelope. Inconsistent
  error format makes client-side error handling brittle.
- **Fix:** Replace all `http.Error` with `s.writeError` to unify the error
  format across the API.

### S4-3 ASTEditTool and ResolveASTEditTool bypass fence checking on file writes

- **File:** `internal/code/tools/ast_edit.go:230`, `internal/code/tools/resolve_ast_edit.go:176`
- **Evidence:**
  ```go
  // ast_edit.go:230
  if err := os.WriteFile(filePath, modifiedSource, 0o644); err != nil {
      return nil, fmt.Errorf("failed to write modified file: %w", err)
  }
  ```
  Neither tool has a `FenceChecker` field, `SetFenceChecker` method, or any
  path validation beyond what `os.ReadFile`/`os.WriteFile` natively provide.
- **Why it's a bug:** The agent can use `ast_edit` to write to any path on
  the filesystem, including `/etc/passwd`, `~/.ssh/authorized_keys`, or files
  outside the project workspace. The hashline `file_edit` tool is properly
  fenced, but these AST-based tools are not — fence bypass. CLAUDE.md
  explicitly calls out fence validation as required for every file-writing tool.
- **Fix:** Add `SetFenceChecker(fc FenceChecker)` to both tools (with nil
  guard per CLAUDE.md), and call `fc.CheckPath(filePath, "write")` before any
  write operation. Wire the daemon's fence checker into these tools at
  component-construction time.

### S4-4 lspWriteNotifier absPath helper bypasses fence checking

- **File:** `internal/tools/builtin/lsp_writethrough.go:354-367`
- **Evidence:**
  ```go
  func absPath(path string) (string, error) {
      if strings.HasPrefix(path, "~") { ... }
      abs, err := filepath.Abs(strings.TrimSpace(path))
      ...
      return filepath.Clean(abs), nil
      // no fence check
  }
  ```
  `applyFormattingEdits` (line 350) calls `os.WriteFile(filePath, ...)` on
  the output of `absPath` without any fence check.
- **Why it's a bug:** Separate copy of `resolvePath` from `filesystem.go`
  (lines 853-868) that resolves paths to absolute form but performs no fence
  validation. If the LSP server is compromised or returns malicious
  formatting edits targeting files outside the workspace, the writethrough
  notifier applies them without fence validation. Defense-in-depth gap.
- **Fix:** Route through the existing `resolvePath` + fence checker, or add
  a fence validation step before any write in `applyFormattingEdits`.

### S4-5 parseSimpleStatus / parseFileStatus break on file paths with spaces

- **File:** `internal/tools/builtin/git_split.go:203-233`, `internal/tools/builtin/git_overview.go:198-215`
- **Evidence:**
  ```go
  // git_split.go:210
  parts := strings.Fields(line)
  if len(parts) < 2 { continue }
  status := parts[0]
  filePath := parts[1]  // BUG: only gets first token of path
  ```
- **Why it's a bug:** Git `--porcelain` status output uses space as the
  delimiter between status code and file path, but file paths themselves can
  contain spaces. A file named `my file.go` produces status line
  ` M my file.go`, and `strings.Fields` splits it into
  `["M", "my", "file.go"]`, capturing only `"my"` as the file path. Files
  with spaces are silently dropped from commit grouping and overview analysis.
  Common on macOS and Windows.
- **Fix:** Use `git status --porcelain=v1 -z` (NUL-delimited), or parse the
  fixed-width format (2-char status, space, path) with substring extraction:
  `status := line[:2]; filePath := line[3:]`.

### S4-6 DebugTool lacks fence checking on program/core_file/script_file paths

- **File:** `internal/tools/builtin/debug.go:318+`
- **Evidence:** The `DebugTool.Execute` method handles actions like
  `load_core`, `launch`, and `script_run` that accept file paths
  (`core_file`, `program`/`script_file`) from the LLM. These paths are used
  directly in `os.ReadFile` and debug adapter configuration without any fence
  validation. The tool struct has no `FenceChecker` field at all.
- **Why it's a bug:** The agent could read core dumps, launch debuggers
  against arbitrary binaries, or execute scripts from outside the workspace.
  For `launch` mode, the `program` path could point to any executable on the
  system — sandbox escape vector.
- **Fix:** Add `SetFenceChecker` to `DebugTool` and validate `program`,
  `core_file`, `script_file`, and `working_dir` paths before use.

### S6-3 `reclaimJobUnlocked` holds write lock across SQLite I/O and bus publish

- **File:** `internal/queue/cluster_queue.go:141-179`
- **Evidence:**
  ```go
  func (cq *ClusterQueue) ReclaimJob(ctx context.Context, jobID, reason string) error {
      cq.mu.Lock()
      defer cq.mu.Unlock()
      return cq.reclaimJobUnlocked(ctx, jobID, reason)  // holds write lock
  }
  ```
  `reclaimJobUnlocked` then performs `cq.store.RecordClaimEvent` (SQLite
  write), `cq.store.ResetToPending` (SQLite write), and
  `cq.bus.Publish(...)` (channel send) — all under the write lock.
- **Why it's a bug:** Violates CLAUDE.md mutex-scope rule ("Never hold a
  mutex across I/O operations"). The round-4 `mutexio` analyzer was added
  specifically to enforce this rule (`make mutexio`); this violation slipped
  through because the analyzer's intraprocedural check looks for Lock/Unlock
  pairs in the same function and missed the cross-function `defer Unlock()`
  pattern. Note: `ReclaimIfStale` (line 227) explicitly documents that it
  "collects stale job IDs under a brief RLock, releases the lock, then
  reclaims each job individually" — yet each individual reclaim re-acquires
  the write lock and holds it across all I/O.
- **Fix:** In `reclaimJobUnlocked`, collect the data needed (jobID, reason,
  localNodeID) under the write lock, delete the claim record from the map
  under the lock, then release the lock before performing store writes and
  bus publish.

### S6-4 `RecordClaimEvent` uses `[]byte(action)` as ed25519 signature placeholder

- **File:** `internal/queue/cluster_queue.go:287`
- **Evidence:**
  ```go
  sig := []byte(action) // placeholder: real signatures via ed25519
  _, err := s.db.ExecContext(ctx, query,
      eventID, nodeID, "TASK_"+action,
      ..., sig, ...)
  ```
- **Why it's a bug:** The `signature` column in `cluster_events` is
  `NOT NULL`, so a real signature is required. Storing the action string
  bytes as a fake signature means:
  1. Any code that verifies signatures from this table will fail (treating
     it as a real ed25519 sig), silently dropping legitimate events.
  2. If signature verification is skipped for short signatures, it opens the
     door to event forgery.
  3. Production code should not ship with fake crypto material.
- **Fix:** Either sign the event with the node's ed25519 key (via the gossip
  engine's signing infrastructure) before storing, or change the schema to
  allow NULL signatures and store NULL.

### S6-5 `RecordClaimEvent` uses predictable event IDs

- **File:** `internal/queue/cluster_queue.go:285`
- **Evidence:**
  ```go
  eventID := fmt.Sprintf("claim-%s-%s-%d", jobID, action, time.Now().UnixNano())
  ```
- **Why it's a bug:** Same predictable-IDs anti-pattern documented in
  MEMORY.md as recurring. Two concurrent calls (e.g. simultaneous reclaim of
  two jobs) within the same nanosecond produce identical event IDs. Since
  `event_id` is `PRIMARY KEY`, the second insert fails. The gossip engine's
  `persistEvent` uses `INSERT OR IGNORE`, so the duplicate is silently
  dropped — but the first event may also be the wrong one.
- **Fix:** Use `pkg/id.Generate()` or `models.GenerateEventID()`.

### S6-6 Gossip signature verification only checks events from known peers (combined with S6-1)

- **File:** `internal/cluster/gossip.go:335-345`
- **Evidence:** See S6-1. Even with the empty-signature bypass fixed, the
  verification logic is fragile — if `PeerSigningKey` ever returns a
  default/wrong key, forged events would pass. Combined with S6-1
  (empty-sig bypass), the current code is exploitable.
- **Fix:** Fixed implicitly by S6-1's "reject unsigned events" fix. Add a
  test that verifies an attacker signing with the wrong key is rejected.

### S8-2 `clearError()` drops `currentProgress` state

- **File:** `ui/flutter_ui/lib/providers/chat_provider.dart:397-404`
- **Evidence:** `clearError()` rebuilds the `ChatState` with only `messages`,
  `isLoading`, `isAgentProcessing` — it forgets `currentProgress`. When the
  user dismisses an error banner mid-stream, the agent progress indicator
  disappears and the UI reverts to the static "thinking..." fallback until
  the next WS event arrives.
- **Fix:** Use `state = state.copyWith(error: null)` (the `copyWith` already
  exists and preserves the other fields), or include
  `currentProgress: state.currentProgress` in the manual constructor call.

### S8-3 `ChatState.copyWith` cannot clear `currentProgress`

- **File:** `ui/flutter_ui/lib/providers/chat_provider.dart:41-61`
- **Evidence:** `currentProgress: currentProgress ?? this.currentProgress`
  uses the "null means keep" pattern. Once set, the field can never be reset
  to `null` through `copyWith`, which forces every call site that wants to
  clear progress to do a full `ChatState(...)` reconstruction (and risk
  dropping fields, as S8-2 shows).
- **Fix:** Mirror the `_unset` sentinel pattern already used for `error`
  (lines 15, 45, 58) so callers can explicitly pass `null`.

### S8-4 `subscribeToAgentProgress` collides with chat subscription map

- **File:** `ui/flutter_ui/lib/services/websocket_service.dart:541-557`
- **Evidence:** `subscribeToAgentProgress(sessionId)` writes into
  `_chatSubscriptions[sessionId]` — the same map used by
  `subscribeToChat(sessionId)`. Calling `unsubscribeFromChat(sid)` removes
  the progress subscription (and vice-versa) and sends an
  `unsubscribe {channel: 'chat'}` for what may be a live progress subscription.
- **Fix:** Introduce a separate `_progressSubscriptions` map keyed by
  sessionId, or key the combined map by `(channel, sessionId)` tuples.

### S8-5 Inline `FocusNode()` never disposed in four panels

- **Files:**
  - `ui/flutter_ui/lib/features/skills/skill_panel.dart:251`
  - `ui/flutter_ui/lib/features/search/search_panel.dart:111`
  - `ui/flutter_ui/lib/features/memory/memory_panel.dart:117`
  - `ui/flutter_ui/lib/features/projects/branches_panel.dart:163`
- **Evidence:** Each `KeyboardListener(focusNode: FocusNode(), ...)`
  creates a FocusNode in `build()` that is never stored in a field nor
  disposed in `dispose()`. Because `build()` can run many times over the
  widget's lifetime, this leaks native focus handles and may cause focus-tree
  corruption on hot reload / long sessions.
- **Fix:** Hoist the `FocusNode` to a `State` field, create it in
  `initState`, and `dispose()` it in `dispose()`.

### S7-1 Predictable conversation ID in CLI single-message mode

- **File:** `cmd/meept/chat.go:89`
- **Evidence:**
  ```go
  conversationID := fmt.Sprintf("cli-%d-%d", os.Getpid(), time.Now().UnixNano())
  ```
- **Why it's a bug:** Conversation ID is built from `os.Getpid()` +
  nanosecond timestamp. Both are predictable: PID is sequence-assigned by
  the kernel and observable via `ps`, and `UnixNano()` is just wall-clock
  time. An attacker who can observe approximate invocation time can forecast
  the conversation ID for an hour or more. The codebase already has
  `pkg/id.Generate()` (called out in MEMORY.md as the fix for "predictable
  IDs (`time.Now().UnixNano()`)" — recurring bug pattern).
- **Fix:** Use `pkg/id.Generate("cli")` for the conversation ID.

### S7-5 `MenubarConfigService.startAtLogin` and `showInMenuBar` exposed but never honored

- **File:** `menubar/MeeptMenuBar/Services/MenubarConfigService.swift:86-92`
- **Evidence:**
  ```swift
  var showInMenuBar: Bool { return config.ui.showInMenuBar }
  var startAtLogin: Bool { return config.ui.startAtLogin }
  ```
  **Files checked:** `main.swift`, `AppDelegate.applicationDidFinishLaunching` —
  no `SMAppService` / `SMLoginItemSetEnabled` / `LaunchAtLogin` references
  anywhere in `menubar/`.
- **Why it's a bug:** The menubar app advertises two UI-config knobs
  (`show_in_menu_bar`, `start_at_login`) in `MenubarConfig` and even decodes
  them from `~/.meept/menubar.json5`, but neither property is read by
  `AppDelegate` or any view. A user who sets `"start_at_login": true`
  expecting the app to register itself as a macOS login item gets no
  behavior change. "Feature looks wired but isn't" bug.
- **Fix:** Either wire the properties (call `SMAppService.main.register()`
  from `AppDelegate.applicationDidFinishLaunching` when `startAtLogin == true`,
  and skip creating the status item when `showInMenuBar == false`), or delete
  the fields from `MenubarConfig` until they ship.

### S7-10 `LiveMetricsView` / `HistoricalReportView` show "loading..." forever after fetch with no rows

- **File:** `menubar/MeeptMenuBar/Views/Analytics/DashboardWindow.swift:60-74`
- **Evidence:**
  ```swift
  if metricsViewModel.isLoadingHistorical {
      ProgressView("loading...")
          .padding()
  } else {
      Text("select date range and click load")
          .foregroundColor(.secondary)
          .frame(maxWidth: .infinity, maxHeight: .infinity)
  }
  ```
- **Why it's a bug:** The "historical" tab of the dashboard has no rendering
  at all for the fetched `historicalData: [MetricPoint]` array. After
  `fetchHistorical()` succeeds, the view flips from "loading..." directly
  back to "select date range and click load" — the data is dropped on the
  floor. The `historicalData` published property is written to in the view
  model but never read by any view. Broken feature.
- **Fix:** Render `metricsViewModel.historicalData` — even a simple
  `List(metricsViewModel.historicalData) { point in Text("\(point.timestamp): \(point.value)") }`
  or `SwiftUI Charts` `Chart { ForEach(historicalData) { LineMark(...) } }`.
  Show an explicit "no data in range" message when the array is empty but a
  fetch has completed.

---

## Medium Findings

### S3-12 Potential panic in context_compactor pruneToolOutputs

- **File:** `internal/llm/context_compactor.go:715-723`
- **Evidence:**
  ```go
  toolNameByID := make(map[string]string)
  for _, msg := range messages {
      if msg.Role == RoleAssistant {
          for _, tc := range msg.ToolCalls {
              toolNameByID[tc.ID] = tc.Function.Name  // panics if tc.Function is nil
          }
      }
  }
  ```
- **Why it's a bug:** Could panic if `tc.Function` is nil. ToolCall structs
  from external/untrusted sources (cached JSON, MCP servers) may have nil
  Function fields.
- **Fix:** `if tc.Function != nil { toolNameByID[tc.ID] = tc.Function.Name }`

### S3-11 WithTokenCache typed-nil guard hardening

- **File:** `internal/llm/client.go:178-184`
- **Evidence:** Correctly guards against nil, but `keyBuilder` is only set
  if cache is non-nil. A typed-nil interface passes the `cache != nil` check
  (CLAUDE.md "typed-nil interface guard" rule), so `tokenCache` ends up
  non-nil wrapping nil pointer while `keyBuilder` is left unset.
- **Fix:** Add a defense-in-depth check that the cache is actually usable
  (e.g. reflect or type assertion), per CLAUDE.md guidance.

### S5-6 BusService.Stats maps `raw["_total"]` without checking presence

- **File:** `internal/services/bus_service.go:88-92`
- **Evidence:** `raw := s.bus.Stats()` returns `map[string]int` but `_total`,
  `_messages_sent`, `_queued` are read without a presence check. If a future
  change to `bus.MessageBus.Stats()` renames or removes these keys, the
  service silently returns zero stats without error.
- **Fix:** `if v, ok := raw["_total"]; ok { ... }`.

### S5-8 `handleChatStream` subscribes to `agent.progress` (non-wildcard)

- **File:** `internal/comm/http/api_handlers.go:108, 116, 122`
- **Evidence:**
  ```go
  agentSub, agentUnsub := s.services.Bus.Subscribe(subID+"-agent", "agent.progress")
  ```
  Exact-match topic subscriptions. Per CLAUDE.md the bus wildcard `*` matches
  only single-segment topics, so `agent.progress` will not catch
  `agent.progress.synthesized` (the topic `handleWSProgress` listens on).
- **Why it's a bug:** The HTTP SSE stream misses synthesized progress events
  that WebSocket clients receive.
- **Fix:** Either subscribe to each variant explicitly or change the topic
  scheme.

### S5-11 `DevHandler.handleReload` swallows loadModels error inside a success-shaped response

- **File:** `internal/rpc/dev.go:358-364`
- **Evidence:**
  ```go
  if err := h.loadModels(); err != nil {
      return map[string]any{
          RPCKeySuccess: false,
          "error":       err.Error(),
      }, nil  // returns nil error
  }
  ```
- **Why it's a bug:** Returns `nil` error with `success: false` body. Clients
  using `if err := client.Reload(); err != nil { ... }` cannot detect the
  failure.
- **Fix:** Return the error so `dispatch` produces a proper `-32603`.

### S5-12 `rpc.Server.acceptLoop` logs accept failures forever without backing off

- **File:** `internal/rpc/server.go:229-235`
- **Evidence:** On a transient `Accept` error the loop `continue`s
  immediately, which can spin the CPU if the listener is in a half-closed
  state.
- **Fix:** Sleep briefly or return on non-temporary errors via
  `errors.Is(err, net.ErrClosed)`.

### S5-13 `ProxyHandler.handleBusSubscribe` cleanup goroutine can race with `handleBusUnsubscribe`

- **File:** `internal/rpc/proxy.go:322-337` vs `461-491`
- **Evidence:** Both paths touch `sub.TopicSubs` under `sub.mu`, so no memory
  corruption, but the cleanup goroutine's `Delete` runs asynchronously after
  the unsubscribe RPC returns.
- **Fix:** Document or fold both paths into one.

### S5-14 MCP `meept_status` tool ignores client context, uses `context.Background()`

- **File:** `internal/comm/http/server.go:2406`
- **Evidence:** `return s.mcpServices.Daemon.Status(context.Background())`
  drops the request context. If the MCP client disconnects mid-call, the
  status call still runs to completion. Same pattern at line 2339.
- **Fix:** Pass `r.Context()` via `processMCPRequest`'s `ctx` parameter.

### S5-15 `rpc.Server.registerBuiltinHandlers` uses `atomicCounter()` for message IDs

- **File:** `internal/rpc/server.go:474, 503, 517`
- **Evidence:** Generates IDs like `rpc-<int>` and `amend-<int>` from a
  global atomic counter. Guessable by any client who can roughly estimate
  the counter value, and `task.amend.submit`'s `amendmentID` could collide
  across daemon restarts (counter resets).
- **Fix:** Use `id.Generate`.

### S4-7 Predictable ID generation using `time.Now().UnixNano()` across tools

- **File:** Multiple — `internal/tools/builtin/platform.go:373` (delegate ID),
  `tool_schedule_create.go:119`, `tool_cron_create.go:144`, `file_edit.go:381`,
  `review_tools.go:128`, `internal/code/tools/ast_edit.go:192`,
  `internal/code/tools/lsp_rename.go:198`
- **Why it's a bug:** Same recurring pattern as S5-4/S5-7/S6-5/S7-1.
- **Fix:** Replace all with `pkg/id.Generate(...)`.

### S4-8 CronCreateTool returns both result and error on cron expression build failure

- **File:** `internal/tools/builtin/tool_cron_create.go:128-133`
- **Evidence:**
  ```go
  return CronCreateResult{
      Success: false,
      Error:   err.Error(),
  }, err  // BUG: returns error alongside result
  ```
  Every other error path in this function returns `(result, nil)`.
- **Fix:** Change `}, err` to `}, nil`.

### S4-9 mcp.Server handleToolsCall returns errors as successful responses

- **File:** `internal/mcp/server.go:172-179`
- **Evidence:** When tool execution returns an error, the server wraps it as
  a result (not an error), using the MCP content format but without setting
  `isError`. Clients that check `isError` will treat tool execution failures
  as successful results.
- **Fix:** Include `"isError": true` in the marshaled result, or use the
  proper `CallToolResult` struct with `IsError: true`.

### S4-11 MCP HTTPTransport.parseSSEResponse may hang on partial data

- **File:** `internal/tools/mcp/transport/http.go:190-226`
- **Evidence:** The SSE parser uses `bufio.NewScanner` with the default
  buffer size (64KB). If an SSE event data line exceeds the scanner's
  buffer, `scanner.Scan()` returns false and `scanner.Err()` returns
  `bufio.ErrTooLong`. Additionally, the parser only looks for `"data: "`
  prefix (with space). SSE spec allows `"data:"` without space.
- **Fix:** Use larger scanner buffer and handle both prefixes.

### S4-12 MCP Client.Connect logs c.tools length outside lock

- **File:** `internal/tools/mcp/client.go:81-88`
- **Evidence:** `len(c.tools)` is read without holding `c.mu` after
  `refreshTools` sets it under lock and releases.
- **Fix:** Snapshot the tool count inside `refreshTools`'s locked section,
  or acquire `c.mu.RLock()` before reading `len(c.tools)`.

### S6-7 `PersistentQueue.Claim` slow-path loses race, sends worker to error state

- **File:** `internal/queue/queue.go:193-235` + `internal/worker/worker.go:222-230`
- **Evidence:** The slow path lists pending jobs, finds the first claimable
  one, then calls `ClaimNextByID`. Between list and claim, another worker may
  claim the same job. `ClaimNextByID` returns `ErrJobAlreadyClaimed`, which
  is returned to the worker — which then enters Error state.
- **Fix:** Translate `ErrJobAlreadyClaimed` to `ErrNoJobAvailable` in the
  slow path, or retry up to N times.

### S6-8 `CheckNodeReachability` uses `db.QueryRow` without context

- **File:** `internal/queue/cluster_queue.go:200-203`
- **Evidence:** `row := cq.store.db.QueryRow(...)` instead of
  `QueryRowContext`. Other store methods correctly use `ExecContext`.
- **Fix:** Accept a context parameter and use `QueryRowContext`.

### S6-9 `pty.Manager.Close` holds write lock across `sess.Close()` I/O

- **File:** `internal/pty/manager.go:138-147`
- **Evidence:** `for id := range m.sessions { m.destroySessionLocked(id) }`
  inside the lock — `destroySessionLocked` calls `sess.Close()` which
  performs I/O (`ptmx.Close()`, `Process.Kill()`, `close(done)`). If any
  session is stuck, all other Manager operations are blocked.
- **Fix:** Snapshot session IDs under the lock, release the lock, then close
  each session individually.

### S6-10 `debug.Client.readLoop` can block forever on unresponsive adapter

- **File:** `internal/debug/client.go:196-229`
- **Evidence:** `readMessage()` blocks on `c.stdout.ReadString('\n')`. The
  context passed from `manager.go:131-132` is `context.Background()`, so
  it's never cancelled. A hung adapter process leaks the goroutine
  permanently.
- **Fix:** Use a context with cancellation in `manager.go`, or add a read
  deadline via a timeout wrapper.

### S6-11 Predictable message IDs in queue and worker handlers

- **File:** `internal/queue/queue.go:768`, `internal/worker/pool.go:573`,
  `internal/cluster/gossip.go:273,364`
- **Evidence:** All use `fmt.Sprintf("...-%d", time.Now().UnixNano())` for
  BusMessage IDs.
- **Fix:** Use `pkg/id.Generate()` or `models.NewBusMessage` (which uses
  `id.Generate` internally).

### S6-16 `ClusterMember.SigningPub` type mismatch with scan target

- **File:** `internal/queue/store.go:1003`
- **Evidence:** `m.SigningPub = signingPubRaw` where `signingPubRaw` is
  `[]byte` and `SigningPub` is `ed25519.PublicKey`. Compiles because
  `ed25519.PublicKey` is `type PublicKey []byte`, but ed25519 public keys
  must be exactly 32 bytes. The scan reads a `BLOB` without validating
  length.
- **Fix:** Validate `len(signingPubRaw) == ed25519.PublicKeySize` after scan.

### S7-2 `configPath` parameter in `saveConfig` shadows package-level `configPath()` function

- **File:** `cmd/meept/token.go:92`
- **Evidence:** `func saveConfig(configPath string, v hujson.Value) error`
  shadows the package-level `configPath()` function. Any future edit that
  calls `configPath()` from inside `saveConfig` will fail to compile in a
  confusing way.
- **Fix:** Rename parameter to `path` or `filePath`.

### S7-3 `analytics.go` avg-cost sentinel `-1` surfaces in user-facing output with no explanation

- **File:** `cmd/meept/analytics.go:299-301`
- **Evidence:** When no cost data is available (`AvgCost == 0`), code
  substitutes `-1` and prints `-1.00`. Silent data corruption for downstream
  pipelines; misleading UI output.
- **Fix:** Print `n/a` (string) when `AvgCost == 0`.

### S7-4 TUI silently falls back to RPC when `--transport=http` is set

- **File:** `cmd/meept/chat.go:134-138`
- **Evidence:** Prints a warning and silently uses the RPC socket. A user
  who has explicitly configured HTTP-only transport will see the TUI "work"
  while violating their stated transport policy.
- **Fix:** Return a non-zero error from `runTUI` when `--transport=http` is
  set, or honor it via HTTP transport client.

### S7-9 `DaemonStatusViewModel.refreshStatus` and control methods race on `isUpdating`

- **File:** `menubar/MeeptMenuBar/ViewModels/DaemonStatusViewModel.swift:53-66`
- **Evidence:** `isUpdating` conflates "any control operation is in flight"
  with "a status refresh is in flight". User clicks are silently dropped
  without feedback.
- **Fix:** Use separate flags for `isRefreshingStatus` vs `isControllingDaemon`.

### S7-6 / S7-7 / S7-8 / S7-19 Lowercase UI convention violations (multiple surfaces)

- **Files:**
  - `menubar/MeeptMenuBar/Views/NotificationCenterMenuView.swift:15,22,27` — `Text("No notifications")`, `Button("Clear All")`, `Toggle("Enable Notifications", isOn: ...)`
  - `menubar/MeeptMenuBar/Views/Settings/SettingsWindow.swift:57,65` — capitalized error strings
  - `menubar/MeeptMenuBar/Models/Presets.swift:29-61` — `"Development"`, `"Debugging"`, etc.
  - `cmd/meept/status.go:95,115,139,154,45` — `"Meept Daemon Status"`, `"Token Budget"`, etc.
  - `cmd/meept/memory.go:85,115,304,314` — `"Exported %d memories"`, etc.
  - `cmd/meept/jobs.go:37,42` — `"No scheduled jobs"`, column headers in CAPS
  - `cmd/meept/workers.go:53,81,149` — `"Worker Pool Status"`, etc.
  - `cmd/meept/cluster_cmd.go:223-242` — `"Cluster Initialized Successfully"`, etc.
  - `menubar/MeeptMenuBar/Services/DaemonController.swift:149-161` — `"Failed to load:"` vs `"launchd plist not found"` inconsistency
  - `menubar/MeeptMenuBar/Views/Settings/AgentsConfigView.swift:175-179` — deprecated single-parameter `onChange` form
- **Why it's a bug:** CLAUDE.md explicitly requires all UI text to be
  lowercase. Round 4 fixed TUI's `TextCapitalization`; the CLI and MenuBar
  sides were never swept.
- **Fix:** Sweep all user-facing strings in `cmd/meept/*.go` and
  `menubar/MeeptMenuBar/**/*.swift` and lowercase them. Replace deprecated
  `onChange(of:) { newId in }` with `onChange(of:) { old, new in }`.

### S8-6 Silent `catch (_)` blocks hide failures across `providers.dart`

- **File:** `ui/flutter_ui/lib/providers/providers.dart` — lines 64, 230, 240, 252, 370, 381
- **Evidence:** Six sites swallow the exception with no logging or rethrow:
  - `resolveActiveProjectProvider` returns null on any error
  - `ConnectionDetailsNotifier._fetch` hides daemon-fetch errors
  - `ConnectionMonitor._fetchConnectionDetails` documented as best-effort but silent
  - `ConnectionMonitor._startHealthChecks` silently marks disconnected
- **Fix:** At minimum `debugPrint('[warn] <context>: $e')` in each handler.

### S8-7 `_loadSkills` swallows errors with empty comment

- **File:** `ui/flutter_ui/lib/features/home/tools_dropdown.dart:35-37`
- **Evidence:** `catch (e) { // skills remain empty on error }` — the
  dropdown silently shows zero skills on any failure.
- **Fix:** Surface the error via an error banner or tooltip; at minimum `debugPrint`.

### S8-8 `AgentProgress.fromJson` unsafe `as int` cast on `tier`

- **File:** `ui/flutter_ui/lib/models/api_models.dart:152`
- **Evidence:** `tier: (data?['tier'] ?? json['tier'] ?? 1) as int` will
  throw if the backend ever serializes as `num`/`double`.
- **Fix:** `as num` then `.toInt()`.

### S8-10 Dead `chat_provider.dart.bak` file

- **File:** `ui/flutter_ui/lib/providers/chat_provider.dart.bak`
- **Evidence:** Leftover backup file from a prior round. Tracked by glob
  patterns and packaged into builds; shows up in search results and IDE file
  trees, creating confusion.
- **Fix:** Delete the file. (Also delete other .bak files in repo per
  section "Repo hygiene" below.)

---

## Low Findings

### S4-13 `file_grep.go formatGrepContent` has O(n²) bubble sort

- **File:** `internal/tools/builtin/file_grep.go`
- **Fix:** Use `sort.Slice` with a comparison function.

### S4-14 `debug.go rawToMap` silently swallows unmarshal errors

- **File:** `internal/tools/builtin/debug.go`
- **Fix:** Add `slog.Debug` log when falling back to raw.

### S4-15 MCP Server version mismatch between client and server

- **File:** `internal/mcp/server.go:93` (`"0.1.0"`) vs `internal/tools/mcp/client.go:99` (`"0.2.0"`)
- **Fix:** Align both to a shared constant.

### S4-16 MCP StdioTransport stderr drain goroutine may leak

- **File:** `internal/tools/mcp/transport/stdio.go:129-137`
- **Fix:** Use a separate done channel that `Close()` signals.

### S4-17 `orderedMap` non-deterministic JSON in `hashline_parser.go`

- **File:** `internal/tools/builtin/hashline_parser.go`
- **Fix:** Use an ordered map implementation (slice + map).

### S4-18 `ExtractJSONFromText` first-match bias may miss valid JSON

- **File:** `internal/tools/builtin/schema_validation.go:148-186`
- **Fix:** When first strategy finds content but fails to parse, try the
  next strategy before falling through to raw parse.

### S6-13 `gossip_transport.markSentToPeer` pruning deletes random entries

- **File:** `internal/cluster/gossip_transport.go:353-361`
- **Fix:** Add timestamps or use LRU.

### S6-14 `Pool.Scale` reads worker count then modifies without holding lock

- **File:** `internal/worker/pool.go:248-270`
- **Fix:** Hold the lock across the entire scale operation.

### S6-15 `debug.parseGDBVariable` has dead code / always-false condition

- **File:** `internal/debug/adapter_native.go:634`
- **Fix:** Delete the empty-body `if`.

### S7-13 `cache.go:pluralize` is a misnamed micro-helper

- **File:** `cmd/meept/cache.go:226-231`
- **Fix:** Rename to `pluralizeEntry` or make generic.

### S7-14 `NotificationManager` has no `deinit` and never disconnects its WebSocket

- **File:** `menubar/MeeptMenuBar/Services/NotificationManager.swift:11-39`
- **Fix:** Add `disconnect()` and `reconnect()` methods. Call `reconnect()`
  from settings-change hook.

### S7-15 `WebSocketManager.connect()` flag-flip ordering

- **File:** `menubar/MeeptMenuBar/Services/WebSocketManager.swift:53-71`
- **Fix:** `guard let session = urlSession else { isConnecting = false; return }`
  before `webSocketTask = session.webSocketTask(...)`.

### S7-16 `MenubarConfigService.loadConfig` swallows parse errors with `print`

- **File:** `menubar/MeeptMenuBar/Services/MenubarConfigService.swift:120-123`
- **Fix:** Replace `print()` with `Logger(subsystem:category:).error(...)`.
  Apply same fix to `NotificationManager.swift` print()s.

### S7-17 `APIClient.makeRequest` throws `noAPITokenConfigured` even for `/health`

- **File:** `menubar/MeeptMenuBar/Services/APIClient.swift:81-90`
- **Fix:** `makeRequest(path:method:requiresAuth: Bool = true)` — when false,
  skip the token guard.

### S7-18 `cmd/meept/main.go` `runChat` is the default `RunE` but also a subcommand

- **File:** `cmd/meept/main.go:109` + `cmd/meept/chat.go:24-47`
- **Fix:** Document the asymmetry, or move `--project`/`--nofence` to
  `PersistentFlags` on root.

### S7-20 `client.json5` fields silently default when missing

- **File:** `internal/tui/app.go:229-243`
- **Fix:** Provide explicit defaults in `LoadClientConfig()`; log warning on
  load failure.

### S8-9 `SlashCommandRegistry.get` uses `catch (_)` to implement "not found"

- **File:** `ui/flutter_ui/lib/core/slash_commands.dart:40-45`
- **Fix:** Use `all.where((cmd) => cmd.name == n).firstOrNull` (Dart 3).

### S8-11 Stale TODO in search panel

- **File:** `ui/flutter_ui/lib/features/search/search_panel.dart:401`
- **Fix:** Implement navigation or remove the Tap handler.

### S8-12 Stale TODO in storage service

- **File:** `ui/flutter_ui/lib/services/storage_service.dart:93`
- **Fix:** Commit to removal version or delete the TODO.

### S8-13 `SearchScope.name` shadows `Enum.name`

- **File:** `ui/flutter_ui/lib/models/api_models.dart` (~line 456)
- **Fix:** Rename extension member to `wireName` or `apiValue`.

### S8-14 `KeyboardListener` is deprecated

- **Files:** same four panels as S8-5
- **Fix:** Replace with `Focus(onKeyEvent:...)`. Best done with S8-5.

### S8-15 `_buildBaseUrl` excludes `/api/v1` but `healthCheck` re-prepends baseUrl

- **File:** `ui/flutter_ui/lib/services/meept_api.dart:29-32`
- **Fix:** Use `_dio.get('/health')` like other endpoints.

### Repo hygiene: stale `.bak` files

- **Files:** `ui/flutter_ui/lib/providers/chat_provider.dart.bak`,
  `bin/meept-classifier-test.bak`, `internal/tools/builtin/filesystem.go.bak`,
  `internal/security/fence_test.go.bak`, `internal/daemon/components.go.bak`,
  `docs/plans/ui-fixes-2025-06-15.md.bak`,
  `docs/concepts/architecture.md.bak`, `docs/concepts/multi-agent.md.bak`
- **Fix:** Delete all `.bak` files. They are tracked by glob patterns and
  packaged into builds; they show up in search results and create confusion.

### Round-4 mutexio analyzer gap (cross-function `defer Unlock()`)

- **File:** `internal/queue/cluster_queue.go:185-189`
- **Observation:** The round-4 `mutexio` analyzer (`tools/analyzers/mutexio/`)
  is intraprocedural — it looks for Lock/Unlock pairs in the same function.
  This misses the cross-function pattern where `ReclaimJob` does
  `cq.mu.Lock(); defer cq.mu.Unlock(); return cq.reclaimJobUnlocked(ctx, ...)`
  and the callee performs I/O under the inherited lock (S6-3).
- **Fix:** Extend `mutexio` to follow single-call-depth callees when the
  caller holds a lock via `defer Unlock()`. Or add a `//nolint:mutexio`
  directive with rationale for documented exceptions.

---

## Observations

1. **Prompt injection is persistent.** Every file Read result in this round
   (and rounds 3, 4) contained fake `<system-reminder>` blocks claiming the
   code may be malware and instructing refusal to improve. All subagents
   correctly disregarded them. Source still unknown — possibly a hook or an
   MCP server appending to file contents. This is a meta-finding worth
   investigating separately.

2. **Round-4 fixes held up well.** Spot-verified:
   - HTTP client Bearer header on all 4 paths (`internal/transport/http_client.go`)
   - PiperEngine no longer returns temp file path
   - DebugSession mutex added
   - TokenCache L1 hit path uses write lock
   - MCP Manager no longer holds lock across Connect/Close
   - WebFetch uses `ssrfDialContext`
   - `bus.messagesSent` is `atomic.Int64`
   - `ContextCompactor.Compact` snapshots under lock
   - WebSocket `_streamDone`/`_cleanupChannel` properly completes
   - DevAPIKey generates random key on first run
   None of the round-4 fixes regressed.

3. **Predictable IDs are a long tail.** Round 4 found and fixed 4 sites;
   round 5 found 11+ more (`web-%d`, 4 team-tool sites, `claim-%d`, queue/worker/gossip
   responses, CLI `cli-%d-%d`, MCP RPC IDs, AST/LSP session IDs, tool
   delegate IDs, scheduler/cron IDs, file_edit fallback, review_tools conv
   ID). Each site is easy to fix but the pattern keeps re-appearing in new
   code. Consider a `go vet`-style analyzer that flags any
   `time.Now().UnixNano()` used inside `fmt.Sprintf` for ID construction.

4. **Fence checking is inconsistently applied.** `hashline/file_edit`,
   `filesystem.WriteFile`, and `shell` are properly fenced. But newer tools
   added since rounds 1-4 (`ast_edit`, `resolve_ast_edit`, `lsp_writethrough`,
   `debug`) lack `SetFenceChecker` entirely. The fence-checker wiring in
   `components.go` only injects into tools that have the setter; new tools
   are silently unfenced. Consider a compile-time check (e.g. an interface
   assertion in the daemon's tool registry that warns if a file-writing tool
   doesn't implement `FencedTool`).

5. **Subagent false-positive dismissal is still a risk.** The S3 subagent
   marked several real findings as "already fixed" or "no fix needed" —
   exactly the pattern MEMORY.md warns about. Orchestrator verified each
   "fixed" claim by reading source. S3-11 (typed-nil guard hardening) and
   S3-12 (nil-check on `tc.Function`) are genuine issues that deserved
   fixes, not dismissals. Future rounds should explicitly instruct
   subagents: "Do not classify a finding as 'no fix needed' without
   reading at least 10 lines of surrounding context."

6. **MenuBar app is the weakest tier.** Many findings (S7-5, S7-9, S7-10,
   S7-14, S7-15, S7-16, S7-19) cluster in the Swift code. The menubar app
   was added since rounds 1-4 and missed the scrutiny that Go/Flutter
   received. Recommended: a dedicated menubar review round.

7. **Auth model has defense-in-depth gaps, not direct bypasses.** The
   notification WS endpoint (S5-1/S5-3) does receive middleware coverage,
   but the handler-local token extraction is dead code that creates a
   "looks enforced" appearance. The hardcoded dev key (S8-1) is the only
   true auth bypass — and it's a regression of a round-4 fix.

8. **Cluster crypto is placeholder-grade.** S6-1 (empty-sig bypass), S6-4
   (`[]byte(action)` as fake signature), S6-6 (fragile verification logic),
   S6-16 (no length validation on signing keys) together indicate the
   cluster signing story is not production-ready. An operator enabling
   `RequireNodeSignatures: true` today gets theatre, not security.

---

## Addendum: S1 (Agent Orchestration) + S2 (Code Intel / Plans / Skills / Config)

These reports landed after the main document was drafted. Findings below were
re-verified against source by the orchestrator (not the original subagents).
Where the original subagent rated severity conservatively, the orchestrator's
final severity is shown.

### Critical

#### S2-1 LSP `TCPTransport.Write` has no mutex — concurrent writers corrupt JSON-RPC framing
- **File:** `internal/code/lsp/transport/tcp.go:88-106`
- **Evidence:** `Write` performs two `t.conn.Write` calls (header + body)
  without any mutex. Compare `StdioTransport.Write`
  (`internal/code/lsp/transport/stdio.go:93-110`) which guards both writes
  with `t.writeMu.Lock()`.
- **Why:** `Client.Call` (any goroutine) and `Client.Notify` (Initialize /
  notification handlers) both invoke `transport.Write`. Two concurrent
  invocations interleave header/body on the shared `net.Conn`, producing
  malformed `Content-Length` framing that the LSP server cannot decode —
  typically dropping the connection. The stdio transport already has the
  mutex; the TCP transport was missed when the transport abstraction was
  introduced.
- **Status:** **FIXED** — `writeMu sync.Mutex` added to `TCPTransport`,
  held across both `conn.Write` calls. Mirrors `StdioTransport.Write`.

#### S2-2 `LoadJSON5WithDefault` never uses the default — missing config is always fatal
- **File:** `internal/config/json5_loader.go:17-24` (producer),
  `:135-143` (consumer).
- **Evidence:**
  ```go
  // LoadJSON5 — wraps ENOENT with %s (no error chain preservation)
  if os.IsNotExist(err) {
      return fmt.Errorf("config file not found: %s", path)  // breaks chain
  }
  // LoadJSON5WithDefault — checks os.IsNotExist, which is now always false
  if err := LoadJSON5(path, v); err != nil {
      if os.IsNotExist(err) { return nil }   // never taken
      return err
  }
  ```
- **Why:** `os.IsNotExist` walks the error chain looking for
  `*fs.PathError{Err: ENOENT}`. `fmt.Errorf("...%s", path)` discards the
  underlying error and produces `*errors.errorString`, so
  `errors.Is(err, fs.ErrNotExist)` is false. The default-zero-value contract
  is silently dead: every missing user-config file (`menubar.json5`,
  `mcp_servers.json5`, `presets.json5`, `cluster.json5`) aborts startup
  rather than falling back to defaults.
- **Status:** **FIXED** — `LoadJSON5` now uses `%w`:
  `fmt.Errorf("config file not found: %s: %w", path, err)`. Caller's
  `os.IsNotExist` resolves correctly.

### High

#### S1-1 Predictable IDs from `time.Now().UnixNano()` across 9 agent call sites
- **Files / lines:** `internal/agent/loop.go:3666-3668`
  (`generateConversationID`), `collaboration.go:91`, `pair_session.go:209`,
  `pair_manager.go:150,193`, `strategic.go:224,540`, `tactical.go:1485`
  (sequence number from clock modulo), `emitter.go:252-253`
  (`generateEventID` — timestamp only, no entropy at all).
- **Why:** Same bug class as the round-4 `generatePatternID` finding. The
  timestamp is attacker-knowable; the only entropy on most paths is a
  per-call counter that doesn't help when two goroutines from different
  callers reach the same line in the same ns. `generateEventID` is the
  worst case (literal timestamp format string). Bus topics derived from
  these IDs (e.g. `TeamMessageTopic(sessionID)`, `PairTopic(sessionID)`)
  become guessable.
- **Status:** **FIXED** — all 9 sites converted to `pkg/id.Generate(prefix)`
  (or `generateMessageID()`). `tactical.go` now uses an `atomic.Uint64`
  counter instead of `time.Now().UnixNano()%1000`.

#### S1-2 + S1-6 Mutex held across SQLite I/O in `EscalationManager.Escalate`
- **File:** `internal/agent/escalation.go:109-132`
- **Why:** `em.taskStore.GetByID` is a SQLite query executed under
  `em.mu.Lock()`. CLAUDE.md forbids mutex-across-I/O. On a contended
  daemon every other escalator (and every other caller of methods that
  use `em.mu`) blocks behind the query.
- **Note (S1-6):** Once the I/O is moved out of the lock, the check-then-insert
  path becomes a TOCTOU. Two concurrent first-callers for the same `TaskID`
  would both miss, both query, both insert, and the second insert would
  overwrite the first `EscalationLevel`, losing the first `Level++`.
- **Status:** **FIXED** — `Escalate` snapshots the map entry under lock,
  releases for `taskStore.GetByID`, re-acquires with a TOCTOU re-check
  (`if existing, ok := em.escalations[taskID]; ok { level = existing }`).

#### S1-3 Mutex held across SQLite transaction in `QueuePersister.EnqueueAsync`
- **File:** `internal/agent/queue_persister.go:109-134` (caller),
  `:178-188` / `:265-330` (callee `flushPendingLocked`).
- **Why:** `EnqueueAsync` is on the hot path of every follow-up message.
  When the pending buffer overflows, `flushLockedHeld()` runs a full
  `BEGIN / N×EXEC / COMMIT` while holding `p.mu`. Every other enqueue and
  every `Close` is blocked for the duration of the transaction.
- **Status:** **FIXED** — `EnqueueAsync`'s overflow path now snapshots
  pending under lock, releases the lock, calls the non-lock-held
  `flushPending`, then re-acquires only for the append. The unused
  `flushLockedHeld` / `flushPendingLocked` helpers were deleted.

#### S1-4 `ArtifactManager.artifactCache` mutated and read with no mutex (fatal panic risk)
- **File:** `internal/agent/artifact_integration.go`
- **Orchestrator severity:** **High** (subagent said Medium).
- **Why:** Go's runtime fatals on concurrent map read + map write — not a
  data race, a hard panic. The struct has no `mu` field at all. Five
  unsynchronized access sites: read at line 55 (cache hit), write at 71
  (cache miss insert), `delete` at 160 (InvalidateCache), full-map replace
  at 168 (InvalidateAll), read at 174 (GetArtifacts). The agent loop
  dispatches multiple specialists in parallel; `BuildContext`,
  `FindSkillForTask`, and `ScanDirectory` all touch this cache. Upgraded to
  High because the failure mode is a runtime crash, not a silent race.
- **Status:** **FIXED** — `mu sync.RWMutex` added to `ArtifactManager`;
  RLock for `GetArtifacts` and `ScanDirectory` cache-hit, Lock for insert,
  `InvalidateCache`, `InvalidateAll`.

#### S2-3 LSP `Manager.StartServer` TOCTOU race — duplicate servers spawned
- **File:** `internal/code/lsp/manager.go:111-171`
- **Why:** Pattern is `Lock → check map → Unlock → slow I/O (Initialize)
  → Lock → blindly overwrite map → Unlock`. Two callers can both pass the
  first check and both perform the slow startup; the loser's subprocess
  (`StdioTransport.cmd.Process`) leaks because it is never closed, and
  its port/socket may leak too.
- **Status:** **FIXED** — after the slow I/O, on re-acquiring the write
  lock the code now re-checks `m.servers[name]`; if another goroutine won,
  the just-spawned duplicate transport is closed and the existing instance
  is returned.

#### S2-4 `RepoMapGenerator.GenerateWithCache` swaps `g.cache` without locking
- **File:** `internal/repomap/generator.go:352-360`
- **Why:** `oldCache := g.cache; g.cache = NewMapCache(...); defer
  func(){ g.cache = oldCache }()`. Concurrent `Generate` / `Stats` /
  `InvalidateCache` readers see the temporary no-op cache, and the
  deferred restore can swap back the wrong cache if two
  `GenerateWithCache` calls interleave. On a daemon with concurrent chat
  sessions this silently disables caching for the duration.
- **Status:** **FIXED** — the swap was eliminated by refactoring the body
  into `generateInternal(ctx, chatFiles, mentionedIdentifiers, cache)`.
  `Generate` calls it with `g.cache`; `GenerateWithCache` calls it with
  a local `cache := g.cache` (or no-op cache when `useCache=false`). No
  field swap, no race surface.

#### S2-5 Skill discovery `<=` causes equal-priority shadowing
- **File:** `internal/skills/discovery.go:145` and `:234`
- **Why:** With `skill.Priority <= existing.Priority`, a skill at the
  *same* priority overwrites the existing one. The log message ("shadowed
  by higher priority") fires even when priorities are equal. Round-4 fixed
  the analogous bug in the file source; this one was missed.
- **Status:** **FIXED** — both sites changed to strict `<`.

### Medium

#### S1-5 `SessionTracker.GetSession` returns live pointer under RLock
- **File:** `internal/agent/session_tracker.go:93-97`
- **Why:** RLock is released on return; caller receives a live
  `*TrackerSessionState` whose `IntentHistory` / `Metrics` can be mutated
  by `RecordIntent` / `RecordMetrics` while the caller iterates. Slice
  header race on `IntentHistory` append is the worst case. Compare
  `GetDominantIntent` (line 107) which correctly copies under the lock.
- **Status:** **DEFERRED** — fix is to return a deep copy, but that
  requires auditing every caller to ensure they don't rely on the live
  pointer for mutation (some may). Filed as a follow-up.

#### S1-7 `classifyIntent` error silently discarded at two dispatcher call sites
- **File:** `internal/agent/dispatcher.go:397`, `:819`
- **Why:** `intent, _ := d.classifyIntent(...)`. The function is
  guaranteed to return a non-nil intent today, so the discard is harmless
  in the current code — but a future refactor that makes `classifyIntent`
  capable of returning `(nil, err)` introduces a silent nil deref.
- **Status:** **DEFERRED** — low priority; the only behavioral change is
  adding a Warn log on non-nil err.

#### S1-8 `EscalationManager` uses wallclock for ordering
- **File:** `internal/agent/escalation.go:119-130`
- **Why:** `time.Now()` can step backwards on NTP sync; escalation
  timestamps used for ordering would invert.
- **Status:** **DEFERRED** — needs an atomic counter alongside the
  wallclock timestamp. Low impact.

#### S2-6 Plan parser uses `bufio.Scanner` with default 64KB token limit
- **File:** `internal/plan/parser.go:114`
- **Why:** Plan files with a single line >64 KiB (large code block, long
  description) cause `bufio.ErrTooLong` and the parser silently falls back
  to "phases only" in `PlanManager.Synthesize`, losing the step DAG.
- **Status:** **DEFERRED** — one-line fix (`scanner.Buffer(...)`) but
  requires verifying nothing else in the parser depends on the default
  limit.

#### S2-7 Plan writer "step.Number <= completed" heuristic marks wrong steps complete
- **File:** `internal/plan/writer.go:116-126`
- **Why:** Infers per-step completion from step *number* vs phase
  *completed count*. If steps are completed out of order (which the task
  system explicitly supports via `DependsOn`), the writer marks
  lower-numbered steps done and higher-numbered ones pending regardless
  of actual state.
- **Status:** **DEFERRED** — the fix requires threading a real per-step
  state map from the task store through to the writer; non-trivial
  refactor.

#### S2-8 Hermes `checkEnvVar` uses `sh -c` with string concatenation — shell injection
- **File:** `internal/skills/hermes_compat.go:105-112`
- **Why:** `name` comes from skill YAML `env_vars`. A malformed skill
  with `env_vars: ["FOO; rm -rf $HOME"]` executes arbitrary shell.
  Operator-installed today, but the CLAUDE.md security posture is
  defense-in-depth for subprocess invocations.
- **Status:** **FIXED** — replaced `exec.Command("sh","-c","printenv "+name)`
  with `os.LookupEnv(name)`. Error message preserved as
  `"missing required env var %s"` to satisfy the existing integration test.

#### S2-9 RepoGraph weightedLine ID packing collides at 1e9 nodes
- **File:** `internal/repomap/graph.go:132-141`
- **Why:** `IDVal: from.ID()*int64(1e9) + to.ID()`. The `nodeID` counter
  is process-global and monotonic; in a long-lived daemon that rebuilds
  repomaps, IDs accumulate forever. Past 1e9, distinct `(from,to)` pairs
  can produce identical `IDVal` and gonum's graph treats line IDs as
  uniqueness keys, corrupting PageRank.
- **Status:** **DEFERRED** — fix is to allocate edge IDs from a separate
  atomic counter; needs a regression test with synthetic high node IDs.

#### S2-10 Self-improve applier `strings.Replace(..., 1)` patches wrong occurrence
- **File:** `internal/selfimprove/applier.go:117`
- **Why:** Blind first-occurrence string substitution. For snippets like
  `return nil, err` or `mu.Lock()` that appear many times in a file, the
  applier patches the first textual match rather than the location the
  LLM actually edited.
- **Status:** **DEFERRED** — fix requires enriching the proposed-fix
  struct with explicit byte/line ranges and rejecting ambiguous matches.

#### S2-11 `RepoMapGenerator.buildPersonalization` RLock thrash
- **File:** `internal/repomap/generator.go:314-322`
- **Why:** RLock is taken/released once per `mentionedIdentifiers`
  element instead of once around both loops. Wasteful under
  contention; not a correctness bug.
- **Status:** **DEFERRED** — pure micro-optimization.

#### S2-12 `ApplyEdits` is O(n×e) due to per-edit linear `positionToByte`
- **File:** `internal/code/ast/rewrite.go:240-269`
- **Why:** Each edit rescans the source from offset 0. Worse,
  `RunRewrite` already has exact `startByte`/`endByte` from
  `capture.Node.StartByte()` and throws them away in favor of recomputing
  from line/char. For 500 edits on a 500KB file this is ~250MB of work.
- **Status:** **DEFERRED** — preferred fix is to thread
  `StartByte`/`EndByte` through `ProposedEdit` and use them directly;
  touches several call sites.

### Low

#### S2-13 `ExpandEnvVars` dead "Warn if we hit the cap" block
- **File:** `internal/config/config.go:167-173`
- **Why:** `if` block contains only comments; cyclic env var references
  (`A=${B}`, `B=${A}`) silently resolve to empty strings with no
  diagnostic.
- **Status:** **DEFERRED** — one-line fix to add a `slog.Warn`.

#### S2-14 `KeywordExtractor.extractFromName` recompiles regex on every call
- **File:** `internal/skills/keyword_extractor.go:119`
- **Why:** `regexp.MustCompile` per call; runs once per skill during
  `CapabilityIndex.Rebuild` (100 skills → 100 compilations).
- **Status:** **DEFERRED** — hoist to package-level `var`.

#### S2-15 `ConfirmPlan` only allows `StateCompleted`
- **File:** `internal/plan/manager.go:261-302`
- **Why:** Plans in `failed` or `cancelled` (which `IsTerminal()` returns
  true for) cannot be confirmed, even though the signoff table supports
  `Action="confirmed"` regardless.
- **Status:** **DEFERRED** — API-shape decision; needs UX input.

#### S2-16 Plan handler has no retry/backoff on bus handler error
- **File:** `internal/plan/handler.go:76-101`
- **Why:** Transient SQLite busy/locked errors are logged and dropped —
  phase progress permanently undercounted, plan can never reach
  `completed` via `OnStepCompleted`.
- **Status:** **DEFERRED** — needs DLQ/retry design.

#### S2-17 LSP `Manager.StopServer` removes from map before Close
- **File:** `internal/code/lsp/manager.go:174-200`
- **Why:** `delete` precedes `Client.Close`/`Transport.Close`; on failure
  the OS process / TCP socket may still be alive with no manager handle
  to reach it.
- **Status:** **DEFERRED** — small re-ordering fix; verify no callers
  rely on the current order.

#### S2-18 `parseCompositeDuration` `HasSuffix` greedy `m` vs `ms` bug
- **File:** `internal/config/json5_loader.go:224-250`
- **Why:** For `1m30ms`, the `m` suffix loop consumes `1m` correctly,
  but then the `s` suffix loop runs `HasSuffix("30ms","s")` → true and
  consumes the `s` inside `ms`, leaving `raw="30m"`. Wrong duration.
- **Status:** **DEFERRED** — switch to `go.time.ParseDuration` or a
  longest-match regex.

#### S2-19 Plan IDs use timestamp + reset-on-restart atomic counter
- **File:** `internal/plan/plan.go:88-107`
- **Why:** Same pattern as round-4 `generatePatternID`. Counter resets on
  every daemon restart → collision risk if timestamps also collide
  (rapid CI restarts). Three call sites: `generatePlanID`,
  `generatePhaseID`, `generateSignoffID`.
- **Status:** **DEFERRED** — mechanical replacement with
  `pkg/id.Generate("plan-")` etc.

#### S2-20 `positionToByte` silently clamps to EOF
- **File:** `internal/code/ast/rewrite.go:254-269`
- **Why:** Out-of-range (line, char) returns `len(source)`; `ApplyEdits`
  then splices at EOF — appending rather than replacing.
- **Status:** **DEFERRED** — needs `(int, error)` signature change.

#### S2-21 `GoLinter.TypeCheck` always uses stderr, drops unrecognized errors
- **File:** `internal/lint/languages/go_lint.go:86-105`
- **Why:** `go build` failures that don't match the parse regex (linker
  errors, "command not found") are silently dropped.
- **Status:** **DEFERRED** — small fix to surface raw stderr as a
  fallback `LinterResult`.

#### S2-22 `ASTResolveTool` writes file with mode `0o000`
- **File:** `internal/code/tools/ast_resolve.go:116`
- **Why:** `os.WriteFile(path, data, 0)` — if the file does not already
  exist, it is created with mode `000` (unreadable by anyone).
- **Status:** **FIXED** — changed to `0o644` for consistency with
  `ast_edit.go` / `lsp_rename.go`.

#### S1-9 `SessionTracker.GetIdleSessions` returns slice of live pointers
- **File:** `internal/agent/session_tracker.go:335-349`
- **Why:** Same shape as S1-5; lower severity because callers only read
  persistence-relevant fields and `PersistIdleSessions` has a TOCTOU
  re-check.
- **Status:** **DEFERRED** — same fix as S1-5.

#### S1-10 `TeamOrchestrator.Status` returns live pointer
- **File:** `internal/agent/team_orchestrator.go:384-396`
- **Why:** RLock released on return; caller gets live
  `*TeamSessionState`. `MemberResults` is a map — concurrent
  `ReceiveResult` writes racing with iteration can panic. Pair
  orchestrator solved this by returning a snapshot struct.
- **Status:** **DEFERRED** — define `TeamSessionStateSnapshot` and
  deep-copy under lock.

#### S1-11 `generateMessageID` fallback uses timestamp (no entropy)
- **File:** `internal/agent/handler.go:1378-1386`
- **Why:** Only reached on `crypto/rand.Read` failure (rare, but possible
  on entropy-starved kernels during early boot). Two callers in the same
  ns produce identical IDs.
- **Status:** **DEFERRED** — switch fallback to `pkg/id.Generate()` and
  bump primary `randBytes` from 4 to 16 bytes.

---

## Fix Phase

The fix phase applied the `oneshot-yeet` skill to resolve findings
systematically. Below is the final accounting. "Fixed" means the change
was made, build + vet + affected tests pass, and the fix was verified by
re-reading the source.

### Issues Fixed

#### Critical (5/5 fixed)
- S4-1 WebSearchTool SSRF — `DialContext: ssrfDialContext(false)` added
- S4-2 MCP HTTPTransport SSRF — new local `ssrfDialContext(false)` in
  transport package; `SetAllowPrivateRanges(allow)` test hook added
- S6-1 Cluster signature bypass via empty `Signature` — rejected when
  `RequireNodeSignatures` is true
- S6-2 `reclaimJobUnlocked` nil-deref on `cq.store.ResetToPending` —
  nil guard added
- S8-1 Hardcoded dev key fallback in `api_client.dart` — `else` branch
  deleted; release builds now reject unauthenticated requests

#### S2 Critical (2/2 fixed — addendum)
- S2-1 LSP `TCPTransport.Write` mutex — `writeMu sync.Mutex` added
- S2-2 `LoadJSON5` `%s` → `%w` so `LoadJSON5WithDefault` works

#### High (17/17 fixed)
- S4-3 `ast_edit` fence — `SetFenceChecker` + `CheckPath` before write
- S4-4 `resolve_ast_edit` fence — same pattern
- S4-4b `lsp_writethrough.applyFormattingEdits` — converted to method
  so it can use the existing `fenceChecker`
- S4-5 git `--porcelain` path parsing — fixed-width extraction (status
  `line[:2]`, path `line[3:]`) replaces `strings.Fields` so paths with
  spaces survive; rename branch handles `\t`-separated paths
- S4-6 `DebugTool` fence — `SetFenceChecker` validates `program` /
  `core_file` / `script_file` / `cwd` before any action runs
- S4-8 `CronCreateTool` double-error (`return result, err`) — changed to
  `return result, nil` matching every other error path
- S5-5 (notification WS origin check) — see main body
- S6-3 (cluster queue I/O under mutex — caller side)
- S7-1 `cli-%d-%d` conversation ID — `pkg/id.Generate("cli-")`
- S7-10 Dashboard historical-data view wiring
- Plus round-4-style cluster/queue/worker/daemon/rpc/gossip ID
  replacements (8 sites) and `tool_web_search` SSRF dial-in
- S1-1 Agent predictable IDs (9 sites in `internal/agent/`) — addendum
- S1-2 + S1-6 `EscalationManager.Escalate` — lock released around SQLite
  query, TOCTOU re-check on re-acquire
- S1-3 `QueuePersister.EnqueueAsync` — flush moved out of the lock;
  dead helpers removed
- S1-4 `ArtifactManager.artifactCache` — `sync.RWMutex` added
- S2-3 LSP `StartServer` TOCTOU — duplicate-spawn loser is closed
- S2-4 RepoMap cache swap — refactor eliminates the swap
- S2-5 Skills `<=` → `<`

#### Medium (3/8 fixed)
- S2-8 Hermes `checkEnvVar` shell injection — replaced with
  `os.LookupEnv`; error message updated to match integration test
- (Plus main-body fixes from S4/S5/S6/S7/S8 — see main document)

#### Low (2/12 fixed)
- S2-22 `ast_resolve` file mode `0` → `0o644`

### Issues Deferred (Follow-up Round)

The initially-deferred items below were resolved in a follow-up
`oneshot-yeet` pass that dispatched 6 parallel fixer subagents (one
per cluster). Each item below is annotated with its resolution.
The only items still marked DEFERRED after the follow-up are S4-10
(requires MCP-spec conformance test, out of scope), S2-7 (needs
per-step state map refactor — punted to round 6), and the round-4
mutexio analyzer enhancement.

**S1 cluster — all 6 resolved:**
- **S1-5** ✅ `GetSession` now returns a deep copy via `cloneTrackerSessionState`
  (`internal/agent/session_tracker.go`).
- **S1-7** ✅ Both `classifyIntent` call sites in `dispatcher.go` now log
  a Warn on non-nil error.
- **S1-8** ✅ Added `Seq uint64` field to `EscalationLevel` and
  `seq atomic.Uint64` to `EscalationManager`; incremented under lock.
- **S1-9** ✅ `GetIdleSessions` returns deep copies (same helper as S1-5).
- **S1-10** ✅ Defined `TeamSessionStateSnapshot`; `Status` copies
  `MemberResults` map and `Roster` slice under RLock.
- **S1-11** ✅ `generateMessageID` fallback calls `id.Generate("msg")`;
  primary `randBytes` bumped 4→16.

**S2 cluster — 14 of 15 resolved (S2-7 still deferred):**
- **S2-6** ✅ Plan parser scanner buffer raised to 1MB.
- **S2-7** ⏩ Still deferred — requires per-step state map refactor.
- **S2-9** ✅ `weightedLine` now uses an `atomic.Int64` edge counter
  instead of `from.ID()*1e9 + to.ID()` packing. Regression test added.
- **S2-10** ✅ Applier returns an error on ambiguous (multi-occurrence)
  snippets instead of patching the first textual match.
- **S2-11** ✅ Single RLock wraps both personalization loops.
- **S2-12** ✅ `ProposedEdit` now carries `StartByte`/`EndByte`;
  `ApplyEdits` uses them directly when populated.
- **S2-13** ✅ Dead Warn block filled with a real `slog.Warn`.
- **S2-14** ✅ Regex hoisted to package-level `var nameSplitter`.
- **S2-15** ✅ `ConfirmPlan` now accepts `completed`, `failed`, `cancelled`.
- **S2-16** ✅ Plan handler retries transient SQLite busy errors 3×
  (50/100/200ms backoff).
- **S2-17** ✅ `delete(m.servers, name)` moved after Close calls.
- **S2-18** ✅ Replaced hand-rolled `parseCompositeDuration` with
  `time.ParseDuration` plus a `d`→`h` preprocessor.
- **S2-19** ✅ All three ID generators call `id.Generate`.
- **S2-20** ✅ `positionToByte` returns `(int, error)`; `ApplyEdits`
  skips with Warn on out-of-range.
- **S2-21** ✅ `GoLinter.TypeCheck` surfaces raw stderr as a fallback
  `LinterResult` when no errors match the parse regex.

**S4 cluster — 9 of 10 resolved (S4-10 still deferred):**
- **S4-7** ✅ All 7 tool-package sites converted to `id.Generate(...)`
  (`platform.go`, `tool_schedule_create.go`, `tool_cron_create.go`,
  `file_edit.go`, `review_tools.go`, `ast_edit.go`, `lsp_rename.go`).
- **S4-9** ✅ `handleToolsCall` sets `"isError": true` on tool error.
- **S4-10** ⏩ Still deferred — needs MCP-spec conformance test for
  `AskTool` `TerminateHint`.
- **S4-11** ✅ SSE parser: 10MB scanner buffer, both `data: ` and
  `data:` prefixes, surfaces `bufio.ErrTooLong`.
- **S4-12** ✅ `Client.Connect` snapshots `len(c.tools)` under RLock.
- **S4-13** ✅ Bubble sort replaced with `sort.Ints`.
- **S4-14** ✅ `rawToMap` logs `slog.Debug` on unmarshal failure.
- **S4-15** ✅ `const Version = "0.2.0"` in `internal/mcp`; client
  references `ClientVersion` with a cross-reference comment.
- **S4-16** ✅ `StdioTransport.Close` signals `stderrDone` (via
  `sync.Once`); drain goroutine exits cleanly.
- **S4-17** ✅ `orderedMap` now a struct with `[]string` key slice +
  backing map; `MarshalJSON` preserves insertion order.
- **S4-18** ✅ `ExtractJSONFromText` tries each strategy in turn and
  only stops when one both finds and parses content.

**S5 cluster — all 11 resolved:**
- **S5-2** ✅ CORS `*` replaced with `isLocalOrigin` echo in both branches.
- **S5-3** ✅ NotificationHandler token-extraction dead code deleted;
  relies on middleware.
- **S5-6** ✅ `BusService.Stats` uses presence checks.
- **S5-8** ✅ `handleChatStream` subscribes to both `agent.progress`
  and `agent.progress.synthesized`.
- **S5-9** ✅ Dead method check + TrimPrefix fallback removed from
  `handleChatQueueStatus`.
- **S5-10** ✅ All `http.Error` in `api_handlers.go` replaced with
  `s.writeError`.
- **S5-11** ✅ `handleReload` returns the error to dispatch.
- **S5-12** ✅ `acceptLoop` returns on `net.ErrClosed` and sleeps 50ms
  on transient errors.
- **S5-13** ✅ `handleBusUnsubscribe` deletes synchronously before
  returning.
- **S5-14** ✅ MCP tool handlers thread `r.Context()` through
  `processMCPRequest`.
- **S5-15** ✅ Already migrated to `id.Generate` in a prior commit
  (verified; no change needed).

**S6 cluster — all 9 resolved:**
- **S6-7** ✅ `Claim` slow path translates `ErrJobAlreadyClaimed` →
  `ErrNoJobAvailable`.
- **S6-8** ✅ `CheckNodeReachability` takes `ctx` and uses
  `QueryRowContext`.
- **S6-9** ✅ `pty.Manager.Close` snapshots IDs under lock, releases,
  then destroys each session.
- **S6-10** ✅ `debug.Client` stores a `cancelFunc`; `Close` cancels
  before killing the subprocess, unblocking `readLoop`.
- **S6-11** ✅ Already migrated to `id.Generate` in a prior commit
  (verified; no change needed).
- **S6-13** ✅ `sentEvents` upgraded to `map[string]time.Time`; pruning
  evicts 500 oldest (LRU).
- **S6-14** ✅ `Pool.Scale` uses RLock for count read; documented
  collect-under-lock pattern.
- **S6-15** ✅ Empty-body `if` deleted from `parseGDBVariable`.
- **S6-16** ✅ `scanClusterMember` validates
  `len(signingPubRaw) == ed25519.PublicKeySize` (or 0).

**S7 cluster — all 15 resolved:**
- **S7-2** ✅ `saveConfig` parameter renamed to `filePath`.
- **S7-3** ✅ `analytics.go` prints `"n/a"` when `AvgCost == 0`.
- **S7-4** ✅ `runTUI` returns an error on `--transport=http`.
- **S7-5** ✅ Deleted unwired `startAtLogin` / `showInMenuBar` per
  CLAUDE.md "no stub code" rule.
- **S7-6/7/8/19** ✅ Comprehensive lowercase sweep across `cmd/meept/*.go`
  and `menubar/MeeptMenuBar/**/*.swift` (status, memory, jobs, workers,
  cluster_cmd, cache, NotificationCenterMenuView, SettingsWindow, Presets,
  DaemonController).
- **S7-9** ✅ `DaemonStatusViewModel` split into `isRefreshingStatus`
  and `isControllingDaemon`.
- **S7-10** ✅ `HistoricalReportView` renders `historicalData` in a `List`;
  `MetricPoint` conforms to `Identifiable`.
- **S7-13** ✅ `pluralize` renamed to `pluralizeEntry`.
- **S7-14** ✅ `NotificationManager` has `deinit`, `disconnect`,
  `reconnect`; `print()` → `Logger`.
- **S7-15** ✅ `WebSocketManager.connect` guards `urlSession` before
  flag flip; `print()` → `Logger`.
- **S7-16** ✅ `MenubarConfigService.loadConfig` uses `Logger.error`.
- **S7-17** ✅ `APIClient.makeRequest(requiresAuth: true)`; `/health`
  passes `false`.
- **S7-18** ✅ Asymmetry documented in `cmd/meept/main.go`.
- **S7-20** ✅ `LoadClientConfig` warns on missing/invalid fields and
  applies explicit defaults via `checkClientConfigDefaults`.

**Test flakes — both resolved:**
- `TestAgentHandler_SessionPersistence` ✅ `saveSessions` and
  `persistSessions` now use a `writeAtomic` (temp + rename) helper;
  background persistence in `getOrCreateSession` made synchronous.
- `TestApp_SessionsKeyNavigatesToView` ✅ `Modal.HandleKey` does
  case-insensitive matching for single-character keys (preserves exact
  match for multi-char bindings like `ctrl+s`).

### Verification (Follow-up Round)

- `go build ./...` — passes clean
- `go vet ./...` — passes clean
- `go test -count=1 -timeout 300s ./...` — **all packages pass**, zero
  failures, zero flakes (including the two pre-existing flakes, now fixed)
- Telegram persistence verified with `-count=10`; TUI sessions-key test
  verified with `-count=10`
- MCP transport tests updated to call `transport.SetAllowPrivateRanges(true)`
  so the SSRF dialer accepts `httptest.NewServer` (127.0.0.1)
- Swift: `swift build` in `menubar/` completes with 0 errors / 0 warnings

