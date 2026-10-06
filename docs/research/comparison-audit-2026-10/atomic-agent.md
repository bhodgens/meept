# 429 / Retry-After RFC Audit — `atomic-agent`

**Ref:** `58075e4afe16ed0c982284318bfc22a524f7d5a5` ("Bring the agent-core changes of
desktop 0.0.1 to main (#608)"), 2026-10-05, shallow clone, 2773 tracked files.
**Version:** `package.json:3` — `"version": "0.6.6"`.
**Audit date:** 2026-10-05. Node v22.22.0 used for all behavioural probes
(the repo declares `engines.node >=25.7.0`, `package.json:18-20`; V8 date
parsing semantics tested here are unchanged between the two majors for every
form exercised).

---

## 0. Delegation chain (axis 9) — resolves first, because it decides the whole audit

**This repo implements 429 handling itself. There is no delegated LLM SDK.**

Evidence — `package.json:82-113` (`dependencies`) and `package.json:115-131`
(`devDependencies`) contain no `openai`, `anthropic`, `@anthropic-ai/sdk`,
`ai` (Vercel AI SDK), `@google/generative-ai`, `google-genai`, or any LangChain
package. Verified against the lockfile too:

```
$ git grep -c '"openai"' -- package-lock.json ; … '"anthropic"' ; '"@anthropic-ai/sdk"' ; '"ai":' ; 'google-genai' ; 'langchain'
(no matches on any)
$ ls node_modules vendor
ls: cannot access 'node_modules': No such file or directory
ls: cannot access 'vendor': No such file or directory
```

There is therefore no `node_modules` tree to read and no upstream retry source
to delegate to — every retry path below is first-party TypeScript. Two
*spawned* externals do exist and are audited in §4.3: `claude` and `codex`
(`src/llm/provider/subscription-cli/`), which are CLI processes rather than
SDKs and are driven by an NDJSON/JSON contract rather than HTTP headers.

Two hand-rolled HTTP clients coexist, and **the live path for provider 429s is
the OpenAI-compatible one** (`src/llm/provider/openai/openai-http.ts`). The
local llama.cpp path (`src/llm/llama-server-client.ts`) reads **no response
headers at all** — see §4.1.

---

## 1. Consumer or producer? (axis 1)

**Consumer** — it reads `Retry-After` from responses it received.

Every non-test hit under `src/` for the literal `"retry-after"` is a read of an
inbound header:

| Location | Direction |
|---|---|
| `src/llm/provider/openai/openai-http.ts:789` — `parseRetryAfterMs(res.headers.get("retry-after"))` | read (inbound) |
| `src/tools/os/web-fetch.ts:542` — `([key]) => key.toLowerCase() === "retry-after"` over curl's `%{header_json}` | read (inbound) |
| `src/tools/os/http-request-fetch.ts:489` — `%header{retry-after}` in curl's `-w` meta | read (inbound) |
| `src/tools/os/web-search/transport/search-http.ts:267` — same curl meta, search transport | read (inbound) |
| `src/channels/discord/discord-api.ts:243` — `Number(res.headers.get("retry-after"))` | read (inbound) |
| `src/github/github-api.ts:236` — `res.headers.has("retry-after")` | read (presence only, no value) |

The one **producer** hit is unrelated to a provider 429: `src/http/route-sessions.ts:141`
sends its own `429` for a full steering inbox (`"steering inbox for session ${id} is
full"`). It sets no `Retry-After` and is not an LLM path.

Full enumeration method for the absence/scope claims: `git grep -n '"retry-after"\|Retry-After\|retryAfter' -- src`
(non-test) plus `git grep -rn -i 'anthropic-ratelimit\|x-ratelimit\|x-should-retry\|retry-after-ms\|x-codex' -- src scripts eval eval-agents`,
and a complete read of every module that parses or consumes a cooldown value
(the eight modules in §2). Enumeration gap: the `AGENTS.md` / `README.md` prose
mentions were read for claims, not treated as behaviour.

---

## 2. The four parsers (axis 2)

There is no single parser. Three independent implementations exist, which is
itself a finding — the codebase documents this for the tools layer
(`src/tools/os/retry-after-header.ts:1-8`, "defined once here rather than
drifting between two copies") but the LLM path was never folded into it.

### 2.1 LLM path — `openai-http.ts:973-984` (live provider path)

```ts
function parseRetryAfterMs(header: string | null): number | null {
  if (!header) return null;
  const seconds = Number(header);
  if (Number.isFinite(seconds) && seconds >= 0) {
    return Math.round(seconds * 1000);
  }
  const date = Date.parse(header);
  if (!Number.isNaN(date)) {
    return Math.max(0, date - Date.now());
  }
  return null;
}
```

`Number(header)` is a **lax numeric coercion over the whole trimmed string**, not
`strconv.Atoi`-equivalent strict digit parsing. Probe results (verbatim,
executed against a copy of this function):

```
value                             | openai Number()    | tools/web-fetch | web-search
"120"                             | 120000ms           | 120000ms        | 10000ms
"30; foo"                         | null               | null            | null
"10abc"                           | null               | null            | null
"0x1F"                            | 31000ms            | null            | null
"1e3"                             | 1000000ms          | null            | null
"30.5"                            | 30500ms            | null            | null
"+30"                             | 30000ms            | null            | null
" 30 "                            | 30000ms            | 30000ms         | 10000ms
"-5"                              | 0ms                | 0ms             | 0ms
""                                | null               | null            | null
"soon"                            | null               | null            | null
"Fri, 31 Dec 2027 23:59:59 GMT"   | 39037748775ms      | 43070399000ms   | 10000ms
"Friday, 31-Dec-27 23:59:59 GMT"  | 39037748775ms      | 43070399000ms   | 10000ms
"Fri Dec 31 23:59:59 2027"        | 39062948775ms      | 43095599000ms   | 10000ms
"2027-12-31T23:59:59Z"            | 39037748775ms      | 43070399000ms   | 10000ms
"Thu, 20 Aug 2026 12:00:30 GMT"   | 0ms                | 30000ms         | 10000ms
"Thu, 20 Aug 2026 11:00:00 GMT"   | 0ms                | 0ms             | 0ms
```

### 2.2 Tools layer (shared) — `src/tools/os/retry-after-header.ts:18-36`

```ts
  if (/^\d+$/.test(text)) {
    const seconds = Number.parseInt(text, 10);
    return Number.isFinite(seconds) ? seconds * 1000 : null;
  }
  const dateMs = Date.parse(text);
```

Anchored regex `^\d+$` — the file's own comment says why: *"Anchored so a
partially-numeric value like `10abc` is rejected rather than silently read as
10 seconds."* Rejects hex/exponent/plus-sign forms the LLM parser accepts. Used
by `web-fetch.ts:519` and `http-request-fetch.ts:364`.

### 2.3 Search transport — `src/tools/os/web-search/transport/retry-after.ts:34-52`

Same anchored `^\d+$`, plus a hard `[0, 10_000]` clamp (`MAX_RETRY_AFTER_MS`,
line 14, documented at line 13 as *"Ceiling on a server-advertised `Retry-After`,
so one hostile header cannot stall a turn"*).

### 2.4 Discord (out of scope for LLM, audited for completeness) — `discord-api.ts:236-245`

```ts
  const header = Number(res.headers.get("retry-after"));
  return Number.isFinite(header) ? header : 1;
```

Also lax-coerced, and **HTTP-date `Retry-After` is not honoured at all** here:
`Number("Fri, 31 Dec 2027 23:59:59 GMT")` is `NaN`, so it silently degrades to a
hardcoded `1` second. Discord prefers its JSON body `retry_after`, so in practice
the header is the fallback and a date form costs one extra immediate retry.

### Form matrix

| Form | LLM (live) | tools (`retry-after-header`) | web-search | Discord |
|---|---|---|---|---|
| delay-seconds | yes — **lax** (`Number()`: accepts hex `0x1F`→31 s, exponent `1e3`→1000 s, `+30`, `30.5`, ` 30 `) | yes — strict `^\d+$` | yes — strict `^\d+$` | yes — lax `Number()` |
| IMF-fixdate | yes (`Date.parse`) | yes | yes | **no** (→ `1` s) |
| RFC850 | yes | yes | yes | no |
| asctime | yes, **but timezone-wrong** (see below) | yes, same bug | same | no |
| RFC3339 | yes (compat, undocumented) | yes | yes | no |
| `retry-after-ms` | **no** | **no** | **no** | no |

**asctime is not GMT.** RFC 9110 §5.6.7 defines asctime as GMT; V8 parses it as
**local time** because it carries no zone. Verified:

```
$ node -e 'console.log(new Date(Date.parse("Fri Dec 31 23:59:59 2027")).toISOString())'
TZ=UTC              -> 2027-12-31T23:59:59.000Z   (correct)
TZ=America/New_York -> 2028-01-01T04:59:59.000Z   (+5 h)
(this host: TZ=America/Denver -> 2028-01-01T06:59:59.000Z, +7 h)
```

The three caps (§3) hide the practical damage — a 5–14 h error on a value that is
then clamped to 5 s / 10 s / 180 s — but the parse is wrong, and the repo's own
`src/cli/task-command.ts:300-311` documents this exact `Date.parse` leniency as a
bug pattern it deliberately avoids elsewhere ("Anything else is rejected rather
than handed to a lenient `Date.parse`, which reads 'Oct 2' and '-5' as dates in
2001"). The 429 parsers were not held to that standard.

---

## 3. Honored or ignored? (axis 3)

**Honored on every path, but `partial` throughout — every one of them caps, and
two caps are severe.**

### 3.1 LLM path

`openai-http.ts:927-936`:
```ts
function resolveWaitMs(err: unknown, attemptNumber: number): number {
  const exp = OPENAI_BACKOFF_BASE_MS * Math.pow(2, attemptNumber - 1);
  const jitter = exp * (Math.random() * 0.4 - 0.2);
  const backoff = Math.max(0, Math.round(exp + jitter));
  const retryAfter =
    err instanceof OpenAiHttpError && err.retryAfterMs !== null
      ? Math.min(err.retryAfterMs, OPENAI_RETRY_AFTER_CAP_MS)
      : 0;
  return Math.max(backoff, retryAfter);
}
```
`OPENAI_RETRY_AFTER_CAP_MS = 5_000` (`openai-http.ts:460`), justified in the file:
*"Interactive turns cannot absorb a 'come back in 60s' wait; a provider asking for
more than this gets the capped wait and then the next attempt."* That is a
deliberate product decision, but it inverts the header: `Retry-After: 3600` is
answered with 5 s, twice, then the turn fails. A provider telling you the window
is an hour away is precisely the case the RFC exists for.

The parsed value is **not** overwritten by the exponential term — `Math.max`
takes the larger — so the common `Retry-After` (< 5 s) genuinely wins over a
150 ms base. That is the one thing this implementation gets unambiguously right.

Second input: `parseRetryInfoDelayMs` (`openai-http.ts:949-967`) reads Gemini's
`google.rpc.RetryInfo` `retryDelay` Duration string with an anchored regex
`/^(\d+(?:\.\d+)?)s$/` — a genuine bonus over the header, and it flows through the
same 5 s cap.

Third input, and the largest one: a **body-text hint**.
`parse-provider-error-body.ts:101-102` —
```ts
const RETRY_HINT =
  /\b(?:retry|try again|please wait)(?:\s+\w+){0,2}?\s+(?:in|after)\s+(\d+(?:\.\d+)?)\s*(ms|milliseconds?|s|secs?|seconds?|m|mins?|minutes?)\b/i;
```
capped at `RETRY_HINT_MAX_MS = 180_000` (`parse-provider-error-body.ts:140`),
delivered via `provider-error-verdict.ts:62` —
`delayMs: Math.min(reason.delayMs ?? RETRY_HINT_MAX_MS, RETRY_HINT_MAX_MS)` —
and slept by the agent loop at `agent-loop.ts:2583-2592`:
```ts
      const nextRetryMs = Math.min(
        retryHint !== null
          ? Math.max(1, retryHint.delayMs)
          : Math.min(
              PROVIDER_WAIT_MAX_BACKOFF_MS,
              PROVIDER_WAIT_BASE_MS * 2 ** outageAttempts,
            ),
        Math.max(1, providerWaitCfg.maxWaitMs - outageWaitedMs),
      );
```
So **the longest wait a server-sent `Retry-After` can ever buy on the LLM path is
3 minutes**, inside a 5-minute total park budget — and the HTTP client's own 5 s
cap means the header value itself is already clipped before it gets here. The
`Math.max(1, …)` is a good guard: it forbids the zero-delay hot-retry the
protocol warns about, at the cost of a 1 ms sleep for a genuinely expired window.

### 3.2 Tools layer

`web-fetch.ts:391-398` and `http-request-fetch.ts:216-224`:
```ts
  const backoff = cfg.retryBaseDelayMs * 2 ** attempt;
  const chosen = retryAfterMs !== null ? retryAfterMs : backoff;
  return Math.min(cfg.retryMaxDelayMs, Math.max(0, chosen));
```
`retryMaxDelayMs: 5_000` (`web-fetch.ts:38`, `http-request-fetch.ts:73`). Honors,
capped to 5 s.

### 3.3 Web-search

`retry-after.ts:58-66`: `if (input.retryAfterMs !== null) return clampDelay(input.retryAfterMs);` — strict
precedence over its own ladder, clamped to 10 s.

### 3.4 Web-search cooldown park — the one place a long header is taken seriously

`provider-cooldown.ts:168-172`:
```ts
      const advertised =
        retryAfterMs === null
          ? 0
          : Math.min(MAX_HONOURED_RETRY_AFTER_MS, Math.max(0, retryAfterMs));
      const parkMs = Math.max(escalated, advertised);
```
`MAX_HONOURED_RETRY_AFTER_MS = DEFAULT_MAX_MS = 15 * 60_000` (`provider-cooldown.ts:87, 95`) — a
15-minute park that **takes the max of the server's number and its own
escalation ladder**, and persists to disk across processes (`#256`). This is the
best `Retry-After` handling in the repo. It applies to web-search providers
(Exa, DuckDuckGo), not to LLM providers.

---

## 4. Live paths, one by one

### 4.1 Cloud / OpenAI-compatible providers — the live 429 path

Pipeline: `openAiFetch` → `httpErrorFromResponse` (`openai-http.ts:778-808`) reads the
header once per response:

```ts
  const retryAfterMs =
    parseRetryAfterMs(res.headers.get("retry-after")) ??
    (res.status === 429 || res.status === 503
      ? parseRetryInfoDelayMs(text)
      : null);
```

carried on the typed error (`openai-http.ts:75`, `retryAfterMs: number | null = null`), and
consumed by `runOpenAiWithRetry` (`openai-http.ts:873-919`) → `resolveWaitMs` → `sleep`.
**The parsed value reaches the sleep. Nothing overwrites it.** (axis 3 pass.)

Provider presets that use this path include `anthropic`
(`provider-presets.ts:104-117`, `baseUrl: "https://api.anthropic.com"`,
`apiKeyHeader: x-api-key`, `headers: { "anthropic-version": "2023-06-01" }`) — so
Anthropic is spoken to over this hand-rolled client and **its
`anthropic-ratelimit-*-reset` headers are never read** (see §6).

### 4.2 Local llama.cpp (TurboQuant) — a 429 path that parses nothing

**Confirmed: the local inference path does not read `Retry-After` at all.**
`src/llm/llama-server-client.ts` has zero `headers.get` calls; the only header
access in `src/llm/**` is `openai-http.ts:789` (verified by
`grep -rn 'headers\.get\|headers\[' src/llm --include=*.ts | grep -v test` → one
hit). Errors are built by `buildHttpError` (`llama-server-client.ts:385-400`),
which carries status and body text only:

```ts
  const base = `llama-server returned http ${response.status}`;
  return new LlamaServerError(
    detail ? `${base}: ${detail}` : base,
    response.status,
    url,
  );
```

So a local 429 (llama.cpp returns 429 for a busy/parallel-slot refusal) is
classified but **never waited on per the server's advice**:

- `isRetryableLlamaError` (`llama-server-client.ts:1668-1677`) returns true only for
  `status === null` or `500 ≤ status < 600`. **A 429 is not retried at the client
  layer at all** — no backoff, no header.
- `classifyFailure` (`classify-failure.ts:25-27, 60`) files 429 as `transport`
  via `LLAMA_ENDPOINT_UNAVAILABLE_STATUSES`.
- `shouldAdvance` (`should-advance.ts:87`) marks it `immediate: true` → the circuit
  breaker arms on first occurrence, and the chain advances to the next link.
- If the chain is exhausted, the agent loop's outage wait catches it
  (`agent-loop.ts:2576-2593`) at the generic `PROVIDER_WAIT_BASE_MS * 2**n`
  (2 s base, 30 s ceiling — `agent-loop.ts:617, 623`), bounded by
  `providerWaitCfg.maxWaitMs`.

Net: a local 429 rotates to a fallback provider immediately, then the parked
retry uses a computed backoff. The header, if llama.cpp ever sent one, would be
ignored. This is the matrix's "different 429 semantics" case, and the honest
answer is that **there are none** — a local 429 is treated as "this endpoint is
unavailable, go elsewhere," which is defensible for a single-user local server
with no quota to burn, but is not RFC-conformant honoring.

The GBNF grammar path (`src/llm/grammar/`) contains no 429 handling at all
(`grep -rn '429\|retry' src/llm/grammar/*.ts` excluding tests → no matches); it
operates on already-streamed text, so it is out of scope by construction.

### 4.3 Subscription CLIs (`claude`, `codex`) — a delegated layer with no 429 semantics

Registered at `register-cli-adapters.ts:12-18`, configured per-provider
(`llm.providers[].subscriptionCli.binPath`), so this is an **opt-in** path — a
provider must be configured with a CLI binary; nothing is on by default.

`claude-cli-adapter.ts:214-220` reads a **stream notice**, not an HTTP status:
```ts
  if (parsed.type === "rate_limit_event") {
    const info = parsed.rate_limit_info ?? {};
    return info.status && info.status !== "allowed"
      ? { kind: "notice", message: `claude rate limit ${info.status}${…}` }
```
`api_error_status` is only used for 401/403 (`claude-cli-adapter.ts:137-145`); a
`rate_limit_event` becomes a **log notice, never a wait**. `codex-cli-adapter.ts`
has no rate-limit branch at all — a `turn.failed` with a rate-limit message
becomes a generic `SubscriptionCliInvocationError` (`codex-cli-adapter.ts:134`),
classified `tool` (not `transport`) unless it matches the auth patterns
(`subscription-cli-errors.ts:78-84`). **Neither CLI adapter ever sleeps out a
subscription rate limit**; both fall through to the generic outage park if the
error classifies as transport. `SubscriptionCliInvocationError` is *not* in
`classifyFailure`'s transport arms (`classify-failure.ts:73-78` handles only the
`NotInstalled`/`Auth` variants), so a claude rate limit ends the turn as a `tool`
failure.

### 4.4 Other consumers (non-LLM, for completeness)

- `github-api.ts:228-241` — 403/429 detection is status + `x-ratelimit-remaining`
  + `retry-after` *presence*; the header **value is never read or slept on**.
  `github-skill-client.ts:270-278` reads only `x-ratelimit-remaining === "0"` on a
  403, and has no 429 branch at all (a GitHub 429 falls to the generic
  "unexpected" error).
- `discord-api.ts:210-213, 236-245` — honors body `retry_after` then header,
  clamped to 10 s, exactly one retry (`attempt === 0` gate).
- `telegram/outbound-sender.ts:169-184, 289-301` — reads the grammy's
  `error_code === 429` + `parameters.retry_after`, clamps to `RETRY_AFTER_MAX_SECONDS`,
  exactly one retry, then drops the chunk. Correct for the Bot API contract
  (which sends a number, never a date).
- `src/tools/os/web-search/` — the strongest path in the repo (§3.4).

---

## 5. Past / unparseable / negative values (axis 4)

All three parsers clamp a past date to **0**, never a negative sleep, and all
three return `null` for garbage so computed backoff takes over:

- `openai-http.ts:981` — `return Math.max(0, date - Date.now());`
- `retry-after-header.ts:35` — `return Math.max(0, dateMs - now);`
- `retry-after.ts:51` via `clampDelay` (line 68-71) — `if (!Number.isFinite(ms) || ms <= 0) return 0;`

Test evidence in-tree (`src/tools/os/retry-after-header.test.ts:20-24`,
`web-search/transport/retry-after.test.ts:29-33`): *"clamps an
already-elapsed date to zero rather than negative"*.

**The one hole is `Number(header)` at `openai-http.ts:975-976`.** The guard is
`seconds >= 0`, so `Retry-After: -5` → `Number("-5") = -5` → `>= 0` false → falls
through to `Date.parse("-5")`, which V8 reads as **2001-05-01** (measured) → a
date ~25 years in the past → `Math.max(0, …)` → `0`. So it lands on 0 by luck,
not by design, and only after a nonsense date parse. The same input on the
`/^\d+$/`-anchored parsers is rejected outright (`"-5"` does not match
`^\d+$`), which is the correct behaviour.

`"0"` → `0` in all three. That is a genuine zero-delay hot retry, and it is
**shipped as a test fixture**: `openai-http.test.ts:150` and `:176` both use
`errorResponse(429, …, { "retry-after": "0" })`. The loop's own
`Math.max(1, retryHint.delayMs)` (`agent-loop.ts:2585`) prevents the 1 ms sleep
case at the outer layer, but inside `runOpenAiWithRetry` a `retry-after: 0`
yields `resolveWaitMs` = `max(jittered backoff, 0)` = the backoff, so the client
still waits ≥ 150 ms. Not a burn.

**The real burn risk is the 5 s cap, not the parse.** Three 429s at
`retry-after: 3600` = three attempts 5 s apart, then a fallback advance, then a
2 s-outage park. A provider whose window is 60 minutes has been told nothing
useful and is re-fired 3–4 times inside 30 seconds.

---

## 6. Provider reset headers (axis 5)

**None read. Enumeration method:** `git grep -i 'anthropic-ratelimit\|x-ratelimit\|x-should-retry\|retry-after-ms\|x-codex' -- src scripts eval eval-agents`
→ `0 matches` for `anthropic-ratelimit`, `retry-after-ms`, `x-ratelimit-reset*`,
`x-should-retry`, `x-codex`. The only `x-ratelimit-*` hits in the repo are
`x-ratelimit-remaining`, presence/equality only (`github-api.ts:229`,
`github-skill-client.ts:272`) — a *remaining counter*, not a reset instant.

The gap matters because `anthropic` is a shipped preset (§4.1). Anthropic's
`anthropic-ratelimit-requests-reset` / `-tokens-reset` are RFC3339 instants that
name the real window; ignoring them leaves the client with only a header that is
itself capped at 5 s. Meept's baseline reads all of
`anthropic-ratelimit-{tokens,requests,concurrent}-reset` plus
`X-Codex-Primary-Reset-At`; atomic-agent reads none of them.

Not read either: `retry-after-ms` (OpenAI's ms integer). A provider sending only
`retry-after-ms: 2500` with no `retry-after` gets a pure exponential backoff.

---

## 7. 429 detection (axis 6)

**Mixed — and unusually well done.** Detection is on the typed HTTP status
everywhere in the LLM path:

- `openai-http.ts:845` — `return err.status >= 500 || err.status === 429 || err.status === 408;`
- `should-advance.ts:87` — `return status === 429 || status === 408 || status >= 500;`
- `agent-loop.ts:522` — `return err.status >= 500 || err.status === 408 || err.status === 429;`
- `link-failure-kind.ts:77-81` — `status >= 500 || status === 408 || status === 429 || readProviderErrorVerdict(err)?.kind === "retry_after"`
- `web-search/.../assert-provider-status.ts:26` — `if (response.status === 429) throw new WebSearchRateLimitedError(…)` — a **typed** 429 outcome.
- `web-fetch.ts:48` / `http-request-fetch.ts` — `RETRYABLE_STATUSES = new Set([429, 502, 503, 504])`

Body-text regex is used **only to disambiguate a 429 that is really a billing
refusal** — never to decide whether it is a 429. That is the correct
architecture and is called out in the code (`parse-provider-error-body.ts:12-16`:
"a 429 for exhausted credit was parked and retried as rate limiting (42 times per
worker, once)"). `isCreditExhausted` (`openai-http.ts:854-863`) then makes such a
429 **non-retryable** (`openai-http.ts:844`), which is the right call and is a
direct answer to the protocol's "429 rotates to a different model/provider
instead of waiting out the window" concern — it rotates only when waiting cannot
help.

Credit-vs-rate-limit disambiguation is careful: a 429 counts as billing only when
it asked for no cooldown *and* has no funds wording *and* no rate-limit wording
(`parse-provider-error-body.ts:211-217`), with `RATE_LIMIT_WORDING` explicitly
allowed to outweigh money words on a 429 (`:92-98`).

---

## 8. Default attempt counts (axis 7)

| Layer | Default | Set at |
|---|---|---|
| Cloud HTTP client, per logical completion | **3** | `openai-http.ts:426` — `export const OPENAI_MAX_ATTEMPTS = 3;` |
| Cloud stream reopen | shares the same 3 | `openai-provider.ts:387` — `const budget = createOpenAiAttemptBudget();` |
| Local llama client | **3** | `config-schema.ts:3233` — `COMPLETION_RETRIES: 3` (→ `load-config.ts:202-204`), consumed at `llama-server-client.ts:1501-1513`. But see §4.2: a 429 is not in `isRetryableLlamaError`, so the effective count for a local 429 is **1**. |
| Agent-loop outage park | **300 000 ms total** budget, 2 s → 30 s steps | `config-schema.ts:2851-2858` — `providerWait: { enabled: true, maxWaitMs: 300_000 }`, **default ON**, ~unlimited attempts within 5 minutes at up to 30 s spacing |
| `os.web.fetch` | **2** extra attempts | `web-fetch.ts:36` — `maxRetries: 2` |
| `os.http.request` | **2** extra attempts | `http-request-fetch.ts:72` — `maxRetries: 2` |
| web-search 429 ladder | **2** extra attempts | `retry-after.ts:23-26` — `DEFAULT_SEARCH_RETRY_POLICY = { maxRetries: 2, baseDelayMs: 500 }` |
| Discord | **1** retry | `discord-api.ts:210` — `if (res.status === 429 && attempt === 0)` |
| Telegram | **1** retry, then drop | `outbound-sender.ts:169-184` |

Nothing defaults to 1 on the LLM path — the loop exists and is on.

---

## 9. Short-retry burn (axis 8)

**Two mechanisms, both mitigated:**

1. **Breaker pre-arm on first 429.** `shouldAdvance` returns
   `immediate: true` for a 429 (`should-advance.ts:87`), which arms the circuit
   breaker at once rather than after `failureThreshold`
   (`should-advance.ts:14-22`). Default cooldown ladder
   `[30_000, 60_000, 300_000]` (`fallback-config.ts:23`), with
   `probeThrottleMs: 300_000`. So even though the first 5 s-capped retry fires, the
   *link* is then quarantined for ≥ 30 s and probed at most every 5 minutes.
2. **Web-search cooldown park** (§3.4) — 60 s doubling to 15 min, persisted to
   disk.

Residual burn: within one HTTP client call, three 429s can land ~5 s apart
against the same window. Within the *outer* park, `PROVIDER_WAIT_BASE_MS * 2**n`
can re-fire a step every 2–30 s for 5 minutes (≈ 10–60 attempts) against a
provider whose window may be an hour. Given `providerWait.enabled: true` by
default and `maxWaitMs: 300_000`, this is the most expensive failure mode in the
system: **bounded, but 60 re-fires is a lot of a per-minute quota.**

---

## 10. Gaps, ranked

1. **`Number()` instead of a strict integer parse on the live provider path**
   (`openai-http.ts:975`). Accepts `0x1F`→31 s, `1e3`→1000 s, `30.5`→30.5 s,
   `+30`. Rejects are the harness's most common 429 shape? No — but a hostile or
   buggy proxy can make the client sleep 5 s (capped) on nonsense, and the two
   sibling parsers in the same repo are correct, so the inconsistency is
   self-evident.
2. **The 5 s cap on `Retry-After` for cloud LLM providers** (`openai-http.ts:460`).
   A `Retry-After: 3600` is answered with 5 s, three times. The header is read,
   parsed correctly, and then deliberately discarded — the protocol's "a parser
   whose result is never slept on is a fail" applies in weakened form: it *is*
   slept on, but floored at 5 s regardless of what was asked.
3. **No `anthropic-ratelimit-*-reset` / `retry-after-ms` support at all** (§6),
   on a repo that ships an `anthropic` preset.
4. **asctime parsed as local time** (§2), a 5–14 h error.
5. **Local llama.cpp 429 reads no headers and is not retried at the client**
   (§4.2) — the 3-attempt default does not apply to it.
6. **Subscription-CLI rate limits produce no wait at all** (§4.3).
7. **Three parsers for one header**, none shared with the LLM path — the drift in
   #1 and #4 is the direct consequence.

### What is right, and worth keeping

- Anchored `^\d+$` in the two tools parsers, with in-tree tests naming the
  failure mode they prevent.
- Strict precedence: the server's number wins over computed backoff in all four
  `chosen`/`computeRetryDelayMs`/park sites.
- `Math.max(1, retryHint.delayMs)` (`agent-loop.ts:2585`) — a hard floor against
  the zero-delay hot loop.
- Every past date clamps to 0; no negative sleep is reachable on any path.
- Status-code detection everywhere; body regex only to split billing from
  throttling.
- The web-search cooldown ladder with disk persistence is a better design than
  anything on the LLM path.

---

## Appendix — commands run (all read-only)

```
git rev-parse HEAD                                  # 58075e4afe16ed0c982284318bfc22a524f7d5a5
git grep -n '"retry-after"|Retry-After|retryAfter' -- src        # consumer/producer map
git grep -rn -i 'anthropic-ratelimit|x-ratelimit|x-should-retry|retry-after-ms|x-codex' \
    -- src scripts eval eval-agents                              # provider-header sweep: 0
git grep -c '"openai"' -- package-lock.json                       # SDK sweep: no hits
grep -rn 'headers.get|headers\[' src/llm --include=*.ts | grep -v test   # 1 hit
grep -rn '429' src/llm --include=*.ts | grep -v test              # classification map
node -e '<verbatim copies of the 3 parsers>'                      # form matrix (§2)
node -e 'new Date(Date.parse("Fri Dec 31 23:59:59 2027"))'       # asctime TZ proof, 3 zones
```

No repository file was modified.