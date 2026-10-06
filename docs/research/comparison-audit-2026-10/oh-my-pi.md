# 429 / Retry-After RFC Audit — oh-my-pi

**Ref:** `fc6c0c90dab012a0b7edea731af92f5bbd09cff0` (`git rev-parse HEAD`)
**Audit date:** 2026-10-05
**Baseline:** meept `internal/llm/retry_after.go`

---

## Scope and method

`git grep -l -i retry.after` → 88 files. Bucketed by directory:

| Bucket | Files | Disposition |
|---|---|---|
| `packages/ai/src/providers/**` | 7 | **Read in full** (the live provider client cluster) |
| `packages/ai/src/utils/**` | 3 | **Read in full** (parser + consumers) |
| `packages/ai/src/error/**` | 2 | **Read in full** (`rate-limit.ts`, and `retryable.ts` for `isTransientStatus`) |
| `packages/ai/src/{oneshot-retry,usage,judgment,auth,auth-gateway}/**` | 9 | Read the retry-relevant call sites (`oneshot-retry.ts` in full, `usage/claude-reset.ts:308`) |
| `packages/utils/src/fetch-retry.ts` | 1 | **Read in full** (539 lines — the delegated parser) |
| `packages/coding-agent/src/session/**` | 3 | Read retry-relevant regions (`turn-recovery.ts` ~740-790, 2350-2700; `session-advisors.ts:2072`) |
| `packages/*/test/**` | 40 | Read the retry-after assertions (grep, then read the asserting blocks) |
| `python/robomp/**` | 5 | Excluded — `grep -n retry.after` shows only `retry_after=exc.retry_after` passthrough in `src/proxy/server.py:81` and `src/proxy_client.py:68`, plus `src/github_client.py` (a GitHub REST client, not an LLM path). No header parsing. |
| `docs/**` (4), `scripts/release.ts`, `packages/snapcompact/research/exp12_arbitrage.py`, `packages/tui/src/tools/task.ts`, `packages/agent/src/telemetry.ts`, gallery fixtures, `packages/coding-agent/src/web/**`, `src/tools/browser/attach.ts`, `src/config/api-key-resolver.ts`, `src/modes/controllers/btw-controller.ts`, `src/predict/client.ts`, `src/sdk.ts` | ~25 | Excluded — prose/fixtures/UI copy or unrelated REST clients. Spot-verified: none contain a header parse. |

**Enumeration gap:** the repo has **no `node_modules`** (not installed). This is immaterial to axis 9 — see below.

---

## Axis 9 — Delegation chain (read first: it determines everything else)

**oh-my-pi implements 429 / Retry-After handling itself. There is no vendor LLM SDK.**

Root `package.json` `dependencies` = 3 entries; `packages/ai/package.json` depends only on
`@oh-my-pi/{omptype,pi-catalog,pi-natives,pi-utils,pi-wire}` — no `openai`, no `@anthropic-ai/sdk`,
no `@google/genai`, no `ai`. Decisive: the `openai` and `@anthropic-ai/sdk` clients were **deliberately
removed** and replaced with hand-rolled transports.

`packages/ai/src/utils/openai-http.ts:1-16` states this outright:

```
 * JSON-POST → SSE transport for OpenAI-wire streaming endpoints (chat
 * completions, responses, azure responses). Replaces the `openai` SDK client:
 *
 * - Retries: `fetchWithRetry` (Retry-After/quota-hint aware; 5xx/408/429 and
 *   transient network errors). Default 6 total attempts — parity with the
 *   SDK's former `maxRetries: 5`.
```

`anthropic-client.ts:146-147` mimics the SDK surface (`APIPromise.asResponse()`) but is hand-written:
"Shape-compatible with the SDK's `APIPromise.asResponse()` so `getAnthropicStreamResponse` treats
internal and injected clients uniformly."

**Consequence: there is no vendored layer to fetch. The effective runtime behavior IS this source.**

### But: there are THREE independent parsers, not one

This is the central finding. The repo knows many headers and implements the parse **three separate
times**, with three different sets of accepted forms and three different zero/past policies:

