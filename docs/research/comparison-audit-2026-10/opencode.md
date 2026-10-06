# 429 / Retry-After RFC Audit — opencode

**Ref:** `652c090dc119b5f3dc1e5e0bf1c4b40d9721f0ef` (`652c090`, `chore: update nix node_modules hashes`, shallow clone, 6627 tracked files)
**Repo path:** `/var/folders/mf/1mvt9vbx7q3f9ynln1p79cfr0000gn/T/meept-compare/opencode`
**Read-only:** no repo file was modified. Scratch scripts live in `/tmp/429audit/`.

---

## 0. TL;DR

opencode does **not** implement 429/Retry-After in one place. There are **two independent live
retry paths**, both of which parse `Retry-After`, and a third file that only *produces* it.

1. **Default path (shipped):** Vercel AI SDK `ai@6.0.168` — the repo sets `maxRetries: input.retries ?? 0`
   for the main session turn, i.e. **the SDK's own 429 retry is disabled by default**. The
   repo's own `SessionRetry` schedule then does the retrying, reading headers off
   `APIError.data.responseHeaders`.
2. **Opt-in path (default off):** `@opencode-ai/llm` `RequestExecutor` (gated by
   `OPENCODE_EXPERIMENTAL_NATIVE_LLM`) — its own `retryAfterMs()` parser plus a `Retry-After`-derived
   sleep, capped at `MAX_DELAY_MS = 10_000`.
3. **Producer only:** `packages/console/.../zen/util/handler.ts:513` *sets* `retry-after` on opencode's
   own gateway responses (and `packages/console/.../ipRateLimiter.ts:24` is where the value comes from).

Both parsers use **`parseFloat`/`Number`, not a whole-value strict integer parse**, so an
RFC3339 date in `Retry-After` is misread. Both are *honored* (the sleep derives from the parsed
value), which is the important part; neither is a "parse and then overwrite with backoff" bug.

No `retry.fallbackChains` / model-rotation-on-429 exists in this repo (see Axis 9 absence claim).

---

## 1. Consumer or producer?

**Both, in different files.** This matters — a naive grep count mixes them.

**Consumers (read a server-sent value):**

- `packages/opencode/src/session/retry.ts:51` and `:59` — the *live* session retry schedule reads
  `headers["retry-after-ms"]` then `headers["retry-after"]`.
- `packages/llm/src/route/executor.ts:94` and `:97` — the native/Effect executor reads the same two
  headers off a normalized header map.
- `script/github/close-prs.ts:307` — a repo-maintenance script reading GitHub's `retry-after` (and
  `x-ratelimit-reset`, `x-ratelimit-remaining`). Not product code.

**Producer (set only):**

- `packages/console/app/src/routes/zen/util/handler.ts:511-514`:
  ```ts
  const headers = new Headers()
  if (error.retryAfter) {
    headers.set("retry-after", String(error.retryAfter))
  }
  ```
  `error.retryAfter` originates in `packages/console/app/src/routes/zen/util/ipRateLimiter.ts:24`
  (`getRetryAfterDay(now)`) — an opencode gateway day-bucket, i.e. a legitimate delta-seconds producer.

**False positives in the original grep:** `packages/sdk/js/src/gen/core/serverSentEvents.gen.ts`
matches `retry-after` only as Hey-API `sseMaxRetryAttempts` / `sseDefaultRetryDelay` codegen
scaffolding (`serverSentEvents.gen.ts:33` `* Maximum number of retry attempts before giving up.`) —
no HTTP header semantics. `packages/app/src/context/server-session.test.ts` matched the phrase
"retry after an earlier delta" in a test name, not a header.

---

## 2. Forms parsed

Per-parser table. `Date.parse` is the only date mechanism; there is **no** explicit RFC 9110 §5.6.7
three-format handling anywhere in the repo.

| Form | `retry.ts` (live, V1 sessions) | `llm/executor.ts` (opt-in) | `ai@6.0.168` (SDK, retries>0 only) |
|---|---|---|---|
| delay-seconds | **yes, LAX** — `Number.parseFloat` (prefix match) | **yes, lax-lenient** — `Number(value)` | **yes, LAX** — `parseFloat` (prefix match) |
| IMF-fixdate | yes (incidental, via `Date.parse`) | yes (incidental) | yes (incidental) |
| RFC850 (`Friday, 31-Dec-27 …`) | **no** — `Date.parse` rejects it | **no** | **no** |
| asctime (`Fri Dec 31 23:59:59 2027`) | **not accepted correctly** — parses, but wrong instant/timezone | no | no |
| RFC3339 | **yes but as a bug source** (see below) | **yes but as a bug source** | **yes but as a bug source** |
| `retry-after-ms` | **yes, READ** (highest priority) | **yes, READ** (highest priority) | **yes, READ** (highest priority) |

