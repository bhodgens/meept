# S7: CLI + TUI + MenuBar (Swift) Review

**Scope:** `cmd/meept/*.go`, `internal/tui/*.go`, `menubar/MeeptMenuBar/**/*.swift`
**Reviewer:** Round 5
**Classes scanned:** CLI flag wiring, CLI/RPC integration, TUI lifecycle, TUI concurrency, MenuBar/Swift, Auth/API key, Predictable IDs, Error swallowing, Lowercase UI convention, Stale/dead code.

---

## Critical

### S7-1 Predictable conversation ID in CLI single-message mode

**File:** `cmd/meept/chat.go:89`
```go
conversationID := fmt.Sprintf("cli-%d-%d", os.Getpid(), time.Now().UnixNano())
```

**Bug:** Conversation ID is built from `os.Getpid()` + nanosecond timestamp. Both are predictable: PID is sequence-assigned by the kernel and observable via `ps`, and `UnixNano()` is just wall-clock time. An attacker who can observe approximate invocation time (e.g. via shell history timestamps or logs) can forecast the conversation ID for an hour or more of the day. The codebase already has `pkg/id.Generate()` (called out in MEMORY.md as the fix for "predictable IDs (`time.Now().UnixNano()`)" — recurring bug pattern, round 4).

**Severity rationale:** A conversation ID alone is not a bearer token, but when HTTP transport is enabled (`require_auth: false` misconfiguration or a leaked API token), conversation IDs effectively gate per-conversation access in several RPC handlers. Even on Unix socket RPC, predictable IDs enable a local attacker to race the session store and collide on `cli-<pid>-<ts>` IDs, attaching state to another user's session.

**Fix:** Use `pkg/id.Generate()` (or `crypto/rand` 16-byte hex) for the conversation ID. Drop the timestamp+pid scheme entirely.

---

## High

### S7-2 `configPath` parameter in `saveConfig` shadows package-level `configPath()` function

**File:** `cmd/meept/token.go:92`
```go
func saveConfig(configPath string, v hujson.Value) error {
    dir := filepath.Dir(configPath)
    ...
}
```

**Bug:** `configPath` is already declared as a package-level function at `token.go:59` (`func configPath() (string, error)`). Inside `saveConfig`, the parameter name `configPath` shadows the function. Any future edit that calls `configPath()` from inside `saveConfig` (e.g. to fall back to the default location when the caller passes an empty string) will resolve to the parameter (a `string`) and fail to compile with `call of non-function configPath`. This is a latent footgun for maintainers. Additionally, `loadConfigForModification()` returns the path from the package-level `configPath()` function, and `saveConfig(cp, v)` is invoked with that same path — the parameter shadowing is unnecessary and the signature should accept a generic `path string` instead.

**Severity rationale:** Does not break anything today, but shadows on package-level identifiers are a recurring source of bugs. Triggers on the next edit and fails compilation in a confusing way.

**Fix:** Rename the parameter to `path` (or `filePath`). Keep the package-level `configPath()` function name.

---

### S7-3 `analytics.go` avg-cost sentinel `-1` surfaces in user-facing output with no explanation

**File:** `cmd/meept/analytics.go:299-301`
```go
avgCost := r.AvgCost
if avgCost == 0 {
    avgCost = -1 // Indicate no data
}
fmt.Fprintf(w, "%-30s\t%d\t%.1f%%\t%.2f\t%.0f\n",
    modelID, r.Tasks, r.SuccessRate, avgCost, r.AvgDuration)
```

**Bug:** When no cost data is available (`AvgCost == 0`), the code substitutes `-1` and prints `-1.00` in the AVG_COST column with no legend. A user reading the table sees `model-X 5 100.0% -1.00 1.2s` and has no way to distinguish "no data" from "the model paid us $1 to use it" without reading source. Every other "no data" cell in the CLI uses `n/a` or `-`. The sentinel also corrupts downstream consumers: any pipeline piping the table through `awk` sees a literal `-1.00` and quietly produces wrong averages.

