# 429 / Retry-After RFC Audit — duckagent

- **Repo**: `/var/folders/mf/1mvt9vbx7q3f9ynln1p79cfr0000gn/T/meept-compare/duckagent`
- **Ref**: `06550f42d39675dc9a08837f56d695d4f0a932eb` (`git rev-parse HEAD`)
- **Tree**: Rust, `edition = "2024"`, package `duckagent` v0.1.3, binary `duck` (`src/main.rs`)
- **Tracked files**: 339 (`git ls-files | wc -l`)
- **Read-only audit**: `git status --porcelain` clean before and after; no repo file modified.

## Headline

duckagent **never reads the `Retry-After` response header anywhere in its model-request
path**. There is no parser to audit: the LLM call sites discard the `Response` headers and
flatten every non-2xx into a single `anyhow` string. 429 is then classified by *substring
matching on that flattened string*, and the retry sleep is a **hardcoded constant
1 second**. This is worse than a buggy parser — there is no parser.

---

## Axis 1 — Consumer or producer?

**Consumer-side intent, but implemented as neither.** Explicit determination, as requested:

- **Not a producer.** No code in this repo emits `Retry-After` or a 429 response. The
  matrix's "placeholder proxy for secrets" is `src/sandbox/network_proxy.rs`, and it is a
  **transparent byte-forwarding reverse proxy**, not a rate limiter. It clones the upstream
  `HeaderMap` and writes every header through except three hop-by-hop names:
  `src/sandbox/network_proxy.rs:417-431` —
  ```rust
  let headers = response.headers().clone();
  ...
  if matches!(name_lower.as_str(), "content-length" | "transfer-encoding" | "connection") {
      continue;
  }
  ```
  So a `Retry-After` sent by an upstream provider *would* transit the proxy, but the proxy
  neither generates nor interprets it. `StatusCode::TOO_MANY_REQUESTS` appears exactly once
  in the whole tree, and it is a *reason-phrase table entry*, not a status being produced:
  `src/client/sse.rs:63` — `StatusCode::TOO_MANY_REQUESTS => "Too Many Requests",`
  (grep `TOO_MANY_REQUESTS` across `src/` returns only this line.)
  `src/gateway/channels/api_server.rs` (51 lines, read in full) emits **no HTTP status
  codes and no headers at all** — it is a trait impl that just pushes onto the outbox.
- **Not a real consumer either.** The LLM transports receive `reqwest::blocking::Response`
  and pass it to `ensure_success_response`, which reads only `.status()` and `.text()`.

So: a *consumer* of LLM-provider 429s by intent, with **zero header parsing**. It reads
429s out of an error *message*.

## Axis 2 — Forms parsed

| Form | Parsed? |
|---|---|
| delay-seconds | **no** |
| IMF-fixdate | **no** |
| RFC850 | **no** |
| asctime | **no** |
| RFC3339 | **no** |
| `retry-after-ms` | **no** |

Exhaustive method (this repo is small, so I enumerated rather than sampled):

1. `grep -rniE "retry[-_]?after"` over `*.rs`, `*.toml`, `*.md`, `*.py` (excluding
   `Cargo.lock`) returns exactly **4 hits, all in 2 unrelated contexts** — a local Rust
   variable name in `src/sandbox/backends/linux_proxy_routing.rs:546,550`
   (`should_retry_after_lo_up`), and Discord's **JSON body** field in
   `src/gateway/channels/discord.rs:611`. Zero of them read an HTTP header.
2. `grep -rn "\.headers()|HeaderMap|header("` over `src/` — every hit is either a
   request-side `.header(...)` *setter*, or a response `.headers()` clone for an unrelated
   purpose (`src/web/mod.rs:447` reads `CONTENT_TYPE`; `src/mcp/transport_http.rs:145` and
   `src/sandbox/network_proxy.rs:417` clone for forwarding). **No site reads
   `RETRY_AFTER` or any `*ratelimit*` name.**
3. `grep -rn "retry-after-ms|retry_after_ms|anthropic-ratelimit|x-ratelimit-reset|
   x-should-retry"` across the entire tree: **0 matches** (Cargo.lock included).