### The RFC3339 prefix-parse bug (measured, not inferred)

`packages/opencode/src/session/retry.ts:59-65`:
```ts
const retryAfter = headers["retry-after"]
if (retryAfter) {
  const parsedSeconds = Number.parseFloat(retryAfter)
  if (!Number.isNaN(parsedSeconds)) {
    // convert seconds to milliseconds
    return cap(Math.ceil(parsedSeconds * 1000))
  }
```
`parseFloat("2027-12-31T23:59:59Z") === 2027` (the year prefix). I ported the exact function to a
file and ran it under bun (`/tmp/429audit/parse-check.mjs`), reproducing:

```
{"retry-after":"2027-12-31T23:59:59Z"}         2027000     <- 33.8 minutes, not 2027-12-31
{"retry-after":"2020-01-01T00:00:00Z"}         2020000     <- 33.7 minutes from a PAST date
{"retry-after":"30; foo"}                      30000       <- prefix accepted, RFC forbids
```

A past RFC3339 date therefore becomes a **+33.7 minute sleep**, not a clamp-to-zero, and a future
one becomes a wildly wrong short sleep. Note the `Date.parse` branch below it is **unreachable for
any RFC3339 value**, so the "HTTP date" comment at `retry.ts:66` misleads: only IMF-fixdate-ish
values actually get there.

The same prefix bug exists in the delegated SDK at `ai@6.0.168` (`getRetryDelayInMs`,
`dist/index.mjs` ~offset 70503): `const timeoutSeconds = parseFloat(retryAfter); if (!Number.isNaN(timeoutSeconds)) ms = timeoutSeconds * 1e3;`

The one parser that is **not** prefix-lax is the native executor, `packages/llm/src/route/executor.ts:100-101`:
```ts
const seconds = Number(value)
if (Number.isFinite(seconds)) return Math.max(0, seconds * 1000)
```
`Number("30; foo")` is `NaN`, so that value falls through to `Date.parse` → `undefined` → computed
backoff. That is *stricter* than the other two. (Trade-off: it also mis-handles `1e3` and has no
`Number.isInteger` check, so `Retry-After: 3.7` becomes 3700 ms.)

`asctime` deserves an explicit note: `Date.parse("Fri Dec 31 23:59:59 2027")` returns
`2028-01-01T06:59:59Z` — 7 hours off, because V8 parses asctime as **local time** and the value is
GMT. Wrong instant, silently.

---

## 3. Honored or ignored?

**`honored` on the live path, `partial` on the opt-in path.**

Live path: the parsed value *is* the sleep.
`packages/opencode/src/session/retry.ts:195` → `:201`:
```ts
const wait = delay(meta.attempt, SessionV1.APIError.isInstance(error) ? error : undefined)
const now = yield* Clock.currentTimeMillis
yield* opts.set({ ..., next: now + wait })
return [meta.attempt, Duration.millis(wait)] as [number, Duration.Duration]
```
The `Duration` returned to `Effect.retry` is the actual inter-attempt wait. Verified by the repo's
own tests: `packages/opencode/test/session/retry.test.ts:52` `expect(SessionRetry.delay(4, error)).toBe(1500)`
for `retry-after-ms: 1500`, and `:86` `:89` for 50 s / 700000 ms — values *larger* than the
exponential curve, i.e. the header wins outright. The only clamp is the 32-bit timer ceiling
(`retry.ts:30` `RETRY_MAX_DELAY = 2_147_483_647`) applied by `cap()` at `retry.ts:43-45`.

Opt-in path: **honored but capped**, which the protocol defines as `partial`.
`packages/llm/src/route/executor.ts:346`:
```ts
if (error.retryAfterMs !== undefined) return Effect.succeed(Math.min(error.retryAfterMs, MAX_DELAY_MS))
```
`MAX_DELAY_MS = 10_000` (`executor.ts:38`). A server saying `Retry-After: 3600` is slept for **10 s**,
3 more times, then the 429 surfaces. Measured (`/tmp/429audit/exec-check.mjs`): `retry-after: 30` →
parsed 30000 → slept 10000.