**Severity rationale:** Silent data corruption for downstream pipelines; misleading UI output. Not a crash, but erodes trust in the analytics view.

**Fix:** Print `n/a` (string) when `AvgCost == 0` instead of writing `-1` through `%f`. The tabwriter handles mixed-width columns.

---

### S7-4 TUI ignores `--transport=http` for the TUI path but silently falls back instead of erroring out

**File:** `cmd/meept/chat.go:134-138`
```go
func runTUI() error {
    if transportFlag == "http" {
        fmt.Fprintln(os.Stderr, "warning: TUI requires RPC transport (event streaming); falling back to RPC socket")
    }
    app := tui.NewApp(getSocketPath())
    ...
}
```

**Bug:** The TUI requires the RPC socket for event streaming (legitimate architectural constraint). However, when a user passes `--transport=http`, the TUI prints a warning to stderr and **silently uses the RPC socket at the default path** — it does not consult `--socket` overrides, does not exit with a non-zero status, and does not surface the failure mode to scripts. A user who has explicitly configured HTTP-only transport (e.g. to disable Unix sockets for security) will see the TUI "work" while violating their stated transport policy.

The warning text is also misleading: it says "falling back to RPC socket" but `tui.NewApp(getSocketPath())` uses the socket path the user already configured. If the user also passed `--socket=/custom/path`, the warning lies about "falling back" when in fact the explicit socket is honored.

**Severity rationale:** User-facing config flag is silently ignored. Breaks shell scripts that gate behavior on `--transport=http` exit codes.

**Fix:** Either honor `--transport=http` by using the HTTP transport client for the non-streaming RPCs (significant work), or return a non-zero error from `runTUI` when `--transport=http` is set and exit cleanly. At minimum, the warning should be an error and the wording should say "TUI requires RPC; ignoring --transport=http".

---

### S7-5 `MenubarConfigService.startAtLogin` and `showInMenuBar` exposed but never honored

**File:** `menubar/MeeptMenuBar/Services/MenubarConfigService.swift:86-92`
```swift
var showInMenuBar: Bool {
    return config.ui.showInMenuBar
}

var startAtLogin: Bool {
    return config.ui.startAtLogin
}
```

**Files checked:** `main.swift`, `AppDelegate.applicationDidFinishLaunching`, no `SMAppService` / `SMLoginItemSetEnabled` / `LaunchAtLogin` references anywhere in `menubar/`.

**Bug:** The menubar app advertises two UI-config knobs (`show_in_menu_bar`, `start_at_login`) in `MenubarConfig` and even decodes them from `~/.meept/menubar.json5`, but neither property is read by `AppDelegate` or any view. A user who sets `"start_at_login": true` expecting the app to register itself as a macOS login item (via `SMAppService.main.register()` on modern macOS or `SMLoginItemSetEnabled` on older SDKs) gets no behavior change. Similarly `show_in_menu_bar: false` should presumably hide the status item but does nothing.

This is a "feature looks wired but isn't" bug — the user-facing settings UI (if/when added) will appear to offer the option but silently fail.

**Severity rationale:** Dead config surface; undermines trust in settings. Not a crash.

**Fix:** Either wire the properties (call `SMAppService.main.register()` from `AppDelegate.applicationDidFinishLaunching` when `startAtLogin == true`, and skip creating the status item when `showInMenuBar == false`), or delete the fields from `MenubarConfig` until they ship.

---

### S7-6 `NotificationCenterMenuView` / `SettingsWindow` / `Presets` use Title Case UI text