4. Read in full: `src/client.rs` (581 lines), `src/client/sse.rs` (70),
   `src/client/anthropic.rs`, `openai_compat.rs`, `codex.rs`, `gemini.rs`,
   `gemini_cloudcode.rs`, `copilot_acp.rs` (198), `bedrock.rs` (250) — via the
   `ensure_success_response` / `status` / `headers` / `retry` / `429` greps quoted above,
   plus `copilot_acp.rs` and `bedrock.rs` read end-to-end.

**Enumeration gap (stated):** `src/sandbox/vendor/bubblewrap/**` (vendored C) — I grepped
it for `retry-after|429` (0 matches) and did not read it line-by-line; it is a Linux
namespace sandbox with no HTTP client, so it cannot affect the 429 path. `benchmark/*.py`
(6 files) — grepped for `retry|429|backoff|rate.?limit`: 0 matches.

## Axis 3 — Honored or ignored?

**Ignored — there is nothing to honor.** The retry sleep is a compile-time constant:

`src/client.rs:29-30`
```rust
const MODEL_RETRY_COUNT: usize = 5;
const MODEL_RETRY_DELAY: Duration = Duration::from_secs(1);
```

and the sole sleep in the retry loop is that constant, with no other input:

`src/client.rs:328-336`
```rust
if attempt < MODEL_RETRY_COUNT {
    emit_status(on_update, format!("Retrying model {label} ({}/{MODEL_RETRY_COUNT})...", attempt + 1));
    thread::sleep(MODEL_RETRY_DELAY);
```

`MODEL_RETRY_DELAY` is a non-`mut` `const`, so no server-supplied value could reach it even
in principle. There is no clamp, no cap, no `min()`, no composition. This is the
"computed backoff overwrites the parse" bug in its degenerate form: the parse step does not
exist.

For completeness, the one *other* backoff ladder in the model path
(`src/provider/mod.rs:28-35`, `MODEL_REFRESH_BACKOFFS`) is also a fixed
`[1s, 2s, 5s, 10s, 30s, 60s]` array indexing on attempt number
(`src/provider/mod.rs:587-590`), unrelated to HTTP status.

## Axis 4 — Past / unparseable values

**n/a — never parsed.** No date can be parsed, so no past-date clamp exists. Note the
related positive: there is no negative-sleep or zero-sleep hot-loop bug, because the sleep
is the constant 1s. The failure is over-retrying (Axis 8), not under-sleeping.

## Axis 5 — Provider reset headers

**None.** Zero matches for `anthropic-ratelimit-tokens-reset`,
`anthropic-ratelimit-requests-reset`, `anthropic-ratelimit-input-tokens-reset`,
`anthropic-ratelimit-output-tokens-reset`, `x-ratelimit-reset-requests`,
`x-ratelimit-reset-tokens`, `x-ratelimit-reset`, `x-should-retry`, `retry-after-ms`
(tree-wide grep). Notably `anthropic-ratelimit-*reset` is the single highest-value header
for Anthropic's own 429s and duckagent ships a dedicated Anthropic transport
(`src/client/anthropic.rs`) that ignores it.

## Axis 6 — 429 detection

**string-regex (substring) on a flattened error string, plus one genuine status-code site.**

The shared error path formats status + reason + body into one string and discards the
headers:

`src/client/sse.rs:6-23`
```rust
pub(crate) fn ensure_success_response(url: &str, response: Response) -> Result<Response> {
    let status = response.status();
    if status.is_success() { return Ok(response); }
    let body = response.text()...;
    bail!(
        "provider returned error for {url}: HTTP status {} ({})\nbody: {}",
        status.as_u16(), status.canonical_reason()... , body
    );
}
```

Classification then substring-matches that string:

`src/client.rs:478-484`
```rust
text.contains("http status 404")
    || (text.contains("http status 429")
        && (text.contains("quota") || text.contains("billing")
            || text.contains("balance") || text.contains("credit")))
```

