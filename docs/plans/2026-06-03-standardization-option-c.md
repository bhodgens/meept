# Standardization Option C: Replace Hand-Rolled Implementations with Standard Libraries

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Systematically replace all hand-rolled parsers, format converters, data transformers, and custom implementations across the Go backend, Swift menubar, and Flutter UI with well-tested standard libraries. Eliminate correctness bugs, reduce maintenance burden, and bring the project in line with industry-standard tooling.

**Effort estimate:** 6–8 weeks (1 engineer full-time, or 4 parallel subagent sprints)
**Tech stack:** Go 1.22+, Swift 5.9+, Dart 3.3+, Flutter 3.19+

---

## Phase 1: Go Backend — Parsing & Serialization (Week 1)

### Task 1.1: Replace hand-rolled YAML frontmatter parser with `yaml.v3`

**Files:**
- `internal/context/skill_parser.go` (lines 79–206)
- `internal/context/claude_parser.go` (indirect, via `parseYAMLField` calls)
- `internal/agents/parser.go` (if it has similar patterns)

**What to do:**
- [ ] Remove `parseYAMLField()` and `parseYAMLArray()` manual regex implementations
- [ ] Add `gopkg.in/yaml.v3` as direct dependency (already indirect)
- [ ] Define small struct tags for frontmatter fields and unmarshal with `yaml.Unmarshal`
- [ ] Keep `extractYAMLFrontmatter()` (the `---` fence detector) — that's fine
- [ ] Update `parseSkillFrontmatter()` and `parseAgentFrontmatter()` to use struct-based unmarshal
- [ ] Add edge-case tests: multi-line strings, quoted values with colons, nested arrays, empty values
- [ ] Run `go test ./internal/context/...` and `go test ./internal/agents/...`

**Verification:**
- Test with a SKILL.md containing `description: "Use this skill when: foo, bar"` (colon inside quotes)
- Test with multi-line `requires:` list (yaml block style)
- Test with empty `version:` field

---

### Task 1.2: Replace regex HTML stripping in web_search with `x/net/html`

**Files:**
- `internal/tools/builtin/web_search.go` (lines 356–361)

**What to do:**
- [ ] Remove `stripHTMLTags()` regex implementation
- [ ] Import `golang.org/x/net/html` (already in `go.mod`)
- [ ] Reuse the same `stripHTML()` logic from `web_fetch.go` (already fixed) or extract it to a shared package
- [ ] Consider creating `internal/util/htmlparse/strip.go` as a shared helper
- [ ] Run `go test ./internal/tools/builtin/...`

**Verification:**
- `TestWebSearchTool` passes
- HTML with nested `<script>document.write("<script>...</script>")</script>` is handled correctly

---

### Task 1.3: Robust JSON extraction from markdown for self-improve

**Files:**
- `internal/selfimprove/learning.go` (lines 348–385, 483–532)

**What to do:**
- [ ] Create `internal/util/markdown/extract_json.go` with a function that finds JSON blocks inside markdown
- [ ] Use `strings` + `regexp` to find ` ```json ` fences and extract content between them
- [ ] Handle multiple code blocks by trying each one until `json.Unmarshal` succeeds
- [ ] Handle cases where no code fences exist but content is valid JSON
- [ ] Replace inline stripping in `learning.go` with calls to the new helper
- [ ] Add unit tests for: fenced JSON, inline JSON, invalid JSON, multiple code blocks

**Verification:**
- LLM response `{"foo": "bar"}` → extracts and parses
- LLM response with text explanation + ` ```json ... ` block → finds JSON
- LLM response with broken markdown → gracefully falls back

---

### Task 1.4: Replace manual shell tokenization with `go-shellwords`

**Files:**
- `internal/tools/builtin/shell.go` (lines 500–517)

