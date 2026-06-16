# S2 — Code Intel + Plans + Skills + Config + Self-Improve + RepoMap + Lint Findings

Scope: `internal/code/`, `internal/plan/`, `internal/skills/`, `internal/config/`,
`internal/selfimprove/`, `internal/repomap/`, `internal/lint/`.

Round 5 systematic review. Prior 4 rounds fixed 104 findings; only NEW issues below.

## Critical

### S2-1 LSP TCP transport Write has no mutex — concurrent writers corrupt JSON-RPC framing
- **File:** `internal/code/lsp/transport/tcp.go:88-106`
- **Evidence:**
```go
func (t *TCPTransport) Write(ctx context.Context, data []byte) error {
    if deadline, ok := ctx.Deadline(); ok {
        _ = t.conn.SetWriteDeadline(deadline)
        defer func() { _ = t.conn.SetWriteDeadline(time.Time{}) }()
    }
    header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
    if _, err := t.conn.Write([]byte(header)); err != nil { ... }
    if _, err := t.conn.Write(data); err != nil { ... }
    return nil
}
```
Compare to `StdioTransport.Write` at `internal/code/lsp/transport/stdio.go:93-110`:
```go
func (t *StdioTransport) Write(ctx context.Context, data []byte) error {
    header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
    t.writeMu.Lock()
    defer t.writeMu.Unlock()
    if _, err := t.stdin.Write([]byte(header)); err != nil { ... }
    if _, err := t.stdin.Write(data); err != nil { ... }
    return nil
}
```
- **Why:** `Client.Call` (client.go:141-189) and `Client.Notify` (client.go:192-214) both call `transport.Write` and can run concurrently — `Call` from any goroutine and `Notify` from `Client.Initialize` or the `readLoop` notification handlers. Two goroutines that enter `TCPTransport.Write` simultaneously can interleave their header/content writes on the shared `net.Conn`, producing malformed frames (e.g. `Content-Length: 73\r\n\r\n{...}Content-Length: 50\r\n\r\n{...}` where the first body is truncated or the second header is appended to the first body). The LSP server on the other end then reads a wrong Content-Length, fails to decode the JSON, and typically drops the connection. The stdio transport already has `writeMu sync.Mutex` for exactly this reason.
- **Fix:** Add `writeMu sync.Mutex` to `TCPTransport` and hold it across both `t.conn.Write` calls, mirroring `StdioTransport`. (The deadline-set can stay outside the lock.)

### S2-2 LoadJSON5WithDefault silently never uses the default — missing-config is always fatal
- **File:** `internal/config/json5_loader.go:135-143` (caller), `internal/config/json5_loader.go:17-24` (producer)
- **Evidence:**
```go
// json5_loader.go:17-24
func LoadJSON5(path string, v any) error {
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return fmt.Errorf("config file not found: %s", path)  // wraps err
        }
        return fmt.Errorf("failed to read config file %s: %w", path, err)
    }
    ...
}

// json5_loader.go:135-143
func LoadJSON5WithDefault(path string, v any) error {
    if err := LoadJSON5(path, v); err != nil {
        if os.IsNotExist(err) {   // ALWAYS false — err is a *fmt.wrapError, not *fs.PathError
            return nil
        }
        return err
    }
    return nil
}
```
- **Why:** `os.IsNotExist` inspects the error chain for a `*fs.PathError` with `syscall.ENOENT`. `LoadJSON5` replaces that with `fmt.Errorf("config file not found: %s", path)` — the `%s` verb does not preserve the underlying error, and `fmt.Errorf` without `%w` produces a plain `*errors.errorString` that fails `errors.Is(err, fs.ErrNotExist)`. As a result `LoadJSON5WithDefault` always re-evaluates to the second branch (`return err`) for a missing file, defeating the entire purpose of the helper. Callers that depend on the default-zero-value behavior (menubar/mcp/presets/cluster config) will abort startup whenever the user hasn't created the file, rather than falling back to defaults.
- **Fix:** In `LoadJSON5`, preserve the original error with `fmt.Errorf("config file not found: %s: %w", path, err)`; or in `LoadJSON5WithDefault`, use `strings.Contains(err.Error(), "config file not found")` / `errors.Is(err, fs.ErrNotExist)` after switching the wrap. The cleanest fix is `return fmt.Errorf("config file not found: %s: %w", path, err)` and keep the `os.IsNotExist` check in the caller.