So a rate-limit 429 **is** retried (it does not match the non-retryable set), but the
decision to retry is inferred from English words in a provider's error prose, not from the
status class. A 429 body that happens to contain the word "credit" is treated as a billing
failure and **never retried**; a 429 whose body says nothing recognizable gets the full
6-attempt treatment. This is the "string matching on the error body" antipattern.

**The one status-code-correct site is a different component**: the Discord gateway channel
adapter checks the real status — `src/gateway/channels/discord.rs:606-614`:
```rust
let status = response.status();
let value = response.json::<Value>()...;
if status.as_u16() == 429 {
    let retry_after = value["retry_after"].as_f64().unwrap_or(1.0);
    thread::sleep(Duration::from_millis((retry_after * 1000.0) as u64));
    bail!("discord POST {path} rate limited: {value}");
}
```
This is Discord's **JSON body** field (`retry_after`, float *seconds*), not the HTTP
`Retry-After` header — so it is not RFC 9110 handling either. It is also **sleep-then-fail**:
it burns the wait and then `bail!`s, so the message is still lost (the caller does not
re-dispatch). It is a gateway egress adapter, not the LLM path.

Separately, `src/web/mod.rs:524-529` treats 429 as a signal to escalate to a headless
browser rather than to wait:
```rust
if let Some(status) = candidate.status && matches!(status, 403 | 429 | 503) {
    return Some(format!("http_status_{status}"));
}
```
— i.e. treat 429 as a block, not a wait.

## Axis 7 — Default attempt count

**6 total attempts per model (5 retries), set as a constant, and it applies to 429s.**

`src/client.rs:292` — `for attempt in 0..=MODEL_RETRY_COUNT {` with
`MODEL_RETRY_COUNT = 5` (`src/client.rs:29`) ⇒ attempts 0..5 = **6 requests**.

Multiplied across candidates: `src/client.rs:281-285`
```rust
let candidates = crate::model_config::request_candidate_runtimes(&self.runtime)
    .unwrap_or_else(|_| vec![self.runtime.clone()]);
...
for (model_index, candidate) in candidates.iter().enumerate() {
```
and `request_candidate_runtimes` (`src/model_config.rs:249-294`) returns the active model
plus **every** other saved model as fallback candidates. So the worst case is
`6 × N_saved_models` requests at 1 second apart.

The capability exists and the default is >1, so this axis passes — the defect is the
constant delay, not the count.

## Axis 8 — Short-retry burn

**Yes — this is the core defect and the worst-consequence axis.**

A server that answers 429 with `Retry-After: 3600` gets re-hit **6 times at 1-second
intervals** (`src/client.rs:336`, `thread::sleep(MODEL_RETRY_DELAY)` = 1s), then the loop
rotates to the next fallback model (`src/client.rs:286-291`, "Trying fallback model ...")
which is likely **the same provider on a different model**, re-burning the same
per-token-rate-limit window from a second model slot. The server's instruction to stop
for an hour is met with ~6 requests/second-ish bursts per model per turn. Any client-side
rate-limit *reset* logic at the provider counts these as violations and lengthens the
penalty.

There is no short-retry escape hatch and no classification of throttle-vs-quota beyond the
substring list in Axis 6.

## Axis 9 — Delegation chain

**No LLM SDK. No delegation for the 429 question — duckagent implements the HTTP path
directly with `reqwest`, so its own (absent) behavior IS the effective runtime behavior.**

Evidence:

- `Cargo.toml` `[dependencies]` (read in full) lists `reqwest = { version = "0.12", ... }`
  as the only HTTP client, plus `chrono 0.4`, `regex 1.12`, `tokio 1.0`. **No**
  `async-openai`, `openai`, `anthropic`, `google-generativeai`, `langchain`, `genai`, or
  any vendor SDK.
- `grep -niE '^name = "(openai|anthropic|tokio-openai|async-openai|langchain|ollama|
  google-generativeai|genai)"' Cargo.lock` → **0 matches**, confirming nothing is even
  transitively present.