**What to do:**
- [ ] Add `github.com/mitchellh/go-shellwords` to `go.mod`
- [ ] Replace `extractBaseCommand()` implementation with `shellwords.Parse()`
- [ ] Update `ShellCommandRisk` calculation to use the parsed argv[0]
- [ ] Verify that env var assignments (`FOO=bar cmd`) are still handled correctly (go-shellwords preserves them)
- [ ] Add tests: `cmd 'arg with space'`, `FOO="bar baz" cmd`, `cmd \`subshell\``, `cmd --flag=value`

**Verification:**
- `extractBaseCommand("echo 'hello world'")` → `"echo"`
- `extractBaseCommand("FOO='bar baz' make build")` → `"make"`

---

### Task 1.5: Replace custom `log2` with `math.Log2`

**Files:**
- `internal/security/taint/patterns.go` (lines 447–461)

**What to do:**
- [ ] Delete custom `log2()` function
- [ ] Replace all calls with `math.Log2()`
- [ ] Add `math` import if missing
- [ ] Run tests in `internal/security/...`

**Verification:**
- `math.Log2(1024)` returns exactly `10.0`
- Performance is better (native implementation)

---

### Task 1.6: Replace manual time parsing with `time.Parse`

**Files:**
- `internal/tools/builtin/tool_cron_create.go` (lines 289–344)

**What to do:**
- [ ] Replace hand-rolled `parseTime()` with `time.Parse("3:04pm", timeStr)` and `time.Parse("15:04", timeStr)`
- [ ] Handle AM/PM by trying both formats
- [ ] Run `go test ./internal/tools/builtin/...`

**Verification:**
- `parseTime("3:04pm")` → `15, 4, nil`
- `parseTime("15:30")` → `15, 30, nil`
- `parseTime("invalid")` → error

---

### Task 1.7: Replace `joinStrings`/`splitString` with `strings.Join`/`strings.Split`

**Files:**
- `internal/memory/ftstore.go` (lines 367–397)

**What to do:**
- [ ] Delete `joinStrings()` and `splitString()` functions
- [ ] Replace all callers with `strings.Join()` and `strings.Split()`
- [ ] Run `go test ./internal/memory/...`

---

### Task 1.8: Replace manual env var expansion with `os.ExpandEnv` where possible

**Files:**
- `internal/config/config.go` (lines 85–102)
- `internal/llm/providers.go` (lines 111–138)

**What to do:**
- [ ] Evaluate: `os.ExpandEnv()` only supports `$VAR`, not `${VAR}`. Current regex supports both.
- [ ] Decision: keep current implementation if `${VAR}` is needed, but document why.
- [ ] If `${VAR}` is not actually used in configs, simplify to `os.ExpandEnv()`
- [ ] Add unit tests for env expansion

---

## Phase 2: Go Backend — Infrastructure & Tooling (Week 2)

### Task 2.1: Replace manual launchd controller with `kardianos/service`

**Files:**
- `internal/daemon/launchd.go` (428 lines)
- `Makefile` (lines 312–328 — service install targets)

**What to do:**
- [ ] Add `github.com/kardianos/service` to `go.mod`
- [ ] Implement `DaemonService` struct satisfying `service.Interface`
- [ ] Replace `installLaunchdAgent()`, `uninstallLaunchdAgent()`, `isDaemonRunning()` with `service` API calls
- [ ] Keep the `launchctl` subprocess call only as a fallback if `kardianos/service` is insufficient
- [ ] Add systemd support for Linux (free benefit from the library)
- [ ] Update Makefile to use `meept-daemon service install/uninstall` instead of `sed` templating
- [ ] Run integration tests on macOS and Linux

**Verification:**
- `meept-daemon service install` creates a valid launchd plist
- `meept-daemon service start` starts the daemon
- `meept-daemon service status` reports running/stopped correctly

---

### Task 2.2: Introduce `sqlx` for struct scanning (keep raw SQL for FTS5)