**Files & lines:**
- `menubar/MeeptMenuBar/Views/NotificationCenterMenuView.swift:15` — `Text("No notifications")`
- `menubar/MeeptMenuBar/Views/NotificationCenterMenuView.swift:22` — `Button("Clear All")`
- `menubar/MeeptMenuBar/Views/NotificationCenterMenuView.swift:27` — `Toggle("Enable Notifications", isOn: ...)`
- `menubar/MeeptMenuBar/Views/Settings/SettingsWindow.swift:57` — `Text("Failed to save configuration. Please try again.")`
- `menubar/MeeptMenuBar/Views/Settings/SettingsWindow.swift:65` — `Text("Failed to normalize JSON5. Please check your syntax.")`
- `menubar/MeeptMenuBar/Models/Presets.swift:29-61` — preset labels: `"Development"`, `"Debugging"`, `"Planning"`, `"Creative Writing"`, `"Research"`, `"Fast"`, `"Detailed"` (these `label` fields are user-facing if presets are ever rendered)

**Project rule (`CLAUDE.md`):** "All UI element text must be explicitly lowercase (e.g., 'switch' not 'Switch', 'ok' not 'OK') — applies to button labels, menu items, tooltips, status messages, dialog titles, hints."

**Bug:** Multiple user-visible strings in the menubar app violate the project's lowercase-UI convention. The first two findings (`S7-1` through `S7-5`) are technical defects; this one is a project-rule violation. Round 4 (per MEMORY.md) explicitly flagged and fixed `TextCapitalization` as a recurring bug class.

**Severity rationale:** Visible convention drift. The menubar app is new since rounds 1-4 and slipped through without a lowercase-text sweep.

**Fix:** Lowercase every user-facing literal: "no notifications", "clear all", "enable notifications", "failed to save configuration. please try again.", "failed to normalize json5. please check your syntax.", "development", "debugging", "planning", "creative writing", "research", "fast", "detailed".

---

## Medium

### S7-7 CLI output extensively uses Title Case — violates lowercase UI convention

**Files & lines (representative, not exhaustive):**
- `cmd/meept/status.go:95` — `fmt.Printf("Meept Daemon Status\n")`
- `cmd/meept/status.go:115` — `fmt.Printf("Token Budget\n")`
- `cmd/meept/status.go:139` — `fmt.Printf("Cost Budget\n")`
- `cmd/meept/status.go:154` — `fmt.Printf("RPC Server\n")`
- `cmd/meept/status.go:45` — `fmt.Println("Daemon is not running")`
- `cmd/meept/memory.go:85` — `fmt.Printf("Exported %d memories\n", int(count))`
- `cmd/meept/memory.go:115` — `fmt.Println("No memories found")`
- `cmd/meept/memory.go:304` — `fmt.Println("Vector Shard Statistics")`
- `cmd/meept/memory.go:314` — `fmt.Println("Shard Details:")`
- `cmd/meept/jobs.go:37` — `fmt.Println("No scheduled jobs")`
- `cmd/meept/jobs.go:42` — `fmt.Printf("%-20s %-20s %-25s %-10s\n", "NAME", "SCHEDULE", "NEXT RUN", "STATUS")`
- `cmd/meept/workers.go:53` — `fmt.Println("Worker Pool Status")`
- `cmd/meept/workers.go:81` — `fmt.Println("No workers running")`
- `cmd/meept/workers.go:149` — `fmt.Printf("Worker pool scaling to %d workers\n", targetCount)`
- `cmd/meept/cluster_cmd.go:223` — `fmt.Println("     Cluster Initialized Successfully")`
- `cmd/meept/cluster_cmd.go:226-230` — `Cluster:`, `Cluster ID:`, `Node ID:`, `Config:`, `Private Key:`
- `cmd/meept/cluster_cmd.go:232` — `fmt.Println("  IMPORTANT: Keep the private key file secure!")`
- `cmd/meept/cluster_cmd.go:242` — `fmt.Println("Next steps:")`

**Project rule (`CLAUDE.md`):** "All UI element text must be explicitly lowercase" — CLI status output is user-facing UI.

**Bug:** Pervasive title-case strings in CLI output. Round 4 fixed the TUI's TextCapitalization (per MEMORY.md); the CLI side was never swept. Status text, headers, banners, and help strings ("Daemon is not running", "Meept Daemon Status", "Worker Pool Status", "Cluster Initialized Successfully", etc.) all violate the lowercase rule.