## High

### S2-3 LSP Manager.StartServer has TOCTOU race — duplicate servers can be spawned
- **File:** `internal/code/lsp/manager.go:111-171`
- **Evidence:**
```go
func (m *Manager) StartServer(ctx context.Context, name string, cfg config.LSPServerConfig) (*ServerInstance, error) {
    m.mu.Lock()
    if srv, ok := m.servers[name]; ok {
        m.mu.Unlock()
        return srv, nil
    }
    m.mu.Unlock()              // (A) release lock

    // ... transport create, client.Initialize — slow I/O without the lock ...

    m.mu.Lock()
    m.servers[name] = srv      // (B) blindly overwrite
    m.mu.Unlock()
    return srv, nil
}
```
- **Why:** Two callers can both pass the check at (A), both build a transport/initialize a client, and then both write to `m.servers[name]` at (B). The loser's transport is never closed — its subprocess (`StdioTransport.cmd.Process`) leaks until the OS reaps it, and its port/socket may also leak. `StartServerForLanguage` is called from the agent tool hot path via `GetServerForLanguage`, so the race is realistically triggerable under concurrent tool calls.
- **Fix:** Either hold `m.mu` for the entire `StartServer` (acceptable because LSP startup is rare and the lock is an RWMutex that doesn't block reads elsewhere), or re-check after acquiring the write lock at (B) and close the just-spawned loser transport.

### S2-4 RepoMapGenerator.GenerateWithCache swaps g.cache without locking
- **File:** `internal/repomap/generator.go:352-360`
- **Evidence:**
```go
func (g *RepoMapGenerator) GenerateWithCache(ctx context.Context, chatFiles, mentionedIdentifiers []string, useCache bool) (*RenderedMap, error) {
    if !useCache {
        oldCache := g.cache
        g.cache = NewMapCache(CacheConfig{EnableMemoryCache: false})
        defer func() { g.cache = oldCache }()
    }
    return g.Generate(ctx, chatFiles, mentionedIdentifiers)
}
```
- **Why:** `g.cache` is read by `Generate`, `InvalidateCache`, and `Stats` — and is mutated here without acquiring `g.mu`. A concurrent `Generate`/`Stats`/`InvalidateCache` can observe the temporary no-op cache, or worse, the deferred restore can swap back the wrong cache if two `GenerateWithCache` calls interleave (the second `oldCache` snapshot captures the temporary cache, not the real one). On a daemon with concurrent chat sessions using repomap, this silently disables caching for the duration and may leave `g.cache` permanently pointing at the throwaway.
- **Fix:** Either hold `g.mu` around the swap/restore (and make `Generate`/`Stats`/`InvalidateCache` take the lock when reading `g.cache`), or drop the swap pattern in favor of a local `cache := g.cache; if !useCache { cache = NewMapCache(...) }` and pass `cache` explicitly to a `generateWith(cache, ...)` helper.

### S2-5 Skill discovery equal-priority shadowing uses `<=` — later source silently overwrites earlier
- **File:** `internal/skills/discovery.go:141-158` (also `:230-244` in `DiscoverMetadataOnly`)
- **Evidence:**
```go
for _, skill := range sourceSkills {
    key := normalizeName(skill.Name)
    existing, exists := skills[key]
    if exists {
        if skill.Priority <= existing.Priority {     // <-- should be strict <
            d.logger.Debug("Skill shadowed by higher priority", ...)
        } else {
            continue
        }
    }
    skills[key] = skill
}
```
- **Why:** The CLAUDE.md spec ("first-wins by tier") says higher-priority sources (lower `Priority` value) shadow lower ones. With `<=`, a skill at the *same* priority as an existing one overwrites it. The iteration order of `d.sources` is `[FileSource, ClaudeSource]` for the default config — a Claude skill with `Priority=PriorityClaude=2` would be overwritten by a later-added Hermes skill also at priority 2 if Hermes is added to the same FileSource run, etc. Inconsistent with "priority shadowing" semantics and with the `existing` branch's log message that says "shadowed by higher priority" (but the branch is taken even when priorities are equal).
- **Fix:** Change `<=` to `<` at line 145 and line 234. If equal-priority sources should be merged by source-precedence order, document that explicitly and use `<` plus a stable source ordering.

## Medium

### S2-6 Plan parser uses bufio.Scanner with default 64KB token limit
- **File:** `internal/plan/parser.go:114`
- **Evidence:**
```go
scanner := bufio.NewScanner(strings.NewReader(content))
for scanner.Scan() { ... }
```
- **Why:** `bufio.Scanner` has a `MaxTokenSize` of `bufio.MaxScanTokenSize` (64 KiB). Plan files routinely embed multi-paragraph summaries, code blocks, and phase descriptions; once a single line (e.g. a long description line or a fenced block that lacks intervening newlines) exceeds 64 KiB, `scanner.Scan` returns false with `bufio.ErrTooLong` and `ParsePlanContent` returns `scanning plan content: bufio.Scanner: token too long`. The caller in `PlanManager.Synthesize` (manager.go:352) treats this as a soft "use phases only" path, silently losing all parsed step DAGs and dependency relationships for very large plans.
- **Fix:** Call `scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)` (or higher) before the scan loop, or read the file via `strings.Split(content, "\n")` since the content is already fully in memory.

### S2-7 Plan writer "step.Number <= completed" heuristic marks wrong steps complete
- **File:** `internal/plan/writer.go:116-126`
- **Evidence:**
```go
for j := range pp.Steps {
    step := &pp.Steps[j]
    if completed >= total {
        step.State = StepStatusCompleted
    } else if step.Number <= completed {
        step.State = StepStatusCompleted
    } else {
        step.State = StepStatusPending
    }
}
```
- **Why:** This infers per-step completion solely from step *number* vs the phase's *completed count*. If steps were completed out of order (which the task system explicitly supports via `DependsOn`), or if some steps were skipped/failed, the writer incorrectly marks lower-numbered steps as completed and higher-numbered ones as pending, regardless of the actual step state. After `Synthesize` → `UpdatePlanStatus`, the `plan.md` file diverges from the task store and the dashboard shows misleading progress. Also: when called with `total=0`, `total` is set to `len(pp.Steps)` but `completed` is unchanged — if `completed > len(pp.Steps)` the `completed >= total` branch triggers and marks everything done.
- **Fix:** Either accept a real per-step state map (stepNumber → status) as input instead of just the aggregate counts, or skip the inference entirely and preserve `step.State` from the parsed file. At minimum, gate the heuristic on `completed == number of steps with Number ≤ completed` which isn't given either — the data simply isn't sufficient for this inference.

### S2-8 Hermes checkEnvVar uses sh -c with string concatenation — shell injection from config
- **File:** `internal/skills/hermes_compat.go:105-112`
- **Evidence:**
```go
func (c *DefaultPrerequisiteChecker) checkEnvVar(name string) error {
    out, err := exec.Command("sh", "-c", "printenv "+name).Output()
    ...
}
```
- **Why:** `name` comes from `HermesSkillMetadata.Prerequisites.EnvVars` parsed from the skill YAML. A malicious or malformed skill file with `env_vars: ["FOO; rm -rf $HOME"]` would execute arbitrary shell. In the meept model skills are operator-installed (so this is closer to "trusted config" than "user input"), but (a) the project's CLAUDE.md security posture is defense-in-depth for subprocess invocations, (b) `exec.LookPath` for commands already uses the safe pattern in the adjacent `CheckCommands` (line 117), and (c) there's no validation that `name` matches a valid POSIX env var identifier. Shell metacharacters in a skill file should fail safe, not execute.
- **Fix:** Drop the shell entirely: `if v, ok := os.LookupEnv(name); !ok || v == "" { return error }`. This matches `CheckCommands`/`CheckPythonPackages` which use `exec.LookPath` / direct `exec.Command("pip", "show", pkg)` without a shell wrapper.

### S2-9 RepoGraph weightedLine ID packing can collide at 1e9 nodes
- **File:** `internal/repomap/graph.go:132-141` (with `nodeID` counter at `:41`)
- **Evidence:**
```go
func newWeightedLine(from, to graph.Node, weight float64) *weightedLine {
    // Pack two int64 node IDs into a single unique ID using integer arithmetic.
    // This requires node IDs to stay well below 1e9 (1,000,000,000) to avoid overflow.
    return &weightedLine{
        ...
        IDVal: from.ID()*int64(1e9) + to.ID(),
    }
}
```
- **Why:** The global `nodeID` counter (`atomic.Int64`, line 41) is monotonically incremented for every node across every `BuildGraph` invocation in the process lifetime. In a long-running daemon that rebuilds repomaps (e.g. per-session or on file-watch), IDs accumulate forever. Once any node ID exceeds 1e9, two distinct `(from, to)` pairs can produce the same `IDVal` (e.g. `from=1e9, to=0` collides with `from=0, to=1e9`). The gonum graph treats line IDs as uniqueness keys, so colliding IDs cause edges to be dropped or merged — corrupting the PageRank input and producing subtly wrong symbol rankings. The 1e9 threshold is not unrealistic for a long-lived daemon with many rebuilds.
- **Fix:** Allocate edge IDs from a separate atomic counter (`var edgeID atomic.Int64; ... IDVal: edgeID.Add(1)`), or use a `struct{ from, to int64 }` key map. The packed-ID optimization is a micro-optimization that's not worth the correctness risk.

### S2-10 self-improve applier `strings.Replace(..., 1)` can patch the wrong occurrence
- **File:** `internal/selfimprove/applier.go:117`
- **Evidence:**
```go
original, fixed, err := parseDiff(fix.Diff)
...
newContent := strings.Replace(string(content), original, fixed, 1)
if newContent == string(content) {
    return nil, fmt.Errorf("original code not found in file")
}
```
- **Why:** `parseDiff` extracts the original snippet from the diff, and the applier replaces the first occurrence in the file. If the same snippet appears more than once (very common — think `return nil, err`, `if err != nil {`, `mu.Lock()`, import statements, blank lines), the replacement targets the first textual match rather than the location the LLM actually patched. The backup is then taken from the right file, but the edit is applied at the wrong site. The "original not found" branch is a poor signal because partial-match-then-wrong-location is the more likely failure mode in practice.
- **Fix:** Require the diff to carry explicit byte/line ranges (the proposed-fix struct should already have them) and apply the replacement at the recorded position rather than blind string substitution. At minimum, fail closed if `strings.Count(content, original) > 1` unless the diff anchors the location.

### S2-11 RepoMapGenerator.buildPersonalization reads watchedFiles under RLock but iterates with `+=`
- **File:** `internal/repomap/generator.go:314-322`
- **Evidence:**
```go
for _, ident := range mentionedIdentifiers {
    g.mu.RLock()
    for _, file := range g.watchedFiles {
        if matchesPathComponents(filepath.Base(file), ident) {
            pers[file] += 1.5
        }
    }
    g.mu.RUnlock()
}
```
- **Why:** Not a data race (RLock is held for the read of `watchedFiles`), but two issues: (1) the RLock is taken/released once per identifier in the outer loop instead of once around the whole nesting — needlessly contended; (2) `pers[file] += 1.5` accumulates across identifiers, which means a file that matches N identifiers gets N×1.5 weight. Whether that's desired depends on intent — but the API docstring says "Medium bias for files matching mentioned identifiers" without specifying. If the intent is a flat 1.5× bias for any match, the code is wrong; if the intent is linear scaling, it's undocumented. Either way, the loop structure (release and re-acquire the RLock per identifier) is wasteful.
- **Fix:** Hoist the RLock to wrap both loops. Document whether per-identifier accumulation is intended.

### S2-12 Code Intel ApplyEdits uses `positionToByte` which is O(n) per edit — O(n×e) total
- **File:** `internal/code/ast/rewrite.go:240-252` (`ApplyEdits`), `:254-269` (`positionToByte`)
- **Evidence:**
```go
func ApplyEdits(source []byte, edits []ProposedEdit) []byte {
    sortedEdits := ...
    result := make([]byte, len(source))
    copy(result, source)
    for _, edit := range sortedEdits {
        startByte := positionToByte(result, edit.StartLine, edit.StartChar)
        endByte := positionToByte(result, edit.EndLine, edit.EndChar)
        ...
    }
}
```
- **Why:** `positionToByte` scans the source linearly from offset 0 to find the byte offset for a given (line, char). Because edits are applied in reverse order (line > first), each subsequent lookup walks a *fresh* `result` slice from the start. For a file with E edits, this is O(E × N) where N is the file length. On a 50 KB file with 50 matched edits this is 2.5 MB of work — fine. On a 500 KB generated file with 500 edits it becomes 250 MB, which starts to matter for the `ast_edit` tool. Worse, `RunRewrite` already computes byte-accurate `startByte`/`endByte` via `capture.Node.StartByte()` (rewrite.go:152-153) and then *throws them away* in favor of recomputing from line/char. The line/char fields exist only for the preview JSON.
- **Fix:** Either (a) carry `StartByte`/`EndByte` on `ProposedEdit` and use them directly in `ApplyEdits` (preferred — they're already computed and exact), or (b) memoize line-start offsets in `ApplyEdits` via `bytes.IndexByte` from the previous search position since edits are sorted.

## Low

### S2-13 ExpandEnvVars has dead code — "Warn if we hit the cap" is a no-op
- **File:** `internal/config/config.go:167-173`
- **Evidence:**
```go
    // Warn if we hit the cap (cycle detected) - caller should log
    if envVarPattern.MatchString(result) {
        // Return result with remaining unresolved vars
        // Caller responsible for logging warning about potential cycle
    }
}
return result
```
- **Why:** The `if` block contains only comments — no `slog.Warn`, no error, nothing. The function signature returns `string` so there's no way for the caller to know the cap was hit anyway. Cyclic env var references (`A=${B}`, `B=${A}`) silently resolve to empty strings without any diagnostic. Round-4 fixed a similar predictable-ID issue with `generatePatternID`; this is in the same file family and the comment suggests the author intended a warning but never wired it.
- **Fix:** Either delete the dead block, or add `slog.Warn("env var expansion hit maxPasses — possible cycle", "input", s)` and return the result. The latter is more useful given the project's structured-logging posture.

### S2-14 KeywordExtractor compiles `regexp.MustCompile` on every call
- **File:** `internal/skills/keyword_extractor.go:119`
- **Evidence:**
```go
func (ke *KeywordExtractor) extractFromName(name string) []string {
    ...
    parts := regexp.MustCompile(`[-_\s]+`).Split(nameLower, -1)
    ...
}
```
- **Why:** `extractFromName` is called once per skill during `Extract` (keyword_extractor.go:78-104), and `Extract` runs during `CapabilityIndex.Rebuild` which iterates the whole skill registry. For 100 skills this is 100 redundant regex compilations. Regex compilation is relatively expensive (microseconds each, milliseconds aggregate). The class-level fix is a package-level `var nameSplitter = regexp.MustCompile(...)` initialized once.
- **Fix:** Hoist the regex to a package-level `var` or a field on `KeywordExtractor` constructed in `NewKeywordExtractor`.

### S2-15 Plan state machine — `ConfirmPlan` allows only `StateCompleted`, skipping failed/cancelled
- **File:** `internal/plan/manager.go:261-302`
- **Evidence:**
```go
if plan.State != StateCompleted {
    return fmt.Errorf("plan %s is in state %s, expected completed", planID, plan.State)
}
```
- **Why:** A plan that went `executing → failed` (or `cancelled`) cannot be confirmed even when `StateFailed.IsTerminal() == true`. The signoff table supports `Action="confirmed"` regardless, but the manager hard-fails. The convention elsewhere (e.g. `CancelPlan` at line 305) uses `IsTerminal()` as the gate. This is more an API inconsistency than a bug — if a user wants to "confirm" a failed plan as genuinely done (no more work), they have to go through `CancelPlan` instead, which doesn't record a signoff.
- **Fix:** Allow confirm from any terminal state, or add a separate `AcknowledgePlan` for failed/cancelled plans. At minimum, document why `completed` is the sole gateway.

### S2-16 Plan handler has no retry/backoff on bus handler error
- **File:** `internal/plan/handler.go:76-101`
- **Evidence:**
```go
func (h *PlanHandler) handleStepCompleted(ctx context.Context, msg *models.BusMessage) {
    ...
    if err := h.manager.OnStepCompleted(ctx, payload.TaskID, payload.StepID); err != nil {
        h.logger.Error("plan handler: failed to process step completed", ...)
    }
}
```
- **Why:** If `store.IncrementPhaseProgress` fails with a transient SQLite busy/locked error (which is common under WAL mode with multiple writers), the event is logged and dropped — the phase progress is permanently undercounted and the plan can never reach `completed` via `OnStepCompleted`. The plan handler is a bus subscriber with no DLQ, no retry, no requeue. For a system whose sole job is tracking plan progress, this is a correctness concern.
- **Fix:** Either retry transient errors with backoff inside the handler, or publish the event back onto a retry topic. At minimum, increment a Prometheus counter and emit an alertable log level (Warn persists; Error should page).

### S2-17 LSP Manager.StopServer doesn't remove server from map if Close fails
- **File:** `internal/code/lsp/manager.go:174-200`
- **Evidence:** `StopServer` does `delete(m.servers, name)` *before* calling `srv.DocMgr.CloseAll` / `srv.Client.Shutdown` / `srv.Client.Close`. If any of those fail the server is already gone from the map but the OS process / TCP connection may still be alive, with no way for the operator to reach it through the manager. Conversely, if a second caller looks up the server between the `delete` and the close, they get `nil` and may spawn a duplicate. Combined with S2-3, the LSP lifecycle has multiple race windows.
- **Fix:** Move `delete` to *after* the close calls, or return the closed server to the map on failure.

### S2-18 parseCompositeDuration iterates suffix list with `HasSuffix` — `m` vs `ms` ordering bug
- **File:** `internal/config/json5_loader.go:224-250`
- **Evidence:**
```go
units := map[string]int64{"d": 86400e9, "h": 3600e9, "m": 60e9, "s": 1e9, "ms": 1e6, "us": 1e3, "ns": 1}
for _, suffix := range []string{"d", "h", "m", "s", "ms", "us", "ns"} {
    for strings.HasSuffix(raw, suffix) {
        prefix := raw[:len(raw)-len(suffix)]
        ...
        d += int64(float64(f) * float64(units[suffix]))
        raw = prefix
    }
}
```
- **Why:** When parsing `1ms`, the outer loop hits `m` first (before `ms`) because the suffix slice order is `["d","h","m","s","ms",...]`. `strings.HasSuffix("1ms", "m")` is false (good), so it falls through to `ms` and consumes `1`. But for `1m30ms`, the `m` loop consumes `1m` → `raw="30ms"`, then `s` loop runs `HasSuffix("30ms","s")` → true → consumes the `s` leaving `raw="30m"` → then `m` again consumes the trailing `m` leaving `raw="30"`. This gives `1m + 30s(?) + ...` — wait, the `s` suffix incorrectly matched the `s` inside `ms`. The inner loop should only consume a suffix if the remaining string is a complete unit, but `HasSuffix` is greedy.
- **Fix:** Use a regex like `(\d+(?:\.\d+)?)(ns|us|ms|s|m|h|d)$` anchored to the end, longest-match-first, or use `go.time.ParseDuration` directly since the format is already Go-compatible.

### S2-19 Plan ID generation uses timestamp + atomic counter — predictable and collides across restarts
- **File:** `internal/plan/plan.go:88-107`
- **Evidence:**
```go
var planIDCounter atomic.Uint64
func generatePlanID() string {
    seq := planIDCounter.Add(1)
    return fmt.Sprintf("plan-%s-%04d", time.Now().UTC().Format("20060102150405"), seq)
}
```
- **Why:** Same bug class as the round-4 `generatePatternID` finding. The counter resets to 0 on every daemon restart, so after a restart the next `CreatePlan` produces `plan-<timestamp>-0001` just like the first call after the previous start — collision risk if timestamps also collide (e.g. two starts within the same second during testing/CI). The `%04d` width also overflows at 9999 plans per second, which is unrealistic but worth noting. Per the project memory, the round-4 reviewer flagged the same pattern in `learning.go` (which was fixed with SHA-256). The plan package still uses the old pattern in three places (`generatePlanID`, `generatePhaseID`, `generateSignoffID`).
- **Fix:** Use `pkg/id.Generate()` or a UUID library for all plan/phase/signoff IDs, consistent with the round-4 fix in `learning.go`.

### S2-20 AST positionToByte returns `len(source)` instead of erroring on out-of-range
- **File:** `internal/code/ast/rewrite.go:254-269`
- **Evidence:**
```go
func positionToByte(source []byte, line, char int) int {
    currentLine := 0
    currentChar := 0
    for i, b := range source {
        if currentLine == line && currentChar == char {
            return i
        }
        if b == '\n' { currentLine++; currentChar = 0 } else { currentChar++ }
    }
    return len(source)  // <-- silently clamps to end-of-file
}
```
- **Why:** If an edit's (line, char) exceeds the file's actual dimensions (e.g. the file was modified between parse and apply, or the LLM produced a stale range), the function returns the file end and `ApplyEdits` happily splices at EOF — appending the new text rather than replacing the intended range. The caller has no way to detect this. For a structural edit tool this is a footgun: a single stale edit can corrupt the file.
- **Fix:** Return `(int, error)` or a sentinel `(-1, false)` and have `ApplyEdits` skip/fail edits that don't resolve.

### S2-21 GoLinter.TypeCheck ignores its own error and always uses stderr
- **File:** `internal/lint/languages/go_lint.go:86-105`
- **Evidence:**
```go
err := cmd.Run()
if err == nil {
    return nil, nil
}
results, _ := parseGoErrors(stderr.String(), "")
return results, nil
```
- **Why:** `err == nil` means `go build` succeeded; non-nil means it failed. The code then returns the parsed stderr as lint results. But the `_` discards the error from `parseGoErrors` — that's fine since it's always nil. The real issue: `go build` emits errors to stderr in a format that `parseGoErrors` expects, but `go build` failures include linker errors, flag errors, and environment errors (e.g. "go: command not found", "permission denied") that don't match the regex and are silently dropped. The user sees "no lint errors" when the build is actually broken in a way the regex doesn't recognize.
- **Fix:** If no regex matches and stderr is non-empty, return a single LinterResult with the raw stderr as the message.

### S2-22 ASTResolveTool writes file with `0o000` permissions (mode 0)
- **File:** `internal/code/tools/ast_resolve.go:116`
- **Evidence:**
```go
if err := os.WriteFile(filePath, result, 0); err != nil {
    return nil, fmt.Errorf("failed to write file: %w", err)
}
```
- **Why:** `os.WriteFile(path, data, 0)` passes perm=0. On Unix, the file is created with mode `000` (no read, no write, no execute for anyone) *if* the file doesn't already exist — the operator then has to `chmod` it before they can read or edit it. If the file does exist, `WriteFile` only applies the perm on creation, so existing files are fine. For a tool whose purpose is to apply AI-proposed edits to source files, creating an unreadable file on first edit is a nasty surprise.
- **Fix:** Use `0o644` (consistent with `ast_edit.go:230`, `lsp_rename.go:316`, and the rest of the codebase).

---

## Severity Summary
- **Critical: 2** (S2-1 LSP TCP write mutex, S2-2 LoadJSON5WithDefault always errors)
- **High: 3** (S2-3 LSP StartServer race, S2-4 RepoMap cache swap, S2-5 skills `<=` shadowing)
- **Medium: 7** (S2-6 plan scanner, S2-7 plan writer heuristic, S2-8 hermes shell injection, S2-9 graph ID collision, S2-10 applier wrong-match, S2-11 buildPersonalization, S2-12 ApplyEdits O(n×e))
- **Low: 10** (S2-13 dead code, S2-14 regex recompile, S2-15 confirm gating, S2-16 no retry, S2-17 StopServer ordering, S2-18 duration parsing, S2-19 predictable IDs, S2-20 silent range clamp, S2-21 swallowed linker errors, S2-22 mode 0 file)
