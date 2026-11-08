# Adversarial regression audit — 2026-11-06 wave: agent/tools/skills fix commits

- **HEAD audited:** 7b510ed4ffb2374d8609ade9ce569ef0e17ba49f (`git rev-parse HEAD` verified)
- **Started:** 2026-11-07 00:05 UTC
- **Method:** `git show <commit>` per fix; touched files read at committed HEAD state (`git show HEAD:<path>` — working tree carries sibling WIP in dispatcher*/loop_test.go); sibling call-site greps; `go build` of all 7 touched packages green; targeted `go test -run` on the new pins green (all `ok`).
- **Commits in scope:** 16c4ad21 (exposure hard gate), 69cd0ea6 (meta-tool ceiling), 7e223ee9 (baseline-presence cap), 791486c7 (skills origin no-clobber), 494cb6bb (turn-journal FIFO rings), f5eb3e03 (memory recall global→user), 96470584 (PII DeobfuscateUntagged), 78a1da70 (bridge scope filter)

## Findings

### F-A [MEDIUM] 16c4ad21: ClearConversation plan-prune wired to a method production never calls; the live /reset path still leaks
`internal/agent/loop.go:9348-9359` (HEAD) adds the F-4 prune inside `AgentLoop.ClearConversation`:
```go
func (l *AgentLoop) ClearConversation(id string) {
	l.conversations.Delete(id)
	...
	l.exposureMu.Lock()
	delete(l.exposurePlans, id)
```
But the production conversation-clear path is `ChatHandler.ClearConversation` (`internal/agent/handler.go:3001-3004`), which bypasses the method entirely:
```go
func (h *ChatHandler) ClearConversation(conversationID string) {
	if h.loop != nil && h.loop.conversations != nil {
		h.loop.conversations.Delete(conversationID)
	}
}
```
`internal/daemon/session_rpc.go:84` (`/reset` RPC) calls `chatHandler.ClearConversation(req.SessionID)`. Grep of all non-test callers of `ClearConversation` across `internal/` + `cmd/`: the only caller of the loop method is `loop_test.go:199`. Consequence: the exposure-plan map growth per conversationID and the "recycled conversationID silently resumes a frozen episode" failure (the commit message's own stated F-4 motivation) remain live on every real reset. The prune is dead code on the wired path.

### F-B [MEDIUM] 16c4ad21: hard gate is fail-OPEN for tools registered after the plan snapshot — promotion fully bypassable via tool_search
The gate (`internal/agent/executor.go:1283-1284`, HEAD) refuses only when the frozen snapshot knows the name:
```go
if plan := exposurePlanFromContext(ctx); plan.Enforcing() &&
	plan.IsKnown(toolName) && !plan.IsActive(toolName) {
```
`knownNames`/`ceilingNames` are built once per conversation at `ensureExposurePlan` (`internal/agent/loop.go:2177-2211`, sticky via the `if p, ok := l.exposurePlans[conversationID]; ok { return p }` early-return at 2165) and never re-scanned. A tool registered mid-conversation (MCP server connect/reconnect, the exact scenario the drift comment at executor.go covers for the *vanishing* direction) is `IsKnown == false`, so the gate skips it and `registry.Get(toolName)` finds and executes it — no `tool_view` promotion, no ceiling check. Reachable deliberately: for an agent whose `l.registry` is the unfiltered/full registry (ceiling == known == snapshot), `tool_search` falls back to the daemon-wide scope and lists the new tool; the model then calls it directly and the hard gate waves it through. The gate's permission purpose (F-2 comment: "a live execution gate (F-1) made that a permission bypass") is defeated for every post-arm registration.

### F-C [MEDIUM] 16c4ad21: codemode tool-bridge bypasses the row-4 execution gate
`internal/tools/builtin/codemode.go:194` (`codemodeBridgeCall`, HEAD):
```go
tool := reg.Get(name)
...
res, err := tool.Execute(ctx, argsMap)
```
The bridge re-enters the agent's filtered registry directly, never through `Executor.Execute` — the row-4 gate lives only there. A model whose plan has `codemode` active (in the predicted/core set) can execute ANY tool in the filtered registry from inside a sandbox script — including known-but-inactive tools the gate exists to refuse — without the `tool_view` promotion round-trip. The ceiling/group filtering is respected (T5 no-widening holds), but the active-set enforcement is 100% bypassed on this lane. Related, same class: the SKILL.state-runtime lane (`internal/agent/skill_state_option.go:42`, `ExecuteSkillToolCalls` → `executeToolCalls`) is reached from `SkillStateRuntime` loops whose ctx never passed through `reasoningCycle`'s `WithExposurePlan` (`loop.go:4729`), so state-mode skill tool calls run un-gated — lower confidence that any armed plan applies there by design; flagged, not scored.

### F-D [MEDIUM] f5eb3e03: fixed the tool lane against a "loop recall" the loop doesn't actually use — loop injection STILL passes ScopeGlobal verbatim
The commit maps declared-global → user purview in `internal/tools/builtin/memory.go:40-41` citing "the loop/dispatcher injection path (`internal/agent/scope_wiring.go` memoryScopeOptions)". But grep of `memoryScopeOptions(` callers: only `internal/agent/dispatcher_scope.go:83` — the AgentLoop's own recall, `buildMemoryContext`, builds the filter verbatim at `internal/agent/loop.go:7706` (HEAD):
```go
memory.WithScopeFilter(memory.Scope(scopeIdentity.MaxScope), scopeIdentity.ProjectID, scopeIdentity.UserID),
```
For a `memory_max_scope: global` chief-of-staff agent, the C2 predicate (`internal/memory/scope_filter.go:110-124`, `visible`: "a global-scope caller sees only global rows") hides every project/user row from its prompt-injected recall, while its `memory_search`/`memory_get_context`/`SearchWithGraph` tool lane (post-fix) now spans all projects. The fix reversed the contradiction it was written to close instead of closing it: tool recall and loop recall now disagree in the opposite direction, violating the file's own "three-site rule" (same identity for recall filters and tool-execution ctx, `scope_wiring.go:11-13`). Corollary touching 78a1da70: bridges folded to `user` scope (global+project source mix) are invisible to the verbatim-global loop caller — its injected recall silently loses bridge rows its own tools surface.

### F-E [LOW] 791486c7: origin parser fails OPEN on quoted values and CRLF files that the YAML loader parses as `origin: user`
`internal/skills/lifecycle/origin.go:45,60-62` (HEAD): guard fires only on `HasPrefix(text, "---\n")` and line match `l == "origin:" || strings.HasPrefix(l, "origin: ")` with an exact-token value switch. Two fail-opens:
1. `origin: "user"` (quoted) → value `"user"` → `Origin("\"user\"")` matches no case → `default: return OriginUnknown` → unprotected. The actual reader, `internal/skills/models.go:271` (`Origin string yaml:"origin"`), parses quoted `"user"` as user — so the guard's vocabulary and the loader's diverge; a legitimately-user-stamped file written with YAML quoting gets zero protection.
2. A CRLF-written SKILL.md (`---\r\n...origin: user`) → `HasPrefix(text, "---\n")` fails at line 45 → `OriginUnknown` → unprotected (and `stampSkillOrigin` at writer.go:375 has the same LF-only assumption, so the round-trip never normalizes it).
The commit's fail-soft posture ("guard fires ONLY on a proven origin: user") makes these silent-protection gaps rather than crashes, but "proven" should include proven-for-the-parser-that-renders-the-file.

### F-F [LOW] 791486c7: all refused funnels share one sidecar path — earlier candidates are silently overwritten
`internal/skills/lifecycle/writer.go:277-279` (HEAD): every refused non-user write goes to the single `targetPath + ".new"` via `os.WriteFile`. Agent-then-evolver-then-review refusals of the same skill clobber each other's candidates with no warning or naming distinction (the install-sync `<file>.new` convention is one-pending-change; here refusals recur every cycle — see F-G). Refusal errors quote the sidecar path so a human sees where the *latest* candidate went, but the discarded prior candidate is unrecoverable.

### F-G [LOW] 791486c7: no proposal-dedup for user-origin skills — evolver re-proposes and re-refuses every cycle
`internal/skills/lifecycle/evolver.go:818`: `applyProposal` returns the wrapped `ErrUserClobber` as a plain apply failure; nothing records the refusal, so the next evolution cycle regenerates the same refine/create proposal, re-refuses, and rewrites the shared sidecar indefinitely (unbounded churn, report noise, no acknowledge path that marks the skill "user-locked, stop proposing"). The guard works; the retry loop around it was not updated for a *permanent* refusal class.

### F-H [LOW] 78a1da70: the bridge LISTING surface is not scope-gated — same leak class, still open
The fix gates `mergeBridges` recall (`internal/memory/manager.go:811` `bridgeVisible`) and the memvid fast path (`manager.go:1079`), but `internal/services/memory_service.go:130-158` `Bridges()` returns every persisted bridge record — full `Content`, `EvidenceIDs`, and `Metadata` (which now carries the folded scope identity) — via `store.ListBridges(ctx)` with no scope filter at any option site (it takes no identity at all). Whatever audience the GUI/TUI bridges view serves, a caller barred from a project's memory rows by C2 can read bridges distilled from them there. Pre-existing endpoint, but the commit's "close cross-project bridge leak" claim holds only for the recall lane; the leak path is the visibility/dashboard lane. (daemon/components.go:7008 exposes the store; verify consumer audience before triaging severity upward.)

## Reviewed clean (no findings)

- **7e223ee9** — reviewed clean: `baselinePresent` checks canonical names against the parent `r.tools` (the same registry `filterTools` builds the child from — correct side); all `BaselineTools` names are canonical (`shell_execute` alias never appears in baseline); `nil`-predicate and `limit<=0` paths delegate to `ScopedToolNames` byte-identically; the only remaining `ScopedToolNames` caller is the `limit<=0` recursion; MCP additional_tools grants correctly never presence-dropped.
- **494cb6bb** — reviewed clean: both dedup sets (`runStartedRuns`+`startedOrder`, `closedRuns`+`closedOrder`) are mutated exclusively under the single `runStartedMu` (verified every method at HEAD: writer.go:111-117, 176-182); `markDedup` runs only after the already-in-set early-return, so `order` can never hold duplicates; eviction-forgotten turnIDs yield duplicate `run_started`/`run_closed` rows only — no unique index in `turn_journal` (store_sqlite.go:19-31) and `OpenRuns` GROUP-BY tolerates it (store_sqlite.go:126-143); failure-keeps-marker behavior is unchanged pre/post fix.
- **96470584** — reviewed clean: `DeobfuscateUntagged` reads `s.obfuscateMap`/`s.sessionTags` under `s.mu.RLock` matching the writers' `Lock` (obfuscate runs under write lock; `tagPlaceholder` is only called from the locked lane); the fallback logic preserves sessionless untagged restore and refuses tagged cross-session restore in all branches; the only remaining process-wide `Deobfuscate` caller is `deobfuscateMessage` (secrets.go:419) behind `DeobfuscateMessages`, which has zero production callers — dead lane, flagged below as an observation for cleanup, not an output path.
- **69cd0ea6** — reviewed clean with the intended fallback documented: the `t.scope` daemon-wide fallback for ctx-less callers is explicitly documented and tested (`tool_ceiling_seam_test.go`); `tool_view` refusal is strictly cache-read-and-Promote-side-effect-free for out-of-ceiling names while in-ceiling names still `record` + `fireActivate` normally (tool_view.go HEAD lines 126-157) — activation path intact; the `filterTools` meta-union is the same daemon flag (`cfg.Agent.Tools.DynamicToolExposure`) the loop gates the enforcing plan on (components.go:3868), so ceiling-side and registry-side unions cannot diverge. Minor note: the union also lands in scoped agents whose `spec.ToolExposure` resolves to full/indexed (never gated) — a slight spec-grant widening, same shape as the pre-existing codemode union, acceptable.
- **16c4ad21 core mechanics** — the ctx seam itself (`WithExposurePlan`/`exposurePlanFromContext`), the unreachable-branch F-1 move, and the `GrantedToolNames` alias fix for the ceiling-fallback are correct at every audited call-site; findings above cover the missed lanes/drift, not the main path.

## AUDIT COMPLETE — 8 findings (0 HIGH, 4 MEDIUM, 4 LOW), 4 commits reviewed clean, 1 (16c4ad21) split: core clean + 3 MEDIUM + 1 LOW(lane)


## Closeout (2026-11-06 ~19:00 MST, fix pass)
All 8 findings fixed + pinned at HEAD 1f9f4835+:
- F-A CLOSED: ChatHandler.ClearConversation routes through AgentLoop.ClearConversation (exposure-plan prune lives on the real /reset path). Pin: TestChatHandlerClearConversationPrunesExposurePlan (mutation-proven RED on the old direct-Delete routing). Loop method also made nil-safe per the setter convention.
- F-B CLOSED: ExposurePlan carries liveSource (the agent's FILTERED registry). IsKnown + Promote fall back to it when the frozen snapshot misses a post-arm registration — gate no longer fail-OPEN for MCP-mid-episode tools; ceiling no longer mis-refuses legitimate new tools; a name absent from BOTH keeps the row-5 refusal. Pin: TestExposurePlanLiveRegistryFallback (mutation-proven).
- F-C CLOSED: agent.ExposureGateRefusal exported; the codemode sandbox bridge applies the same row-4 verdict before reg.Get, and SkillStateRuntime.Run arms the conversation's plan onto its ctx (ExecuteSkillToolCalls fans it to the executor gate). Pins: TestCodemodeBridgeHonorsExposureGate (mutation-proven), gate shared in executor.go.
- F-D CLOSED: buildMemoryContext folds through recallScopeOptions (memoryScopeOptions), same as the dispatcher lane and the tool lane — three-site rule intact. Pin: TestRecallScopeOptionsDeclaredGlobalFoldsToUser (mutation-proven: verbatim global filter hides the project row).
- F-E CLOSED: skillFileOrigin strips CR (CRLF files) and one layer of YAML quotes (vocabulary match with the loader). Pins: TestSkillFileOriginQuotedAndCRLF incl. end-to-end refusal of a quoted user stamp.
- F-F CLOSED: refused candidates land in PER-FUNNEL sidecars (SKILL.md.new-agent/.new-evolver/.new-review; unstamped keeps .new) — earlier candidates no longer clobbered. Existing pins updated to the new naming.
- F-G CLOSED: processProposal skips user-locked (origin: user) skills BEFORE the verifier LLM call; recorded as a Skipped with the "user-locked" rationale. Pin: TestEvolverSkipsUserLockedSkill (mutation-proven).
- F-H CLOSED: Manager.ListBridgesScoped applies bridgeVisible to the LISTING lane; MemoryService.Bridges takes scope options; the memory.bridges RPC passes user purview + cfg.MultiUser.Enabled (single-user byte-identical; multiuser fails closed on user rows). Pin: TestListBridgesScopedGatesListing (cross-project row asserted hidden).
AUDIT CLOSED - 8/8 fixed.
