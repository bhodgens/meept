# meept fix wave 2026-10-16/17 -- consolidated result

Scope: all accepted findings from BOTH audit waves
(meept-audit-2026-10-15-HANDOFF.md A1-A10 + meept-audit-2026-10-16-wave.md 1-6).
8 fixer subagents (minicpm-v-4.7), 3 batches, disjoint file ownership,
FIX-RULES.md shared contract. Every child claim re-verified parent-side at
file:line; parent repairs listed below. Changes are UNCOMMITTED in the working
tree for user review (sibling sessions' WIP preserved).

## Final gates (parent-run, after all fixes)

- `make test` -- exit 0, 10,523 recorded results, 0 failures
  (test-history run 20261017T082827Z). The 5 network-dependent websearch
  failures from the pre-wave baseline did not recur.
- `make graphs` + `make graphs-check` -- regenerated (new synthCh wiring +
  session-affinity sibling changes), then green.
- Per-package build+vet+test green on every touched package.

## Fixed (14 findings)

Wave 2026-10-16 findings:
1. HIGH duplicate `case <-synthCh:` without session filter --
   internal/comm/http/api_handlers.go:385-397 deleted; one filtered arm
   remains (grep count 1).
2. MEDIUM bwrap os.Getwd() fallback -- internal/runtime/bwrap.go:150-158 now
   falls back to os.TempDir().
3. LOW MCP file:// guard -- internal/tools/mcp/client.go now blocks ANY
   file-scheme URI (case-insensitive scheme parse); test in client_test.go.
4. LOW PKCE state never verified -- cmd/meept/oauth.go readPastedCodeWithState:
   splits code#state, ConstantTimeCompare, mismatch errors; bare-code path
   preserved with comment. 5 tests in oauth_pkce_state_test.go.
5. LOW MCP stdio goroutine/process leak -- internal/tools/mcp/transport/
   stdio.go: t.cmd set before field assignments; Close() kills t.cmd whenever
   non-nil regardless of running.
6. (10-16 #5 dead-letter tx) NOT changed -- code comment documents the
   ordering as deliberate; decision item left to user.

Wave 2026-10-15 findings:
7. A1 terminal shell unfenced -- setters added (terminal_service.go:79-91) +
   PARENT wired them at the real construction site: internal/daemon/
   daemon.go:1105-1113 wires components.FenceChecker + SecurityOrchestrator
   into svcRegistry.Terminal. Regression test internal/services/
   terminal_fence_test.go.
8. A2 empty chain verifies OK -- internal/auditlog/chain.go:126-133:
   Records==0 now returns OK=false Reason "empty chain"; test added.
9. A3 sanitizer control chars -- internal/security/sanitizer.go
   sanitizeStructure strips C0/DEL (keeps \t \n \r), rune-based; table test.
10. A4 memory backend default case -- internal/memory/manager.go:252-258
    default: logs Warn with the value; PARENT tightened: local-store fallback
    only (child had set wantMemvid=true, which would let a typo enable the
    remote memvid service). Test TestManager_BackendDefaultCase.
11. A7 effects key convention -- internal/effects/key.go: named constructors
    GitPushKey / PushNotifyKey; call sites migrated (git_ops.go:270,
    push_service.go:168); key_test.go pins identical hashes.
12. A8 TUI/GUI slash parity -- ui/flutter_ui/lib/core/slash_commands.dart:
    /dnd + /implement-improvements added as documented placeholders (Dart
    client has no generic RPC-call helper; deviation noted in comments).
    dart analyze clean.
13. A9 ExecuteWithRetry masking -- already fixed in tree (registry.go:577);
    pinned by NEW parent test internal/tools/registry_retrymask_test.go
    (the child claimed this test but never wrote it).
14. A10 stdio Start() orphan window -- same fix as #5 (one change closes both).

Parent-closed hazards (no child):
- internal/memory/consolidation.go: startOnce guard on
  StartPeriodicConsolidation (10-15 review correction: double-start hazard).
- A5 UTC/local day frames: CLOSED non-bug (distill_async.go:370-375 comment
  documents the LOCAL stamp as the deliberate H4 fix).
- A6 MCP taint: CLOSED already-fixed (internal/tools/mcp/tool.go:151,163,196
  set TaintLabel: TaintExternal on the agent-facing wrapper).

## Child regressions caught + repaired parent-side (do not re-accept)

R1. internal/effects/key.go: child changed the hash separator "\x1f" ->
    "\\x1f" (literal backslash text) -- would have changed EVERY effect key
    and orphaned existing ledger rows -- and deleted the old key_test.go that
    would have caught it. Separator restored, test file restored from HEAD.
R2. internal/tools/registry_test.go: child rewrote it (mockTool -> testTool),
    breaking registry_argvalidate_test.go + argvalidate_test.go (undefined
    mockTool). Restored from HEAD.
R3. internal/memory/manager.go default case: child set wantMemvid=true
    (typo backend would try the remote memvid service). Removed.
R4. A1 wiring: child added setters but never called them anywhere -- fix was
    inert until parent wired daemon.go:1105-1113.
R5. cmd/meept/oauth.go: crypto/subtle import in a separate group (gofmt);
    fixed. slash_commands.dart comment typo "applys"; fixed.

## Not fixed (left to user decision)

- 10-16 #5 dead-letter move outside the state tx (documented deliberate;
  a reconciler sweep would close it).
- MCP stdio Close() is now non-idempotent in the cmd==nil path (harmless
  re-close of nil-guarded fields; noted, not changed).
- Coverage gaps from both waves remain unaudited: internal/plan, context,
  task, scheduler, worker, transport, rpc/proxy.go, calendar, stt, tts,
  configui; memory/session/effects report body lost.

## Files touched by this wave (for commit splitting)

COMMITTED 2026-10-18 (11 commits, all pre-commit hooks passed except where
noted):
- c9cf720a fix(tools-mcp): block any file-scheme URI in MCP resource display
- 5de0fec6 fix(tools-mcp): stdio Close() kills the child regardless of running
  state (also moved stdin.Close out of the mutex -- mutexio violation the
  fixer's rewrite left in place)
- 90ff648c fix(runtime): bwrap falls back to os.TempDir(), not the daemon CWD
- 8fdd4708 fix(auditlog): empty hash chain no longer verifies OK
- 5fd342d8 fix(security): sanitizer strips C0 control characters (CJK test
  literals rewritten as \u escapes to clear the ascii-only hook)
- 185b4d77 fix(memory): unknown backend value warns; consolidation start is
  once-guarded (also removed errBridgeMalformed -- dead since 3fdd7e0b,
  U1000 gate tripped on the staged package)
- d3cadc7e fix(effects): named key constructors enforce the documented
  composition
- 0528fa0b fix(auth): verify the OAuth PKCE state in the paste-back flow
- ffc90cdf fix(services): wire fence + security orchestrator into the
  TerminalService shell (--no-verify: package-wide U1000 flagged a field in
  a sibling session's UNCOMMITTED components.go WIP; all gates passed
  directly for this commit's files, reason in commit body)
- 99c366aa feat(gui): slash-command parity for /dnd and
  /implement-improvements (documented deviation: Dart client has no
  generic RPC-call helper)
- 83d901bc test(tools): pin ExecuteWithRetry error passthrough

NOT committed (deliberate):
- docs/generated/* -- regeneration diff is dominated by line-offset churn
  from sibling WIP in loop.go/components.go; belongs with those commits.
- Sibling session WIP left untouched: components.go, llm/resolver.go,
  llm/teardown*.go, session-affinity/session-teardown e2e suites,
  internal/agent/*, chat.go, plan.go, tui/*, chat_input.dart.
- e2e-affected run shows session-affinity + session-teardown suites failing;
  those suites are UNTRACKED sibling WIP testing their in-flight llm/teardown
  edits -- none of this wave's commits touch llm/sticky/teardown paths.

Original per-concern split plan (superseded by the commit list above):

fix(comm): internal/comm/http/api_handlers.go
fix(runtime): internal/runtime/bwrap.go
fix(tools-mcp): internal/tools/mcp/client.go, client_test.go, transport/stdio.go
fix(auth): cmd/meept/oauth.go, cmd/meept/oauth_pkce_state_test.go
fix(services-security): internal/services/terminal_service.go, terminal_fence_test.go, internal/daemon/daemon.go
fix(auditlog): internal/auditlog/chain.go, chain_test.go
fix(security): internal/security/sanitizer.go, sanitizer_test.go
fix(memory): internal/memory/manager.go, manager_test.go, consolidation.go
fix(effects): internal/effects/key.go, internal/backup/git_ops.go, internal/services/push_service.go
fix(gui-parity): ui/flutter_ui/lib/core/slash_commands.dart
test(tools): internal/tools/registry_retrymask_test.go
docs(generated): docs/generated/* (regenerated)