---

## 4. Past / zero / negative / garbage

| Input | `retry.ts` result | `executor.ts` result | `ai@6.0.168` result |
|---|---|---|---|
| `retry-after-ms: 0` | **0 ms (hot loop)** | 0 ms | 0 ms |
| `retry-after-ms: -5` | **-5 ms (negative sleep!)** | 0 (clamped) | ignored → exponential |
| `retry-after: 0` | **0 ms** | 0 ms | 0 ms |
| `retry-after: -5` | **-5000 ms** | 0 ms (clamped) | ignored → exponential |
| past IMF-fixdate | **falls through to exponential** (2000 at attempt 1) | 0 ms | ignored → exponential |
| past RFC3339 | **+2020000 ms** (prefix bug) | 0 ms | ignored → exponential |
| `not-a-number` | 2000 (exponential) | undefined → jitter | exponential |
| `retry-after-ms: 999999999999` | `RETRY_MAX_DELAY` (2147483647) | `Math.min(…, 10000)` | ignored (>60 s rule) |

Evidence, `packages/opencode/src/session/retry.ts:52-56` (zero is truthy, so the `if` passes and
`parseFloat` yields `0`, which passes `!Number.isNaN`):
```ts
const retryAfterMs = headers["retry-after-ms"]
if (retryAfterMs) {
  const parsedMs = Number.parseFloat(retryAfterMs)
  if (!Number.isNaN(parsedMs)) {
    return cap(parsedMs)
```
`cap` is `Math.min` only — **no lower bound**. So `"0"` → 0 ms and `"-5"` → -5 ms. The repo's own
tests use `"retry-after-ms": "0"` as a fixture (`retry.test.ts:100`, `:130`,
`packages/llm/test/executor.test.ts:138`, `:199`, `:236`, `:261`, `:286`) — i.e. the zero-delay case is
*exercised as normal*, and `Effect.sleep(0)` / `Duration.millis(0)` yields immediately: a genuine
hot-retry vector against a window the server asked to be respected. Past *IMF-fixdate* is handled
correctly (`retry.ts:68` `if (!Number.isNaN(parsed) && parsed > 0)`), and
`packages/opencode/test/session/retry.test.ts:78-82` pins that behavior — so the omission is specific
to the numeric branches, not the date branch.

This is the single worst correctness gap: **the vendor `retry-after-ms` value is trusted with no
floor and no negative rejection**, while the more careful delegated SDK guards it with
`0 <= ms && (ms < 60_000 || ms < exponentialBackoffDelay)` (`ai@6.0.168`, `getRetryDelayInMs`).

---

## 5. Provider reset headers

**Read for diagnostics only, never used as a delay.**

`packages/llm/src/route/executor.ts:127-131`:
```ts
const anthropic = /^anthropic-ratelimit-(.+)-(limit|remaining|reset)$/.exec(name)
if (!anthropic) return
if (anthropic[2] === "limit") return addRateLimitValue(limit, anthropic[1], value)
if (anthropic[2] === "remaining") return addRateLimitValue(remaining, anthropic[1], value)
return addRateLimitValue(reset, anthropic[1], value)
```
Same for OpenAI-style at `executor.ts:118-125` (`x-ratelimit-limit-*`, `x-ratelimit-remaining-*`,
`x-ratelimit-reset-*`). The populated `reset` map (which carries the RFC3339
`anthropic-ratelimit-requests-reset` values, per `packages/llm/test/executor.test.ts:239`) flows into
`HttpRateLimitDetails` (`packages/llm/src/schema/errors.ts:18-23`) and onto the error — and then
**nothing reads it**. I enumerated every `rateLimit` reference in `packages/{llm,core,opencode}/src`
(`executor.ts`, `schema/errors.ts:31,78`) — there is no consumer outside the executor that computes a
sleep. So: `anthropic-ratelimit-*-reset` is **captured, not honored**. That is the main
compatibility loss vs. the meept baseline, which treats them as a real retry source
(`retry_after.go` step 4).

Not present anywhere in `src`: `x-should-retry`, `x-ratelimit-reset` (bare),
`anthropic-ratelimit-concurrent-reset` handling as a delay. `x-ratelimit-reset` *is* read in
`script/github/close-prs.ts:308-312`, again only in the maintenance script.