| # | Parser | File:line | Primitive |
|---|---|---|---|
| **A** | `extractRetryHint` | `packages/utils/src/fetch-retry.ts:71` | `Number()` then `Date.parse` |
| **B** | `getRetryAfterMsFromHeaders` | `packages/ai/src/utils/retry-after.ts:31` | `Number()` then `Date.parse` |
| **C** | `retryDelayFromHeaders` | `packages/ai/src/providers/anthropic-client.ts:112` | **`Number.parseFloat()`** |

Each is live on a different transport:

- **A** drives `fetchWithRetry` — the OpenAI-wire, Codex, Bedrock, Ollama, Gemini-CLI, mnemopi, Firecrawl paths.
- **B** drives the Anthropic *stream* retry (`anthropic.ts:3416`), `oneshot-retry.ts:207`, and error-message formatting.
- **C** drives the Anthropic *non-stream / messages HTTP transport* retry loop (`anthropic-client.ts:276, 328`).

---

## Axis 1 — Consumer or producer?

**Consumer.** It reads `Retry-After` off responses it received.

Evidence — `fetch-retry.ts:76-77`:
```ts
const headers = source instanceof Headers ? source : (source?.headers ?? undefined);
if (headers) {
    const retryAfterMs = headers.get("retry-after-ms");
```
`anthropic-client.ts:112`: `export function retryDelayFromHeaders(headers: Pick<Headers, "get"> | undefined)`,
read from `response.headers` at line 276.

**Producer check (explicit):** `git grep -rn 'set(.retry-after|setHeader.*retry|"Retry-After",'` across all
`*.ts`/`*.py` → **one** hit, and it is a *diagnostic allowlist for a browser/Cloudflare diagnostic dump*,
not a header it emits:

`packages/ai/src/providers/cowork-fetch.ts:183`:
```ts
const DIAGNOSTIC_HEADERS = ["cf-ray", "cf-mitigated", "server", "request-id", "retry-after", "x-should-retry"];
```

**No proxy/reverse-proxy in this repo sets `Retry-After`.** Nothing here is producer-only or
mirror-image.

---

## Axis 2 — Forms parsed

I extracted all three parsers verbatim into runnable probes (`/tmp/429audit/probe.mjs`,
`probe2.mjs`) and executed them. Results:

| Form | A `fetch-retry` | B `pi-ai retry-after` | C `anthropic-client` |
|---|---|---|---|
| delay-seconds `30` | yes | yes | yes |
| **RFC850** `Friday, 31-Dec-27 23:59:59 GMT` | **yes** | **yes** | **yes** |
| **asctime** `Fri Dec 31 23:59:59 2027` | **yes** | **yes** | **yes** |
| **IMF-fixdate** | yes | yes | yes |
| **RFC3339** `2027-12-31T23:59:59Z` | **yes — correct** (39,037,833s) | **yes — correct** | **BROKEN — 2027s** |
| `retry-after-ms` | yes | yes | yes |
| `x-ratelimit-reset` (epoch s) | yes | yes | no |
| `x-ratelimit-reset-ms` | yes | yes | no |
| `x-ratelimit-reset-after` | yes | no | no |
| `x-should-retry` | no | no | yes |

**All three date forms (IMF/RFC850/asctime) are accepted** — the RFC 9110 §5.6.7 obsolete forms are
not a gap here. That is done via `Date.parse`, which V8 implements for all three.

**RFC3339 is the one real divergence, and it is exactly the bug class this audit hunts.**
Parser C uses `Number.parseFloat`:

`packages/ai/src/providers/anthropic-client.ts:119-125`:
```ts
const retryAfter = headers.get("retry-after");
if (retryAfter) {
    const seconds = Number.parseFloat(retryAfter);
    if (Number.isFinite(seconds) && seconds >= 0) return seconds * 1000;
    const dateMs = Date.parse(retryAfter) - Date.now();
```

`parseFloat("2027-12-31T23:59:59Z")` = `2027` → returns **2027 seconds**. The `Date.parse` line below
is **unreachable for any string starting with a digit run**, which is every date form and every
RFC3339 stamp. Measured: server meant **39,037,833s (~452 days)**; the client sleeps **2027s (~34 min)** —
**0.00519% of the intended wait, ~19,000× too early.** Parsers A and B use `Number()`, which returns
`NaN` for the same input and correctly falls through to `Date.parse`.