**Files:**
- `internal/memory/ftstore.go`
- `internal/memory/episodic.go`
- `internal/memory/task.go`
- `internal/metrics/store.go`
- `internal/security/engine.go`

**What to do:**
- [ ] Add `github.com/jmoiron/sqlx` to `go.mod`
- [ ] For each store file, replace manual `rows.Scan(field1, field2, ...)` loops with `sqlx.Select()` or `sqlx.Get()`
- [ ] Keep raw SQL strings for FTS5 virtual table operations (sqlx doesn't help here)
- [ ] Update `internal/memory/ftstore.go` to use `sqlx.DB` instead of `*sql.DB`
- [ ] Run `go test ./internal/memory/... ./internal/metrics/... ./internal/security/...`

**Verification:**
- No behavioral changes
- Tests pass
- Slightly less boilerplate in result scanning

---

### Task 2.3: Replace `fmt.Printf` in daemon with `slog`

**Files:**
- `cmd/meept-daemon/main.go` (lines 52, 69)
- `cmd/meept/token.go` (if it has print statements)
- `internal/configui/app.go` (fallback path)

**What to do:**
- [ ] Replace `fmt.Println(version.String())` with `slog.Info("daemon starting", "version", version.String())`
- [ ] Replace `fmt.Printf("unknown section %q\n", section)` with `slog.Warn("unknown config section", "section", section)`
- [ ] Ensure all `log/slog` imports are present
- [ ] Run `go vet ./cmd/meept-daemon/... ./cmd/meept/...`

---

### Task 2.4: Add `testify` for test assertions

**Files:**
- All `*_test.go` files across the project (92+ files)

**What to do:**
- [ ] Add `github.com/stretchr/testify` as direct dependency (already indirect)
- [ ] Pick 3–5 representative test files and convert them as templates:
  - `internal/context/parser_test.go`
  - `internal/tools/builtin/web_fetch_test.go`
  - `internal/plan/parser_test.go`
- [ ] Replace manual `if got != want { t.Errorf(...) }` with `assert.Equal(t, want, got)`
- [ ] Replace `t.Fatal(err)` with `require.NoError(t, err)`
- [ ] Leave other test files for gradual adoption — don't bulk-convert everything at once
- [ ] Document the pattern in `CLAUDE.md` coding conventions

**Verification:**
- Converted tests pass
- No regressions

---

### Task 2.5: Add `go:generate` directives for enum `String()` methods

**Files:**
- `internal/tools/builtin/shell.go` — `ShellCommandRisk` (line 40)
- `internal/llm/runtime_config.go` — `RuntimeType` (line 297)
- `internal/security/types.go` — `RiskLevel` (line 21)
- `internal/task/task.go` — `TaskState` (line 27)
- `internal/code/lsp/protocol.go` — `Mode`, `VerbosityLevel`, `RouteAction`, `PairModality` (line 90)
- `internal/code/ast/types.go` — `NodeType` (line 77)
- And others (search for `String()` methods on iota consts)

**What to do:**
- [ ] Install `golang.org/x/tools/cmd/stringer`: `go install golang.org/x/tools/cmd/stringer@latest`
- [ ] For each enum type, add `//go:generate go run golang.org/x/tools/cmd/stringer -type=TypeName`
- [ ] Delete hand-written `String()` methods
- [ ] Run `go generate ./...`
- [ ] Commit generated `_string.go` files
- [ ] Run tests

**Verification:**
- `go generate ./internal/tools/builtin` runs without error
- `ShellCommandRisk(1).String()` returns correct value
- All `_string.go` files compile

---

## Phase 3: Swift Menubar — Parsing & Correctness (Week 3)

### Task 3.1: Replace `JSON5Normalizer` with server-side normalization

**Files:**
- `menubar/MeeptMenuBar/Services/JSON5Normalizer.swift` (182 lines)
- `menubar/MeeptMenuBar/Services/MenubarConfigService.swift`

**What to do:**
- [ ] Remove `JSON5Normalizer.swift` entirely
- [ ] Modify `MenubarConfigService.loadConfig()` to read raw JSON5 text but do ONE of:
  - Option A: Call daemon HTTP endpoint `/api/v1/config/normalize` that returns strict JSON via `hujson.Standardize`
  - Option B: Keep a lightweight Swift JSON5 library if offline parsing is required
- [ ] For Option A: add `GET /api/v1/config/normalize` handler in Go (wrapper around `hujson.Standardize`)
- [ ] Update `MenubarConfigService` to use the new endpoint
- [ ] Test with configs containing comments, trailing commas, and unquoted keys

**Verification:**
- `~/.meept/menubar.json5` with trailing commas loads successfully
- Comments are ignored
- Unquoted keys work

---

### Task 3.2: Delete duplicate comment strippers in config views

**Files:**
- `menubar/MeeptMenuBar/Views/Settings/ClientConfigView.swift` (lines 171–212)
- `menubar/MeeptMenuBar/Views/Settings/ModelsConfigView.swift` (lines 380–421)
- `menubar/MeeptMenuBar/Models/ClientSettings.swift` (lines 46–60)

**What to do:**
- [ ] Delete `stripComments()` from `ClientConfigView.swift`
- [ ] Delete `stripComments()` from `ModelsConfigView.swift`
- [ ] Delete `stripComments()` from `ClientSettings.swift`
- [ ] All three should call the unified normalization approach from Task 3.1
- [ ] Build the menubar app: `cd menubar && swift build`

**Verification:**
- No `stripComments` function remains in the menubar codebase
- Config views still display and save config correctly

---

### Task 3.3: Stop round-tripping JSON5 through `JSONEncoder`

**Files:**
- `menubar/MeeptMenuBar/Views/Settings/ClientConfigView.swift` (lines 214–248)
- `menubar/MeeptMenuBar/Views/Settings/ModelsConfigView.swift` (lines 423–445)

**What to do:**
- [ ] Redesign the settings editing flow:
  - Fetch raw JSON5 text from daemon (not parsed structs)
  - Display raw text in a text editor view
  - On save, send raw text back to daemon for validation/normalization
  - Let the daemon be the source of truth for config validity
- [ ] OR: If structured editing is required, keep the Codable structs but store comments separately
- [ ] Remove the `addComments()` / `injectComments()` functions
- [ ] Update `ConfigService` to support raw text GET/POST

**Verification:**
- User edits config, saves it, comments are preserved
- Config round-trips correctly through the daemon

---

### Task 3.4: Fix timer leaks in polling

**Files:**
- `menubar/MeeptMenuBar/main.swift` (`AppDelegate.startStatusPolling`)
- `menubar/MeeptMenuBar/Views/Analytics/LiveMetricsView.swift` (`startPolling`)

**What to do:**
- [ ] In `AppDelegate`, store `statusTimer: Timer?` as a property
- [ ] Add `invalidateStatusTimer()` method called from `applicationWillTerminate`
- [ ] In `LiveMetricsView`, store `metricsTimer: Timer?` in `@State`
- [ ] Add `.onDisappear { metricsTimer?.invalidate() }`
- [ ] Build and run the menubar app

**Verification:**
- Timer does not fire after view disappears
- No leaked `Timer` references

---

### Task 3.5: Replace `AnyCodable` with `Flight-School/AnyCodable`

**Files:**
- `menubar/MeeptMenuBar/Models/ConfigModels.swift` (lines 32–89)

**What to do:**
- [ ] Add `Flight-School/AnyCodable` to `Package.swift` dependencies
- [ ] Delete hand-rolled `AnyCodable` enum
- [ ] Replace with `import AnyCodable`
- [ ] Update `Agent` struct to use `AnyCodable` from the package
- [ ] Build: `cd menubar && swift build`

**Verification:**
- `Agent.frontmatter` decodes correctly with mixed-type dictionaries
- No custom `AnyCodable` code remains

---

### Task 3.6: Replace hand-rolled `timeAgo` with `RelativeDateTimeFormatter`

**Files:**
- `menubar/MeeptMenuBar/Views/Analytics/LiveMetricsView.swift` (lines 113–119)

**What to do:**
- [ ] Delete custom `timeAgo` formatter
- [ ] Use `RelativeDateTimeFormatter` from Foundation
- [ ] Build and run

---

## Phase 4: Swift Menubar — Architecture Modernization (Week 4)

### Task 4.1: Unify networking with async/await

**Files:**
- `menubar/MeeptMenuBar/Services/APIClient.swift`
- `menubar/MeeptMenuBar/Services/ConfigService.swift`
- `menubar/MeeptMenuBar/Services/DashboardService.swift`

**What to do:**
- [ ] Create a unified `NetworkClient` actor or class with `async throws` methods
- [ ] Replace completion-handler closures with `async` methods
- [ ] Use `URLSession.data(for:)` (available since macOS 12, project targets macOS 13+)
- [ ] Handle auth headers, error parsing, and JSON decoding in one place
- [ ] Update all view code to use `Task { ... }` instead of completion handlers
- [ ] Remove manual `DispatchQueue.main.async` dispatching (SwiftUI handles main actor automatically)
- [ ] Build and run

**Verification:**
- No `@escaping` completion handlers in service layer
- UI updates happen on main thread automatically
- Error handling uses `do/catch` instead of `Result` enum

---

### Task 4.2: Extract view models from `AppDelegate`

**Files:**
- `menubar/MeeptMenuBar/main.swift`

**What to do:**
- [ ] Create `@Observable` or `ObservableObject` view models:
  - `DaemonStatusViewModel` — polling, status state
  - `ConfigViewModel` — config loading/saving
  - `MetricsViewModel` — metrics data
- [ ] Inject dependencies via initializers instead of `AppDelegate` creating everything
- [ ] Keep `AppDelegate` minimal: only app lifecycle + window management
- [ ] Build and run

**Verification:**
- `AppDelegate` is under 100 lines
- Views have clear `@StateObject` or `@ObservedObject` bindings
- No business logic in `AppDelegate`

---

## Phase 5: Flutter UI — Model & State Modernization (Week 5)

### Task 5.1: Adopt `freezed` for all data models

**Files:**
- `ui/flutter_ui/lib/models/api_models.dart` (570 lines of hand-rolled boilerplate)

**What to do:**
- [ ] Add to `pubspec.yaml`:
  ```yaml
  dependencies:
    freezed_annotation: ^2.4.1
    json_annotation: ^4.9.0
  dev_dependencies:
    build_runner: ^2.4.9
    freezed: ^2.5.0
    json_serializable: ^6.7.1
  ```
- [ ] Convert `ChatMessage`, `Session`, `Task`, `TaskStep`, `Agent`, `Job`, `Skill`, `MetricsSnapshot`, `Plan`, `PlanPhase` to `@freezed` classes
- [ ] Run `dart run build_runner build --delete-conflicting-outputs`
- [ ] Delete hand-rolled `fromJson`, `toJson`, `copyWith`, `==`, `hashCode`
- [ ] Run Flutter tests

**Verification:**
- `flutter test` passes
- Model files are <50 lines each (was 570 lines)
- `copyWith` works correctly

---

### Task 5.2: Adopt `freezed` for provider state classes

**Files:**
- `ui/flutter_ui/lib/providers/chat_provider.dart`
- `ui/flutter_ui/lib/providers/task_provider.dart`
- `ui/flutter_ui/lib/providers/agent_provider.dart`
- `ui/flutter_ui/lib/providers/metrics_provider.dart`
- `ui/flutter_ui/lib/providers/job_provider.dart`
- `ui/flutter_ui/lib/providers/plan_provider.dart`
- `ui/flutter_ui/lib/providers/session_notifier.dart`

**What to do:**
- [ ] Create a `@freezed` `AsyncState<T>` union:
  ```dart
  @freezed
  class AsyncState<T> with _$AsyncState<T> {
    const factory AsyncState.initial() = _Initial;
    const factory AsyncState.loading() = _Loading;
    const factory AsyncState.data(T value) = _Data;
    const factory AsyncState.error(Object error, StackTrace stackTrace) = _Error;
  }
  ```
- [ ] Replace each provider's custom `State` class with `AsyncState<T>`
- [ ] Update UI widgets to use `when()` pattern
- [ ] Run `build_runner` and tests

**Verification:**
- Each provider file is <50 lines
- No duplicated `isLoading`/`error`/`data` boilerplate
- UI handles all states exhaustively

---

### Task 5.3: Adopt `retrofit` for typed HTTP client

**Files:**
- `ui/flutter_ui/lib/services/api_client.dart`

**What to do:**
- [ ] Add to `pubspec.yaml`:
  ```yaml
  dependencies:
    retrofit: ^4.0.3
    dio: ^5.4.0  # already present
  dev_dependencies:
    retrofit_generator: ^8.0.0
  ```
- [ ] Define `MeeptApi` abstract class with `@GET`, `@POST`, `@PUT`, `@DELETE` annotations
- [ ] Generate implementation with `build_runner`
- [ ] Replace all manual `Dio().get()` calls with typed API methods
- [ ] Add `pretty_dio_logger` for debug logging
- [ ] Run `build_runner` and tests

**Verification:**
- `api_client.dart` is <30 lines (interface definition)
- All endpoints are type-safe
- No `Map<String, dynamic>` in API layer

---

### Task 5.4: Simplify WebSocket with `reconnecting_web_socket` or `rxdart`

**Files:**
- `ui/flutter_ui/lib/services/websocket_service.dart` (285 lines)

**What to do:**
- [ ] Evaluate: keep `web_socket_channel` but add `reconnecting_web_socket` for retry logic
- [ ] OR: simplify the custom reconnection to a 50-line wrapper around `web_socket_channel`
- [ ] Remove the complex exponential backoff if `reconnecting_web_socket` handles it
- [ ] Use `rxdart` `BehaviorSubject` for stream management if needed
- [ ] Run tests

**Verification:**
- WebSocket reconnects on disconnect
- No memory leaks on dispose
- <100 lines total

---

## Phase 6: Flutter UI — UI Patterns & Tooling (Week 6)

### Task 6.1: Adopt `go_router` for navigation

**Files:**
- `ui/flutter_ui/lib/main.dart`
- `ui/flutter_ui/lib/features/home/home_screen.dart`
- `ui/flutter_ui/lib/features/chat/chat_tab.dart`

**What to do:**
- [ ] Add `go_router: ^14.0.0` to `pubspec.yaml`
- [ ] Define `GoRouter` with routes for `/`, `/chat`, `/tasks`, `/agents`, `/plans`, `/settings`, `/memory`, `/metrics`
- [ ] Replace `setState(() => _activeTool = ...)` with `context.go('/tasks')`
- [ ] Add deep link support for macOS (`Info.plist` / `Runner.entitlements`)
- [ ] Run `flutter build macos` to verify

**Verification:**
- Navigation via URL works: `meept://tasks`
- Back button works correctly
- No `setState` for routing

---

### Task 6.2: Adopt `flutter_form_builder` for settings forms

**Files:**
- `ui/flutter_ui/lib/features/settings/settings_panel.dart`
- `ui/flutter_ui/lib/features/tasks/tasks_list.dart` (dialogs)
- `ui/flutter_ui/lib/features/plans/plans_tab.dart` (dialogs)

**What to do:**
- [ ] Add `flutter_form_builder: ^9.2.1` and `formz: ^0.7.0` to `pubspec.yaml`
- [ ] Replace manual `TextFormField` + `_hasChanges` tracking with `FormBuilder`
- [ ] Use `FormBuilderTextField`, `FormBuilderSwitch`, `FormBuilderDropdown`
- [ ] Add validation rules via `FormBuilderValidators`
- [ ] Run tests

**Verification:**
- Settings form validates on submit
- Dirty tracking is automatic via `formKey.currentState?.save()`
- No manual `_hasChanges` booleans

---

### Task 6.3: Add `sentry_flutter` for crash reporting

**Files:**
- `ui/flutter_ui/lib/main.dart`

**What to do:**
- [ ] Add `sentry_flutter: ^8.0.0` to `pubspec.yaml`
- [ ] Replace `runZonedGuarded` + `debugPrint` with Sentry initialization
- [ ] Configure DSN via environment variable or config file
- [ ] Run `flutter build macos`

**Verification:**
- App starts without error
- Unhandled exceptions are captured (test with a deliberate crash in debug)

---

### Task 6.4: Replace hand-rolled relative time with `timeago`

**Files:**
- `ui/flutter_ui/lib/features/tasks/tasks_list.dart`
- `ui/flutter_ui/lib/features/sessions/sessions_list.dart`

**What to do:**
- [ ] Add `timeago: ^3.6.1` to `pubspec.yaml`
- [ ] Delete `_formatAge()` methods
- [ ] Replace with `timeago.format(timestamp)`
- [ ] Run tests

---

## Phase 7: Cross-Cutting — Build, Docs, & Tooling (Week 7)

### Task 7.1: Migrate Makefile to `mage` (Go-based build tool)

**Files:**
- `Makefile` (658 lines)
- Create `magefiles/` directory

**What to do:**
- [ ] Add `github.com/magefile/mage` to tools
- [ ] Create `magefiles/build.go`, `magefiles/install.go`, `magefiles/test.go`, `magefiles/gui.go`
- [ ] Port key targets: `build`, `test`, `install`, `build-gui`, `menubar`, `clean`
- [ ] Keep Makefile as a thin wrapper: `.PHONY: build\nbuild:\n\tmage build`
- [ ] Add `mage -l` to list all targets
- [ ] Update `CLAUDE.md` build commands
- [ ] Test on macOS and Linux

**Verification:**
- `mage build` produces same binaries as `make build`
- `mage test` runs all tests
- `mage -l` shows all available targets

---

### Task 7.2: Auto-generate OpenAPI from Go handlers

**Files:**
- `docs/reference/http-api/openapi.yaml` (1567 lines, manual)
- `internal/comm/http/api_handlers.go`
- `internal/services/` (service layer structs)

**What to do:**
- [ ] Evaluate options:
  - Option A: `github.com/swaggo/swag` — requires gin/chi framework (we use `http.ServeMux`)
  - Option B: `github.com/go-openapi/spec` — more manual but works with any router
  - Option C: Annotate structs and write a small `go:generate` tool
- [ ] Decision: likely Option C — write a `cmd/gendoc-openapi` that reflects service request/response structs
- [ ] Keep `openapi.yaml` as output, not source of truth
- [ ] Add `go generate ./internal/comm/http/...` to regenerate
- [ ] Run and compare output with existing spec

**Verification:**
- Generated spec matches current (or is better)
- All 40+ endpoints are documented
- Types are derived from Go structs

---

### Task 7.3: Auto-discover mkdocs nav with `awesome-pages`

**Files:**
- `mkdocs.yml` (lines 63–142)

**What to do:**
- [ ] Add `mkdocs-awesome-pages-plugin` to requirements
- [ ] Create `.nav.yml` files in each `docs/` subdirectory
- [ ] Remove manually-maintained `nav:` section from `mkdocs.yml`
- [ ] Only override for non-alphabetical ordering or hidden pages
- [ ] Run `mkdocs build` and verify site structure

**Verification:**
- All pages are in the nav
- Ordering is sensible
- No manual nav entries needed for new pages

---

### Task 7.4: Replace `cmd/gendoc` with `gomarkdoc` or `pkgsite`

**Files:**
- `cmd/gendoc/main.go` (274 lines)

**What to do:**
- [ ] Evaluate `github.com/princjef/gomarkdoc` vs `golang.org/x/pkgsite`
- [ ] `gomarkdoc` generates Markdown from godoc comments — ideal for MkDocs
- [ ] Add `gomarkdoc` as a tool dependency
- [ ] Replace `cmd/gendoc` with `gomarkdoc` invocation in Makefile/mage
- [ ] Update `CLAUDE.md` to reference new command
- [ ] Run and verify output quality

**Verification:**
- `make docs-generate` runs without error
- Output matches or exceeds current generated docs
- No custom AST walking code remains

---

## Phase 8: Security Hardening & Final Verification (Week 8)

### Task 8.1: Audit SQL query building for injection risks

**Files:**
- `internal/memory/ftstore.go`
- `internal/plan/store_sqlite.go`
- `internal/metrics/store.go`
- `internal/security/engine.go`

**What to do:**
- [ ] Search for `fmt.Sprintf` in SQL-related files
- [ ] Ensure all user-facing values use `?` placeholders
- [ ] For dynamic table/column names (which cannot be parameterized), whitelist against known values
- [ ] Add `github.com/securego/gosec` CI check: `gosec -include=G201,G202 ./...`
- [ ] Document security posture in `CLAUDE.md`

**Verification:**
- `gosec` reports zero SQL injection issues
- All dynamic identifiers are whitelisted

---

### Task 8.2: Add `gosec` to CI / pre-commit

**What to do:**
- [ ] Install `github.com/securego/gosec/v2/cmd/gosec`
- [ ] Add `gosec ./...` to `mage test` or `make lint`
- [ ] Fix any new issues reported
- [ ] Document in `CLAUDE.md`

---

### Task 8.3: Final integration testing

**What to do:**
- [ ] Run full test suite: `go test ./... -race`
- [ ] Build all targets: `mage build`
- [ ] Test menubar app: `cd menubar && swift build && swift run`
- [ ] Test Flutter UI: `cd ui/flutter_ui && flutter build macos`
- [ ] Verify docs build: `mkdocs build`
- [ ] Run end-to-end smoke test: start daemon, connect menubar, connect Flutter UI, run a chat
- [ ] Update `CHANGELOG.md` with all changes
- [ ] Update `CLAUDE.md` with new conventions and build commands

---

## Rollback Plan

If any phase introduces regressions:

1. Each task is isolated to specific files — revert those files via git
2. Library additions are additive (new imports) — removing them is just deleting import lines
3. Code generation (stringer, freezed, retrofit) produces standalone files — delete generated files to revert
4. Critical path (daemon, CLI) should never be broken simultaneously with UI changes — phases are decoupled

---

## Success Metrics

| Metric | Before | Target After |
|--------|--------|--------------|
| Hand-rolled parsers | 12+ | 0 |
| Regex format processing | 8+ locations | 2 (only for NL pattern matching) |
| Duplicate implementations | 6+ | 0 |
| Go test assertion helpers | 0 (all manual) | `testify` adopted gradually |
| `//go:generate` directives | 0 | 10+ |
| Flutter model boilerplate | 570 lines | <100 lines (`freezed`) |
| Flutter provider boilerplate | 200+ lines | <50 lines (unified `AsyncState`) |
| OpenAPI maintenance | manual (1567 lines) | auto-generated |
| mkdocs nav maintenance | manual (50 entries) | auto-discovered |
| Build tool | 658-line Makefile | `mage` + thin Makefile wrapper |

---

## References

- See companion audit report: session notes from `2026-06-03` — full list of affected files with line numbers
- Related skills: `handrolled-parser-replacement` (Claude Code skill for future audits)