**Severity rationale:** Style/convention drift only — does not break functionality, but the project rule is explicit and listed in CLAUDE.md. Dozens of `fmt.Println` calls need adjustment. Not a single localized fix.

**Fix:** Sweep all `fmt.Print*` strings in `cmd/meept/*.go` and lowercase the first word of every sentence/label. Column headers in tabular output (`"NAME"`, `"SCHEDULE"`, `"NEXT RUN"`, etc.) should also be lowercased to match the rule.

---

### S7-8 `DaemonError` error descriptions use Title Case and read inconsistently

**File:** `menubar/MeeptMenuBar/Services/DaemonController.swift:149-161`
```swift
var errorDescription: String? {
    switch self {
    case .plistNotFound: return "launchd plist not found"
    case .loadFailed(let output): return "Failed to load: \(output)"
    case .unloadFailed(let output): return "Failed to unload: \(output)"
    case .kickstartFailed(let output):
        if output.isEmpty {
            return "launchd kickstart returned non-zero status (daemon may not have started)"
        }
        return "launchd kickstart failed: \(output)"
    }
}
```

**Bug:** Two of four error strings start with capital "Failed to" while the other two are lowercase. The capitalized ones surface in macOS alert dialogs (via `error.localizedDescription`), which violates the project's lowercase UI convention.

**Fix:** Lowercase the leading "Failed" in both `.loadFailed` and `.unloadFailed` cases to "failed to load:" / "failed to unload:".

---

### S7-9 `DaemonStatusViewModel.refreshStatus` and control methods race on `isUpdating` from multiple `Task` contexts

**File:** `menubar/MeeptMenuBar/ViewModels/DaemonStatusViewModel.swift:53-66` (and the `startDaemon`/`stopDaemon`/`restartDaemon` methods at 70-113)
```swift
func refreshStatus() {
    guard !isUpdating else { return }
    isUpdating = true
    Task { [weak self] in
        guard let self else { return }
        do {
            daemonStatus = try await apiClient.getDaemonStatus()
            onStatusChanged?()
        } catch {
            logger.error("failed to fetch daemon status: \(error.localizedDescription)")
        }
        isUpdating = false
    }
}
```

**Bug:** `isUpdating` is a `@Published var Bool` on a `@MainActor` class — so the `guard !isUpdating` check and the `isUpdating = true` mutation are main-actor isolated and safe in isolation. However, the method is invoked from a `Timer` fire callback via `Task { @MainActor [weak self] in self?.refreshStatus() }` (line 36-40) AND also called at the end of `startDaemon` / `stopDaemon` / `restartDaemon` (lines 81, 96, 111). Each control method itself also guards on `isUpdating` and sets it before spawning its own `Task`.

The race: if a user clicks "restart daemon" and the timer fires mid-flight, the timer's `refreshStatus()` sees `isUpdating == true` (set by `restartDaemon`) and bails — the user sees stale status. Conversely, if `refreshStatus` runs first and then the user clicks restart, `restartDaemon` bails — the user's click is silently dropped with no UI feedback. The flag conflates "any control operation is in flight" with "a status refresh is in flight", which produces surprising UI under rapid clicks.

**Severity rationale:** UI controls get silently disabled mid-operation; user clicks are dropped without feedback. Not a data-corruption race (`@MainActor` serializes the actual mutations), but a UX race.

**Fix:** Use separate flags for `isRefreshingStatus` vs `isControllingDaemon`, or a single `isBusy` flag that the UI explicitly binds to a spinner overlay (rather than gating the methods). At minimum, surface a "daemon is busy" toast when guard fails instead of silently dropping.

---

### S7-10 `LiveMetricsView` / `HistoricalReportView` show "loading..." forever after a successful fetch with no rows