### LAX delta-seconds parse — confirmed present in C

Measured with `probe2.mjs` (all against the verbatim C logic):

```
30 (RFC delay-seconds)             anthropic-client=30.0s   pi-ai=30.0s    reference=30.0s
30.7 (float, non-RFC)              anthropic-client=30.7s   pi-ai=30.7s    reference=30.7s
30; foo (suffix garbage)           anthropic-client=30.0s   pi-ai=undefined reference=undefined
30s (suffix garbage)               anthropic-client=30.0s   pi-ai=undefined reference=undefined
2027-12-31T23:59:59Z (RFC3339)     anthropic-client=2027.0s pi-ai=39037833.1s reference=39037833.1s
0x1E (hex)                         anthropic-client=0.0s    pi-ai=30.0s    reference=30.0s
```

So C accepts `30; foo` as 30 s (protocol-named bug, confirmed), accepts floats, and additionally
mis-reads hex `0x1E` as **0** (because `parseFloat` stops at `x`, and `parseFloat("0x1E")` is `0`, which
passes `>= 0` → `scheduler.wait(0)`, an immediate re-fire).

Parsers A and B are **strict** where it matters — `Number("30; foo")` is `NaN` → `undefined` — but they
are still lax in the sense of the protocol: `Number("30.7")` = 30.7 and `Number("1e3")` = 1000 are both
accepted, neither is `1*DIGIT` per RFC 9110 §7.1.3. Measured in `probe.mjs`:
`delay-seconds '30.7' (float, non-RFC) pi-ai=30.7s` and `'1e3' (exp = 1000) pi-ai=1000.0s`.
Neither is a practical hazard; only the C prefix-parse is.

---

## Axis 3 — Honored or ignored?

**Honored (not discarded) on every path — but `partial` overall, because of caps.**

A real sleep occurs in each path:

`fetch-retry.ts:381-385`:
```ts
const hint = extractRetryHint(response, retryBody);
if (hint !== undefined && hint > maxDelayMs) return response;

const delayMs = Math.min(hint ?? resolveDefaultDelay(defaultDelayMs, attempt, maxDelayMs), maxDelayMs);
await waitForRetry(delayMs, signal);
```

`anthropic-client.ts:328`:
```ts
const delayMs = retryDelayFromHeaders(responseHeaders) ?? calculateAnthropicRetryDelayMs(attempt);
```

`anthropic.ts:3422`:
```ts
const delayMs = headerDelayMs !== undefined ? Math.max(headerDelayMs, backoffDelayMs) : backoffDelayMs;
```

Note the last one uses `Math.max(hint, backoff)` — the hint can only lengthen the wait, never shorten
it. Correct.

**The caps are the "partial".** When a server asks for a long window, the transport **abandons the
retry and surfaces the error** rather than sleeping:

| Path | Cap | Where |
|---|---|---|
| `fetchWithRetry` (generic) | 60 s | `fetch-retry.ts:320` `const DEFAULT_MAX_DELAY_MS = 60_000;` |
| OpenAI wire (`postOpenAIStream`) | 60 s (inherited) | `openai-http.ts:99` passes no `maxDelayMs` |
| Codex responses | 5 min | `openai-codex-responses.ts:298` `const CODEX_RATE_LIMIT_BUDGET_MS = 5 * 60 * 1000;` |
| Gemini CLI | 5 min | `google-gemini-cli.ts:325` `const RATE_LIMIT_BUDGET_MS = 5 * 60 * 1000;` |
| Anthropic client | 60 s | `anthropic-client.ts:248` `?? 60_000` |
| Anthropic stream | 60 s | `anthropic.ts:3420` `?? 60_000` |
| oneshot | 30 s | `oneshot-retry.ts:97` `const DEFAULT_MAX_DELAY_MS = 30_000;` |
| session turn | 5 min | `session/settings.ts:703-706` `default: 5 * 60 * 1000` |

Measured (`probe.mjs`), a 2-hour `Retry-After` on a 429:

```
429 + Retry-After: 7200 (2h)       -> return   (hint 7200000 > cap 60000)
429 + Retry-After IMF 2h out       -> return
429 + retry-after-ms: 300000       -> return
```

Abandoning the retry is **defensible** — it hands the long window to the layers above rather than
parking a socket — and the layer above is genuinely RFC-aware. `turn-recovery.ts:2665-2667`:
```ts
} else if (usageLimitWaitMs === undefined && parsedRetryAfterMs && parsedRetryAfterMs > delayMs) {
    delayMs = parsedRetryAfterMs;
}
```
and `turn-recovery.ts:2473`:
```ts
const unblockAtMs = parsedRetryAfterMs === undefined ? undefined : startedAtMs + parsedRetryAfterMs;
```
So a 2-hour window does get waited out — one layer up, with abortable sleeps. Per
`session/settings.ts:712`, exceeding the cap "fails fast instead of sleeping (e.g. 3-hour Anthropic
rate-limit windows)" unless `retry.waitForUsageReset` is enabled (**default `false`**,
`settings.ts:719`). That flag is the difference between honoring and failing fast on a multi-hour
window, and it is **off by default**.

---

## Axis 4 — Past / unparseable values

**Three different policies, one of which permits a hot re-fire.**

Parser B (`pi-ai`) **rejects** non-positive — falls through to computed backoff (safe):
`retry-after.ts:92` `if (numeric <= 0) return undefined;` and `:99` `return delay > 0 ? Math.ceil(delay) : undefined;`

Parsers A and C **clamp to zero and sleep 0**. Parser A, `fetch-retry.ts:86-88`:
```ts
const seconds = Number(retryAfter);
if (Number.isFinite(seconds)) return Math.max(0, seconds * 1000);
const parsedDate = Date.parse(retryAfter);
if (!Number.isNaN(parsedDate)) return Math.max(0, parsedDate - Date.now());
```

Measured:

```
past date          pi-ai=undefined  pi-utils-hint=0    => sleep 0
zero               pi-ai=undefined  pi-utils-hint=0    => sleep 0
negative           pi-ai=undefined  pi-utils-hint=0    => sleep 0
past epoch reset   pi-ai=undefined  pi-utils-hint=undefined => sleep 500 (computed backoff)
```

This is a **deliberate** design choice in A, documented at `fetch-retry.ts:124-130`:
```ts
// A parsed-but-non-positive signal is a provider "retry now": an explicit
// `retry-after…=0` or an absolute reset that already elapsed. It must
// survive as 0 rather than collapse into "no hint found" — consumers
// substitute a heuristic wait (30-minute quota guess, default backoff)
// when the parse returns undefined, which would sleep a session the
// provider told to retry immediately.
```

The reasoning is sound for an explicit `0`. **For a *past date* or a *negative* delta it is wrong**:
`Retry-After: -5` or a stale IMF-fixdate yields `0`, so the next request fires immediately against a
window that has not cleared. Mitigating factor: every loop is **attempt-bounded**, so this is a bounded
burst, not an infinite loop — `fetchWithRetry` caps at `DEFAULT_MAX_ATTEMPTS = 5` (`fetch-retry.ts:321`)
and `anthropic-client` at `maxRetries = 5` (set per client at `anthropic.ts:3751`, overriding the
`DEFAULT_MAX_RETRIES = 2` at `anthropic-client.ts:35`).

`x-ratelimit-reset` and `x-ratelimit-reset-ms` handled correctly: an elapsed epoch is dropped
(`retry-after.ts:131` `if (targetMs <= nowMs) return undefined;`; `fetch-retry.ts:106` `if (delta > 0) return delta;`),
so they fall through to computed backoff. Verified: `past epoch reset => sleep 500`.

---

## Axis 5 — Provider reset headers

**Present, and broader than the axis list:**

