# Verified Anchors - branch 04-tool-surface (verified at 40f6fa5a)

Grep-verified. Use these exact shapes; do not invent APIs.

- `internal/agent/executor.go:1161` — `func (e *Executor) Execute(ctx context.Context, toolCall llm.ToolCall) *ExecutionResult`; `:1555` `ExecuteAll`; `:1702` `ExecuteSequential`; `:2031` `MockTool.Execute` (test fixture already in-package — use it). NO timeout logic exists today (grep 'timeout' empty) — the per-tool timeout (T1) is the RED.
- `internal/code/lsp/client.go` — package lsp; rename contract: `nil` edit means "no edits" (see :422/:449 nolint comments). Navigation methods (definition/references/symbols) are the T2 additions.
- `internal/code/lsp/manager.go:17` — `type Manager struct`; `:67` `GetServerForLanguage(ctx, languageID) (*ServerInstance, error)`; `:86` `StartServerForLanguage`; `:111` `StartServer(ctx, name, cfg config.LSPServerConfig)`; `:191` StopServer; `:248` Close; `:253` RunningServers.
- `internal/skills/discovery.go:31` — `DefaultTiers() []DiscoveryTier`; `:63-82` `DiscoveryOption`/`WithTiers`/`WithSources`/`WithDiscoveryLogger` (option pattern for the catalog leaf).
- `internal/skills/lazy_loader.go:11` — `type LazySkillLoader struct` (LRU on-demand bodies); `:36-41` `LazyLoaderOption`/`WithLoaderLogger`.
- `internal/tui/render/markdown.go:20` — `NewMarkdownRenderer(width int, darkMode bool)`; `:45` `(m *MarkdownRenderer) Render(markdown string) (string, error)` — message-quote rendering (T5) extends this.
- `internal/agent/context_injector.go:16` — `type ContextInjector struct`; `:23` `NewContextInjector(...)` — skill-catalog injection (T3) wires here.
- `internal/tools/interface.go:24` — `type Tool interface` (Name/Description/Parameters/Execute).

New symbols your tests reference (contracts T1-T5 in .hermes/contracts/04-tool-surface.md — undefined today, that IS the RED): per-tool timeout in ExecutionResult, lsp navigation client methods + lsp_navigate builtin tool, skill catalog context injection, message quote rendering, gh_issues tool.

RED-gate convention (repo precedent commit 537c92fd): if a test file's
undefined-symbol errors break `go vet`/`go build` of the NON-test package,
park it as `<name>_test.go.staged` and say so. If `go build` stays green and
only `go test` fails on undefined symbols, keep it live.

e2e: suite dir `e2e/suites/tool-surface/` (NEW). Build tag `//go:build e2e`,
import `github.com/caimlas/meept/e2e/harness`. Register in `e2e/manifest.json`.

ANTI-VACUOUS BAR: assert values, error identity, state transitions. Timeout
tests assert the result carries the timeout marker AND the wall-clock bound
(elapsed < configured timeout + slack) using a MockTool that sleeps. Quote
tests assert exact rendered output for a quoted block. Catalog tests assert
the injected block contains the pinned skill names and respects the byte cap.