**`retry-after-ms`: READ, not merely set** — three independent readers, quoted above
(`retry.ts:51`, `executor.ts:94`, `ai@6.0.168 getRetryDelayInMs`). The original 5-file grep hit was
2 source + 3 test-ish files; the tests corroborate rather than duplicate.

---

## 6. 429 detection

**Mixed: typed error class *and* HTTP status code *and* body regex — layered.**

- **Status code, typed:** `packages/llm/src/route/executor.ts:242-252` builds a
  `RateLimitReason` (or `QuotaExceededReason` when the body matches `/insufficient[-_\s]?quota|quota[-_\s]?exceeded/i`).
  `retryable` is a class getter, `packages/llm/src/schema/errors.ts:82-84`: `get retryable() { return true }`.
  Contrast `QuotaExceededReason` at `errors.ts:93-95`: `return false` — **quota is correctly separated
  from throttle**, which is the exact distinction the meept baseline makes.
- **Status code from the SDK:** `APICallError` marks `429` retryable by default —
  `@ai-sdk/provider@3.0.8`, `APICallError` constructor: `isRetryable = statusCode != null && (statusCode === 408 || statusCode === 409 || statusCode === 429 || statusCode >= 500)`.
- **Body string/regex as a *fallback*:** `packages/opencode/src/session/retry.ts:33-41`,
  ```ts
  const RETRYABLE_MESSAGE_PATTERNS = [
    /429|500|502|503|504|524/i,
    /rate increased too quickly|rate limit|rate-limit|rate_limit|too many requests/i,
    ...
  ```
  and `retryable()` at `retry.ts:92-98` will retry when `status >= 500` **or** the message/body
  matches — i.e. a 429 whose SDK error lacks `isRetryable` still retries via
  `/429|.../ ` on the message. `retry.ts:148-153` also lowercases the message for
  `too_many_requests` / `exhausted` / `unavailable` on non-APIError shapes.
  Net: not body-only, but body matching is load-bearing for providers whose SDK does not set the flag.
- **Config-file retries** are classified explicitly: `packages/opencode/src/provider/error.ts:117-137`
  maps `insufficient_quota`, `usage_not_included`, `invalid_prompt` to `isRetryable: false` and
  `server_is_overloaded`/`server_error` to `true`.

---

## 7. Default attempt count — three nested defaults

This is the most consequential thing in the audit, because the *inner* default is zero.

1. **AI SDK layer: effectively 0 for the main session turn.**
   `packages/opencode/src/session/llm.ts:323`:
   ```ts
   maxRetries: input.retries ?? 0,
   ```
   and `StreamInput.retries` is optional (`llm.ts:46` `retries?: number`) and **not supplied** by the
   main turn — `packages/opencode/src/session/prompt.ts:1272` calls `handle.process({ user, agent, permission, sessionID, ..., model, toolChoice })`
   with no `retries` key. So the SDK's own 429 retry (default 2 in
   `ai@6.0.168` `prepareRetries`: `const maxRetriesResult = maxRetries != null ? maxRetries : 2;`)
   is **turned off on the hot path**; all retrying is delegated to `SessionRetry` below.
   Explicit `retries: 2` exists for the title-generation subcall (`prompt.ts:234`) and `project-copy.ts:50`;
   `prompt.ts:1312` sets `retries: 0` for the structured-output rethrow.
2. **Repo retry schedule: 5 retries** — `packages/opencode/src/session/retry.ts:31`
   `export const RETRY_MAX_RETRIES = 5`, enforced `retry.ts:193` `if (meta.attempt > RETRY_MAX_RETRIES) return Cause.done(meta.attempt)`.
   Pinned by `packages/opencode/test/session/retry.test.ts:127` `test("policy stops after five retries")`.
3. **Native executor: 2** — `packages/llm/src/route/executor.ts:36` `const MAX_RETRIES = 2`,
   defaulted `executor.ts:355` `retries = MAX_RETRIES`. **Default-off feature** (gated by
   `OPENCODE_EXPERIMENTAL_NATIVE_LLM`, `packages/opencode/src/effect/runtime-flags.ts:54`
   `experimentalNativeLlm: bool("OPENCODE_EXPERIMENTAL_NATIVE_LLM")` — `bool()` is
   `Config.withDefault(false)` at `runtime-flags.ts:4`; test `packages/opencode/test/effect/runtime-flags.test.ts:62`
   `expect(flags.experimentalNativeLlm).toBe(false)`). Not shipped behavior.

