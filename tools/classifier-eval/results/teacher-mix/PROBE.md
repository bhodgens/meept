# Teacher-Mix Model Probe (leaf 01)

Date: 2026-09-21. Environment: opencode CLI 1.1.51 at
`/Applications/OpenCode.app/Contents/MacOS/opencode-cli`.

## Findings

1. The brew wrapper `opencode` (node script) is BROKEN on this machine
   (dyld: libllhttp.9.3.dylib). Never used.
2. The CLI crashes at plugin load with the USER config
   (`~/.config/opencode/opencode.jsonc`): plugin `opencode-brain`
   throws `fn3 is not a function` -> fatal at `src/plugin/index.ts:87`.
3. FIX (used for all CLI calls): minimal temp config +
   `XDG_CONFIG_HOME=/tmp/oc-cfg` (contains only `opencode.jsonc` with the
   `$schema` key). Auth still resolves from `~/.local/share/opencode/auth.json`.
4. `zai-coding-plan/glm-5.3-flash` via CLI under the temp config returned
   `Error: Authentication Failed` (plan-key auth likely requires a plugin
   from the user config). FALLBACK route verified instead: direct HTTP
   POST `https://api.z.ai/api/coding/paas/v4/chat/completions` with an
   env-provided Z.AI credential (never stored or printed), model
   `glm-5.3-flash` -> HTTP 200 in 8.3s, content "OK".
5. `opencode-go/deepseek-v4-flash` via CLI: OK.
6. `opencode-go/deepseek-v4.1-flash` via CLI: OK.

## Route table

| route | model id | transport | verdict |
|---|---|---|---|
| worker-a | opencode-go/deepseek-v4-flash | cli (XDG_CONFIG_HOME=/tmp/oc-cfg) | OK |
| worker-b | glm-5.3-flash | http-fallback api.z.ai/api/coding/paas/v4 (env key) | FALLBACK |
| judge | opencode-go/deepseek-v4.1-flash | cli (XDG_CONFIG_HOME=/tmp/oc-cfg) | OK |

## Smoke classification (all three routes, sample message)

| route | latency | response |
|---|---|---|
| worker-a | 6.5s | {"intent": "debug", "confidence": 0.95, "reason": "Diagnose why search fails for hermes, then fix it"} |
| worker-b | 8.3s | HTTP 200 (smoke was a plain "say OK"; classification smoke lands in leaf 02's first case) |
| judge | 3.5s | {"intent": "debug", "confidence": 0.9, "reason": "Diagnose and fix broken search for hermes"} |

Expected lane `debug` returned by both CLI workers. Latencies 2-9s/call;
48-case two-worker sweep estimated 6-12 min plus judge calls.