- `reqwest 0.12` is a transport only; it performs **no** application-level 429/backoff
  handling (no such feature is enabled — `default-features = false`, features
  `blocking, json, multipart, rustls-tls`).

**Subprocess delegates exist but are not the 429 path, and they hide errors:**

- `src/client/bedrock.rs:17-25` shells out to the AWS CLI:
  ```rust
  let output = Command::new("aws").args(["bedrock-runtime", "converse", "--cli-input-json", &path_arg]).output()...
  if !output.status.success() {
      let stderr = String::from_utf8_lossy(&output.stderr);
      bail!("Bedrock Converse failed: {}", stderr.trim());
  }
  ```
  Bedrock throttling (`ThrottlingException`, HTTP 429) arrives as **stderr text** with the
  status flattened into an exit code — so the only throttling signal duckagent ever sees is
  a prose string, which `is_non_retryable_model_error` will most likely not classify, and
  which carries no delay. AWS CLI has its own internal retry, but duckagent neither
  configures nor observes it: `grep` for `AWS_MAX_ATTEMPTS|max_attempts|max-attempts`
  across `src/` → **0 matches**. So the delegated layer's default (CLI-internal retries)
  is unobservable to the retry loop.
- `src/client/copilot_acp.rs:9-16` spawns `copilot --acp --stdio` (overridable via
  `COPILOT_ACP_COMMAND`) and speaks JSON-RPC over stdio — no HTTP status exists in this
  protocol at all, so a 429 inside the ACP agent is invisible.
- `src/setup.rs:616` mentions `qwen auth`, and `src/gateway/channels/whatsapp.rs:283`
  spawns `node` — neither is an LLM inference transport for the 429 path.

So for the audit's headline question there is **no delegated layer to exonerate or
condemn**: the answer is the code above.

---

## Comparison to the baseline (Meept `internal/llm/retry_after.go`)

| Capability | Meept | duckagent |
|---|---|---|
| Parse `Retry-After` | `ParseRetryAfter(http.Header)`, strict `strconv.Atoi` over whole value | **absent** |
| IMF-fixdate / RFC850 / asctime | all three | none |
| RFC3339 compat | yes | none |
| `retry-after-ms` | — | none |
| Provider reset headers | `anthropic-ratelimit-*`, `X-Codex-Primary-Reset-*` | none |
| Throttle vs quota classification | typed `QuotaResetError`, never short-retried | substring on prose |
| Sleep source | `retryAt` / `now+delta`, clamped | `const Duration::from_secs(1)` |

duckagent is not a partial implementation of the baseline; it is the absence of the
feature on the LLM path.

## Findings summary

| # | Finding | Location | Severity |
|---|---|---|---|
| F1 | `Retry-After` never read; headers discarded at the single shared error funnel | `src/client/sse.rs:6-23` | Critical |
| F2 | 429 re-fired 6×/model at a fixed 1s interval, ignoring the server's window | `src/client.rs:29-30, 292, 336` | Critical |
| F3 | 429 retry decision made by substring-matching English words in provider prose | `src/client.rs:478-484` | High |
| F4 | On 429+quota wording, no retry at all — rotates straight to fallback model | `src/client.rs:466, 480-483, 324-327` | High |
| F5 | Worst case `6 × N_saved_models` requests; fallback rotation usually hits the same provider's rate-limit window | `src/client.rs:281-285`, `src/model_config.rs:249-294` | High |
| F6 | Discord adapter sleeps on a JSON body field then still `bail!`s (message lost) | `src/gateway/channels/discord.rs:610-614` | Medium |
| F7 | Bedrock throttling arrives as stderr prose; AWS CLI retry unobservable | `src/client/bedrock.rs:22-25` | Medium |
| F8 | No test covers `Retry-After`/429 anywhere in the tree | tree-wide grep | Low |

**Coverage note:** `is_non_retryable_model_error` is tested, but only for the string it
matches on (`src/client.rs:570-579`) — the test asserts `"HTTP status 429 body:
insufficient_quota"` is non-retryable, i.e. it **pins the F3/F4 substring behavior as
intended**, and there is no test asserting any header is read.