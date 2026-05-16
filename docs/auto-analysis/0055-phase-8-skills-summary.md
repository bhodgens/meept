# Phase 8 Skills System - Test Summary

- **Phase**: 8
- **Date**: 2026-05-16
- **CLI binary**: `/Users/caimlas/go/bin/meept`
- **Tests run**: 6

## Test Results

### Test 1: `meept clawskills list` - FAILED (not implemented)
- Actual output: `Error: accepts at most 1 arg(s), received 2`
- Expected: List of available ClawSkills
- Root cause: No `clawskills` subcommand exists. The entire clawskills marketplace feature is missing from Go codebase.

### Test 2: `meept clawskills search "test"` - FAILED (not implemented)
- Actual output: `Error: accepts at most 1 arg(s), received 3`
- Expected: Search results for "test"
- Same root cause as test 1.

### Test 3: Skill discovery tiers - PASSED
- `~/.meept/skills/` exists (empty) -- user tier
- `~/.config/meept/skills/` does not exist -- system tier (not an error)
- `.meept/skills/` does not exist -- project tier (not an error)
- Default tiers correctly defined in `internal/skills/discovery.go:22-33`

### Test 4: `meept chat "list all available skills"` - PARTIAL
- Chat works (LLM responds)
- LLM did not perform skill listing/matching (likely because no skills were discovered at config time)
- Skills require `skills.enabled = true` in config (was `false`)

### Test 5: `meept chat "use a skill to help me"` - PARTIAL
- Chat works, LLM responded with general help request
- Did not trigger skill execution (no skills available in registry)

### Test 6: Skill shadowing - PASSED
- Created user-tier skill (`~/.meept/skills/test-skill/SKILL.md`)
- Created project-tier shadowing skill (`.meept/skills/test-skill/SKILL.md`)
- Project-tier correctly overwrote user-tier after daemon restart
- `meept skills list` showed the project-tier description

## Additional tests performed

- `meept skills list` (RPC) - Works when skills enabled, returns "No skills found." when empty.
- `meept skills show <name>` - Returns skill details via RPC.
- `meept skills run <name>` - Executes skill via RPC.
- `go test ./internal/skills/...` - All 15 tests pass.
- Shadow test: verified project tier Priority 300 > user tier Priority 200.

## Bugs / Issues Found

### BUG 1: Skills disabled by default in user config (Critical)
- **File**: `~/.meept/meept.json5` line 340 has `"enabled": false`
- **Impact**: `skills.list` RPC handler never registered → proxy times out after 10s
- **Detail**: Proxy at `proxy.go:77` registers `skills.list` handler that forwards to bus. Direct handler at `daemon.go:165` is guarded by `Skills.Enabled`, so when disabled the proxy has no backend to forward to.
- **Fix**: Guard proxy skills registrations with `Skills.Enabled` check, or provide error fallback.
- **Related docs**: `/Users/caimlas/git/meept/docs/auto-analysis/0055-skills-rpc-disabled-by-config.md`

### BUG 2: ClawSkills system completely unimplemented (High)
- **Impact**: `clawskills search/install/list/update` documented in CLI help and docs but have no Go implementation
- **Evidence**: Zero Go source files reference clawskills. No `ClawSkillsConfig` in schema. No `clawskills` CLI subcommand. No RPC handlers.
- **Config**: `[clawskills]` section in TOML templates is silently ignored (no schema struct maps it)
- **Fix needed**: Full implementation of `internal/clawskills/` package, CLI subcommands, and RPC handlers
- **Related docs**: `/Users/caimlas/git/meept/docs/auto-analysis/0055-clawskills-missing-implementation.md`

### BUG 3: Confusing error for unknown subcommands (Low-Medium)
- Running `meept clawskills list` gives `Error: accepts at most 1 arg(s), received 2` instead of a helpful message indicating no such subcommand
- **Root cause**: Root command has `MaximumNArgs(1)` and `clawskills` is not a registered subcommand, so cobra tries root with 2 args
- **Fix**: Add a `clawskills` placeholder subcommand that says "not yet implemented"

## Skills System Architecture (verified working)

- `internal/skills/` - 14 Go source files, all unit tests pass
- Discovery: 3-tier filesystem scan (project > user > system)
- Shadowing: Higher-priority tier overwrites lower-priority by normalized name
- Registry: `NewRegistry()` → `RegisterAll(discovered)`
- Executor: `NewExecutor(LLMResolver, opts...)` needs LLM resolver
- RPC: `RegisterSkillsHandlers(server, registry, executor)` at `daemon.go:166`
- Bus: `SkillsHandler.Start()` subscribes to `skills.list`, `skills.get`, `skills.execute`
- Lazy loading: `SkillIndex` (metadata only) → `LazySkillLoader` (on-demand body)
- Capability matching: `CapabilityIndex` + `KeywordExtractor` for skill suggestion

## Post-test cleanup

- Removed test skill directories (`.meept/skills/` and `~/.meept/skills/test-skill/`)
- Re-enabled skills in config (`true`) for future testing.