---

## 8. Short-retry burn

**Yes, reachable — two mechanisms, one systemic.**

1. **Zero/negative delay ⇒ hot loop.** `retry-after-ms: 0` → `Duration.millis(0)` and
   `retry-after-ms: -5` → `-5` (`retry.ts:52-56`, verified by execution). Five such attempts happen
   back-to-back against a window the server explicitly asked to be respected. The tests normalize this
   by using `"retry-after-ms": "0"` as their fast fixture (`retry.test.ts:100`, `:130`) — so the test
   suite itself demonstrates the hot path rather than catching it.
2. **Ceiling-bound retries still burn.** Even on the honest path, a 429 with no usable header falls to
   exponential with `RETRY_MAX_DELAY_NO_HEADERS = 30_000` (`retry.ts:29`, `:77`) — 5 retries in ~2
   minutes against a window that may be an hour. The repo has **no** "quota vs throttle" duration split
   at this layer; compare meept's `QuotaResetError` hours-long, never-short-retried rule. Note the
   30 s cap only applies when `headers` is absent entirely (`retry.ts:73-77`); if `responseHeaders`
   exists but contains no usable hint, `retry.ts:73` returns `cap(exponential(attempt, random))` with
   **no** 30 s ceiling — so a 429 carrying only `content-type` gets 2/4/8/16/32 s.

