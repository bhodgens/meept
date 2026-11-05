## Interface Contracts

### B1: Per-tool cooperative timeout (Owner: 01-tool-timeout.md; Consumers: 02)

```go
// internal/tools/interface.go - add to the Tool interface:
TimeoutMs() time.Duration // 0 = no declared timeout; >0 = co-operative budget
// ToolDefaults (interface.go:60) returns TimeoutMs() time.Duration { return 0 }
//
// internal/agent/executor.go - the live dispatch wrapper (executeToolWithRetry,
// executor.go:1429; tool.Execute at :1480): when TimeoutMs() > 0, derive a
// context deadline with context.WithTimeout around the single tool.Execute /
// ExecuteStreaming body; on expiry return a structured error:
//   ErrToolTimeout = errors.New("tool timed out")          // sentinel (errors.Is)
//   ToolResult{Success:false, ErrCode:"TOOL_TIMEOUT",      // machine gear
//     Error:"tool call timed out after Nms"}
// No declared budget -> delegate unchanged (opt-in per tool). The wrapper must
// NOT swallow context cancellation from the turn-level deadline: only this
// tool's own timer substitutes the TOOL_TIMEOUT result.
// Consumers: 02 declares TimeoutMs() == 60s on lsp_navigate.
```

### B2: LSP navigation tool (Owner: 02-lsp-navigation.md)

```go
// internal/tools/builtin/lsp_navigate.go (NEW) - single read-only tool:
//   Name() "lsp_navigate"
//   Parameters: operation ("goToDefinition"|"findReferences"|
//       "goToImplementation"|"hover"), file_path (string, req),
//       line (int, one-based), character (int, one-based UTF-16),
//       workspace (string, optional)
// Implements TimeoutMs() -> 60s (B1). Reuses internal/code/lsp.Manager
// (manager.go:52 NewManager, :67 GetServerForLanguage) + Client methods
// client.go:282 GotoDefinition, :308 FindReferences, :331 Hover;
// ADD GotoImplementation (client lacks it today). Result caps
// DEFAULT_MAX_LOCATIONS=100, DEFAULT_MAX_RESULT_CHARS=16000.
// Workspace-required: mirror tools.ErrNoWorkingDir sentinel semantics
// (internal/agent/AGENTS.md) - the tool returns a detectable
// 'lsp tool requires a session workspace cwd' error when unresolvable.
// IsReadOnly -> true.
```

### B3: Durable skill catalog + hot-reload (Owner: 03-skill-catalog.md; Consumers: 04)

```go
// internal/skills/watcher.go (NEW) - fsnotify Watcher over the two discovery
// roots (project .meept/skills + ~/.meept/skills, discovery.go:31-53); on an
// add/change/remove under <root>/<skill>/SKILL.md or <root>/<skill>.md:
//   loader.Invalidate(name) or loader.Clear() (lazy_loader.go:238/:260)
//   re-Discover() (discovery.go:136) -> loader.SetIndex (lazy_loader.go:307)
//   publish bus event skills/change (typed bus.Topic semantics per root
//   AGENTS.md; payload {source:"skills"})
// Replaces the process-cache relist in context_injector.go:112-136
// (visibleSkillLines) with a DURABLE catalog: a session message materialized
// at turn start listing every visible model-invocable skill (C4 lines),
// digest-deduplicated (sha256 over sorted name+description), so the catalog
// is reconstructable after resume/restart, not just from the live cache.
// Discovery stays Authoritative for directory bundles and flat-markdown
// <name>.md files both discovered (dsh 1b).
```

### B4: `/name` direct invocation (Owner: 04-skill-invocation.md; Consumer: 03 context)

```go
// SKILL_GESTURE = /(^|\s)\/([a-z0-9]+(?:-[a-z0-9]+)*)(?=\s|$)/g
// Scan ONLY the claimed user text input (never tool/assistant content). For
// each matched <name> verified present in the catalog/loader (the loaded-body
// lookup decides; unknown or disable-model-invocation names stay prose),
// inject the rendered skill body as an explicit 'user asked for this skill'
// instruction appended to the context, closest to the answer. This is the
// explicit slash gesture, DISTINCT from named_agent_route.go (which routes to
// a named AGENT, not a skill). /usr/bin paths and 5/8 fractions must NOT match.
// Consumers: none downstream (builds on B3 catalog + LazySkillLoader).
```