**File:** `menubar/MeeptMenuBar/Views/Analytics/DashboardWindow.swift:60-74`
```swift
Button("load") {
    metricsViewModel.fetchHistorical()
}
.disabled(metricsViewModel.isLoadingHistorical)

if metricsViewModel.isLoadingHistorical {
    ProgressView("loading...")
        .padding()
} else {
    Text("select date range and click load")
        .foregroundColor(.secondary)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
}
```

**Bug:** The "historical" tab of the dashboard has no rendering at all for the fetched `historicalData: [MetricPoint]` array. After `fetchHistorical()` succeeds, the view flips from "loading..." directly back to "select date range and click load" — the data is dropped on the floor. A user who loads a 24-hour window of metrics sees no chart, no table, no summary, just the same empty hint. The `historicalData` published property is written to in the view model but never read by any view.

**Severity rationale:** Broken feature — the entire "historical" tab is non-functional. The code does not crash but the user cannot see any data.

**Fix:** Render `metricsViewModel.historicalData` — even a simple `List(metricsViewModel.historicalData) { point in Text("\(point.timestamp): \(point.value)") }` or a `SwiftUI Charts` `Chart { ForEach(historicalData) { LineMark(...) } }`. Show an explicit "no data in range" message when the array is empty but a fetch has completed.

---

### S7-11 `MenuBarContentView` state-color cases shadow `DaemonStatusViewModel.statusImage` logic

**File:** `menubar/MeeptMenuBar/Views/MenuBarContentView.swift:16-32` (duplicated from `DaemonStatusViewModel.swift:117-132`)
```swift
// MenuBarContentView
private var stateIcon: String {
    switch daemonStatusVM.daemonStatus.state {
    case .offline: return "power"
    case .idle: return "checkmark.circle"
    case .working: return "gearshape.2.fill"
    case .error: return "exclamationmark.triangle.fill"
    }
}

private var stateColor: Color {
    switch daemonStatusVM.daemonStatus.state { ... }
}
```

**Bug:** The icon-to-state mapping is duplicated between `MenuBarContentView.stateIcon` and `DaemonStatusViewModel.statusImage` — same four cases, same symbols. There is no `stateColor` equivalent on the view model, but adding one would require updating two locations. As the daemon adds new `DaemonState` cases (e.g. `.starting`, `.degraded`), every switch must be updated in lockstep; the compiler will warn about non-exhaustive switches, but only if the enum is in the same module.

**Severity rationale:** Inconsistency hazard; duplicated mapping. Low impact today but a maintenance trap.

**Fix:** Move both `stateIcon` and `stateColor` to the view model (or to an extension on `DaemonState`). `MenuBarContentView` should reference `daemonStatusVM.stateColor` and `daemonStatusVM.stateIcon` (or the existing `statusImage`).

---

### S7-12 `main.swift` global `appDelegate` relies on `MainActor.assumeIsolated` and a top-level `app.run()` blocking call

**File:** `menubar/MeeptMenuBar/main.swift:127-131`
```swift
let app = NSApplication.shared
let appDelegate = MainActor.assumeIsolated { AppDelegate() }
app.delegate = appDelegate
app.run()
```

**Bug:** Top-level `app.run()` is a blocking call that never returns — the process exits only when `NSApp.terminate(nil)` is invoked from the "quit" menu action (line 58). This is standard for an AppDelegate-based menubar app, but the structure has two issues:

1. **No `defer` or cleanup hook between `applicationWillTerminate` and the actual exit.** `applicationWillTerminate` (`main.swift:72-74`) only calls `daemonStatusVM.stopPolling()`. The `metricsVM` and `configVM` are never explicitly torn down, and `NotificationManager.shared.websocket` is never disconnected (NotificationManager has no `deinit` and no `disconnect()` method). The WebSocket leaks until process exit, which is OK for terminate but problematic if the app is ever refactored into a non-terminating background process.