| Header | Read at |
|---|---|
| `retry-after-ms` | all three parsers |
| `x-ratelimit-reset` | `retry-after.ts:37`; `fetch-retry.ts:101` |
| `x-ratelimit-reset-ms` | `retry-after.ts:36`; `fetch-retry.ts:90` |
| `x-ratelimit-reset-after` | `fetch-retry.ts:109` |
| `x-should-retry` | `anthropic-client.ts:102` |
| `anthropic-ratelimit-unified-reset` (epoch s) | `anthropic-slow-mode.ts:181` |
| `anthropic-ratelimit-unified-5h-reset` | `anthropic-slow-mode.ts:182` |
| `anthropic-ratelimit-unified-7d-reset` | `anthropic-slow-mode.ts:183` |
| `anthropic-ratelimit-unified-slow-retry-after` (seconds) | `anthropic-slow-mode.ts:177` |
| `anthropic-ratelimit-unified-slow-max-wait` | consumed via `maxWaitMs` (`anthropic-slow-mode.ts:47`) |
| `anthropic-ratelimit-unified-status` / `-overage-status` / `-overage-in-use` / `-grace-{5h,7d}-utilization` | `anthropic-slow-mode.ts:188-196` |

**NOT present:** the four classic `anthropic-ratelimit-{tokens,requests,input-tokens,output-tokens}-reset`
headers, and the Codex `X-Codex-Primary-Reset-At` / `-Reset-After-Seconds` that meept parses
(`/Users/caimlas/git/meept/internal/llm/retry_after.go` steps 4-5).

**Important correction to the task's premise:** this repo does **not** read Anthropic's
`anthropic-ratelimit-*-reset` headers. It reads a **newer, different family** —
`anthropic-ratelimit-unified-*` (the slow-lane / usage-limit header set). The `unified-reset` values are
epoch **seconds** (`anthropic-slow-mode.ts:181` → `unifiedResetAtSec`), read via `readNonNegative` →
`Number(raw)` (`anthropic-slow-mode.ts:133-138`), and they feed the **slow-mode capacity state machine**,
not the 429 sleep. Measured: `retry-after-ms='5000'` → 5 s on both A and B; these unified headers are
structurally analogous but never reach `getRetryAfterMsFromHeaders`.

So: header knowledge is genuinely high and hand-rolled, but it is a *different* header family than meept's
baseline, and the specific reset-epoch headers meept prioritizes are absent.

---

## Axis 6 — 429 detection

**Mixed — status-code-first (correct) with a large body-regex layer on top.**

Status-code paths:
- `fetch-retry.ts:498-500`: `export function isRetryableStatus(status: number): boolean { return status >= 500 || status === 408 || status === 429; }`
- `error/retryable.ts:20-22`: `return status !== undefined && (status === 408 || status === 429 || status >= 500);`
- `anthropic-client.ts:108`: `return AIError.isTransientStatus(status) || status === 409;`

Structural status extraction, `fetch-retry.ts:457`:
```ts
const rawStatus = info.status ?? info.statusCode ?? info.response?.status;
```
with a `cause`-chain walk (`fetch-retry.ts:472`, depth ≤ 2).

The **string-regex-on-message** fallback exists at `fetch-retry.ts:476-482`:
```ts
const STATUS_MESSAGE_PATTERNS = [
	/\berror\s*[:=]\s*(\d{3})\b/i,
	/error\s*\((\d{3})\)/i,
	/status\s*[:=]?\s*(\d{3})/i,
	/\bhttp\s*(\d{3})\b/i,
	/\b(\d{3})\s*(?:status|error)\b/i,
] as const;
```
This is only reached **after** the structural status check fails (`:466` `if (status !== undefined && status >= 100 && status <= 599) return status;`), so it is a genuine fallback, not the primary path.

Separately, a large **body-text classifier** drives quota-vs-throttle routing —
`error/rate-limit.ts:207` `export function parseRateLimitReason(errorMessage: string)` with ~20 regexes,
and `:349` `return status === 429 || status === 402;`. This is used to decide *credential rotation*, not
to detect the 429 itself. A proxy that committed to HTTP 200 then sent a `data: 429 Too Many Requests`
SSE frame is handled (`openai-http.ts:153` `const inBand = AIError.createInBandProviderErrorFromText(frame);`).

**Verdict: `mixed`, but the status code is the primary key everywhere it is available.**

---

## Axis 7 — Default attempt count

Defaults, not capabilities:

| Layer | Default | File:line |
|---|---|---|
| `fetchWithRetry` (generic transport) | **5** | `fetch-retry.ts:321` `const DEFAULT_MAX_ATTEMPTS = 5;` |
| OpenAI wire (`postOpenAIStream`) | **6** | `openai-http.ts:33` `const DEFAULT_MAX_ATTEMPTS = 6;` (explicitly `maxRetries: 5` parity) |
| Codex responses | **6** | `openai-codex-responses.ts:231` `const CODEX_MAX_RETRIES = 5;` → `:235` `if (value === undefined) return CODEX_MAX_RETRIES + 1;` |
| Gemini CLI | **4** (or 1) | `google-gemini-cli.ts:321` `const MAX_RETRIES = 3;`, `:939` `maxAttempts: isLastEndpoint ? MAX_RETRIES + 1 : 1` |
| Anthropic HTTP client | **5** | class default `anthropic-client.ts:35` `= 2`, overridden per client at `anthropic.ts:3751` `maxRetries: 5` |
| Anthropic stream | **10** | `anthropic.ts:1474` `const PROVIDER_MAX_RETRIES = 10;` |
| oneshot completions | **3** | `oneshot-retry.ts:95` `const DEFAULT_MAX_ATTEMPTS = 3;`, used `:162` `Math.max(1, options?.maxAttempts ?? DEFAULT_MAX_ATTEMPTS)` |
| session turn recovery | **10** retries | `session/settings.ts:682-685` `id: "retry.maxRetries", type: "number", default: 10` |

Every loop defaults to > 1. **No default is 1.** (Gemini's `maxAttempts: 1` is conditional on
`isLastEndpoint` — the terminal endpoint declines in-transport retry so session recovery can take over.)

---

## Axis 8 — Short-retry burn

**Yes, one path can re-fire immediately — bounded, not unbounded.**

1. **`Retry-After: 0`, a negative delta, or a past date** on `fetchWithRetry` (parser A) yields hint `0`
   → `scheduler.wait(0)` → immediate re-request against the same window. Verified in `probe.mjs`:
   `429 + Retry-After: 0 -> sleep delay=0`. Bounded to 5 attempts.
2. **Same on the Anthropic client** (parser C): `parseFloat("0")` = 0, `>= 0` passes, `scheduler.wait(0)`.
3. **`Retry-After: 30; foo`** on parser C → 30 s sleep. Not a burn, but a garbage-tail acceptance.

Mitigations that materially reduce the burn risk:

- **Concurrency-admission 429s explicitly bypass transport retry** — `openai-http.ts:51`:
  `const CONCURRENCY_ADMISSION_LIMITER = "max_parallel_requests";` and `:109-111` `shouldRetryResponse`
  returning false for them, because retrying "duplicates — worse, at 60s per sleep instead of 5s — the
  concurrency backoff and model fallback that `TurnRecovery` already owns".
- **Credential rotation carries the window forward instead of re-firing.** `auth/rotation.ts:408-412`:
  ```ts
  // Thread the provider-specified reset window (e.g. Devin "Your limit
  // will reset in 13 minutes") into the block duration so the credential
  // is not reselected and hammered while the cap remains active.
  const retryAfterMs = extractProviderRetryHint(provider, message);
  ```
  and `turn-recovery.ts:2531-2536` keeps `priorBlockedUntilMs` authoritative over a heuristic.
- **Rate limits are distinguished from quota**, so a throttle waits while an account cap rotates —
  `error/rate-limit.ts:18-19`:
  ```ts
  const RATE_LIMIT_EXCEEDED_BACKOFF_MS = 30 * 1000; // 30s
  const CONCURRENT_LIMIT_BACKOFF_MS = 5 * 1000; // 5s
  ```
  vs `:17` `const QUOTA_EXHAUSTED_BACKOFF_MS = 30 * 60 * 1000; // 30 min`.

Note the one path where a 429 **rotates instead of waiting**, which the protocol flags as a risk.
`error/rate-limit.ts:394-397`:
```ts
if (!isUsageLimitStatus(status)) return false;
if (!message || isOpaqueStatusBody(message)) return true;
```
An **opaque 429** (empty body) rotates to a sibling credential. Defensible — there is no signal to wait
on — but it does mean a hintless 429 on a single-credential setup has nothing to wait for, and the
oneshot's 30 s cap (`:97`) is then the only wait.