**No model rotation / no fallback chain (this answers the matrix's specific worry).** I searched
`fallbackChain`, `fallback_chain`, `retry.fallback`, `fallbacks` across the whole tree
(`grep -rni 'fallbackchain|fallback_chain|retry.fallback'` → zero hits outside
`patches/@ai-sdk%2Fanthropic@3.0.111.patch:129` etc., which is the Anthropic SDK's *server-side*
`fallbacks` beta flag; and `grep -rn 'fallbacks' packages/{opencode,core,llm}/src` → 2 unrelated hits:
`provider.ts:547` a Google ADC env-var comment and `tool/webfetch.ts:52` an HTTP `q=` param). No
rotation on 429 exists in `packages/opencode/src/session` or `packages/core/src/session/runner`
(`grep -rn -i 'nextmodel|rotate|switchmodel|another model'` → 0 hits). So a 429 cannot silently burn
a second provider's quota: it waits and re-fires the *same* model. The real risk is the inverse of
the one described — repeated re-fires against the *same* exhausted window.

`@ai-sdk/anthropic@3.0.111`'s server-side fallback beta (`server-side-fallback-2026-06-01`) is
**never activated**: it requires `anthropicOptions.fallbacks.length > 0`, and no opencode call site
passes `fallbacks` (`provider.ts:149-172` builds provider factories with `options` only).

---

## 9. Delegation chain (the decisive axis)

**It delegates. The expected SDK is present in `dependencies`, pinned in `bun.lock`, and its own
source was read at the pinned version.**

Manifest evidence, `package.json:67` (workspace catalog) and `packages/opencode/package.json:58-76`
(flat, exact):
```json
"@ai-sdk/anthropic": "3.0.111",  "@ai-sdk/openai": "3.0.88",
"@ai-sdk/google": "3.0.73",      "@ai-sdk/openai-compatible": "2.0.41",
"ai": "6.0.168"                  <- catalog, package.json:67
```
Lockfile, `bun.lock:3045`: `"ai": ["ai@6.0.168", "", { "dependencies": { "@ai-sdk/provider-utils": "4.0.23", ... } }]`.
Import sites: `packages/opencode/src/provider/provider.ts:151-157` (`createAnthropic`, `createOpenAI`, …),
`packages/opencode/src/session/llm.ts:280` (`streamText`), `provider/error.ts:1` (`APICallError` from `"ai"`).

**Effective runtime = `ai@6.0.168` `getRetryDelayInMs`.** Fetched
`https://cdn.jsdelivr.net/npm/ai@6.0.168/dist/index.mjs` (78,591-byte `provider-utils@4.0.23` also
fetched; `@ai-sdk/provider@3.0.8` for `APICallError`). Verbatim:
```js
function getRetryDelayInMs({ error, exponentialBackoffDelay }) {
  const headers = error.responseHeaders;
  if (!headers) return exponentialBackoffDelay;
  let ms;
  const retryAfterMs = headers["retry-after-ms"];
  if (retryAfterMs) { const timeoutMs = parseFloat(retryAfterMs);
    if (!Number.isNaN(timeoutMs)) { ms = timeoutMs; } }
  const retryAfter = headers["retry-after"];
  if (retryAfter && ms === void 0) {
    const timeoutSeconds = parseFloat(retryAfter);
    if (!Number.isNaN(timeoutSeconds)) { ms = timeoutSeconds * 1e3; }
    else { ms = Date.parse(retryAfter) - Date.now(); } }
  if (ms != null && !Number.isNaN(ms) && 0 <= ms && (ms < 60 * 1e3 || ms < exponentialBackoffDelay)) {
    return ms; }
  return exponentialBackoffDelay;
}
```
Read-out:
- **Retry predicate is typed + status-driven:** `ai.mjs` — `if (error instanceof Error && APICallError2.isInstance(error) && error.isRetryable === true && tryNumber <= maxRetries)`.
- **maxRetries default 2**, `initialDelayInMs 2000`, `backoffFactor 2`
  (`retryWithExponentialBackoffRespectingRetryHeaders`; `prepareRetries`: `maxRetries != null ? maxRetries : 2`).
- **A header value ≥ 60 s is DISCARDED and replaced by exponential backoff** — the `ms < 60*1e3 ||
  ms < exponentialBackoffDelay` gate. So an hourly OpenAI window is retried at 2/4/8 s. **This is
  the delegated layer's worst behavior**, and it is *better* guarded on negatives (`0 <= ms`) than
  opencode's own `retry.ts`.
- Because `session/llm.ts:323` passes `maxRetries: 0`, `prepareRetries` builds a retry wrapper whose
  `maxRetries === 0` short-circuits (`if (maxRetries === 0) { throw error; }`) and **`getRetryDelayInMs`
  is never called at all** on the default path. The SDK is present and correct-ish, but inert for the
  main turn; `SessionRetry` is the live sleeper. Measured table for the SDK logic is in
  `/tmp/429audit/exec-check.mjs` output.

**Both paths exist simultaneously** (the protocol's "both paths" pitfall): the AI SDK runs the
default V1 turn; the Effect `RequestExecutor` runs the opt-in native runtime
(`packages/opencode/src/session/llm/native-runtime.ts:108-113` calls `input.llmClient.stream(...)`).
I read `executor.ts` in full (385 lines) — it is the only place a `Retry-After`-derived value is
turned into an actual `Effect.sleep` in this repo
(`executor.ts:346` + `:361` `Effect.flatMap((delay) => Effect.sleep(delay))`), and it is
default-off.

**Local patches override vendor behavior but do not touch retry/429:** `package.json:148-168`
`patchedDependencies` patches all eight `@ai-sdk/*` packages. I grepped every patch: the only `429`
/retry-adjacent hits are `patches/@ai-sdk%2Fanthropic@3.0.111.patch:495` (`@@ -4400,6 +4429,7 @@` in
`dist/index.js`, context `input_transformations`) — a content block, not retry logic. The patch adds
`fallbacks` support at `patches/@ai-sdk%2Fanthropic@3.0.111.patch:129`
(`} else if ((anthropicOptions == null ? void 0 : anthropicOptions.fallbacks) && anthropicOptions.fallbacks.length > 0) {`),
which is inert because no call site passes `fallbacks` (see §8). Patch set enumerated in full: 21 files
in `patches/`.

---

## 10. Absence claims (method)

- **`retry.fallbackChains` / pinned fallback chains / classifier-triggered model rotation:** zero hits for
  `fallbackChain|fallback_chain|retry.fallback` across the full worktree (excluding `.git`); `fallbacks`
  in `packages/{opencode,core,llm}/src` → 2 non-429 hits, quoted above. `nextmodel|rotate|switchmodel`
  → 0 hits in `packages/opencode/src/session`. **Gap:** I did not read the `packages/web/src/content/docs/*`
  localized copies beyond `docs/*.mdx` grep; a user-facing *doc* description of a chain without code
  would have been missed. Code-side conclusion is unaffected.
- **RFC850 / asctime / `x-should-retry`:** enumerated every `retry-after`/`retryAfter` site (12 files) and
  every `x-ratelimit*` hit; none reference RFC850 or `x-should-retry`. **Gap:** `packages/app` and
  `packages/desktop` were covered only by the repo-wide grep, not by full file reads.
- **`packages/console` is the only producer.** Read in full: `zen/util/error.ts`, `zen/util/handler.ts`
  (retry-after region), `zen/util/ipRateLimiter.ts`. **Gap:** other console routes
  (`packages/console/app/src/routes/zen/index.tsx` etc.) were grep-only.
- **AC surface:** `packages/opencode/src/cli/cmd/acp.ts:10` `command: "acp"` exists and adds no retry logic;
  `@agentclientprotocol/sdk@0.21.0` is a protocol transport, not an HTTP-retry layer.
  **Tool count ~20 is right** (`packages/core/src/tool/*.ts`: 19 non-test files incl. `registry.ts`).
- **V2 durable runner has no retry:** `packages/core/src/session/runner/llm.ts:55` still carries
  `- [ ] Bound provider retries and repeated identical tool calls.` and `:86`
  `Durable continuation recovery remains a separate future slice with an explicit retry policy.`
  Confirmed: no `retry`/`429` in that file outside those comments. So the V2 path (which is where
  `HttpRateLimitDetails` would live) retries nothing at all.

---

## 11. Comparison to the meept baseline (`internal/llm/retry_after.go`)

| Capability | meept | opencode |
|---|---|---|
| delay-seconds strict whole-value parse | `strconv.Atoi` (strict) | `parseFloat` — **prefix-lax** (`retry.ts:61`) |
| IMF-fixdate | `time.RFC1123` explicit | `Date.parse` incidental |
| RFC850 | explicit layout | **no** |
| asctime | explicit layout | **wrong instant** (local-time parse) |
| RFC3339 compat | explicit, after the RFC forms | prefix-bugged into a ~33 min sleep |
| `retry-after-ms` | n/a (baseline lists it as a vendor ext) | read, but **no floor / negatives accepted** |
| Anthropic reset headers | used as a real retry source (#4) | **diagnostics only** |
| quota vs throttle split | `QuotaResetError`, never short-retried | `QuotaExceededReason.retryable === false` (`errors.ts:93`) — partial win, no duration split |
| standard header beats provider header | explicit precedence | `retry-after-ms` beats `retry-after` (`retry.ts:51` before `:59`) — vendor header wins, inverted vs baseline rule 3 |
| negative/zero | clamped | **not clamped** on the live path |

---

## 12. Files read (not search snippets)

`package.json`; `bun.lock` (filtered); `packages/opencode/package.json`; `packages/llm/package.json`;
`packages/opencode/src/session/retry.ts` (all 209 lines); `packages/opencode/src/session/processor.ts:620-712`;
`packages/opencode/src/session/llm.ts:30-70,196-360`; `packages/opencode/src/session/llm/native-runtime.ts:1-140`;
`packages/opencode/src/session/message-v2.ts:660-720`; `packages/opencode/src/provider/error.ts:1-196`;
`packages/opencode/src/effect/runtime-flags.ts:1-70`; `packages/opencode/src/util/effect-http-client.ts`;
`packages/opencode/src/plugin/openai/ws.ts:225-245`; `packages/opencode/src/cli/cmd/acp.ts:1-30`;
`packages/llm/src/route/executor.ts` (all 385); `packages/llm/src/schema/errors.ts` (all 207);
`packages/llm/test/executor.test.ts:128-300`; `packages/opencode/test/session/retry.test.ts:1-145`;
`packages/core/src/session/runner/llm.ts:1-120,232-262`; `packages/core/src/v1/session.ts:35-60`;
`packages/core/src/session/compaction.ts:195-215`; `packages/console/app/src/routes/zen/util/{error.ts,handler.ts,ipRateLimiter.ts}`;
`script/github/close-prs.ts:280-350`; all 8 `@ai-sdk/*.patch` files (grep-verified no retry hunks);
vendor: `ai@6.0.168/dist/index.mjs`, `@ai-sdk/provider-utils@4.0.23/dist/index.mjs`,
`@ai-sdk/provider@3.0.8/dist/index.mjs`, `@ai-sdk/openai@3.0.88/dist/index.mjs`,
`@ai-sdk/anthropic@3.0.111/dist/index.mjs`.

Reproduced locally: `/tmp/429audit/parse-check.mjs` (JS-parse semantics under bun),
`/tmp/429audit/exec-check.mjs` (both in-repo parsers + the SDK's, ported verbatim).