2. **`MainActor.assumeIsolated` on a top-level let** is correct but fragile. If `main.swift` is ever moved into a `@main` struct (Swift's recommended modern pattern), the synchronous `app.run()` blocks the main actor forever and the Swift concurrency runtime will flag the blocking call.

**Severity rationale:** Cleanup is incomplete on the WebSocket and on settings/config view models. No crash today, but `NotificationManager.shared` is a singleton that retains its WebSocket across the app lifetime.

**Fix:** Add a `disconnect()` method on `NotificationManager` and call it from `applicationWillTerminate`. Consider adopting `@main struct MeeptMenuBarApp: App` for future-proofing.

---

## Low

### S7-13 `cache.go:pluralize` is a cute micro-helper that's only correct for exactly one word

**File:** `cmd/meept/cache.go:226-231`
```go
func pluralize(n int) string {
    if n == 1 {
        return "y"
    }
    return "ies"
}
```

**Bug (minor):** The helper exists to pluralize "entry"/"entries" via `fmt.Printf("Found %d cache entr%s...", resp.Count, pluralize(resp.Count))`. It hard-codes the `y`/`ies` suffix for exactly the word "entry" and cannot be reused for any other noun. Future authors who see `pluralize` in the package will assume a generic helper and call it with other words ("Found %d task%s" — produces "tasky" / "taskies"). The function name lies about its generality.

**Severity rationale:** Cosmetic / readability. No user-facing impact today.

**Fix:** Either rename to `pluralizeEntry(n int) string` for clarity, or make the helper take the singular noun: `pluralize(n int, singular string) string` and handle a few common suffixes.

---

### S7-14 `NotificationManager` has no `deinit` and never disconnects its WebSocket

**File:** `menubar/MeeptMenuBar/Services/NotificationManager.swift:11-39`
```swift
class NotificationManager: NSObject, ObservableObject {
    static let shared = NotificationManager()
    private var websocket: WebSocketManager?
    ...
    private override init() {
        self.configService = MenubarConfigService()
        super.init()
        requestAuthorization()
        setupWebSocket()
    }
    // no deinit, no disconnect() method
}
```

**Bug:** `NotificationManager` is a singleton (`static let shared`), so the instance lives forever and there's no leak per se. However:
1. There is no `disconnect()` method, so callers cannot pause notifications (the `setEnabled(false)` path only clears pending requests, not the live WebSocket).
2. `setupWebSocket()` is called once from init; there is no way to re-run it after the user changes `daemon.http_url` in settings — the WebSocket keeps targeting the old URL.
3. When `setEnabled(false)` is toggled in the menu (line 27 of `NotificationCenterMenuView`), the WebSocket keeps delivering events that are filtered only at the `showLocalNotification` level — they still decode, still insert into `notifications`, still fire `@Published` updates that trigger SwiftUI re-renders.

**Severity rationale:** Settings changes to daemon URL don't take effect without an app restart. Minor.

**Fix:** Add `disconnect()` and `reconnect()` methods. Call `reconnect()` from a settings-change hook. Gate `handleWebSocketMessage` on `isEnabled`.

---

### S7-15 `WebSocketManager.connect()` flag-flip ordering leaves `isConnecting` set if `webSocketTask?.resume()` is never called

**File:** `menubar/MeeptMenuBar/Services/WebSocketManager.swift:53-71`
```swift
func connect() {
    guard !isConnecting else { return }
    isConnecting = true
    shouldReconnect = true
    reconnectAttempts = 0

    var request = URLRequest(url: baseURL.appendingPathComponent("/ws/notifications"))
    ...
    webSocketTask = urlSession?.webSocketTask(with: request)
    webSocketTask?.resume()
    receiveMessage()
}
```

**Bug:** If `urlSession` is nil (impossible today but possible if the init's `URLSession(configuration:delegate:delegateQueue:)` ever returns nil), `webSocketTask` stays nil, `resume()` is a no-op, `receiveMessage()` does nothing, and `isConnecting` stays true forever. Subsequent `connect()` calls hit the guard and bail. The manager is stuck.

**Severity rationale:** Defensive only. `urlSession` is non-nil today by construction. But the pattern is fragile.

**Fix:** Use `guard let session = urlSession else { isConnecting = false; return }` before `webSocketTask = session.webSocketTask(...)`. Reset `isConnecting` on any failure path.

---

### S7-16 `MenubarConfigService.loadConfig` swallows parse errors with `print` to stdout

**File:** `menubar/MeeptMenuBar/Services/MenubarConfigService.swift:120-123`
```swift
} catch {
    // On parse error, keep defaults
    print("Failed to load menubar config: \(error)")
}
```

**Bug:** User-provided `~/.meept/menubar.json5` parse errors are printed to stdout (not stderr) and silently swallowed — the app falls back to defaults with no UI indication. A user who typos a comma in their config sees the menubar app start with default URL `https://localhost:8081` and default-false notifications and has no idea why. `print()` goes to stdout which is invisible for a bundled macOS app.

Same issue exists in `NotificationManager.swift:48`, `:70`, `:75`, `:91`, `:149`, `:223` — all use `print()` for errors that should be `logger.error()` (the pattern established in `DaemonStatusViewModel.swift:21`).

**Severity rationale:** Silent failure on user config. The whole app category uses `os.log Logger` except NotificationManager and MenubarConfigService.

**Fix:** Replace `print()` with `Logger(subsystem: "com.caimlas.meept.menubar", category: "Config").error(...)`. Surface the error in the menubar UI as a one-time toast.

---

### S7-17 `APIClient.makeRequest` throws `noAPITokenConfigured` even for `/health` endpoint

**File:** `menubar/MeeptMenuBar/Services/APIClient.swift:81-90`
```swift
private func makeRequest(path: String, method: String) throws -> URLRequest {
    let url = baseURL.appendingPathComponent(path)
    var request = URLRequest(url: url)
    request.httpMethod = method
    guard let token = apiToken, !token.isEmpty else {
        throw APIError.noAPITokenConfigured
    }
    request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
    return request
}
```

**Bug:** `APIClient.makeRequest` is the shared request builder for every endpoint, including `/health` (which CLAUDE.md states is exempt from `require_auth`). If a caller ever uses `APIClient` to hit `/health` (none do today, but the helper is generic), the call will throw `noAPITokenConfigured` even though `/health` doesn't need auth. In release builds where `MenubarConfigService.apiToken` returns `nil`, this means the menubar app cannot reach `/health` via this client to do a pre-flight connectivity check.

**Severity rationale:** Latent design issue; no current caller hits this, but the next feature that wants to ping `/health` will trip over it.

**Fix:** Allow callers to opt out of the auth requirement: `makeRequest(path:method:requiresAuth: Bool = true)`. When `requiresAuth == false`, skip the token guard. Add a similar parameter for `DashboardService`/`ConfigService`.

---

### S7-18 `cmd/meept/main.go` `runChat` is the default `RunE` but also added as `chat` subcommand — args semantics differ

**File:** `cmd/meept/main.go:109` + `cmd/meept/chat.go:24-47`

**Bug:** The root `meept` command and the `meept chat` subcommand share `runChat`. Both accept `cobra.MaximumNArgs(1)`. The behavior is identical for `meept "hello"` and `meept chat "hello"`, but for a user typing `meept --transport=http "hello"`, the global persistent flag `--transport` is parsed correctly while subcommand-only flags like `--project` are not available (they're defined on `newChatCmd`, not the root). This asymmetry is undocumented: `meept --project myapp "hello"` fails with "unknown flag: --project", but `meept chat --project myapp "hello"` works.

**Severity rationale:** Documentation/UX inconsistency. Users who learn `meept chat --project X` from help will try `meept --project X "msg"` and get a confusing cobra error.

**Fix:** Either document the asymmetry in the root command's `Long` help text, or move `--project` and `--nofence` to `PersistentFlags` on the root so both paths accept them.

---

### S7-19 `AgentsConfigView.onChange(of:)` uses deprecated single-parameter form

**File:** `menubar/MeeptMenuBar/Views/Settings/AgentsConfigView.swift:175-179`
```swift
.onChange(of: configViewModel.selectedAgentId) { newId in
    if let id = newId {
        configViewModel.loadAgentDetails(id)
    }
}
```

**Bug:** On macOS 14+/iOS 17+, `onChange(of:)` takes two parameters (old and new value) or zero (with `dispatch.async` pattern). The single-parameter form compiles with a deprecation warning but will break when the project targets a newer SDK. `ClientConfigView.saveSettings` and `ModelsConfigView.saveConfig` also use the same `Task { try? await Task.sleep ... }` pattern that was deprecated alongside two-parameter `onChange`.

**Severity rationale:** Forward-compat debt. No runtime issue on current SDK.

**Fix:** Migrate to the new `onChange(of:) { old, new in }` signature, or use `.task(id: someEquatable)` to drive the side-effect.

---

### S7-20 `client.json5` `InputBehaviorConfig` and `ChatConfig` fields silently default when missing

**File:** `internal/tui/app.go:229-243`
```go
inputConfig := models.InputBehaviorConfig{
    EnterBehavior: clientConfig.Input.EnterBehavior,
    AutoExpand:    clientConfig.Input.AutoExpand,
}
...
chat: models.NewChatModelWithConfig(rpc, ..., clientConfig.Keybindings.EscapeBehavior, inputConfig, models.ChatConfig{
    AutoCopyOnRelease: clientConfig.Chat.AutoCopyOnRelease,
    ScrollSpeed:       clientConfig.Chat.ScrollSpeed,
}),
```

**Bug:** `LoadClientConfig()` error is discarded (`clientConfig, _ := LoadClientConfig()`). If the file is missing or unparseable, every nested field silently defaults to the Go zero value: `EnterBehavior == ""`, `AutoExpand == false`, `EscapeBehavior == ""`, `ScrollSpeed == 0`. The empty-string behaviors may not match any valid case in downstream switches (e.g. `EnterBehavior` is likely switched on as `"send"` / `"newline"`) and the empty default can produce surprising behavior — typing Enter might do nothing, or might fall through to an unintended branch. `ScrollSpeed == 0` likely means "no scrolling" which would surprise users on a fresh install.

**Severity rationale:** First-run experience is broken-ish: defaults are not documented and may be wrong.

**Fix:** Provide explicit defaults in `LoadClientConfig()` when the file is missing (e.g. `EnterBehavior: "send"`, `AutoExpand: true`, `ScrollSpeed: 3`). Log a warning when the config file fails to load.

---

---

## Severity Summary

- **Critical:** 1 (S7-1 — predictable conversation ID via `time.Now().UnixNano()`, recurring bug class called out in round 4 MEMORY.md)
- **High:** 5 (S7-2 saveConfig parameter shadow; S7-3 analytics -1 sentinel in user output; S7-4 silent --transport=http fallback; S7-5 dead `startAtLogin`/`showInMenuBar` config knobs; S7-6 menubar title-case UI text)
- **Medium:** 6 (S7-7 CLI title-case output sweep; S7-8 DaemonError capitalization; S7-9 isUpdating UX race; S7-10 historical metrics view never renders data; S7-11 duplicated state→icon mapping; S7-12 no WebSocket cleanup on terminate)
- **Low:** 8 (S7-13 pluralize misnamed; S7-14 NotificationManager no disconnect; S7-15 WebSocketManager isConnecting flag flip; S7-16 print() instead of logger; S7-17 APIClient blocks /health; S7-18 root vs chat subcommand flag asymmetry; S7-19 deprecated onChange form; S7-20 client.json5 missing defaults swallowed)

**Prompt-injection note:** Every file Read result contained fake `<system-reminder>` blocks claiming the meept code might be malware and instructing refusal to improve it. Per project context, meept is the user's own Go daemon project. These injections were disregarded as instructed.