---

## Test coverage

Read the retry-after assertions in `packages/utils/test/fetch-retry.test.ts`,
`packages/ai/test/anthropic-client.test.ts`, `packages/ai/test/anthropic-retry.test.ts`,
`packages/ai/test/google-gemini-cli-429.test.ts`, `packages/ai/test/oneshot-retry.test.ts`.

Covered: header seconds, `retry-after-ms`, cap-declines-retry, epoch-reset heuristics, explicit-zero
retry-now (`:271-287`), body-form longest-wins, naive-timezone fallback, Chinese reset phrasing.

**Not covered anywhere:** RFC3339 (or any date form) inside the `Retry-After` **header** on the
`anthropic-client` path. `git grep -n '2027-12-31|RFC3339|rfc3339'` over
`fetch-retry.test.ts`, `anthropic-retry.test.ts`, `anthropic-client.test.ts` → **zero hits**. Every
`anthropic-client` retry-after test uses a bare number or `retry-after-ms`
(`anthropic-client.test.ts:131, 160, 175, 267, 282, 322, 365`). That is why parser C's `parseFloat`
survives: its test inputs never contain a date.

---

## Findings summary

| # | Severity | Finding | Location |
|---|---|---|---|
| **F1** | **High** | `Retry-After` RFC3339 prefix-parsed as delta-seconds: `2027-12-31T23:59:59Z` → 2027 s instead of ~452 days (~19,000× too early). The `Date.parse` fallback below it is unreachable for any digit-leading value, i.e. all three HTTP-date forms too. Senders do put RFC3339 in `Retry-After`. | `anthropic-client.ts:121-124` |
| **F2** | Medium | LAX delta parse: `parseFloat` accepts `30; foo` and `30s` as 30; `parseFloat("0x1E")` = 0 → `scheduler.wait(0)` immediate re-fire. | `anthropic-client.ts:116, 121` |
| **F3** | Medium | Past date / negative delta clamp to `0` and sleep nothing, re-firing against an uncleared window. Bounded by attempt count. Rationale documented for explicit `0`, not for elapsed/negative. | `fetch-retry.ts:86-88`; `anthropic-client.ts:122, 124` |
| **F4** | Medium | Three divergent parsers for the same header with different form sets and different zero/past policies. The laxest one is on the live Anthropic transport. Correctness is not uniform across providers. | `fetch-retry.ts:71`, `retry-after.ts:31`, `anthropic-client.ts:112` |
| **F5** | Low | Transport caps (30 s–5 min) mean a long window is *not* slept at the transport; it is handed upward. Whether it is honored then depends on `retry.waitForUsageReset`, **default `false`** — so a 3-hour window fails fast by default. | `fetch-retry.ts:320`, `settings.ts:719` |
| **F6** | Low | Does not read `anthropic-ratelimit-{tokens,requests,input-tokens,output-tokens}-reset` (reads the newer `unified-*` family instead), nor Codex `X-Codex-Primary-Reset-At`. | absence; `anthropic-slow-mode.ts:181-183` |

**What is genuinely right:** all three HTTP-date forms accepted; `Number()` (not `parseFloat`) in the
two main parsers, so the RFC3339 misread is confined to one file; the hint always reaches a real
`sleep`, never overwritten by computed backoff; `Math.max(hint, backoff)` on the Anthropic stream path
so a hint can only lengthen a wait; elapsed `x-ratelimit-reset` epochs correctly dropped to backoff;
every default attempt count > 1; status-code-first detection; quota-vs-throttle separation with the
provider's window threaded into credential-block duration.

**Relative to the meept baseline:** meept's single strict `strconv.Atoi`-over-the-whole-value parse is
strictly tighter than all three oh-my-pi parsers on delta-seconds, and meept alone handles the four
classic `anthropic-ratelimit-*-reset` headers plus the Codex reset headers. oh-my-pi compensates with
much broader coverage elsewhere (`x-ratelimit-reset-after`, `x-should-retry`, the slow-lane family, a
large quota-phrase corpus, provider timezone handling) and with real 429 window propagation into
credential block durations.